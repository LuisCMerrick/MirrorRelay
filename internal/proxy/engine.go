package proxy

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/LuisCMerrick/RepoGate/internal/accesslog"
	"github.com/LuisCMerrick/RepoGate/internal/auth"
	"github.com/LuisCMerrick/RepoGate/internal/cachectl"
	"github.com/LuisCMerrick/RepoGate/internal/config"
	"github.com/LuisCMerrick/RepoGate/internal/limit"
	"github.com/LuisCMerrick/RepoGate/internal/mirror"
	"github.com/LuisCMerrick/RepoGate/internal/model"
	"github.com/LuisCMerrick/RepoGate/internal/security"
	"github.com/LuisCMerrick/RepoGate/internal/stats"
)

type requestMetaKey struct{}
type selectedMetaKey struct{}
type writerContextKey struct{}

type requestMeta struct {
	repository      model.Mirror
	relativePath    string
	cacheClass      string
	cacheKey        string
	authPartition   string
	objectID        string
	publicBase      string
	requestID       string
	clientIP        string
	clientEncoding  string
	acceptHeader    string
	validatorKey    string
	dynamicTarget   *url.URL
	logicalURL      *url.URL
	auxiliary       bool
	cacheBypass     bool
	followRedirects bool
	rewriteMetadata bool
	rewriteHTML     bool
}

type selectedMeta struct {
	upstream    model.Upstream
	duration    time.Duration
	cacheStatus string
}

type cacheKeyManager interface {
	Key(context.Context, int64, string, string, string) (string, string, error)
}

type Engine struct {
	cfg         config.Config
	registry    *mirror.Registry
	cacheKeys   cacheKeyManager
	stats       *stats.Stats
	logger      *accesslog.Logger
	limiter     *limit.Limiter
	proxy       *httputil.ReverseProxy
	transport   *upstreamNginxTransport
	bufferPool  *bufferPool
	validators  *metadataValidators
	compressors gzipPool
	adminCIDRs  security.CIDRList

	tokenMu      sync.RWMutex
	tokenTargets map[int64]*url.URL
}

func New(cfg config.Config, registry *mirror.Registry, cacheKeys *cachectl.Manager, metric *stats.Stats, logger *accesslog.Logger) *Engine {
	return newEngine(cfg, registry, cacheKeys, metric, logger, net.DefaultResolver)
}

func newEngine(cfg config.Config, registry *mirror.Registry, cacheKeys cacheKeyManager, metric *stats.Stats, logger *accesslog.Logger, resolver security.Resolver) *Engine {
	adminCIDRs, _ := security.ParseCIDRs(cfg.Security.AdminCIDRs)
	transport := newUpstreamNginxTransport(cfg, resolver)
	engine := &Engine{
		cfg:          cfg,
		registry:     registry,
		cacheKeys:    cacheKeys,
		stats:        metric,
		logger:       logger,
		limiter:      limit.New(cfg.Limits.MaxTotalConcurrency, cfg.Limits.MaxIPConcurrency),
		transport:    transport,
		bufferPool:   newBufferPool(cfg.Performance.StreamBufferSize),
		validators:   newMetadataValidators(cfg.Metadata.ValidatorEntries),
		adminCIDRs:   adminCIDRs,
		tokenTargets: make(map[int64]*url.URL),
	}
	transport.engine = engine
	engine.proxy = &httputil.ReverseProxy{
		Rewrite:        engine.rewrite,
		Transport:      transport,
		ModifyResponse: engine.modifyResponse,
		ErrorHandler:   engine.errorHandler,
		BufferPool:     engine.bufferPool,
		FlushInterval:  100 * time.Millisecond,
	}
	return engine
}

func (e *Engine) CloseIdleConnections() {
	e.transport.base.CloseIdleConnections()
}

