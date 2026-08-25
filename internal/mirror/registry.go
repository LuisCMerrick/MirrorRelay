// Package mirror tracks desired and active mirror state and routing.
package mirror

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/textproto"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/LuisCMerrick/MirrorRelay/internal/model"
)

type Loader interface {
	ListMirrors(context.Context) ([]model.Mirror, error)
}

type snapshot struct {
	all    []model.Mirror
	bySlug map[string]model.Mirror
	byHost map[string]model.Mirror
	byID   map[int64]model.Mirror
}

type Registry struct {
	loader Loader
	mu     sync.Mutex
	value  atomic.Value
}

func NewRegistry(loader Loader) *Registry {
	r := &Registry{loader: loader}
	r.value.Store(snapshot{bySlug: make(map[string]model.Mirror), byHost: make(map[string]model.Mirror), byID: make(map[int64]model.Mirror)})
	return r
}

func (r *Registry) Reload(ctx context.Context) error {
	all, err := r.loader.ListMirrors(ctx)
	if err != nil {
		return err
	}
	r.Replace(all)
	return nil
}

// Replace publishes a complete, immutable active-routing snapshot. Management
// code calls this only after the corresponding Managed Upstream Nginx configuration has been
// activated successfully.
func (r *Registry) Replace(all []model.Mirror) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.value.Store(buildSnapshot(all))
}

func buildSnapshot(all []model.Mirror) snapshot {
	bySlug := make(map[string]model.Mirror, len(all))
	byHost := make(map[string]model.Mirror)
	byID := make(map[int64]model.Mirror, len(all))
	active := make([]model.Mirror, 0, len(all))
	for _, value := range all {
		m := clone(value)
		if err := CompilePackagePolicy(&m); err != nil {
			m.PackagePolicy = &model.PackagePolicy{Invalid: err.Error()}
		}
		active = append(active, m)
		m = clone(m)
		bySlug[strings.ToLower(m.Slug)] = m
		byID[m.ID] = m
		if m.PublicMode == "host" && m.PublicHost != "" {
			byHost[strings.ToLower(m.PublicHost)] = m
		}
	}
	return snapshot{all: active, bySlug: bySlug, byHost: byHost, byID: byID}
}

// UpdateUpstreamHealth updates runtime health data without importing pending
// desired configuration from the database into the active routing snapshot.
func (r *Registry) UpdateUpstreamHealth(upstreamID int64, status string, latencyMS int64, message string, checkedAt time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	current := r.value.Load().(snapshot)
	all := make([]model.Mirror, len(current.all))
	changed := false
	for i, repository := range current.all {
		all[i] = clone(repository)
		for j := range all[i].Upstreams {
			if all[i].Upstreams[j].ID != upstreamID {
				continue
			}
			all[i].Upstreams[j].HealthStatus = status
			all[i].Upstreams[j].LatencyMS = latencyMS
			all[i].Upstreams[j].LastError = message
			all[i].Upstreams[j].LastCheck = checkedAt
			changed = true
		}
	}
	if changed {
		r.value.Store(buildSnapshot(all))
	}
}

func (r *Registry) ResolveRequest(host, path string) (model.Mirror, bool) {
	m, _, ok := r.Route(host, path)
	return m, ok
}

// Route resolves both host-mode and path-mode repositories and returns the
// repository-relative escaped path used by the internal Managed Upstream Nginx route.
func (r *Registry) Route(host, path string) (model.Mirror, string, bool) {
	s := r.value.Load().(snapshot)
	if name, _, err := net.SplitHostPort(host); err == nil {
		host = name
	}
	if m, ok := s.byHost[strings.ToLower(strings.Trim(host, "[]"))]; ok && m.Enabled {
		if path == "" {
			path = "/"
		}
		return clone(m), path, true
	}
	var selected model.Mirror
	selectedPrefix := ""
	for _, m := range s.all {
		if !m.Enabled || m.PublicMode == "host" {
			continue
		}
		prefix := m.PublicPath
		if prefix == "" {
			prefix = "/" + m.Slug + "/"
		}
		root := strings.TrimSuffix(prefix, "/")
		if (path == root || strings.HasPrefix(path, prefix)) && len(root) > len(selectedPrefix) {
			selected = m
			selectedPrefix = root
		}
	}
	if selectedPrefix == "" {
		return model.Mirror{}, "", false
	}
	relative := strings.TrimPrefix(path, selectedPrefix)
	if relative == "" {
		relative = "/"
	}
	return clone(selected), relative, true
}

