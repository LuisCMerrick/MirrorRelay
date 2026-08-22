package proxy

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/LuisCMerrick/MirrorRelay/internal/auth"
	"github.com/LuisCMerrick/MirrorRelay/internal/config"
	"github.com/LuisCMerrick/MirrorRelay/internal/model"
	"github.com/LuisCMerrick/MirrorRelay/internal/security"
)

type routeError struct {
	status int
	text   string
}

func (e *routeError) Error() string { return e.text }

func (e *Engine) routeRequest(request *http.Request) (model.Mirror, string, *url.URL, bool, *routeError) {
	if strings.HasPrefix(request.URL.Path, auxiliaryUpstreamPrefix) {
		if containsEncodedPathSeparator(request.URL.EscapedPath()) {
			return model.Mirror{}, "", nil, false, &routeError{status: http.StatusBadRequest, text: "upstream auxiliary resource path contains an encoded separator"}
		}
		route, err := parseAuxiliaryUpstreamRoute(request.URL.Path, request.URL.RawQuery)
		if err != nil {
			return model.Mirror{}, "", nil, false, &routeError{status: http.StatusBadRequest, text: err.Error()}
		}
		repository, found := e.registry.GetByID(route.repositoryID)
		if !found || !repository.Enabled || !repository.HTMLRewriteEnabled || !e.auxiliaryRouteAllowed(repository, request.Host) {
			return model.Mirror{}, "", nil, false, &routeError{status: http.StatusNotFound, text: "upstream auxiliary resource route not found"}
		}
		var selected model.Upstream
		for _, upstream := range repository.Upstreams {
			if upstream.ID == route.upstreamID && upstream.Enabled {
				selected = upstream
				break
			}
		}
		if selected.ID == 0 || !verifyAuxiliaryURLSignature(e.auxiliarySigningKey, repository, selected, route) {
			return model.Mirror{}, "", nil, false, &routeError{status: http.StatusNotFound, text: "upstream auxiliary resource route not found"}
		}
		repository.Upstreams = []model.Upstream{selected}
		return repository, route.target.Path, nil, true, nil
	}
	if repositoryID, ok := parseTokenRoute(request.URL.Path); ok {
		repository, found := e.registry.GetByID(repositoryID)
		if !found || !repository.Enabled || repository.ProxyMode != "registry" || repository.AuthMode != "full_proxy" {
			return model.Mirror{}, "", nil, false, &routeError{status: http.StatusNotFound, text: "token route not found"}
		}
		target := e.tokenTarget(repository)
		if target == nil {
			return model.Mirror{}, "", nil, false, &routeError{status: http.StatusBadGateway, text: "registry token target is not available"}
		}
		target = cloneURL(target)
		target.RawQuery = mergeRawQuery(target.RawQuery, request.URL.RawQuery)
		return repository, target.EscapedPath(), target, false, nil
	}
	repository, relative, found := e.registry.Route(request.Host, request.URL.Path)
	if !found {
		return model.Mirror{}, "", nil, false, &routeError{status: http.StatusNotFound, text: "repository not found"}
	}
	relative, err := applyStripPrefix(relative, repository.StripPrefix)
	if err != nil {
		return model.Mirror{}, "", nil, false, &routeError{status: http.StatusNotFound, text: err.Error()}
	}
	if unsafeRepositoryPath(relative) {
		return model.Mirror{}, "", nil, false, &routeError{status: http.StatusBadRequest, text: "repository path contains an unsafe segment"}
	}
	if blocked, reason := isPackageBlocked(repository, relative); blocked {
		return model.Mirror{}, "", nil, false, &routeError{status: http.StatusForbidden, text: reason}
	}
	if !strings.HasPrefix(relative, "/__fetch/") {
		if !strings.HasPrefix(relative, "/__fetch_template/") {
			return repository, relative, nil, false, nil
		}
		encodedAndPath := strings.TrimPrefix(relative, "/__fetch_template/")
		encodedOrigin, templatePath, found := strings.Cut(encodedAndPath, "/")
		if !found {
			return model.Mirror{}, "", nil, false, &routeError{status: http.StatusBadRequest, text: "invalid adapter template target"}
		}
		decodedOrigin, decodeErr := base64.RawURLEncoding.DecodeString(encodedOrigin)
		if decodeErr != nil {
			return model.Mirror{}, "", nil, false, &routeError{status: http.StatusBadRequest, text: "invalid adapter template target"}
		}
		target, parseErr := parseAbsoluteHTTPURL(string(decodedOrigin))
		if parseErr != nil || !isAllowedRewriteOrigin(repository, target) {
			return model.Mirror{}, "", nil, false, &routeError{status: http.StatusForbidden, text: "adapter target is not allowed"}
		}
		target.Path = "/" + templatePath
		target.RawPath = ""
		target.RawQuery = request.URL.RawQuery
		if target.Scheme != "https" && !(e.cfg.Security.AllowHTTPUpstream && repository.AllowHTTP && target.Scheme == "http") {
			return model.Mirror{}, "", nil, false, &routeError{status: http.StatusForbidden, text: "adapter target protocol is not allowed"}
		}
		return repository, target.EscapedPath(), target, false, nil
	}
	encoded := strings.TrimPrefix(relative, "/__fetch/")
	decoded, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return model.Mirror{}, "", nil, false, &routeError{status: http.StatusBadRequest, text: "invalid adapter target"}
	}
	target, err := parseAbsoluteHTTPURL(string(decoded))
	if err != nil || !isAllowedRewriteOrigin(repository, target) {
		return model.Mirror{}, "", nil, false, &routeError{status: http.StatusForbidden, text: "adapter target is not allowed"}
	}
	if target.Scheme != "https" && !(e.cfg.Security.AllowHTTPUpstream && repository.AllowHTTP && target.Scheme == "http") {
		return model.Mirror{}, "", nil, false, &routeError{status: http.StatusForbidden, text: "adapter target protocol is not allowed"}
	}
	target.RawQuery = mergeRawQuery(target.RawQuery, request.URL.RawQuery)
	return repository, target.EscapedPath(), target, false, nil
}

