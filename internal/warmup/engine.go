// Package warmup provides smart cache pre-fetching and warm-up capabilities.
package warmup

import (
	"bufio"
	"compress/gzip"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"fmt"
	"html"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/LuisCMerrick/MirrorRelay/internal/config"
	"github.com/LuisCMerrick/MirrorRelay/internal/model"
)

type Store interface {
	ListWarmupJobs(context.Context) ([]model.WarmupJob, error)
	GetWarmupJob(context.Context, int64) (model.WarmupJob, error)
	Mirror(context.Context, int64) (model.Mirror, error)
	UpdateWarmupJobProgress(ctx context.Context, id int64, status string, total, completed, failed int, downloadedBytes int64, errMsg, lastRun, nextRun string) error
	UpdateWarmupJobSchedule(context.Context, int64, string) error
}

type AuditRecorder interface {
	Record(user, action, object, detail string, ok bool)
}

type EngineStatus struct {
	Enabled        bool      `json:"enabled"`
	RunningJobs    int       `json:"running_jobs"`
	MaxConcurrency int       `json:"max_concurrency"`
	TotalWarmups   int64     `json:"total_warmups"`
	BytesWarmed    int64     `json:"bytes_warmed"`
	LastWarmupAt   time.Time `json:"last_warmup_at,omitempty"`
}

const maxExpandedWarmupTargets = 10_000

var pypiHrefPattern = regexp.MustCompile(`(?i)href\s*=\s*["']([^"'<>]+)["']`)

type Engine struct {
	cfg           config.Config
	store         Store
	audit         AuditRecorder
	httpClient    *http.Client
	mu            sync.RWMutex
	activeJobs    map[int64]context.CancelFunc
	totalWarmups  int64
	bytesWarmed   int64
	lastWarmupAt  time.Time
	cancelAll     context.CancelFunc
	runCtx        context.Context
	started       bool
	stopping      bool
	downloadSlots chan struct{}
	bandwidth     *bandwidthLimiter
	wg            sync.WaitGroup
}

func NewEngine(cfg config.Config, store Store, audit AuditRecorder) *Engine {
	maxIdleConns := cfg.Transport.MaxIdleConns
	if maxIdleConns <= 0 {
		maxIdleConns = 100
	}
	maxIdleConnsPerHost := cfg.Transport.MaxIdleConnsPerHost
	if maxIdleConnsPerHost <= 0 {
		maxIdleConnsPerHost = 20
	}
	idleConnTimeout := cfg.Transport.IdleConnTimeout
	if idleConnTimeout <= 0 {
		idleConnTimeout = 90 * time.Second
	}
	dialTimeout := cfg.Transport.DialTimeout
	if dialTimeout <= 0 {
		dialTimeout = 30 * time.Second
	}
	keepAlive := cfg.Transport.KeepAlive
	if keepAlive <= 0 {
		keepAlive = 30 * time.Second
	}
	network, address := warmupFrontendEndpoint(cfg)
	dialer := &net.Dialer{Timeout: dialTimeout, KeepAlive: keepAlive}
	transport := &http.Transport{
		MaxIdleConns:          maxIdleConns,
		MaxIdleConnsPerHost:   maxIdleConnsPerHost,
		IdleConnTimeout:       idleConnTimeout,
		TLSHandshakeTimeout:   cfg.Transport.TLSHandshakeTimeout,
		ResponseHeaderTimeout: cfg.Transport.ResponseHeaderTimeout,
		TLSClientConfig:       &tls.Config{InsecureSkipVerify: false},
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			// Warm-up URLs are logical frontend routes. Never dial their host:
			// every connection must re-enter MirrorRelay so that the selected
			// origin is reached only through Managed Upstream Nginx.
			return dialer.DialContext(ctx, network, address)
		},
	}

	concurrency := cfg.Warmup.MaxConcurrency
	if concurrency <= 0 {
		concurrency = 4
	}
	client := &http.Client{
		Transport: transport,
		CheckRedirect: func(request *http.Request, via []*http.Request) error {
			maxHops := cfg.Redirect.MaxHops
			if maxHops <= 0 {
				maxHops = 10
			}
			if len(via) > maxHops {
				return errors.New("warm-up redirect limit exceeded")
			}
			if request.URL.User != nil || (request.URL.Scheme != "http" && request.URL.Scheme != "https") {
				return errors.New("warm-up redirect target must be an HTTP(S) URL without user information")
			}
			// The transport always connects to the clear-text local frontend.
			// Normalize rewritten public HTTPS locations back to that internal
			// route while retaining their logical Host and request URI.
			request.URL.Scheme = "http"
			return nil
		},
	}

	return &Engine{
		cfg:           cfg,
		store:         store,
		audit:         audit,
		httpClient:    client,
		activeJobs:    make(map[int64]context.CancelFunc),
		downloadSlots: make(chan struct{}, concurrency),
		bandwidth:     newBandwidthLimiter(cfg.Warmup.BandwidthLimit),
	}
}