func (r *Registry) List() []model.Mirror {
	s := r.value.Load().(snapshot)
	out := make([]model.Mirror, len(s.all))
	for i := range s.all {
		out[i] = clone(s.all[i])
	}
	return out
}

func (r *Registry) Get(slug string) (model.Mirror, bool) {
	s := r.value.Load().(snapshot)
	m, ok := s.bySlug[strings.ToLower(slug)]
	return clone(m), ok
}

func (r *Registry) GetByID(id int64) (model.Mirror, bool) {
	s := r.value.Load().(snapshot)
	m, ok := s.byID[id]
	return clone(m), ok
}

// Resolve matches exactly the first path segment. It never performs prefix
// matching, so /deb cannot accidentally capture /debian.
func (r *Registry) Resolve(path string) (model.Mirror, string, bool) {
	if !strings.HasPrefix(path, "/") {
		return model.Mirror{}, "", false
	}
	trimmed := strings.TrimPrefix(path, "/")
	segment, rest, found := strings.Cut(trimmed, "/")
	if !found {
		rest = ""
	}
	m, ok := r.Get(segment)
	if !ok || !m.Enabled {
		return model.Mirror{}, "", false
	}
	return m, "/" + rest, true
}

func clone(m model.Mirror) model.Mirror {
	m.Upstreams = append([]model.Upstream(nil), m.Upstreams...)
	m.RewriteHosts = append([]string(nil), m.RewriteHosts...)
	m.HeaderRemove = append([]string(nil), m.HeaderRemove...)
	m.BlockedPackages = append([]string(nil), m.BlockedPackages...)
	m.AllowedPackages = append([]string(nil), m.AllowedPackages...)
	if m.HeaderAdd != nil {
		m.HeaderAdd = make(map[string]string, len(m.HeaderAdd))
		for name, value := range m.HeaderAdd {
			m.HeaderAdd[name] = value
		}
	}
	return m
}

var slugPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)
var headerNamePattern = regexp.MustCompile(`^[!#$%&'*+.^_` + "`" + `|~0-9A-Za-z-]+$`)

var repositoryTypes = map[string]bool{
	"generic": true, "apt": true, "rpm": true, "apk": true, "opkg": true,
	"pypi": true, "npm": true, "maven": true, "nuget": true, "cargo": true,
	"goproxy": true, "conda": true, "docker-registry": true, "oci-registry": true,
	"iso": true,
}