func (e *Engine) ServeHTTP(w http.ResponseWriter, request *http.Request) {
	started := time.Now()
	requestID := newRequestID()
	w.Header().Set("X-Mirror-Request-ID", requestID)
	clientIP := trustedClientIP(request)

	repository, relative, dynamic, auxiliary, routeErr := e.routeRequest(request)
	if routeErr != nil {
		http.Error(w, routeErr.Error(), routeErr.status)
		return
	}
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if repository.AccessPolicy == "admin" && !e.adminCIDRs.Allows(clientIP) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	release, ok := e.limiter.Acquire(clientIP, repository.ID, effectiveRepositoryConcurrency(repository))
	if !ok {
		w.Header().Set("Retry-After", "1")
		http.Error(w, "too many requests", http.StatusTooManyRequests)
		return
	}
	defer release()
	done := e.stats.Begin()
	defer done()

	active, activeErr := activeRepositoryUpstream(repository.Upstreams)
	if activeErr != nil {
		http.Error(w, "repository has no active upstream", http.StatusBadGateway)
		return
	}
	class := classifyObject(repository, relative, dynamic)
	publicBase, publicErr := requestPublicBase(e.cfg, repository, request)
	if publicErr != nil {
		http.Error(w, publicErr.Error(), http.StatusBadRequest)
		return
	}
	upstreamIdentity := repositoryUpstreamIdentity(active, auxiliary)
	objectPath := relative
	objectQuery := request.URL.RawQuery
	logicalURL, logicalErr := repositoryURL(repository, active, relative, request.URL.RawQuery, auxiliary)
	if logicalErr != nil {
		http.Error(w, "invalid repository route", http.StatusBadGateway)
		return
	}
	if dynamic != nil {
		upstreamIdentity = dynamic.Scheme + "://" + dynamic.Host
		objectPath = dynamic.EscapedPath()
		objectQuery = dynamic.RawQuery
		logicalURL = cloneURL(dynamic)
	}
	cacheKey, objectID, keyErr := e.cacheKeys.Key(request.Context(), repository.ID, upstreamIdentity, objectPath, objectQuery)
	if keyErr != nil {
		http.Error(w, "cache generation state unavailable", http.StatusServiceUnavailable)
		return
	}
	acceptHeader := strings.Join(request.Header.Values("Accept"), ",")
	authPartition := credentialPartitionKey(repository, request.Header)
	cacheKey = representationCacheKey(cacheKey, repository, class, acceptHeader) + authPartition
	rewriteMetadata := repository.RewriteEnabled && class == "metadata"
	rewriteHTML := repository.HTMLRewriteEnabled && class == "metadata"
	validatorKey := metadataValidatorKey(repository, upstreamIdentity, objectPath, objectQuery, publicBase, auxiliary,
		acceptsGzip(request.Header.Get("Accept-Encoding")))
	tokenRoute := isTokenRoute(request.URL.Path)
	meta := requestMeta{
		repository:      repository,
		relativePath:    relative,
		cacheClass:      class,
		cacheKey:        cacheKey,
		authPartition:   authPartition,
		objectID:        objectID,
		publicBase:      publicBase,
		requestID:       requestID,
		clientIP:        clientIP,
		clientEncoding:  request.Header.Get("Accept-Encoding"),
		acceptHeader:    acceptHeader,
		validatorKey:    validatorKey,
		dynamicTarget:   dynamic,
		logicalURL:      logicalURL,
		auxiliary:       auxiliary,
		cacheBypass:     tokenRoute,
		followRedirects: shouldFollowRedirects(repository, class, tokenRoute),
		rewriteMetadata: rewriteMetadata,
		rewriteHTML:     rewriteHTML,
	}

	capture := &captureWriter{ResponseWriter: w, status: http.StatusOK, requestID: requestID}
	if rewriteMetadata || rewriteHTML {
		if validator, hit := e.validators.get(validatorKey, time.Now()); hit && conditionMatches(request, validator) {
			capture.Header().Set("ETag", validator.ETag)
			if validator.LastModified != "" {
				capture.Header().Set("Last-Modified", validator.LastModified)
			}
			addVary(capture.Header(), "Accept-Encoding")
			capture.WriteHeader(http.StatusNotModified)
			e.finishRequest(started, clientIP, repository, request, capture, selectedMeta{upstream: active, cacheStatus: "VALIDATOR"})
			return
		}
	}
	ctx := context.WithValue(request.Context(), requestMetaKey{}, meta)
	ctx = context.WithValue(ctx, writerContextKey{}, capture)
	e.proxy.ServeHTTP(capture, request.WithContext(ctx))
	selected, _ := capture.selected.(selectedMeta)
	if selected.upstream.URL == "" {
		selected.upstream = active
	}
	e.finishRequest(started, clientIP, repository, request, capture, selected)
}

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
		repositoryID, relative, err := parseAuxiliaryUpstreamRoute(request.URL.Path)
		if err != nil {
			return model.Mirror{}, "", nil, false, &routeError{status: http.StatusBadRequest, text: err.Error()}
		}
		repository, found := e.registry.GetByID(repositoryID)
		if !found || !repository.Enabled || !repository.HTMLRewriteEnabled || !e.auxiliaryRouteAllowed(repository, request.Host) {
			return model.Mirror{}, "", nil, false, &routeError{status: http.StatusNotFound, text: "upstream auxiliary resource route not found"}
		}
		if unsafeRepositoryPath(relative) {
			return model.Mirror{}, "", nil, false, &routeError{status: http.StatusBadRequest, text: "upstream auxiliary resource path contains an unsafe segment"}
		}
		return repository, relative, nil, true, nil
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

