package proxy

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/LuisCMerrick/MirrorRelay/internal/config"
	"github.com/LuisCMerrick/MirrorRelay/internal/model"
	"github.com/LuisCMerrick/MirrorRelay/internal/security"
)

type upstreamNginxTransport struct {
	cfg      config.Config
	resolver security.Resolver
	base     *http.Transport
	engine   *Engine
}

func newUpstreamNginxTransport(cfg config.Config, resolver security.Resolver) *upstreamNginxTransport {
	dialer := &net.Dialer{Timeout: cfg.Transport.DialTimeout, KeepAlive: cfg.Transport.KeepAlive}
	network, address := cfg.UpstreamEndpoint()
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return dialer.DialContext(ctx, network, address)
		},
		DisableCompression:    true,
		MaxIdleConns:          cfg.Transport.MaxIdleConns,
		MaxIdleConnsPerHost:   cfg.Transport.MaxIdleConnsPerHost,
		IdleConnTimeout:       cfg.Transport.IdleConnTimeout,
		ResponseHeaderTimeout: cfg.Transport.ResponseHeaderTimeout,
		ExpectContinueTimeout: time.Second,
	}
	return &upstreamNginxTransport{cfg: cfg, resolver: resolver, base: transport}
}

func (t *upstreamNginxTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	meta, ok := request.Context().Value(requestMetaKey{}).(requestMeta)
	if !ok {
		return nil, errors.New("missing proxy request metadata")
	}
	started := time.Now()
	currentMeta := meta
	candidates := orderedRequestUpstreams(meta)
	if len(candidates) == 0 {
		return nil, errors.New("no enabled upstream")
	}
	var response *http.Response
	var selected model.Upstream
	var err error
	for index, candidate := range candidates {
		currentMeta, err = t.metaForUpstream(request.Context(), meta, candidate)
		if err != nil {
			return nil, err
		}
		response, err = t.roundTrip(request, currentMeta, candidate.ID)
		if err == nil && !retryableUpstreamStatus(response.StatusCode) {
			selected = candidate
			break
		}
		if response != nil {
			_ = response.Body.Close()
		}
		if index == len(candidates)-1 {
			if err != nil {
				return nil, err
			}
			selected = candidate
			break
		}
	}
	if currentMeta.followRedirects {
		response, currentMeta, err = t.followRedirects(request, response, currentMeta, selected.ID)
		if err != nil {
			return nil, err
		}
	}
	cacheStatus := strings.ToUpper(strings.TrimSpace(response.Header.Get("X-Mirror-Cache")))
	selection := selectedMeta{upstream: selected, duration: time.Since(started), cacheStatus: cacheStatus}
	ctx := context.WithValue(response.Request.Context(), selectedMetaKey{}, selection)
	ctx = context.WithValue(ctx, requestMetaKey{}, currentMeta)
	response.Request = response.Request.WithContext(ctx)
	return response, nil
}

func (t *upstreamNginxTransport) metaForUpstream(ctx context.Context, meta requestMeta, upstream model.Upstream) (requestMeta, error) {
	if meta.dynamicTarget != nil {
		return meta, nil
	}
	logicalURL, err := repositoryURL(meta.repository, upstream, meta.relativePath, meta.logicalURL.RawQuery, meta.auxiliary)
	if err != nil {
		return meta, err
	}
	cacheKey, objectID, err := t.engine.cacheKeys.Key(ctx, meta.repository.ID, repositoryUpstreamIdentity(upstream, meta.auxiliary), meta.relativePath, logicalURL.RawQuery)
	if err != nil {
		return meta, err
	}
	cacheKey = representationCacheKey(cacheKey, meta.repository, meta.cacheClass, meta.acceptHeader) + meta.authPartition
	meta.logicalURL = logicalURL
	meta.cacheKey = cacheKey
	meta.objectID = objectID
	meta.validatorKey = metadataValidatorKey(meta.repository, repositoryUpstreamIdentity(upstream, meta.auxiliary), meta.relativePath,
		logicalURL.RawQuery, meta.publicBase, meta.auxiliary, acceptsGzip(meta.clientEncoding))
	return meta, nil
}