func NormalizeAndValidate(m *model.Mirror, allowHTTP, globallyAllowPrivate bool) error {
	m.Name = strings.TrimSpace(m.Name)
	if m.Name == "" {
		return errors.New("name is required")
	}
	if len(m.Name) > 100 || strings.ContainsAny(m.Name, "\x00\r\n") {
		return errors.New("name must be at most 100 characters and contain no control characters")
	}
	m.Slug = strings.ToLower(strings.Trim(strings.TrimSpace(m.Slug), "/"))
	m.Type = strings.ToLower(strings.TrimSpace(m.Type))
	m.PublicMode = strings.ToLower(strings.TrimSpace(m.PublicMode))
	m.PublicHost = strings.ToLower(strings.TrimSpace(m.PublicHost))
	m.PublicPath = strings.TrimSpace(m.PublicPath)
	m.ProxyMode = strings.ToLower(strings.TrimSpace(m.ProxyMode))
	m.CacheProfile = strings.ToLower(strings.TrimSpace(m.CacheProfile))
	m.RewriteProfile = strings.ToLower(strings.TrimSpace(m.RewriteProfile))
	for i := range m.RewriteHosts {
		m.RewriteHosts[i] = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(m.RewriteHosts[i]), "."))
		if !validHostname(m.RewriteHosts[i]) {
			return fmt.Errorf("rewrite_hosts[%d] must be a hostname", i)
		}
	}
	m.ProfileName = strings.TrimSpace(m.ProfileName)
	m.ProfileVersion = strings.TrimSpace(m.ProfileVersion)
	m.RateLimitProfile = strings.TrimSpace(m.RateLimitProfile)
	m.AccessPolicy = strings.ToLower(strings.TrimSpace(m.AccessPolicy))
	m.StripPrefix = strings.TrimSpace(m.StripPrefix)
	m.AddPrefix = strings.TrimSpace(m.AddPrefix)
	m.HostRewrite = strings.TrimSpace(m.HostRewrite)
	if err := normalizeHeaders(m); err != nil {
		return err
	}
	m.AuthMode = strings.ToLower(strings.TrimSpace(m.AuthMode))
	m.TokenUpstream = strings.TrimSpace(m.TokenUpstream)
	m.BlobRedirectMode = strings.ToLower(strings.TrimSpace(m.BlobRedirectMode))
	m.HealthCheckPath = strings.TrimSpace(m.HealthCheckPath)
	m.HealthMethod = strings.ToUpper(strings.TrimSpace(m.HealthMethod))
	m.RedirectMode = strings.ToLower(strings.TrimSpace(m.RedirectMode))
	if !slugPattern.MatchString(m.Slug) {
		return errors.New("slug must contain only lowercase letters, digits and internal hyphens (1-63 characters)")
	}
	if m.Type == "" {
		m.Type = "generic"
	}
	if !repositoryTypes[m.Type] {
		return fmt.Errorf("unsupported repository type %q", m.Type)
	}
	if m.PublicMode == "" {
		m.PublicMode = "path"
	}
	if m.PublicMode != "path" && m.PublicMode != "host" {
		return errors.New("public_mode must be path or host")
	}
	if m.PublicMode == "host" {
		m.PublicHost = strings.TrimSuffix(m.PublicHost, ".")
		if strings.Contains(m.PublicHost, ":") || !validHostname(m.PublicHost) {
			return errors.New("public_host must be a hostname without scheme or path")
		}
		m.PublicPath = "/"
	} else {
		if m.PublicPath == "" {
			m.PublicPath = "/" + m.Slug + "/"
		}
		trimmedPath := strings.Trim(m.PublicPath, "/")
		if trimmedPath == "" {
			return errors.New("public_path cannot replace the repository index at /")
		}
		for _, segment := range strings.Split(trimmedPath, "/") {
			if segment == "" || segment == "." || segment == ".." {
				return errors.New("public_path must not contain empty, dot or parent segments")
			}
		}
		m.PublicPath = "/" + trimmedPath + "/"
		if strings.ContainsAny(m.PublicPath, "\\%\x00\r\n\t ?#") {
			return errors.New("public_path must be an unescaped URL path without backslashes, whitespace, query or fragment")
		}
		first := strings.Split(strings.Trim(m.PublicPath, "/"), "/")[0]
		switch first {
		case "metrics", "healthz", "_mirror_auth", "_mirrorrelay":
			return errors.New("public_path uses a reserved system prefix")
		}
	}
	if m.ProxyMode == "" {
		m.ProxyMode = "transparent"
	}
	if m.ProxyMode != "transparent" && m.ProxyMode != "rewrite" && m.ProxyMode != "registry" && m.ProxyMode != "custom" {
		return errors.New("proxy_mode must be transparent, rewrite, registry or custom")
	}
	if m.Type == "docker-registry" || m.Type == "oci-registry" {
		m.ProxyMode = "registry"
		m.PullOnly = true
		if m.PublicMode != "host" {
			return errors.New("Docker/OCI Registry V1 requires public_mode=host")
		}
	}
	if len(m.RewriteHosts) == 0 {
		switch m.Type {
		case "pypi":
			m.RewriteHosts = []string{"pypi.org", "files.pythonhosted.org"}
		case "npm":
			m.RewriteHosts = []string{"registry.npmjs.org"}
		case "nuget":
			m.RewriteHosts = []string{"api.nuget.org", "globalcdn.nuget.org"}
		case "cargo":
			m.RewriteHosts = []string{"index.crates.io", "static.crates.io"}
		case "docker-registry", "oci-registry":
			m.RewriteHosts = []string{"auth.docker.io", "registry-1.docker.io", "production.cloudfront.docker.com"}
		}
	}
	if m.CacheProfile == "" {
		m.CacheProfile = "standard"
	}
	if m.ProfileVersion == "" {
		m.ProfileVersion = "1.0.0"
	}
	if m.AuthMode == "" {
		m.AuthMode = "direct"
	}
	if m.AuthMode != "direct" && m.AuthMode != "full_proxy" {
		return errors.New("auth_mode must be direct or full_proxy")
	}
	if m.TokenUpstream != "" {
		parsed, err := url.Parse(m.TokenUpstream)
		if err != nil || !parsed.IsAbs() || parsed.Hostname() == "" || parsed.User != nil {
			return errors.New("token_upstream must be an absolute HTTP(S) URL without credentials")
		}
		if parsed.Scheme != "https" && !(allowHTTP && m.AllowHTTP && parsed.Scheme == "http") {
			return errors.New("token_upstream must use HTTPS")
		}
		parsed.Fragment = ""
		m.TokenUpstream = parsed.String()
	}
	if m.BlobRedirectMode == "" {
		m.BlobRedirectMode = "full_proxy"
	}
	if m.BlobRedirectMode != "pass" && m.BlobRedirectMode != "full_proxy" {
		return errors.New("blob_redirect_mode must be pass or full_proxy")
	}
	switch m.Slug {
	case "admin", "api", "metrics", "healthz":
		return errors.New("slug is reserved")
	}
	if len(m.Upstreams) == 0 {
		return errors.New("at least one upstream is required")
	}
	if m.HealthIntervalSec <= 0 {
		m.HealthIntervalSec = 60
	}
	if m.HealthTimeoutSec <= 0 {
		m.HealthTimeoutSec = 5
	}
	if m.HealthMethod == "" {
		m.HealthMethod = "HEAD"
	}
	if m.HealthMethod != "HEAD" && m.HealthMethod != "GET" {
		return errors.New("health_method must be HEAD or GET")
	}
	if m.HealthExpected == 0 {
		m.HealthExpected = 200
	}
	if m.HealthExpected < 100 || m.HealthExpected > 599 {
		return errors.New("health_expected must be a valid HTTP status")
	}
	if m.ConnectTimeoutSec == 0 {
		m.ConnectTimeoutSec = 10
	}
	if m.ReadTimeoutSec == 0 {
		m.ReadTimeoutSec = 3600
	}
	if m.SendTimeoutSec == 0 {
		m.SendTimeoutSec = 3600
	}
	if m.ConnectTimeoutSec < 1 || m.ConnectTimeoutSec > 75 {
		return errors.New("connect_timeout_sec must be 1..75")
	}
	if m.ReadTimeoutSec < 1 || m.ReadTimeoutSec > 604800 || m.SendTimeoutSec < 1 || m.SendTimeoutSec > 604800 {
		return errors.New("read_timeout_sec and send_timeout_sec must be 1..604800")
	}
	if m.MetadataLimitBytes < 0 || m.MetadataLimitBytes > 512<<20 {
		return errors.New("metadata_rewrite_limit_bytes must be 0..536870912")
	}
	for name, value := range map[string]int{
		"metadata_ttl_sec": m.MetadataTTLSec, "package_ttl_sec": m.PackageTTLSec,
		"immutable_ttl_sec": m.ImmutableTTLSec, "blob_ttl_sec": m.BlobTTLSec,
	} {
		if value < 0 || value > 315360000 {
			return fmt.Errorf("%s must be 0..315360000", name)
		}
	}
	if m.RedirectMode == "" {
		m.RedirectMode = "full_proxy"
	}
	if m.RedirectMode != "pass" && m.RedirectMode != "follow" && m.RedirectMode != "rewrite" && m.RedirectMode != "full_proxy" {
		return errors.New("redirect_mode must be pass, follow, rewrite or full_proxy")
	}
	if m.AccessPolicy == "" {
		m.AccessPolicy = "public"
	}
	if m.AccessPolicy != "public" && m.AccessPolicy != "admin" {
		return errors.New("access_policy must be public or admin")
	}
	for name, value := range map[string]string{"strip_prefix": m.StripPrefix, "add_prefix": m.AddPrefix, "health_check_path": m.HealthCheckPath} {
		if strings.ContainsAny(value, "\x00\r\n\t ?#") {
			return fmt.Errorf("%s must be a URL path without whitespace, query or fragment", name)
		}
	}
	if m.HostRewrite != "" && !validAuthority(m.HostRewrite) {
		return errors.New("host_rewrite must be a valid host or host:port authority")
	}
	for i := range m.Upstreams {
		u := &m.Upstreams[i]
		parsed, err := url.Parse(strings.TrimSpace(u.URL))
		if err != nil || parsed.Hostname() == "" {
			return fmt.Errorf("upstream %d has an invalid URL", i+1)
		}
		if parsed.User != nil || parsed.Fragment != "" || parsed.RawQuery != "" {
			return fmt.Errorf("upstream %d must not contain credentials, query or fragment", i+1)
		}
		if parsed.Scheme != "https" && !(allowHTTP && m.AllowHTTP && parsed.Scheme == "http") {
			return fmt.Errorf("upstream %d must use HTTPS", i+1)
		}
		escapedPath := "/" + strings.Trim(parsed.EscapedPath(), "/")
		if escapedPath != "/" {
			escapedPath += "/"
		}
		decodedPath, decodeErr := url.PathUnescape(escapedPath)
		if decodeErr != nil {
			return fmt.Errorf("upstream %d has an invalid escaped path", i+1)
		}
		parsed.Path = decodedPath
		parsed.RawPath = escapedPath
		u.URL = parsed.String()
		u.Host = strings.TrimSpace(u.Host)
		if u.Host == "" {
			u.Host = parsed.Host
		}
		if !validAuthority(u.Host) {
			return fmt.Errorf("upstream %d host must be a valid host or host:port authority", i+1)
		}
		if u.Priority <= 0 {
			u.Priority = 100
		}
		if u.Weight <= 0 {
			u.Weight = 1
		}
	}
	if m.ConfigState == "" {
		m.ConfigState = "pending"
	}
	if m.AllowPrivate && !globallyAllowPrivate {
		return errors.New("private upstreams are disabled by system policy")
	}
	if m.AllowHTTP && !allowHTTP {
		return errors.New("HTTP upstreams are disabled by system policy")
	}
	if m.InsecureTLS {
		return errors.New("insecure_skip_verify is not supported; upstream TLS certificates must be verified")
	}
	if err := CompilePackagePolicy(m); err != nil {
		return err
	}
	sort.SliceStable(m.Upstreams, func(i, j int) bool { return m.Upstreams[i].Priority < m.Upstreams[j].Priority })
	return nil
}