func (e *Engine) Start(ctx context.Context) {
	e.mu.Lock()
	if e.started {
		e.mu.Unlock()
		return
	}
	e.runCtx, e.cancelAll = context.WithCancel(ctx)
	e.started = true
	e.stopping = false
	e.wg.Add(1)
	runCtx := e.runCtx
	e.mu.Unlock()
	go e.cronLoop(runCtx)
}

func (e *Engine) Stop() {
	e.mu.Lock()
	if !e.started {
		e.mu.Unlock()
		return
	}
	e.stopping = true
	e.cancelAll()
	for _, cancel := range e.activeJobs {
		cancel()
	}
	e.mu.Unlock()

	e.wg.Wait()
	e.httpClient.CloseIdleConnections()
	e.mu.Lock()
	e.started = false
	e.stopping = false
	e.runCtx = nil
	e.cancelAll = nil
	e.mu.Unlock()
}

func (e *Engine) Status() EngineStatus {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return EngineStatus{
		Enabled:        e.cfg.Warmup.Enabled,
		RunningJobs:    len(e.activeJobs),
		MaxConcurrency: e.cfg.Warmup.MaxConcurrency,
		TotalWarmups:   atomic.LoadInt64(&e.totalWarmups),
		BytesWarmed:    atomic.LoadInt64(&e.bytesWarmed),
		LastWarmupAt:   e.lastWarmupAt,
	}
}

func (e *Engine) UpdateConfig(cfg config.Config) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.cfg = cfg
}

func (e *Engine) RunJob(ctx context.Context, jobID int64) error {
	if e.store == nil {
		return errors.New("warmup store is not configured")
	}
	if _, err := e.store.GetWarmupJob(ctx, jobID); err != nil {
		return fmt.Errorf("load warmup job: %w", err)
	}
	e.mu.Lock()
	if !e.started || e.stopping {
		e.mu.Unlock()
		return errors.New("warmup engine is not running")
	}
	if _, exists := e.activeJobs[jobID]; exists {
		e.mu.Unlock()
		return errors.New("warmup job is already running")
	}

	timeout := e.cfg.Warmup.Timeout
	if timeout <= 0 {
		timeout = 15 * time.Minute
	}
	jobCtx, cancel := context.WithTimeout(e.runCtx, timeout)
	e.activeJobs[jobID] = cancel
	e.wg.Add(1)
	e.mu.Unlock()

	go func() {
		defer func() {
			cancel()
			e.mu.Lock()
			delete(e.activeJobs, jobID)
			e.mu.Unlock()
			e.wg.Done()
		}()
		e.executeJob(jobCtx, jobID)
	}()

	return nil
}

func (e *Engine) CancelJob(jobID int64) error {
	e.mu.RLock()
	cancel, exists := e.activeJobs[jobID]
	if !exists {
		e.mu.RUnlock()
		return errors.New("warmup job is not currently running")
	}
	e.mu.RUnlock()

	cancel()
	if e.audit != nil {
		e.audit.Record("system", "cancel", fmt.Sprintf("warmup_job:%d", jobID), "Warmup job cancelled", true)
	}
	return nil
}

