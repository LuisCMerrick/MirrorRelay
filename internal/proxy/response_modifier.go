package proxy

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/LuisCMerrick/MirrorRelay/internal/model"
)

func (e *Engine) modifyResponse(response *http.Response) error {
	meta, ok := response.Request.Context().Value(requestMetaKey{}).(requestMeta)
	if !ok {
		return errors.New("missing proxy request metadata")
	}
	selected, _ := response.Request.Context().Value(selectedMetaKey{}).(selectedMeta)
	if writer, found := responseWriterFromContext(response.Request.Context()); found {
		writer.selected = selected
	}
	for name := range response.Header {
		if strings.HasPrefix(strings.ToLower(name), "x-mirror-internal-") {
			response.Header.Del(name)
		}
	}
	response.Header.Set("X-Mirror-Request-ID", meta.requestID)

	if isRedirect(response.StatusCode) && shouldRewriteRedirect(meta.repository, meta.cacheClass) {
		e.rewriteLocation(response, meta)
	}
	if meta.repository.ProxyMode == "registry" && meta.repository.AuthMode == "full_proxy" {
		if err := e.rewriteBearerChallenges(response, meta); err != nil {
			return err
		}
	}
	if response.Request.Method == http.MethodHead {
		if ((meta.rewriteHTML || (e.cfg.UIEnhancement.Enabled && e.cfg.UIEnhancement.RepositoryBrowser.Enabled)) && shouldRewriteHTMLBody(response)) || (meta.rewriteMetadata && shouldRewriteBody(response)) {
			sanitizeRewrittenMetadataHead(response)
		}
		return nil
	}
	if (meta.rewriteHTML || (e.cfg.UIEnhancement.Enabled && e.cfg.UIEnhancement.RepositoryBrowser.Enabled)) && shouldRewriteHTMLBody(response) {
		metadataConfig := e.cfg.Metadata
		if meta.repository.MetadataLimitBytes > 0 {
			metadataConfig.RewriteBufferLimit = meta.repository.MetadataLimitBytes
		}
		validator, changed, err := rewriteHTMLResponseBody(response, meta.repository, selected.upstream, meta.logicalURL,
			metadataConfig, e.cfg.UIEnhancement, acceptsGzip(meta.clientEncoding), &e.compressors)
		if err != nil {
			return err
		}
		if changed {
			metadataTTL := e.cfg.Cache.MetadataTTL
			if meta.repository.MetadataTTLSec > 0 {
				metadataTTL = time.Duration(meta.repository.MetadataTTLSec) * time.Second
			}
			validator.ExpiresAt = time.Now().Add(metadataTTL)
			e.validators.put(meta.validatorKey, validator)
		}
		return nil
	}
	if meta.rewriteMetadata && shouldRewriteBody(response) {
		metadataConfig := e.cfg.Metadata
		if meta.repository.MetadataLimitBytes > 0 {
			metadataConfig.RewriteBufferLimit = meta.repository.MetadataLimitBytes
		}
		validator, err := rewriteResponseBody(response, meta.repository, meta.publicBase, metadataConfig,
			acceptsGzip(meta.clientEncoding), &e.compressors)
		if err != nil {
			return err
		}
		metadataTTL := e.cfg.Cache.MetadataTTL
		if meta.repository.MetadataTTLSec > 0 {
			metadataTTL = time.Duration(meta.repository.MetadataTTLSec) * time.Second
		}
		validator.ExpiresAt = time.Now().Add(metadataTTL)
		e.validators.put(meta.validatorKey, validator)
	}
	return nil
}

func (e *Engine) rewriteLocation(response *http.Response, meta requestMeta) {
	location := response.Header.Get("Location")
	if location == "" || meta.logicalURL == nil {
		return
	}
	parsed, err := url.Parse(location)
	if err != nil {
		return
	}
	target := meta.logicalURL.ResolveReference(parsed)
	if !isAllowedRewriteOrigin(meta.repository, target) {
		return
	}
	response.Header.Set("Location", meta.publicBase+publicFetchPath(meta.repository, target.String()))
}

