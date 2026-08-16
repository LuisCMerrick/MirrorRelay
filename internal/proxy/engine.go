// Package proxy handles frontend HTTP routing and bounded metadata handling.
package proxy

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/LuisCMerrick/MirrorRelay/internal/accesslog"
	"github.com/LuisCMerrick/MirrorRelay/internal/cachectl"
	"github.com/LuisCMerrick/MirrorRelay/internal/config"
	"github.com/LuisCMerrick/MirrorRelay/internal/limit"
	"github.com/LuisCMerrick/MirrorRelay/internal/mirror"
	"github.com/LuisCMerrick/MirrorRelay/internal/model"
	"github.com/LuisCMerrick/MirrorRelay/internal/security"
	"github.com/LuisCMerrick/MirrorRelay/internal/stats"
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

func (e *Engine) rewrite(proxyRequest *httputil.ProxyRequest) {
	proxyRequest.Out.URL.Scheme = "http"
	proxyRequest.Out.URL.Host = "mirrorrelay-upstream-nginx-internal"
	proxyRequest.Out.Host = "mirrorrelay-upstream-nginx-internal"
	stripUntrustedHeaders(proxyRequest.Out.Header)
}

func (e *Engine) errorHandler(w http.ResponseWriter, _ *http.Request, err error) {
	status := http.StatusBadGateway
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		status = http.StatusGatewayTimeout
	}
	http.Error(w, "upstream unavailable", status)
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
	if e.logger != nil {
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
}