func (e *Engine) cronLoop(ctx context.Context) {
	defer e.wg.Done()
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			e.checkAndRunScheduledJobs(ctx)
		}
	}
}

func (e *Engine) checkAndRunScheduledJobs(ctx context.Context) {
	e.mu.RLock()
	enabled := e.cfg.Warmup.Enabled
	e.mu.RUnlock()

	if !enabled || e.store == nil {
		return
	}

	jobs, err := e.store.ListWarmupJobs(ctx)
	if err != nil {
		return
	}

	now := time.Now().UTC()
	for _, job := range jobs {
		if !job.Enabled || job.CronExpression == "" {
			continue
		}

		nextRun, err := scheduledRunTime(job, now)
		if err != nil {
			slog.Warn("invalid persisted warmup schedule", "job_id", job.ID, "schedule", job.CronExpression, "error", err)
			continue
		}
		if job.NextRunAt == "" && !nextRun.IsZero() {
			_ = e.store.UpdateWarmupJobSchedule(ctx, job.ID, nextRun.Format(time.RFC3339))
		}
		if !nextRun.IsZero() && !now.Before(nextRun) {
			_ = e.RunJob(ctx, job.ID)
		}
	}
}

func scheduledRunTime(job model.WarmupJob, now time.Time) (time.Time, error) {
	if job.NextRunAt != "" {
		return time.Parse(time.RFC3339, job.NextRunAt)
	}
	reference := job.CreatedAt
	if job.LastRunAt != "" {
		if parsed, err := time.Parse(time.RFC3339, job.LastRunAt); err == nil {
			reference = parsed
		}
	}
	if reference.IsZero() {
		reference = now
	}
	return NextRunAt(job.CronExpression, reference)
}

func (e *Engine) executeJob(ctx context.Context, jobID int64) {
	job, err := e.store.GetWarmupJob(ctx, jobID)
	if err != nil {
		if ctx.Err() != nil {
			return
		}
		slog.Error("Warmup job lookup failed", "job_id", jobID, "err", err)
		return
	}

	mirror, err := e.store.Mirror(ctx, job.MirrorID)
	if err != nil {
		now := time.Now().UTC()
		_ = e.store.UpdateWarmupJobProgress(ctx, jobID, "failed", 0, 0, 0, 0, "Mirror not found: "+err.Error(), now.Format(time.RFC3339), nextRunString(job.CronExpression, now))
		return
	}

	nowStr := time.Now().UTC().Format(time.RFC3339)
	_ = e.store.UpdateWarmupJobProgress(ctx, jobID, "running", len(job.URLPatterns), 0, 0, 0, "", nowStr, "")

	targetURLs, err := e.expandTargetURLs(ctx, mirror, job.URLPatterns)
	if err != nil {
		e.updateProgress(jobID, "failed", 0, 0, 0, 0, err.Error(), nowStr, nextRunString(job.CronExpression, time.Now().UTC()))
		return
	}
	total := len(targetURLs)
	if ctx.Err() != nil {
		e.finishCancelledJob(job, total, 0, 0, 0, nowStr, ctx.Err())
		return
	}
	if total == 0 {
		e.updateProgress(jobID, "completed", 0, 0, 0, 0, "No target URLs to warm up", nowStr, nextRunString(job.CronExpression, time.Now().UTC()))
		return
	}

	var completedCount, failedCount int64
	var totalDownloadedBytes int64
	var lastErrStr string
	var errMu sync.Mutex

	var jobWg sync.WaitGroup
	cancelled := false

launchLoop:
	for _, targetURL := range targetURLs {
		if !e.acquireDownloadSlot(ctx) {
			cancelled = true
			break launchLoop
		}

		jobWg.Add(1)
		go func(rawURL string) {
			defer func() {
				e.releaseDownloadSlot()
				jobWg.Done()
			}()

			bytesFetched, err := e.fetchAndWarm(ctx, rawURL)
			if err != nil {
				atomic.AddInt64(&failedCount, 1)
				errMu.Lock()
				lastErrStr = err.Error()
				errMu.Unlock()
			} else {
				atomic.AddInt64(&completedCount, 1)
				atomic.AddInt64(&totalDownloadedBytes, bytesFetched)
				atomic.AddInt64(&e.bytesWarmed, bytesFetched)
			}
		}(targetURL)
	}

	jobWg.Wait()
	completed := atomic.LoadInt64(&completedCount)
	failed := atomic.LoadInt64(&failedCount)
	downloaded := atomic.LoadInt64(&totalDownloadedBytes)
	if cancelled || ctx.Err() != nil {
		e.finishCancelledJob(job, total, int(completed), int(failed), downloaded, nowStr, ctx.Err())
		return
	}

	atomic.AddInt64(&e.totalWarmups, 1)
	e.mu.Lock()
	e.lastWarmupAt = time.Now().UTC()
	e.mu.Unlock()

	finalStatus := "completed"
	if failed > 0 && completed == 0 {
		finalStatus = "failed"
	}

	e.updateProgress(jobID, finalStatus, total, int(completed), int(failed), downloaded, lastErrStr, nowStr, nextRunString(job.CronExpression, time.Now().UTC()))
	if e.audit != nil {
		e.audit.Record("system", "warmup", fmt.Sprintf("warmup_job:%d", jobID), fmt.Sprintf("Warmed %d/%d items (%d bytes)", completed, total, downloaded), finalStatus == "completed")
	}
}