const (
	maxPackagePatterns     = 128
	maxPackagePatternBytes = 512
)

// CompilePackagePolicy validates, bounds and precompiles every repository
// package rule. The compiled policy is immutable and safe for concurrent use by
// the active routing snapshot.
func CompilePackagePolicy(m *model.Mirror) error {
	blocked, blockedValues, err := compilePackagePatterns("blocked_packages", m.BlockedPackages)
	if err != nil {
		return err
	}
	allowed, allowedValues, err := compilePackagePatterns("allowed_packages", m.AllowedPackages)
	if err != nil {
		return err
	}
	m.BlockedPackages = blockedValues
	m.AllowedPackages = allowedValues
	m.PackagePolicy = &model.PackagePolicy{Blocked: blocked, Allowed: allowed}
	return nil
}

func compilePackagePatterns(field string, values []string) ([]model.PackagePattern, []string, error) {
	if len(values) > maxPackagePatterns {
		return nil, nil, fmt.Errorf("%s must contain at most %d patterns", field, maxPackagePatterns)
	}
	compiled := make([]model.PackagePattern, 0, len(values))
	normalized := make([]string, 0, len(values))
	for index, raw := range values {
		pattern := strings.TrimSpace(raw)
		if pattern == "" {
			return nil, nil, fmt.Errorf("%s[%d] must not be empty", field, index)
		}
		if len(pattern) > maxPackagePatternBytes {
			return nil, nil, fmt.Errorf("%s[%d] exceeds %d bytes", field, index, maxPackagePatternBytes)
		}
		if strings.ContainsAny(pattern, "\x00\r\n") {
			return nil, nil, fmt.Errorf("%s[%d] contains control characters", field, index)
		}
		_, globErr := path.Match(pattern, "")
		expression, regexpErr := regexp.Compile(pattern)
		if globErr != nil && regexpErr != nil {
			return nil, nil, fmt.Errorf("%s[%d] is neither a valid glob nor RE2 expression", field, index)
		}
		compiled = append(compiled, model.PackagePattern{Pattern: pattern, Glob: globErr == nil, Regexp: expression})
		normalized = append(normalized, pattern)
	}
	return compiled, normalized, nil
}