func (e *Engine) rewriteBearerChallenges(response *http.Response, meta requestMeta) error {
	values := response.Header.Values("WWW-Authenticate")
	if len(values) == 0 {
		return nil
	}
	rewritten := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if len(trimmed) < len("Bearer") || !strings.EqualFold(trimmed[:len("Bearer")], "Bearer") {
			if containsSecondaryBearerChallenge(trimmed) {
				return errors.New("reject unsafe combined WWW-Authenticate header containing a secondary Bearer challenge")
			}
			rewritten = append(rewritten, value)
			continue
		}
		challenge, err := parseBearerChallenge(value)
		if err != nil {
			return fmt.Errorf("reject unsafe Registry Bearer challenge: %w", err)
		}
		realm, found := challenge.get("realm")
		if !found {
			return errors.New("reject unsafe Registry Bearer challenge: realm is missing")
		}
		target, err := parseAbsoluteHTTPURL(realm)
		if err != nil {
			return fmt.Errorf("reject unsafe Registry Bearer challenge realm: %w", err)
		}
		if !isAllowedRewriteOrigin(meta.repository, target) {
			return fmt.Errorf("reject unsafe Registry Bearer challenge realm host %q", target.Hostname())
		}
		if meta.repository.TokenUpstream != "" {
			configured, parseErr := parseAbsoluteHTTPURL(meta.repository.TokenUpstream)
			if parseErr != nil {
				return fmt.Errorf("reject invalid configured token upstream: %w", parseErr)
			}
			target = configured
		}
		e.tokenMu.Lock()
		e.tokenTargets[meta.repository.ID] = cloneURL(target)
		e.tokenMu.Unlock()
		challenge.set("realm", meta.publicBase+"/_mirror_auth/"+strconv.FormatInt(meta.repository.ID, 10)+"/token")
		rewritten = append(rewritten, challenge.String())
	}
	response.Header.Del("WWW-Authenticate")
	for _, value := range rewritten {
		response.Header.Add("WWW-Authenticate", value)
	}
	return nil
}

func containsSecondaryBearerChallenge(value string) bool {
	quoted, escaped, afterComma := false, false, true
	for index := 0; index < len(value); index++ {
		character := value[index]
		if escaped {
			escaped = false
			continue
		}
		if quoted && character == '\\' {
			escaped = true
			continue
		}
		if character == '"' {
			quoted = !quoted
			continue
		}
		if quoted {
			continue
		}
		if character == ',' {
			afterComma = true
			continue
		}
		if afterComma && (character == ' ' || character == '\t') {
			continue
		}
		if afterComma && len(value)-index >= len("Bearer") && strings.EqualFold(value[index:index+len("Bearer")], "Bearer") {
			next := index + len("Bearer")
			if next == len(value) || value[next] == ' ' || value[next] == '\t' {
				return index > 0
			}
		}
		afterComma = false
	}
	return false
}

func (e *Engine) tokenTarget(repository model.Mirror) *url.URL {
	var target *url.URL
	if repository.TokenUpstream != "" {
		if configured, err := parseAbsoluteHTTPURL(repository.TokenUpstream); err == nil {
			target = configured
		}
	}
	if target == nil {
		e.tokenMu.RLock()
		stored := e.tokenTargets[repository.ID]
		if stored != nil {
			target = cloneURL(stored)
		}
		e.tokenMu.RUnlock()
	}
	if target == nil || !isAllowedRewriteOrigin(repository, target) {
		return nil
	}
	if target.Scheme != "https" && !(target.Scheme == "http" && e.cfg.Security.AllowHTTPUpstream && repository.AllowHTTP) {
		return nil
	}
	return cloneURL(target)
}