func (e *Engine) finishCancelledJob(job model.WarmupJob, total, completed, failed int, downloaded int64, started string, cause error) {
	status := "cancelled"
	message := "Job cancelled"
	nextRun := ""
	if errors.Is(cause, context.DeadlineExceeded) {
		status = "failed"
		message = "Job timed out"
		nextRun = nextRunString(job.CronExpression, time.Now().UTC())
	}
	e.updateProgress(job.ID, status, total, completed, failed, downloaded, message, started, nextRun)
}

func (e *Engine) updateProgress(jobID int64, status string, total, completed, failed int, downloaded int64, message, lastRun, nextRun string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := e.store.UpdateWarmupJobProgress(ctx, jobID, status, total, completed, failed, downloaded, message, lastRun, nextRun); err != nil {
		slog.Warn("Warmup job progress update failed", "job_id", jobID, "status", status, "err", err)
	}
}

func (e *Engine) acquireDownloadSlot(ctx context.Context) bool {
	select {
	case e.downloadSlots <- struct{}{}:
		return true
	case <-ctx.Done():
		return false
	}
}

func (e *Engine) releaseDownloadSlot() {
	<-e.downloadSlots
}

func (e *Engine) expandTargetURLs(ctx context.Context, mirror model.Mirror, patterns []string) ([]string, error) {
	patterns, err := NormalizeURLPatterns(patterns)
	if err != nil {
		return nil, err
	}
	urls := make([]string, 0, len(patterns))
	for _, p := range patterns {
		if target, ok := e.repositoryWarmupURL(mirror, p); ok {
			urls = append(urls, target)
		}
	}

	// If metadata depth > 0, inspect metadata files to extract package URLs
	e.mu.RLock()
	metadataDepth := e.cfg.Warmup.MetadataDepth
	e.mu.RUnlock()
	if metadataDepth > 0 && len(urls) > 0 {
		seen := make(map[string]bool, len(urls))
		for _, target := range urls {
			seen[target] = true
		}
		frontier := append([]string(nil), urls...)
		for depth := 0; depth < metadataDepth && len(frontier) > 0 && ctx.Err() == nil; depth++ {
			var next []string
			for _, u := range frontier {
				if ctx.Err() != nil {
					break
				}
				if !isMetadataFile(u, mirror.Type) {
					continue
				}
				for _, target := range e.extractPackagesFromMetadata(ctx, u, mirror) {
					if seen[target] {
						continue
					}
					if len(urls) >= maxExpandedWarmupTargets {
						return nil, fmt.Errorf("metadata expansion exceeds %d target URLs", maxExpandedWarmupTargets)
					}
					seen[target] = true
					urls = append(urls, target)
					next = append(next, target)
				}
			}
			frontier = next
		}
	}

	return urls, nil
}