func (e *Engine) rewrite(proxyRequest *httputil.ProxyRequest) {
	proxyRequest.Out.URL.Scheme = "http"
	proxyRequest.Out.URL.Host = "repogate-upstream-nginx-internal"
	proxyRequest.Out.Host = "repogate-upstream-nginx-internal"
	stripUntrustedHeaders(proxyRequest.Out.Header)
}

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
		if (meta.rewriteHTML && shouldRewriteHTMLBody(response)) || (meta.rewriteMetadata && shouldRewriteBody(response)) {
			sanitizeRewrittenMetadataHead(response)
		}
		return nil
	}
	if meta.rewriteHTML && shouldRewriteHTMLBody(response) {
		metadataConfig := e.cfg.Metadata
		if meta.repository.MetadataLimitBytes > 0 {
			metadataConfig.RewriteBufferLimit = meta.repository.MetadataLimitBytes
		}
		validator, changed, err := rewriteHTMLResponseBody(response, meta.repository, selected.upstream, meta.logicalURL,
			metadataConfig, acceptsGzip(meta.clientEncoding), &e.compressors)
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

func (e *Engine) errorHandler(w http.ResponseWriter, _ *http.Request, err error) {
	status := http.StatusBadGateway
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		status = http.StatusGatewayTimeout
	}
	var tooLarge *rewriteTooLargeError
	if errors.As(err, &tooLarge) {
		status = http.StatusBadGateway
	}
	slog.Warn("proxy request failed", "status", status, "error", err)
	http.Error(w, http.StatusText(status), status)
}

func (e *Engine) finishRequest(start time.Time, clientIP string, repository model.Mirror, request *http.Request, writer *captureWriter, selected selectedMeta) {
	cacheStatus := selected.cacheStatus
	if cacheStatus == "" {
		cacheStatus = "BYPASS"
	}
	upstreamBytes := uint64(writer.bytes)
	cacheBytes := uint64(0)
	if cacheStatus == "HIT" || cacheStatus == "VALIDATOR" {
		upstreamBytes, cacheBytes = 0, uint64(writer.bytes)
	}
	e.stats.Record(repository.ID, writer.status, uint64(writer.bytes), upstreamBytes, cacheBytes, cacheStatus,
		writer.status >= 500, selected.duration)
	e.logger.Write(accesslog.Entry{
		Time:               start,
		RequestID:          writer.requestID,
		ClientIP:           clientIP,
		Mirror:             repository.Slug,
		Method:             request.Method,
		URI:                sanitizeLogURI(request.URL.RequestURI()),
		Status:             writer.status,
		Bytes:              writer.bytes,
		DurationMS:         time.Since(start).Milliseconds(),
		Upstream:           selected.upstream.URL,
		UpstreamDurationMS: selected.duration.Milliseconds(),
		CacheStatus:        cacheStatus,
	})
}