func parseTokenRoute(value string) (int64, bool) {
	trimmed := strings.TrimPrefix(value, "/_mirror_auth/")
	idStr, _, _ := strings.Cut(trimmed, "/")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}

func isTokenRoute(value string) bool {
	_, ok := parseTokenRoute(value)
	return ok
}

func requestPublicBase(cfg config.Config, repository model.Mirror, request *http.Request) (string, error) {
	scheme := strings.ToLower(strings.TrimSpace(request.Header.Get("X-Forwarded-Proto")))
	if scheme == "" {
		scheme = "https"
	}
	if scheme != "https" && scheme != "http" {
		return "", errors.New("invalid forwarded protocol")
	}
	if repository.PublicMode == "host" && repository.PublicHost != "" {
		return scheme + "://" + repository.PublicHost, nil
	}
	if cfg.HTTP.PublicBaseURL != "" {
		return strings.TrimRight(cfg.HTTP.PublicBaseURL, "/"), nil
	}
	if !validRequestAuthority(request.Host) {
		return "", errors.New("invalid request host")
	}
	return scheme + "://" + request.Host, nil
}

func validRequestAuthority(value string) bool {
	if value == "" || strings.ContainsAny(value, "\x00\r\n/\\ {};$\"") {
		return false
	}
	parsed, err := url.Parse("https://" + value)
	if err != nil || parsed.Host != value || parsed.Hostname() == "" || parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return false
	}
	if port := parsed.Port(); port != "" {
		value, err := strconv.Atoi(port)
		if err != nil || value < 1 || value > 65535 {
			return false
		}
	}
	return true
}

func trustedClientIP(request *http.Request) string {
	return security.RequestClientIP(request)
}

func stripUntrustedHeaders(header http.Header) {
	for _, hop := range []string{"Connection", "Proxy-Connection", "Keep-Alive", "Proxy-Authenticate", "Proxy-Authorization", "TE", "Trailer", "Transfer-Encoding", "Upgrade"} {
		header.Del(hop)
	}
	for name := range header {
		lower := strings.ToLower(name)
		if strings.HasPrefix(lower, "x-mirror-internal-") || lower == "forwarded" || lower == "x-forwarded-for" ||
			lower == "x-forwarded-host" || lower == "x-forwarded-proto" || lower == "x-real-ip" {
			header.Del(name)
		}
	}
	sanitizeProxyCookies(header)
}

func sanitizeProxyCookies(header http.Header) {
	cookieValues := header.Values("Cookie")
	if len(cookieValues) == 0 {
		return
	}
	header.Del("Cookie")
	var preserved []string
	for _, line := range cookieValues {
		for _, part := range strings.Split(line, ";") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			name, _, _ := strings.Cut(part, "=")
			if strings.TrimSpace(name) != auth.CookieName {
				preserved = append(preserved, part)
			}
		}
	}
	if len(preserved) > 0 {
		header.Set("Cookie", strings.Join(preserved, "; "))
	}
}

func applyStripPrefix(relative, prefix string) (string, error) {
	if prefix == "" || prefix == "/" {
		return ensureLeadingSlash(relative), nil
	}
	prefix = "/" + strings.Trim(prefix, "/")
	if relative == prefix {
		return "/", nil
	}
	if !strings.HasPrefix(relative, prefix+"/") {
		return "", errors.New("request does not match repository strip_prefix")
	}
	return strings.TrimPrefix(relative, prefix), nil
}