func isMetadataFile(u, repoType string) bool {
	lower := strings.ToLower(u)
	switch repoType {
	case "apt":
		return strings.HasSuffix(lower, "packages.gz") || strings.HasSuffix(lower, "packages.xz") || strings.HasSuffix(lower, "inrelease") || strings.HasSuffix(lower, "release")
	case "rpm":
		return strings.Contains(lower, "repodata/") || strings.HasSuffix(lower, "primary.xml.gz")
	case "pypi":
		return strings.HasSuffix(lower, "/simple/") || strings.HasSuffix(lower, "/simple")
	case "apk":
		return strings.HasSuffix(lower, "apkindex.tar.gz")
	default:
		return false
	}
}

func (e *Engine) extractPackagesFromMetadata(ctx context.Context, metadataURL string, mirror model.Mirror) []string {
	if !e.acquireDownloadSlot(ctx) {
		return nil
	}
	defer e.releaseDownloadSlot()

	req, err := http.NewRequestWithContext(ctx, "GET", metadataURL, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("User-Agent", "MirrorRelay-Warmup/1.0")
	req.Header.Set("X-MirrorRelay-Warmup", "1")

	resp, err := e.httpClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		return nil
	}
	defer resp.Body.Close()

	var reader io.Reader = e.throttledReader(ctx, resp.Body)
	if strings.HasSuffix(strings.ToLower(metadataURL), ".gz") {
		gz, err := gzip.NewReader(reader)
		if err != nil {
			return nil
		}
		defer gz.Close()
		reader = gz
	}

	var results []string
	reader = io.LimitReader(reader, 10*1024*1024) // 10MB max metadata limit

	switch mirror.Type {
	case "apt":
		scanner := bufio.NewScanner(reader)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "Filename: ") {
				fn := strings.TrimSpace(strings.TrimPrefix(line, "Filename: "))
				if u, ok := e.repositoryWarmupURL(mirror, fn); ok {
					results = append(results, u)
					if len(results) >= 50 {
						break
					}
				}
			}
		}
	case "rpm":
		decoder := xml.NewDecoder(reader)
		for len(results) < 50 {
			token, err := decoder.Token()
			if err != nil {
				break
			}
			start, ok := token.(xml.StartElement)
			if !ok || start.Name.Local != "location" {
				continue
			}
			for _, attribute := range start.Attr {
				if attribute.Name.Local != "href" {
					continue
				}
				ref, err := url.Parse(strings.TrimSpace(attribute.Value))
				if err != nil {
					break
				}
				var target string
				var ok bool
				if ref.IsAbs() {
					target, ok = e.metadataWarmupURL(mirror, metadataURL, attribute.Value)
				} else {
					target, ok = e.repositoryWarmupURL(mirror, attribute.Value)
				}
				if ok {
					results = append(results, target)
				}
				break
			}
		}
	case "pypi":
		scanner := bufio.NewScanner(reader)
		for scanner.Scan() {
			matches := pypiHrefPattern.FindAllStringSubmatch(scanner.Text(), -1)
			for _, m := range matches {
				if len(m) > 1 {
					reference := html.UnescapeString(m[1])
					parsed, err := url.Parse(reference)
					if err != nil {
						continue
					}
					lowerPath := strings.ToLower(parsed.Path)
					if !strings.HasSuffix(lowerPath, ".whl") && !strings.HasSuffix(lowerPath, ".tar.gz") {
						continue
					}
					if target, ok := e.metadataWarmupURL(mirror, metadataURL, reference); ok {
						results = append(results, target)
						if len(results) >= 50 {
							break
						}
					}
				}
			}
			if len(results) >= 50 {
				break
			}
		}
	}

	return results
}