func (t *upstreamNginxTransport) roundTrip(original *http.Request, meta requestMeta, upstreamID int64) (*http.Response, error) {
	out := original.Clone(context.WithValue(original.Context(), requestMetaKey{}, meta))
	out.URL = &url.URL{Scheme: "http", Host: "mirrorrelay-upstream-nginx-internal"}
	out.Host = "mirrorrelay-upstream-nginx-internal"
	out.RequestURI = ""
	stripUntrustedHeaders(out.Header)
	out.Header.Set("X-Mirror-Internal-Repository-ID", strconv.FormatInt(meta.repository.ID, 10))
	out.Header.Set("X-Mirror-Internal-Cache-Key", meta.cacheKey)
	out.Header.Set("X-Mirror-Internal-Request-ID", meta.requestID)
	if t.cfg.Security.ExposeClientIP {
		out.Header.Set("X-Mirror-Internal-Client-IP", meta.clientIP)
	}
	if meta.cacheBypass {
		out.Header.Set("X-Mirror-Internal-Cache-Bypass", "1")
	}
	if meta.rewriteMetadata {
		prepareMetadataRequestHeaders(out.Header)
	} else if meta.rewriteHTML {
		prepareHTMLRewriteRequestHeaders(out.Header)
	}
	if meta.dynamicTarget == nil {
		prefix := "/_repo/"
		if meta.auxiliary {
			prefix = "/_repo_aux/"
		}
		out.URL.Path = prefix + strconv.FormatInt(meta.repository.ID, 10) + "/" + strconv.FormatInt(upstreamID, 10) + "/" + meta.cacheClass + ensureLeadingSlash(meta.relativePath)
		out.URL.RawQuery = original.URL.RawQuery
		return t.base.RoundTrip(out)
	}
	if meta.logicalURL != nil && !sameOrigin(meta.logicalURL, meta.dynamicTarget) {
		out.Header.Del("Authorization")
		out.Header.Del("Cookie")
	}
	target, err := security.ResolveApprovedTarget(out.Context(), meta.dynamicTarget.String(),
		t.cfg.Security.AllowHTTPUpstream && meta.repository.AllowHTTP,
		t.cfg.Security.AllowPrivateUpstream && meta.repository.AllowPrivate,
		t.cfg.Redirect.RejectMixedResult, t.resolver)
	if err != nil {
		return nil, fmt.Errorf("redirect target rejected: %w", err)
	}
	out.URL.Path = "/_target/" + strconv.FormatInt(meta.repository.ID, 10) + "/" + meta.cacheClass + "/" + target.URL.Scheme + "/"
	out.URL.RawQuery = ""
	out.Header.Set("X-Mirror-Internal-Upstream-IP", target.IP.String())
	out.Header.Set("X-Mirror-Internal-Upstream-Port", target.Port)
	out.Header.Set("X-Mirror-Internal-Upstream-Address", target.Address)
	out.Header.Set("X-Mirror-Internal-Upstream-Host", target.Host)
	out.Header.Set("X-Mirror-Internal-Upstream-Authority", target.Authority)
	uri := target.URL.EscapedPath()
	if uri == "" {
		uri = "/"
	}
	if target.URL.RawQuery != "" {
		uri += "?" + target.URL.RawQuery
	}
	out.Header.Set("X-Mirror-Internal-Upstream-URI", uri)
	return t.base.RoundTrip(out)
}

func (t *upstreamNginxTransport) followRedirects(original *http.Request, response *http.Response, meta requestMeta, upstreamID int64) (*http.Response, requestMeta, error) {
	visited := make(map[string]bool)
	if meta.logicalURL != nil {
		visited[meta.logicalURL.String()] = true
	}
	for hop := 0; response != nil && isRedirect(response.StatusCode); hop++ {
		if hop >= t.cfg.Redirect.MaxHops {
			_ = response.Body.Close()
			return nil, meta, errors.New("redirect limit exceeded")
		}
		location := response.Header.Get("Location")
		if location == "" {
			return response, meta, nil
		}
		parsed, err := url.Parse(location)
		if err != nil || meta.logicalURL == nil {
			_ = response.Body.Close()
			return nil, meta, errors.New("invalid redirect location")
		}
		target := meta.logicalURL.ResolveReference(parsed)
		if !isAllowedRewriteOrigin(meta.repository, target) {
			_ = response.Body.Close()
			return nil, meta, fmt.Errorf("redirect target %s is not allowed", target.String())
		}
		if visited[target.String()] {
			_ = response.Body.Close()
			return nil, meta, errors.New("redirect loop detected")
		}
		visited[target.String()] = true
		_ = response.Body.Close()
		next := original.Clone(original.Context())
		if !sameOrigin(meta.logicalURL, target) {
			next.Header.Del("Authorization")
			next.Header.Del("Cookie")
		}
		if response.StatusCode == http.StatusSeeOther && next.Method != http.MethodHead {
			next.Method = http.MethodGet
			next.Body = nil
		}
		meta.dynamicTarget = target
		meta.logicalURL = cloneURL(target)
		meta.cacheClass = classifyObject(meta.repository, target.EscapedPath(), target)
		response, err = t.roundTrip(next, meta, upstreamID)
		if err != nil {
			return nil, meta, err
		}
	}
	return response, meta, nil
}

func retryableUpstreamStatus(status int) bool {
	return status == http.StatusBadGateway || status == http.StatusServiceUnavailable || status == http.StatusGatewayTimeout
}

func sameOrigin(left, right *url.URL) bool {
	if left == nil || right == nil || !strings.EqualFold(left.Scheme, right.Scheme) ||
		!strings.EqualFold(strings.TrimSuffix(left.Hostname(), "."), strings.TrimSuffix(right.Hostname(), ".")) {
		return false
	}
	effectivePort := func(value *url.URL) string {
		if port := value.Port(); port != "" {
			return port
		}
		if strings.EqualFold(value.Scheme, "https") {
			return "443"
		}
		if strings.EqualFold(value.Scheme, "http") {
			return "80"
		}
		return ""
	}
	return effectivePort(left) == effectivePort(right)
}
