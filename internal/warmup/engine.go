// Package warmup provides smart cache pre-fetching and warm-up capabilities.
package warmup

import (
	"bufio"
	"compress/gzip"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
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

type Engine struct {
	cfg          config.Config
	store        Store
	audit        AuditRecorder
	httpClient   *http.Client
	mu           sync.RWMutex
	activeJobs   map[int64]context.CancelFunc
	totalWarmups int64
	bytesWarmed  int64
	lastWarmupAt time.Time
	cancelAll    context.CancelFunc
	wg           sync.WaitGroup
}

func NewEngine(cfg config.Config, store Store, audit AuditRecorder) *Engine {
	transport := &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 20,
		IdleConnTimeout:     90 * time.Second,
		TLSClientConfig:     &tls.Config{InsecureSkipVerify: false},
	}

	if cfg.Server.UnixSocketEnabled && cfg.Server.FrontendSocket != "" {
		transport.DialContext = func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", cfg.Server.FrontendSocket)
		}
	}

	client := &http.Client{
		Transport: transport,
		Timeout:   cfg.Warmup.Timeout,
	}
	if client.Timeout <= 0 {
		client.Timeout = 15 * time.Minute
	}

	return &Engine{
		cfg:        cfg,
		store:      store,
		audit:      audit,
		httpClient: client,
		activeJobs: make(map[int64]context.CancelFunc),
	}
}

func (e *Engine) Start(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	e.cancelAll = cancel

	e.wg.Add(1)
	go e.cronLoop(ctx)
}

func (e *Engine) Stop() {
	if e.cancelAll != nil {
		e.cancelAll()
	}
	e.mu.Lock()
	for _, cancel := range e.activeJobs {
		cancel()
	}
	e.activeJobs = make(map[int64]context.CancelFunc)
	e.mu.Unlock()

	e.wg.Wait()
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
	e.mu.Lock()
	if _, exists := e.activeJobs[jobID]; exists {
		e.mu.Unlock()
		return errors.New("warmup job is already running")
	}

	jobCtx, cancel := context.WithCancel(context.Background())
	e.activeJobs[jobID] = cancel
	e.mu.Unlock()

	go func() {
		defer func() {
			e.mu.Lock()
			delete(e.activeJobs, jobID)
			e.mu.Unlock()
		}()
		e.executeJob(jobCtx, jobID)
	}()

	return nil
}

func (e *Engine) CancelJob(jobID int64) error {
	e.mu.Lock()
	cancel, exists := e.activeJobs[jobID]
	if !exists {
		e.mu.Unlock()
		return errors.New("warmup job is not currently running")
	}
	delete(e.activeJobs, jobID)
	e.mu.Unlock()

	cancel()
	_ = e.store.UpdateWarmupJobProgress(context.Background(), jobID, "cancelled", 0, 0, 0, 0, "Job cancelled by operator", time.Now().UTC().Format(time.RFC3339), "")
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
		if !job.Enabled || job.Status == "running" || job.CronExpression == "" {
			continue
		}

		if shouldRunCron(job.CronExpression, job.LastRunAt, now) {
			_ = e.RunJob(ctx, job.ID)
		}
	}
}

func shouldRunCron(expr, lastRunStr string, now time.Time) bool {
	// Simple intervals: @hourly, @daily, @every 1h, or cron format
	if lastRunStr != "" {
		lastRun, err := time.Parse(time.RFC3339, lastRunStr)
		if err == nil {
			switch strings.TrimSpace(strings.ToLower(expr)) {
			case "@hourly", "0 * * * *":
				if now.Sub(lastRun) < 55*time.Minute {
					return false
				}
			case "@daily", "0 0 * * *", "0 2 * * *":
				if now.Sub(lastRun) < 23*time.Hour {
					return false
				}
			default:
				if strings.HasPrefix(expr, "@every ") {
					durStr := strings.TrimPrefix(expr, "@every ")
					if d, err := time.ParseDuration(durStr); err == nil && d > 0 {
						if now.Sub(lastRun) < d {
							return false
						}
					}
				}
			}
		}
	}
	return true
}