func (e *Engine) repositoryWarmupURL(mirror model.Mirror, repositoryPath string) (string, bool) {
	repositoryPath, err := normalizeURLPattern(repositoryPath)
	if err != nil {
		return "", false
	}
	basePath := ""
	if mirror.PublicMode != "host" {
		basePath = mirror.PublicPath
		if basePath == "" && mirror.Slug != "" {
			basePath = "/" + mirror.Slug + "/"
		}
	}
	return e.frontendBaseURL(mirror) + strings.TrimRight(basePath, "/") + "/" + strings.TrimLeft(repositoryPath, "/"), true
}

func (e *Engine) metadataWarmupURL(mirror model.Mirror, metadataURL, reference string) (string, bool) {
	base, err := url.Parse(metadataURL)
	if err != nil {
		return "", false
	}
	ref, err := url.Parse(strings.TrimSpace(reference))
	if err != nil || ref.User != nil || (ref.IsAbs() && ref.Scheme != "http" && ref.Scheme != "https") {
		return "", false
	}
	target := base.ResolveReference(ref)
	target.Fragment = ""
	if !ref.IsAbs() {
		if !e.isWarmupRepositoryRoute(mirror, target.Path) {
			return "", false
		}
		target.Scheme = "http"
		return target.String(), true
	}
	if e.isPublicWarmupURL(mirror, target) {
		target.Scheme = "http"
		return target.String(), true
	}

	// Absolute metadata links are represented as an adapter route. The normal
	// frontend policy then validates the target and sends it through Managed
	// Upstream Nginx; the warm-up client itself never connects to that host.
	encoded := base64.RawURLEncoding.EncodeToString([]byte(target.String()))
	return e.repositoryWarmupURL(mirror, "/__fetch/"+encoded)
}

func (e *Engine) isPublicWarmupURL(mirror model.Mirror, target *url.URL) bool {
	base, err := url.Parse(e.frontendBaseURL(mirror))
	if err == nil && strings.EqualFold(base.Host, target.Host) && e.isWarmupRepositoryRoute(mirror, target.Path) {
		return true
	}
	e.mu.RLock()
	publicBaseURL := e.cfg.HTTP.PublicBaseURL
	e.mu.RUnlock()
	if publicBaseURL != "" {
		publicBase, err := url.Parse(publicBaseURL)
		if err == nil && strings.EqualFold(publicBase.Host, target.Host) && e.isWarmupRepositoryRoute(mirror, target.Path) {
			return true
		}
	}
	return mirror.PublicMode == "host" && strings.EqualFold(mirror.PublicHost, target.Host) && e.isWarmupRepositoryRoute(mirror, target.Path)
}

func (e *Engine) isWarmupRepositoryRoute(mirror model.Mirror, requestPath string) bool {
	if mirror.PublicMode == "host" {
		return strings.HasPrefix(requestPath, "/")
	}
	prefix := mirror.PublicPath
	if prefix == "" && mirror.Slug != "" {
		prefix = "/" + mirror.Slug + "/"
	}
	root := strings.TrimSuffix(prefix, "/")
	return root != "" && (requestPath == root || strings.HasPrefix(requestPath, prefix))
}

func (e *Engine) frontendBaseURL(mirror model.Mirror) string {
	if mirror.PublicMode == "host" && mirror.PublicHost != "" {
		return "http://" + mirror.PublicHost
	}
	e.mu.RLock()
	cfg := e.cfg
	e.mu.RUnlock()
	network, address := warmupFrontendEndpoint(cfg)
	if network == "tcp" {
		return "http://" + address
	}
	return "http://mirrorrelay-warmup.internal"
}

func warmupFrontendEndpoint(cfg config.Config) (network, address string) {
	network, address = cfg.FrontendEndpoint()
	if network != "tcp" {
		return network, address
	}
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return network, address
	}
	if host == "" {
		host = "127.0.0.1"
	}
	return network, net.JoinHostPort(host, port)
}

func nextRunString(expression string, after time.Time) string {
	next, err := NextRunAt(expression, after)
	if err != nil || next.IsZero() {
		return ""
	}
	return next.UTC().Format(time.RFC3339)
}