func validHostname(value string) bool {
	if value == "" || len(value) > 253 || strings.Contains(value, "..") {
		return false
	}
	if ip := net.ParseIP(value); ip != nil {
		return true
	}
	for _, label := range strings.Split(value, ".") {
		if !slugPattern.MatchString(strings.ToLower(label)) {
			return false
		}
	}
	return true
}

func validAuthority(value string) bool {
	parsed, err := url.Parse("https://" + value)
	if err != nil || parsed.Host == "" || parsed.Host != value || parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return false
	}
	if !validHostname(strings.TrimSuffix(parsed.Hostname(), ".")) {
		return false
	}
	if port := parsed.Port(); port != "" {
		value, err := strconv.Atoi(port)
		return err == nil && value >= 1 && value <= 65535
	}
	return true
}

func normalizeHeaders(m *model.Mirror) error {
	denied := map[string]bool{
		"connection": true, "content-length": true, "host": true, "keep-alive": true,
		"proxy-authenticate": true, "proxy-authorization": true, "te": true, "trailer": true,
		"transfer-encoding": true, "upgrade": true, "forwarded": true, "x-forwarded-for": true,
		"x-forwarded-host": true, "x-forwarded-proto": true, "x-real-ip": true,
	}
	add := make(map[string]string, len(m.HeaderAdd))
	for rawName, value := range m.HeaderAdd {
		name := textproto.CanonicalMIMEHeaderKey(strings.TrimSpace(rawName))
		lower := strings.ToLower(name)
		if name == "" || !headerNamePattern.MatchString(name) || denied[lower] || strings.HasPrefix(lower, "x-mirror-internal-") {
			return fmt.Errorf("header_add contains forbidden header %q", rawName)
		}
		if strings.ContainsAny(value, "\x00\r\n") {
			return fmt.Errorf("header_add %s contains control characters", name)
		}
		add[name] = value
	}
	removeSet := make(map[string]bool, len(m.HeaderRemove))
	remove := make([]string, 0, len(m.HeaderRemove))
	for _, rawName := range m.HeaderRemove {
		name := textproto.CanonicalMIMEHeaderKey(strings.TrimSpace(rawName))
		lower := strings.ToLower(name)
		if name == "" || !headerNamePattern.MatchString(name) || denied[lower] || strings.HasPrefix(lower, "x-mirror-internal-") {
			return fmt.Errorf("header_remove contains forbidden header %q", rawName)
		}
		if _, exists := add[name]; exists {
			return fmt.Errorf("header %s cannot be both added and removed", name)
		}
		if !removeSet[name] {
			removeSet[name] = true
			remove = append(remove, name)
		}
	}
	sort.Strings(remove)
	m.HeaderAdd, m.HeaderRemove = add, remove
	return nil
}