func (e *Engine) executeJob(ctx context.Context, jobID int64) {
	job, err := e.store.GetWarmupJob(ctx, jobID)
	if err != nil {
		slog.Error("Warmup job lookup failed", "job_id", jobID, "err", err)
		return
	}

	mirror, err := e.store.Mirror(ctx, job.MirrorID)
	if err != nil {
		_ = e.store.UpdateWarmupJobProgress(ctx, jobID, "failed", 0, 0, 0, 0, "Mirror not found: "+err.Error(), time.Now().UTC().Format(time.RFC3339), "")
		return
	}

	nowStr := time.Now().UTC().Format(time.RFC3339)
	_ = e.store.UpdateWarmupJobProgress(ctx, jobID, "running", len(job.URLPatterns), 0, 0, 0, "", nowStr, "")

	concurrency := e.cfg.Warmup.MaxConcurrency
	if concurrency <= 0 {
		concurrency = 4
	}

	targetURLs := e.expandTargetURLs(ctx, mirror, job.URLPatterns)
	total := len(targetURLs)
	if total == 0 {
		_ = e.store.UpdateWarmupJobProgress(ctx, jobID, "completed", 0, 0, 0, 0, "No target URLs to warm up", nowStr, "")
		return
	}

	sem := make(chan struct{}, concurrency)
	var completedCount, failedCount int64
	var totalDownloadedBytes int64
	var lastErrStr string
	var errMu sync.Mutex

	var jobWg sync.WaitGroup
	for _, targetURL := range targetURLs {
		select {
		case <-ctx.Done():
			_ = e.store.UpdateWarmupJobProgress(ctx, jobID, "cancelled", total, int(completedCount), int(failedCount), totalDownloadedBytes, "Job cancelled", nowStr, "")
			return
		case sem <- struct{}{}:
		}

		jobWg.Add(1)
		go func(rawURL string) {
			defer func() {
				<-sem
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

	atomic.AddInt64(&e.totalWarmups, 1)
	e.mu.Lock()
	e.lastWarmupAt = time.Now().UTC()
	e.mu.Unlock()

	finalStatus := "completed"
	if atomic.LoadInt64(&failedCount) > 0 && atomic.LoadInt64(&completedCount) == 0 {
		finalStatus = "failed"
	}

	_ = e.store.UpdateWarmupJobProgress(ctx, jobID, finalStatus, total, int(completedCount), int(failedCount), totalDownloadedBytes, lastErrStr, nowStr, "")
	if e.audit != nil {
		e.audit.Record("system", "warmup", fmt.Sprintf("warmup_job:%d", jobID), fmt.Sprintf("Warmed %d/%d items (%d bytes)", completedCount, total, totalDownloadedBytes), finalStatus == "completed")
	}
}

func (e *Engine) expandTargetURLs(ctx context.Context, mirror model.Mirror, patterns []string) []string {
	baseURL := "http://127.0.0.1"
	if e.cfg.Server.LocalPort > 0 {
		baseURL = fmt.Sprintf("http://127.0.0.1:%d", e.cfg.Server.LocalPort)
	}
	basePath := strings.TrimRight(mirror.PublicPath, "/")

	var urls []string
	for _, p := range patterns {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if !strings.HasPrefix(p, "/") {
			p = "/" + p
		}
		full := baseURL + basePath + p
		urls = append(urls, full)
	}

	// If metadata depth > 0, inspect metadata files to extract package URLs
	if e.cfg.Warmup.MetadataDepth > 0 && len(urls) > 0 {
		var packageURLs []string
		for _, u := range urls {
			if isMetadataFile(u, mirror.Type) {
				pkgs := e.extractPackagesFromMetadata(ctx, u, mirror)
				packageURLs = append(packageURLs, pkgs...)
			}
		}
		urls = append(urls, packageURLs...)
	}

	return urls
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

	var reader io.Reader = resp.Body
	if strings.HasSuffix(strings.ToLower(metadataURL), ".gz") {
		gz, err := gzip.NewReader(resp.Body)
		if err == nil {
			defer gz.Close()
			reader = gz
		}
	}

	var results []string
	baseURL := strings.TrimRight(metadataURL, "/")
	scanner := bufio.NewScanner(io.LimitReader(reader, 10*1024*1024)) // 10MB max metadata limit

	switch mirror.Type {
	case "apt":
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "Filename: ") {
				fn := strings.TrimSpace(strings.TrimPrefix(line, "Filename: "))
				if fn != "" {
					u := fmt.Sprintf("http://127.0.0.1%s/%s", strings.TrimRight(mirror.PublicPath, "/"), strings.TrimLeft(fn, "/"))
					results = append(results, u)
					if len(results) >= 50 {
						break
					}
				}
			}
		}
	case "pypi":
		re := regexp.MustCompile(`href="([^"]+\.whl|[^"]+\.tar\.gz)"`)
		for scanner.Scan() {
			matches := re.FindAllStringSubmatch(scanner.Text(), -1)
			for _, m := range matches {
				if len(m) > 1 {
					pkgPath := m[1]
					if !strings.HasPrefix(pkgPath, "http") {
						pkgPath = baseURL + "/" + strings.TrimLeft(pkgPath, "/")
					}
					results = append(results, pkgPath)
					if len(results) >= 50 {
						break
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

func (e *Engine) fetchAndWarm(ctx context.Context, targetURL string) (int64, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", targetURL, nil)
	if err != nil {
		return 0, err
	}

	req.Header.Set("User-Agent", "MirrorRelay-Warmup/1.0")
	req.Header.Set("X-MirrorRelay-Warmup", "1")
	req.Header.Set("Accept-Encoding", "gzip, deflate")

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return 0, fmt.Errorf("HTTP error %d %s", resp.StatusCode, resp.Status)
	}

	// Drain response body to ensure complete cache storage in Managed Upstream Nginx
	n, err := io.Copy(io.Discard, resp.Body)
	return n, err
}