func (e *Engine) fetchAndWarm(ctx context.Context, targetURL string) (int64, error) {
	e.mu.RLock()
	retryCount := e.cfg.Warmup.RetryCount
	e.mu.RUnlock()
	if retryCount < 0 {
		retryCount = 0
	}

	var lastErr error
	for attempt := 0; attempt <= retryCount; attempt++ {
		bytesFetched, retryable, err := e.fetchOnce(ctx, targetURL)
		if err == nil {
			return bytesFetched, nil
		}
		lastErr = err
		if !retryable || attempt == retryCount || ctx.Err() != nil {
			break
		}
		delay := 100 * time.Millisecond
		for backoff := 0; backoff < attempt && delay < 2*time.Second; backoff++ {
			delay *= 2
		}
		if delay > 2*time.Second {
			delay = 2 * time.Second
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return 0, ctx.Err()
		case <-timer.C:
		}
	}
	return 0, lastErr
}

func (e *Engine) fetchOnce(ctx context.Context, targetURL string) (int64, bool, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", targetURL, nil)
	if err != nil {
		return 0, false, err
	}

	req.Header.Set("User-Agent", "MirrorRelay-Warmup/1.0")
	req.Header.Set("X-MirrorRelay-Warmup", "1")
	req.Header.Set("Accept-Encoding", "gzip, deflate")

	resp, err := e.httpClient.Do(req)
	if err != nil {
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		return 0, ctx.Err() == nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		retryable := resp.StatusCode == http.StatusRequestTimeout || resp.StatusCode == http.StatusTooEarly ||
			resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= http.StatusInternalServerError
		return 0, retryable, fmt.Errorf("HTTP error %s", resp.Status)
	}

	// Drain response body to ensure complete cache storage in Managed Upstream Nginx
	buffer := make([]byte, 32*1024)
	n, err := io.CopyBuffer(io.Discard, e.throttledReader(ctx, resp.Body), buffer)
	return n, err != nil && ctx.Err() == nil, err
}

func (e *Engine) throttledReader(ctx context.Context, reader io.Reader) io.Reader {
	if e.bandwidth == nil {
		return reader
	}
	return &bandwidthLimitedReader{ctx: ctx, reader: reader, limiter: e.bandwidth}
}

type bandwidthLimiter struct {
	mu     sync.Mutex
	rate   float64
	burst  float64
	tokens float64
	last   time.Time
}

func newBandwidthLimiter(bytesPerSecond int64) *bandwidthLimiter {
	if bytesPerSecond <= 0 {
		return nil
	}
	burst := float64(bytesPerSecond)
	if burst > 32*1024 {
		burst = 32 * 1024
	}
	if burst < 1 {
		burst = 1
	}
	return &bandwidthLimiter{
		rate:   float64(bytesPerSecond),
		burst:  burst,
		tokens: burst,
		last:   time.Now(),
	}
}

func (l *bandwidthLimiter) maxChunk() int {
	return int(l.burst)
}

func (l *bandwidthLimiter) wait(ctx context.Context, count int) error {
	needed := float64(count)
	for {
		l.mu.Lock()
		now := time.Now()
		l.tokens += now.Sub(l.last).Seconds() * l.rate
		if l.tokens > l.burst {
			l.tokens = l.burst
		}
		l.last = now
		if l.tokens >= needed {
			l.tokens -= needed
			l.mu.Unlock()
			return nil
		}
		waitFor := time.Duration((needed-l.tokens)/l.rate*float64(time.Second)) + time.Millisecond
		l.mu.Unlock()

		timer := time.NewTimer(waitFor)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

type bandwidthLimitedReader struct {
	ctx     context.Context
	reader  io.Reader
	limiter *bandwidthLimiter
}

func (r *bandwidthLimitedReader) Read(buffer []byte) (int, error) {
	if max := r.limiter.maxChunk(); len(buffer) > max {
		buffer = buffer[:max]
	}
	count, err := r.reader.Read(buffer)
	if count == 0 {
		return count, err
	}
	if waitErr := r.limiter.wait(r.ctx, count); waitErr != nil {
		return 0, waitErr
	}
	return count, err
}