func unsafeRepositoryPath(value string) bool {
	if strings.ContainsAny(value, "\x00\r\n\\") {
		return true
	}
	for _, segment := range strings.Split(value, "/") {
		if segment == "." || segment == ".." {
			return true
		}
	}
	return false
}

func mergeRawQuery(first, second string) string {
	if first == "" {
		return second
	}
	if second == "" {
		return first
	}
	return first + "&" + second
}

func repositoryURL(repository model.Mirror, upstream model.Upstream, relative, rawQuery string, auxiliary bool) (*url.URL, error) {
	base, err := url.Parse(upstream.URL)
	if err != nil {
		return nil, err
	}
	if authority := repositoryLogicalAuthority(repository, upstream); authority != "" {
		base.Host = authority
	}
	if auxiliary {
		base.Path = ensureLeadingSlash(relative)
		base.RawPath = ""
		base.RawQuery = rawQuery
		return base, nil
	}
	if repository.AddPrefix != "" {
		base.Path = joinPath(base.Path, repository.AddPrefix)
	}
	base.Path = joinPath(base.Path, relative)
	base.RawPath = ""
	base.RawQuery = rawQuery
	return base, nil
}

func repositoryUpstreamIdentity(upstream model.Upstream, auxiliary bool) string {
	if !auxiliary {
		return upstream.URL
	}
	parsed, err := url.Parse(upstream.URL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return upstream.URL + "#auxiliary-root"
	}
	return parsed.Scheme + "://" + parsed.Host + "/"
}

func repositoryLogicalAuthority(repository model.Mirror, upstream model.Upstream) string {
	if repository.HostRewrite != "" {
		return repository.HostRewrite
	}
	return upstream.Host
}

func classifyObject(repository model.Mirror, rawPath string, dynamic *url.URL) string {
	lower := strings.ToLower(rawPath)
	if repository.ProxyMode == "registry" {
		if strings.Contains(lower, "/blobs/") {
			return "blob"
		}
		return "metadata"
	}
	base := strings.ToLower(path.Base(strings.TrimSuffix(lower, "/")))
	for _, suffix := range []string{".deb", ".rpm", ".ipk", ".apk", ".whl", ".jar", ".zip", ".tar", ".tar.gz", ".tar.xz", ".tar.zst", ".tgz", ".iso", ".crate", ".nupkg"} {
		if strings.HasSuffix(base, suffix) {
			return "package"
		}
	}
	if strings.Contains(lower, "/by-hash/") || strings.Contains(lower, "/blobs/sha256:") {
		return "immutable"
	}
	if repository.Type == "maven" && !strings.Contains(base, "maven-metadata") && !strings.HasSuffix(base, ".xml") {
		return "package"
	}
	if dynamic != nil && !repository.RewriteEnabled {
		return "package"
	}
	return "metadata"
}

func metadataValidatorKey(repository model.Mirror, upstreamIdentity, objectPath, rawQuery, publicBase string, auxiliary, gzip bool) string {
	value := strings.Join([]string{
		strconv.FormatInt(repository.ID, 10), upstreamIdentity, objectPath, rawQuery, publicBase,
		repository.PublicMode, repository.PublicPath, repository.StripPrefix, repository.AddPrefix, repository.HostRewrite,
		repository.ProfileVersion, repository.RewriteProfile, strings.Join(repository.RewriteHosts, ","),
		strconv.FormatBool(repository.HTMLRewriteEnabled), strconv.FormatBool(auxiliary), strconv.FormatBool(gzip),
	}, "\x00")
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func parseAbsoluteHTTPURL(raw string) (*url.URL, error) {
	parsed, err := url.Parse(raw)
	if err != nil || !parsed.IsAbs() || parsed.Hostname() == "" || parsed.Opaque != "" {
		return nil, errors.New("URL must be absolute")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, errors.New("URL protocol is not HTTP(S)")
	}
	if parsed.User != nil {
		return nil, errors.New("URL credentials are not allowed")
	}
	parsed.Fragment = ""
	return parsed, nil
}

func representationCacheKey(base string, repository model.Mirror, class, acceptHeader string) string {
	if repository.ProxyMode != "registry" || class != "metadata" {
		return base
	}
	values := make([]string, 0, strings.Count(acceptHeader, ",")+1)
	for _, value := range strings.Split(acceptHeader, ",") {
		if value = strings.ToLower(strings.TrimSpace(value)); value != "" {
			values = append(values, value)
		}
	}
	if len(values) == 0 {
		values = append(values, "*/*")
	}
	sum := sha256.Sum256([]byte(strings.Join(values, ",")))
	return base + ":accept:" + hex.EncodeToString(sum[:8])
}

func credentialPartitionKey(repository model.Mirror, header http.Header) string {
	if !repository.CacheAuthenticated {
		return ""
	}
	authHeader := header.Get("Authorization")
	var cookieItems []string
	for _, cookieLine := range header.Values("Cookie") {
		for _, part := range strings.Split(cookieLine, ";") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			name, _, _ := strings.Cut(part, "=")
			if strings.TrimSpace(name) != auth.CookieName {
				cookieItems = append(cookieItems, part)
			}
		}
	}
	sort.Strings(cookieItems)
	cookies := strings.Join(cookieItems, ";")
	if authHeader == "" && cookies == "" {
		return ""
	}
	sum := sha256.Sum256([]byte("auth\x00" + authHeader + "\x00" + cookies))
	return ":auth:" + hex.EncodeToString(sum[:8])
}