func sanitizeLogURI(rawURI string) string {
	idx := strings.IndexByte(rawURI, '?')
	if idx == -1 {
		return rawURI
	}
	pathPart := rawURI[:idx]
	queryPart := rawURI[idx+1:]
	values, err := url.ParseQuery(queryPart)
	if err != nil {
		return pathPart + "?[REDACTED]"
	}
	for key := range values {
		lower := strings.ToLower(key)
		if strings.Contains(lower, "token") || strings.Contains(lower, "secret") ||
			strings.Contains(lower, "key") || strings.Contains(lower, "sig") ||
			strings.Contains(lower, "auth") || strings.Contains(lower, "pass") ||
			strings.HasPrefix(lower, "x-amz-") {
			values[key] = []string{"[REDACTED]"}
		}
	}
	return pathPart + "?" + values.Encode()
}

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

func (t *upstreamNginxTransport) roundTrip(original *http.Request, meta requestMeta, upstreamID int64) (*http.Response, error) {
	out := original.Clone(context.WithValue(original.Context(), requestMetaKey{}, meta))
	out.URL = &url.URL{Scheme: "http", Host: "repogate-upstream-nginx-internal"}
	out.Host = "repogate-upstream-nginx-internal"
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

func shouldFollowRedirects(repository model.Mirror, class string, tokenRoute bool) bool {
	if tokenRoute {
		return true
	}
	if repository.ProxyMode == "registry" && class == "blob" {
		return repository.BlobRedirectMode == "full_proxy"
	}
	return repository.RedirectMode == "follow" || repository.RedirectMode == "full_proxy"
}

func shouldRewriteRedirect(repository model.Mirror, class string) bool {
	if repository.ProxyMode == "registry" && class == "blob" {
		return false
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
		if aHost == targetHost {
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

func parseTokenRoute(value string) (int64, bool) {
	parts := strings.Split(strings.Trim(value, "/"), "/")
	if len(parts) != 3 || parts[0] != "_mirror_auth" || parts[2] != "token" {
		return 0, false
	}
	id, err := strconv.ParseInt(parts[1], 10, 64)
	return id, err == nil && id > 0
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

func newRequestID() string {
	var value [16]byte
	if _, err := rand.Read(value[:]); err == nil {
		return hex.EncodeToString(value[:])
	}
	return strconv.FormatInt(time.Now().UnixNano(), 16)
}

type bufferPool struct {
	pool sync.Pool
	size int
}

func newBufferPool(size int) *bufferPool {
	if size != 32<<10 && size != 64<<10 && size != 128<<10 {
		size = 64 << 10
	}
	return &bufferPool{size: size}
}

func (p *bufferPool) Get() []byte {
	if value := p.pool.Get(); value != nil {
		return *(value.(*[]byte))
	}
	buffer := make([]byte, p.size)
	return buffer
}

func (p *bufferPool) Put(value []byte) {
	if cap(value) < p.size {
		return
	}
	value = value[:p.size]
	p.pool.Put(&value)
}

type captureWriter struct {
	http.ResponseWriter
	status      int
	bytes       int64
	wroteHeader bool
	selected    any
	requestID   string
}

func (w *captureWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *captureWriter) WriteHeader(status int) {
	if w.wroteHeader {
		return
	}
	w.wroteHeader = true
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *captureWriter) Write(value []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	written, err := w.ResponseWriter.Write(value)
	w.bytes += int64(written)
	return written, err
}

func (w *captureWriter) Flush() {
	_ = http.NewResponseController(w.ResponseWriter).Flush()
}

func responseWriterFromContext(ctx context.Context) (*captureWriter, bool) {
	value, ok := ctx.Value(writerContextKey{}).(*captureWriter)
	return value, ok
}

var _ httputil.BufferPool = (*bufferPool)(nil)
var _ io.Writer = (*captureWriter)(nil)