func isAllowedRewriteOrigin(repository model.Mirror, target *url.URL) bool {
	if target == nil || target.Hostname() == "" {
		return false
	}
	targetScheme := strings.ToLower(target.Scheme)
	if targetScheme != "http" && targetScheme != "https" {
		return false
	}
	targetHost := strings.ToLower(strings.TrimSuffix(target.Hostname(), "."))
	targetPort := target.Port()
	if targetPort == "" {
		if targetScheme == "https" {
			targetPort = "443"
		} else {
			targetPort = "80"
		}
	}

	for _, upstream := range repository.Upstreams {
		parsed, err := url.Parse(upstream.URL)
		if err == nil {
			uScheme := strings.ToLower(parsed.Scheme)
			uHost := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
			uPort := parsed.Port()
			if uPort == "" {
				if uScheme == "https" {
					uPort = "443"
				} else {
					uPort = "80"
				}
			}
			if uScheme == targetScheme && uHost == targetHost && uPort == targetPort {
				return true
			}
		}
	}

	if repository.TokenUpstream != "" {
		parsed, err := url.Parse(repository.TokenUpstream)
		if err == nil {
			tScheme := strings.ToLower(parsed.Scheme)
			tHost := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
			tPort := parsed.Port()
			if tPort == "" {
				if tScheme == "https" {
					tPort = "443"
				} else {
					tPort = "80"
				}
			}
			if tScheme == targetScheme && tHost == targetHost && tPort == targetPort {
				return true
			}
		}
	}

	for _, allowed := range repository.RewriteHosts {
		allowed = strings.TrimSpace(allowed)
		if allowed == "" {
			continue
		}
		if strings.HasPrefix(allowed, "http://") || strings.HasPrefix(allowed, "https://") {
			parsed, err := url.Parse(allowed)
			if err == nil {
				aScheme := strings.ToLower(parsed.Scheme)
				aHost := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
				aPort := parsed.Port()
				if aPort == "" {
					if aScheme == "https" {
						aPort = "443"
					} else {
						aPort = "80"
					}
				}
				if aScheme == targetScheme && aHost == targetHost && aPort == targetPort {
					return true
				}
			}
			continue
		}

		aHost, aPort, err := net.SplitHostPort(allowed)
		if err != nil {
			aHost = strings.ToLower(strings.TrimSuffix(allowed, "."))
			aPort = ""
		} else {
			aHost = strings.ToLower(strings.TrimSuffix(aHost, "."))
		}
		hostMatches := false
		if aHost == targetHost {
			hostMatches = true
		} else if strings.HasPrefix(aHost, "*.") {
			suffix := aHost[1:]
			if strings.HasSuffix(targetHost, suffix) || targetHost == aHost[2:] {
				hostMatches = true
			}
		}
		if hostMatches {
			if aPort != "" {
				if aPort == targetPort {
					return true
				}
			} else {
				if targetScheme == "https" && targetPort == "443" {
					return true
				}
				if targetScheme == "http" && repository.AllowHTTP && targetPort == "80" {
					return true
				}
			}
		}
	}

	return false
}

func publicFetchPath(repository model.Mirror, target string) string {
	parsed, err := url.Parse(target)
	fragment := ""
	if err == nil {
		fragment = parsed.EscapedFragment()
		parsed.Fragment = ""
		target = parsed.String()
	}
	prefix := repository.PublicPath
	if repository.PublicMode == "host" {
		prefix = "/"
	}
	if err == nil && strings.ContainsAny(parsed.Path+parsed.RawQuery, "{}") {
		origin := parsed.Scheme + "://" + parsed.Host
		result := strings.TrimRight(prefix, "/") + "/__fetch_template/" +
			base64.RawURLEncoding.EncodeToString([]byte(origin)) + ensureLeadingSlash(parsed.Path)
		if parsed.RawQuery != "" {
			result += "?" + parsed.RawQuery
		}
		if fragment != "" {
			result += "#" + fragment
		}
		return result
	}
	result := strings.TrimRight(prefix, "/") + "/__fetch/" + base64.RawURLEncoding.EncodeToString([]byte(target))
	if fragment != "" {
		result += "#" + fragment
	}
	return result
}

func shouldFollowRedirects(repository model.Mirror, class string, tokenRoute bool) bool {
	if tokenRoute {
		return true
	}
	if repository.ProxyMode == "registry" {
		return repository.BlobRedirectMode == "full_proxy"
	}
	if repository.ProxyMode == "packages" || repository.ProxyMode == "transparent" {
		if repository.RedirectMode == "follow" {
			return true
		}
	}
	return false
}

func shouldRewriteRedirect(repository model.Mirror, class string) bool {
	if repository.ProxyMode == "registry" {
		return repository.BlobRedirectMode == "rewrite"
	}
	return repository.RedirectMode == "rewrite"
}

func prepareMetadataRequestHeaders(header http.Header) {
	header.Set("Accept-Encoding", "identity")
	for _, name := range []string{"If-None-Match", "If-Modified-Since", "Range", "If-Range"} {
		header.Del(name)
	}
}

func prepareHTMLRewriteRequestHeaders(header http.Header) {
	header.Set("Accept-Encoding", "identity")
	for _, name := range []string{"If-None-Match", "If-Modified-Since"} {
		header.Del(name)
	}
}