func effectiveRepositoryConcurrency(repository model.Mirror) int {
	if repository.MaxConcurrency > 0 {
		return repository.MaxConcurrency
	}
	switch strings.ToLower(strings.TrimSpace(repository.RateLimitProfile)) {
	case "conservative":
		return 16
	case "balanced":
		return 64
	case "bulk":
		return 256
	default:
		return 0
	}
}

func orderedRepositoryUpstreams(values []model.Upstream) []model.Upstream {
	candidates := make([]model.Upstream, 0, len(values))
	for _, value := range values {
		if value.Enabled {
			candidates = append(candidates, value)
		}
	}
	statusRank := func(status string) int {
		switch status {
		case "healthy":
			return 0
		case "", "unknown":
			return 1
		default:
			return 2
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		left, right := statusRank(candidates[i].HealthStatus), statusRank(candidates[j].HealthStatus)
		if left != right {
			return left < right
		}
		return candidates[i].Priority < candidates[j].Priority
	})
	return candidates
}

func orderedRequestUpstreams(meta requestMeta) []model.Upstream {
	candidates := orderedRepositoryUpstreams(meta.repository.Upstreams)
	if meta.dynamicTarget != nil && len(candidates) > 1 {
		return candidates[:1]
	}
	return candidates
}

func activeRepositoryUpstream(values []model.Upstream) (model.Upstream, error) {
	candidates := make([]model.Upstream, 0, len(values))
	for _, value := range values {
		if value.Enabled {
			candidates = append(candidates, value)
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].Priority < candidates[j].Priority })
	for _, status := range []string{"healthy", "unknown", "unhealthy"} {
		for _, value := range candidates {
			actual := value.HealthStatus
			if actual == "" {
				actual = "unknown"
			}
			if actual == status {
				return value, nil
			}
		}
	}
	return model.Upstream{}, errors.New("no enabled upstream")
}

func ensureLeadingSlash(value string) string {
	if value == "" {
		return "/"
	}
	if strings.HasPrefix(value, "/") {
		return value
	}
	return "/" + value
}

func joinPath(base, relative string) string {
	return strings.TrimRight(base, "/") + "/" + strings.TrimLeft(relative, "/")
}

func cloneURL(value *url.URL) *url.URL {
	copy := *value
	return &copy
}

func isRedirect(status int) bool {
	return status == http.StatusMovedPermanently || status == http.StatusFound || status == http.StatusSeeOther ||
		status == http.StatusTemporaryRedirect || status == http.StatusPermanentRedirect
}

func isPackageBlocked(repository model.Mirror, relativePath string) (bool, string) {
	if len(repository.BlockedPackages) == 0 && len(repository.AllowedPackages) == 0 {
		return false, ""
	}
	cleanPath := strings.TrimPrefix(relativePath, "/")
	baseName := path.Base(cleanPath)

	// Check blocked packages list (blacklist)
	for _, pattern := range repository.BlockedPackages {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			continue
		}
		if matchPattern(pattern, cleanPath, baseName) {
			return true, "package is blocked by repository security policy (" + pattern + ")"
		}
	}

	// Check allowed packages whitelist if configured
	if len(repository.AllowedPackages) > 0 {
		matched := false
		for _, pattern := range repository.AllowedPackages {
			pattern = strings.TrimSpace(pattern)
			if pattern == "" {
				continue
			}
			if matchPattern(pattern, cleanPath, baseName) {
				matched = true
				break
			}
		}
		if !matched {
			return true, "package is not in repository allowed packages whitelist"
		}
	}

	return false, ""
}

func matchPattern(pattern, cleanPath, baseName string) bool {
	if strings.EqualFold(pattern, baseName) || strings.EqualFold(pattern, cleanPath) {
		return true
	}
	if ok, _ := path.Match(pattern, baseName); ok {
		return true
	}
	if ok, _ := path.Match(pattern, cleanPath); ok {
		return true
	}
	if re, err := regexp.Compile(pattern); err == nil {
		if re.MatchString(baseName) || re.MatchString(cleanPath) {
			return true
		}
	}
	return false
}
