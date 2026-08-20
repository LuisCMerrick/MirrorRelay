// Package health provides background health checking for upstream mirrors.
package health

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/LuisCMerrick/MirrorRelay/internal/config"
	"github.com/LuisCMerrick/MirrorRelay/internal/mirror"
	"github.com/LuisCMerrick/MirrorRelay/internal/model"
)

type Store interface {
	UpdateUpstreamHealth(context.Context, int64, string, int64, string, time.Time) error
}

type Checker struct {
	cfg      config.Config
	store    Store
	registry *mirror.Registry
	client   *http.Client

	lastMu     sync.Mutex
	last       map[int64]time.Time
	inFlightMu sync.Mutex
	inFlight   map[int64]bool
	tasks      chan model.Mirror
}

type Result struct {
	UpstreamID int64  `json:"upstream_id"`
	URL        string `json:"url"`
	Healthy    bool   `json:"healthy"`
	Status     int    `json:"status"`
	LatencyMS  int64  `json:"latency_ms"`
	Error      string `json:"error,omitempty"`
}

func New(cfg config.Config, store Store, registry *mirror.Registry) *Checker {
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
	}
	return &Checker{
		cfg:      cfg,
		store:    store,
		registry: registry,
		last:     make(map[int64]time.Time),
		inFlight: make(map[int64]bool),
		tasks:    make(chan model.Mirror, 64),
		client:   &http.Client{Transport: transport},
	}
}

func (c *Checker) Start(ctx context.Context) {
	// Start bounded worker pool (4 workers)
	for i := 0; i < 4; i++ {
		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case repository, ok := <-c.tasks:
					if !ok {
						return
					}
					_, _ = c.CheckMirror(ctx, repository)
					c.inFlightMu.Lock()
					delete(c.inFlight, repository.ID)
					c.inFlightMu.Unlock()
				}
			}
		}()
	}

	go func() {
		ticker := time.NewTicker(c.cfg.Health.WorkerInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				c.runDue(ctx)
			case <-ctx.Done():
				return
			}
		}
	}()
}

func (c *Checker) runDue(ctx context.Context) {
	now := time.Now()
	for _, repository := range c.registry.List() {
		if !repository.Enabled || !repository.HealthCheckEnabled {
			continue
		}
		c.lastMu.Lock()
		due := now.Sub(c.last[repository.ID]) >= time.Duration(repository.HealthIntervalSec)*time.Second
		if due {
			c.last[repository.ID] = now
		}
		c.lastMu.Unlock()
		if !due {
			continue
		}

		c.inFlightMu.Lock()
		running := c.inFlight[repository.ID]
		if !running {
			c.inFlight[repository.ID] = true
		}
		c.inFlightMu.Unlock()

		if running {
			continue
		}

		select {
		case c.tasks <- repository:
		default:
			c.inFlightMu.Lock()
			delete(c.inFlight, repository.ID)
			c.inFlightMu.Unlock()
		}
	}
}

func (c *Checker) CheckMirror(ctx context.Context, repository model.Mirror) ([]Result, error) {
	results := make([]Result, 0, len(repository.Upstreams))
	var firstError error
	for _, upstream := range repository.Upstreams {
		if !upstream.Enabled {
			continue
		}
		result := c.check(ctx, repository, upstream)
		results = append(results, result)
		status := "unhealthy"
		if result.Healthy {
			status = "healthy"
		}
		checkedAt := time.Now()
		if err := c.store.UpdateUpstreamHealth(context.Background(), upstream.ID, status, result.LatencyMS, result.Error, checkedAt); err != nil {
			if firstError == nil {
				firstError = err
			}
			continue
		}
		c.registry.UpdateUpstreamHealth(upstream.ID, status, result.LatencyMS, result.Error, checkedAt)
	}
	return results, firstError
}

func (c *Checker) check(parent context.Context, repository model.Mirror, upstream model.Upstream) Result {
	result := Result{UpstreamID: upstream.ID, URL: upstream.URL}
	timeout := time.Duration(repository.HealthTimeoutSec) * time.Second
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	healthPath := strings.TrimLeft(repository.HealthCheckPath, "/")
	internalPath := "/_health/" + strconv.FormatInt(repository.ID, 10) + "/" + strconv.FormatInt(upstream.ID, 10) + "/" + healthPath
	requestURL := &url.URL{Scheme: "http", Host: "mirrorrelay-upstream-nginx-internal", Path: internalPath}
	method := repository.HealthMethod
	if method == "" {
		method = http.MethodHead
	}
	request, err := http.NewRequestWithContext(ctx, method, requestURL.String(), nil)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	request.Header.Set("Accept-Encoding", "identity")
	request.Header.Set("X-Mirror-Internal-Repository-ID", strconv.FormatInt(repository.ID, 10))
	request.Header.Set("X-Mirror-Internal-Cache-Bypass", "1")
	request.Header.Set("X-Mirror-Internal-Request-ID", "health-"+strconv.FormatInt(time.Now().UnixNano(), 16))
	started := time.Now()
	response, err := c.client.Do(request)
	result.LatencyMS = time.Since(started).Milliseconds()
	if err != nil {
		result.Error = err.Error()
		return result
	}
	defer response.Body.Close()
	if method == http.MethodGet {
		_, _ = io.CopyN(io.Discard, response.Body, 4096)
	}
	result.Status = response.StatusCode
	expected := repository.HealthExpected
	if expected == 0 {
		expected = http.StatusOK
	}
	isRegistryV2 := repository.Type == "docker-registry" || repository.Type == "oci-registry" || strings.HasSuffix(strings.TrimRight(repository.HealthCheckPath, "/"), "v2")
	if isRegistryV2 && (response.StatusCode == http.StatusOK || response.StatusCode == http.StatusUnauthorized) {
		result.Healthy = true
	} else if response.StatusCode == expected {
		result.Healthy = true
	} else {
		result.Healthy = false
		result.Error = fmt.Sprintf("expected %d, got %d", expected, response.StatusCode)
	}
	return result
}
