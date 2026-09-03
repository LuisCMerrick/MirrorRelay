package warmup

import (
	"compress/gzip"
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LuisCMerrick/MirrorRelay/internal/config"
	"github.com/LuisCMerrick/MirrorRelay/internal/model"
)

type mockStore struct {
	mu      sync.RWMutex
	jobs    map[int64]model.WarmupJob
	mirrors map[int64]model.Mirror
}

func (m *mockStore) ListWarmupJobs(ctx context.Context) ([]model.WarmupJob, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var list []model.WarmupJob
	for _, j := range m.jobs {
		list = append(list, j)
	}
	return list, nil
}

func (m *mockStore) GetWarmupJob(ctx context.Context, id int64) (model.WarmupJob, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	j, ok := m.jobs[id]
	if !ok {
		return model.WarmupJob{}, ErrWarmupJobNotFound
	}
	return j, nil
}

func (m *mockStore) Mirror(ctx context.Context, id int64) (model.Mirror, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	mir, ok := m.mirrors[id]
	if !ok {
		return model.Mirror{}, ErrWarmupJobNotFound
	}
	return mir, nil
}

func (m *mockStore) UpdateWarmupJobProgress(ctx context.Context, id int64, status string, total, completed, failed int, downloadedBytes int64, errMsg, lastRun, nextRun string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.jobs[id]
	if !ok {
		return ErrWarmupJobNotFound
	}
	j.Status = status
	j.TotalItems = total
	j.CompletedItems = completed
	j.FailedItems = failed
	j.BytesDownloaded = downloadedBytes
	j.ErrorMessage = errMsg
	j.LastRunAt = lastRun
	j.NextRunAt = nextRun
	m.jobs[id] = j
	return nil
}

func (m *mockStore) UpdateWarmupJobSchedule(ctx context.Context, id int64, nextRun string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	job, ok := m.jobs[id]
	if !ok {
		return ErrWarmupJobNotFound
	}
	job.NextRunAt = nextRun
	m.jobs[id] = job
	return nil
}

var ErrWarmupJobNotFound = errors.New("not found")

func TestWarmupEngine(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-MirrorRelay-Warmup") != "1" {
			t.Errorf("expected X-MirrorRelay-Warmup header")
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("package payload content for test"))
	}))
	defer ts.Close()

	portStr := strings.TrimPrefix(ts.URL, "http://127.0.0.1:")
	port, _ := strconv.Atoi(portStr)

	cfg := config.Config{}
	cfg.Server.LocalPort = port
	cfg.Warmup.Enabled = true
	cfg.Warmup.MaxConcurrency = 2
	cfg.Warmup.Timeout = 5 * time.Second

	store := &mockStore{
		jobs: map[int64]model.WarmupJob{
			1: {
				ID:          1,
				MirrorID:    10,
				Name:        "Test Job",
				URLPatterns: []string{"/debian/dists/bookworm/Release", "/debian/pool/main/t/test/test.deb"},
				Enabled:     true,
			},
		},
		mirrors: map[int64]model.Mirror{
			10: {
				ID:         10,
				Name:       "Debian Mirror",
				Slug:       "debian",
				Type:       "apt",
				PublicPath: "/debian",
			},
		},
	}

	engine := NewEngine(cfg, store, nil)
	ctx := context.Background()
	engine.Start(ctx)
	defer engine.Stop()

	status := engine.Status()
	if !status.Enabled {
		t.Fatalf("expected engine to be enabled")
	}

	err := engine.RunJob(ctx, 1)
	if err != nil {
		t.Fatalf("RunJob failed: %v", err)
	}

	time.Sleep(300 * time.Millisecond)

	updatedJob, _ := store.GetWarmupJob(ctx, 1)
	if updatedJob.Status != "completed" {
		t.Fatalf("expected job status completed, got %q (err: %s)", updatedJob.Status, updatedJob.ErrorMessage)
	}
	if updatedJob.CompletedItems != 2 {
		t.Fatalf("expected 2 completed items, got %d", updatedJob.CompletedItems)
	}
	if updatedJob.BytesDownloaded <= 0 {
		t.Fatalf("expected downloaded bytes > 0, got %d", updatedJob.BytesDownloaded)
	}
}

func TestCronMatching(t *testing.T) {
	now := time.Date(2026, 8, 21, 10, 2, 30, 0, time.UTC)
	tests := []struct {
		expression string
		want       time.Time
	}{
		{"@hourly", time.Date(2026, 8, 21, 11, 0, 0, 0, time.UTC)},
		{"@daily", time.Date(2026, 8, 22, 0, 0, 0, 0, time.UTC)},
		{"*/5 * * * *", time.Date(2026, 8, 21, 10, 5, 0, 0, time.UTC)},
		{"30 3 * * *", time.Date(2026, 8, 22, 3, 30, 0, 0, time.UTC)},
		{"@every 45m", now.Add(45 * time.Minute)},
	}
	for _, test := range tests {
		next, err := NextRunAt(test.expression, now)
		if err != nil {
			t.Fatalf("NextRunAt(%q): %v", test.expression, err)
		}
		if !next.Equal(test.want) {
			t.Fatalf("NextRunAt(%q) = %s, want %s", test.expression, next, test.want)
		}
	}
	for _, invalid := range []string{"nonsense", "* * *", "61 * * * *", "@every 0s", "0 0 31 2 *"} {
		if err := ValidateSchedule(invalid); err == nil {
			t.Fatalf("expected invalid schedule %q to be rejected", invalid)
		}
	}
}

func TestNormalizeURLPatternsRejectsNonRepositoryTargets(t *testing.T) {
	normalized, err := NormalizeURLPatterns([]string{" pool/demo.deb?download=1 ", "/pool/demo.deb?download=1"})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(normalized, []string{"/pool/demo.deb?download=1"}) {
		t.Fatalf("normalized patterns = %v", normalized)
	}
	for _, invalid := range []string{"", "https://origin.example/package", "//origin.example/package", "/../admin", `/pool\package`} {
		if _, err := NormalizeURLPatterns([]string{invalid}); err == nil {
			t.Fatalf("invalid URL pattern %q was accepted", invalid)
		}
	}
}

func TestAPTMetadataDerivedURLUsesTCPFrontendPort(t *testing.T) {
	var requested []string
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requested = append(requested, r.URL.Path)
		mu.Unlock()
		if strings.HasSuffix(r.URL.Path, "Packages.gz") {
			w.Header().Set("Content-Type", "application/gzip")
			gz := gzip.NewWriter(w)
			_, _ = gz.Write([]byte("Package: demo\nFilename: pool/main/d/demo/demo_1_amd64.deb\n"))
			_ = gz.Close()
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Server.UnixSocketEnabled = false
	cfg.Server.LocalPort = port
	cfg.Warmup.MetadataDepth = 1
	engine := NewEngine(cfg, nil, nil)
	repository := model.Mirror{Type: "apt", PublicPath: "/debian"}
	urls, err := engine.expandTargetURLs(context.Background(), repository, []string{"/dists/bookworm/main/binary-amd64/Packages.gz"})
	if err != nil {
		t.Fatal(err)
	}
	want := server.URL + "/debian/pool/main/d/demo/demo_1_amd64.deb"
	if !slices.Contains(urls, want) {
		t.Fatalf("derived URLs do not contain TCP frontend URL %q: %v", want, urls)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(requested) == 0 {
		t.Fatal("metadata endpoint was not requested")
	}
}

func TestRPMMetadataDepthDiscoversPackagesRecursively(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/rpm/repodata/repomd.xml":
			_, _ = w.Write([]byte(`<repomd><data type="primary"><location href="repodata/primary.xml.gz"/></data></repomd>`))
		case "/rpm/repodata/primary.xml.gz":
			gz := gzip.NewWriter(w)
			_, _ = gz.Write([]byte(`<metadata><package><location href="Packages/demo-1.x86_64.rpm"/></package></metadata>`))
			_ = gz.Close()
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	cfg := warmupTestConfig(t, server.URL)
	cfg.Warmup.MetadataDepth = 2
	engine := NewEngine(cfg, nil, nil)
	repository := model.Mirror{Slug: "rpm", Type: "rpm", PublicMode: "path", PublicPath: "/rpm/"}
	urls, err := engine.expandTargetURLs(t.Context(), repository, []string{"/repodata/repomd.xml"})
	if err != nil {
		t.Fatal(err)
	}
	want := server.URL + "/rpm/Packages/demo-1.x86_64.rpm"
	if !slices.Contains(urls, want) {
		t.Fatalf("recursive RPM targets do not contain %q: %v", want, urls)
	}
}

func TestWarmupConnectionsAlwaysReenterConfiguredFrontend(t *testing.T) {
	requestHost := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		requestHost <- request.Host
		_, _ = w.Write([]byte("cached payload"))
	}))
	defer server.Close()

	cfg := warmupTestConfig(t, server.URL)
	cfg.Warmup.RetryCount = 0
	engine := NewEngine(cfg, nil, nil)
	defer engine.httpClient.CloseIdleConnections()

	downloaded, err := engine.fetchAndWarm(t.Context(), "http://origin.invalid/package.tar.gz")
	if err != nil {
		t.Fatalf("warm-up request failed: %v", err)
	}
	if downloaded == 0 {
		t.Fatal("warm-up request did not drain the frontend response")
	}
	select {
	case host := <-requestHost:
		if host != "origin.invalid" {
			t.Fatalf("logical request Host = %q, want origin.invalid", host)
		}
	case <-time.After(time.Second):
		t.Fatal("configured frontend did not receive the warm-up request")
	}
}

func TestAbsoluteMetadataLinkUsesFrontendAdapterRoute(t *testing.T) {
	cfg := config.Default()
	cfg.Server.UnixSocketEnabled = false
	cfg.Server.LocalAddress = "127.0.0.1"
	cfg.Server.LocalPort = 19081
	engine := NewEngine(cfg, nil, nil)
	repository := model.Mirror{Slug: "python", Type: "pypi", PublicMode: "path", PublicPath: "/python/"}
	target, ok := engine.metadataWarmupURL(repository, "http://127.0.0.1:19081/python/simple/demo/", "https://files.example/demo.whl")
	if !ok {
		t.Fatal("absolute metadata URL was rejected")
	}
	prefix := "http://127.0.0.1:19081/python/__fetch/"
	if !strings.HasPrefix(target, prefix) {
		t.Fatalf("absolute metadata URL = %q, want frontend adapter prefix %q", target, prefix)
	}
	decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(target, prefix))
	if err != nil {
		t.Fatal(err)
	}
	if string(decoded) != "https://files.example/demo.whl" {
		t.Fatalf("adapter target = %q", decoded)
	}
}

func TestWarmupRetriesFailedItems(t *testing.T) {
	var mu sync.Mutex
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		attempts++
		attempt := attempts
		mu.Unlock()
		if attempt < 3 {
			http.Error(w, "try again", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	cfg := warmupTestConfig(t, server.URL)
	cfg.Warmup.RetryCount = 2
	engine := NewEngine(cfg, nil, nil)
	defer engine.httpClient.CloseIdleConnections()
	if _, err := engine.fetchAndWarm(t.Context(), "http://packages.invalid/retry"); err != nil {
		t.Fatalf("retrying warm-up request failed: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if attempts != 3 {
		t.Fatalf("request attempts = %d, want 3", attempts)
	}
}

func TestWarmupDownloadConcurrencyIsGlobal(t *testing.T) {
	cfg := config.Default()
	cfg.Warmup.MaxConcurrency = 1
	engine := NewEngine(cfg, nil, nil)
	if !engine.acquireDownloadSlot(t.Context()) {
		t.Fatal("failed to acquire initial download slot")
	}
	acquired := make(chan struct{})
	go func() {
		if engine.acquireDownloadSlot(t.Context()) {
			close(acquired)
		}
	}()
	select {
	case <-acquired:
		t.Fatal("second download exceeded the global concurrency limit")
	case <-time.After(50 * time.Millisecond):
	}
	engine.releaseDownloadSlot()
	select {
	case <-acquired:
		engine.releaseDownloadSlot()
	case <-time.After(time.Second):
		t.Fatal("waiting download did not acquire the released global slot")
	}
}

func TestWarmupBandwidthLimiterHonorsCancellation(t *testing.T) {
	limiter := newBandwidthLimiter(16)
	if err := limiter.wait(t.Context(), 16); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	if err := limiter.wait(ctx, 16); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("bandwidth wait error = %v, want deadline exceeded", err)
	}
}

func TestStopCancelsAndWaitsForActiveWarmupJobs(t *testing.T) {
	requestStarted := make(chan struct{})
	requestCancelled := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		close(requestStarted)
		<-request.Context().Done()
		close(requestCancelled)
	}))
	defer server.Close()

	cfg := warmupTestConfig(t, server.URL)
	cfg.Warmup.MaxConcurrency = 1
	cfg.Warmup.MetadataDepth = 0
	cfg.Warmup.RetryCount = 0
	cfg.Warmup.Timeout = time.Minute
	store := &mockStore{
		jobs: map[int64]model.WarmupJob{
			1: {ID: 1, MirrorID: 10, Name: "blocking", URLPatterns: []string{"/package"}, Enabled: true},
		},
		mirrors: map[int64]model.Mirror{
			10: {ID: 10, Slug: "repo", Type: "generic", PublicMode: "path", PublicPath: "/repo/"},
		},
	}
	engine := NewEngine(cfg, store, nil)
	engine.Start(t.Context())
	if err := engine.RunJob(t.Context(), 1); err != nil {
		t.Fatal(err)
	}
	select {
	case <-requestStarted:
	case <-time.After(time.Second):
		t.Fatal("warm-up request did not start")
	}

	stopped := make(chan struct{})
	go func() {
		engine.Stop()
		close(stopped)
	}()
	select {
	case <-requestCancelled:
	case <-time.After(time.Second):
		t.Fatal("engine shutdown did not cancel the active request")
	}
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("engine shutdown did not wait for the active job")
	}

	job, err := store.GetWarmupJob(t.Context(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != "cancelled" {
		t.Fatalf("job status after shutdown = %q, want cancelled", job.Status)
	}
	if status := engine.Status(); status.RunningJobs != 0 {
		t.Fatalf("running jobs after shutdown = %d", status.RunningJobs)
	}
}

func TestWarmupTimeoutAppliesToWholeJob(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		<-request.Context().Done()
	}))
	defer server.Close()

	cfg := warmupTestConfig(t, server.URL)
	cfg.Warmup.MetadataDepth = 0
	cfg.Warmup.RetryCount = 0
	cfg.Warmup.Timeout = 75 * time.Millisecond
	store := &mockStore{
		jobs: map[int64]model.WarmupJob{
			1: {ID: 1, MirrorID: 10, Name: "timeout", URLPatterns: []string{"/package"}, Enabled: true},
		},
		mirrors: map[int64]model.Mirror{
			10: {ID: 10, Slug: "repo", Type: "generic", PublicMode: "path", PublicPath: "/repo/"},
		},
	}
	engine := NewEngine(cfg, store, nil)
	engine.Start(t.Context())
	defer engine.Stop()
	if err := engine.RunJob(t.Context(), 1); err != nil {
		t.Fatal(err)
	}
	job := waitForWarmupJobStatus(t, store, 1, "failed")
	if job.ErrorMessage != "Job timed out" {
		t.Fatalf("timeout error = %q", job.ErrorMessage)
	}
}

func TestScheduledWarmupRecoversPersistedRunningState(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	cfg := warmupTestConfig(t, server.URL)
	cfg.Warmup.Enabled = true
	cfg.Warmup.MetadataDepth = 0
	cfg.Warmup.RetryCount = 0
	store := &mockStore{
		jobs: map[int64]model.WarmupJob{
			1: {
				ID: 1, MirrorID: 10, Name: "stale", URLPatterns: []string{"/package"}, Enabled: true,
				CronExpression: "@hourly", Status: "running", NextRunAt: time.Now().Add(-time.Minute).UTC().Format(time.RFC3339),
			},
		},
		mirrors: map[int64]model.Mirror{
			10: {ID: 10, Slug: "repo", Type: "generic", PublicMode: "path", PublicPath: "/repo/"},
		},
	}
	engine := NewEngine(cfg, store, nil)
	engine.Start(t.Context())
	defer engine.Stop()
	engine.checkAndRunScheduledJobs(t.Context())
	job := waitForWarmupJobStatus(t, store, 1, "completed")
	if job.NextRunAt == "" {
		t.Fatal("recovered scheduled job did not calculate its next run")
	}
}

func waitForWarmupJobStatus(t *testing.T, store *mockStore, jobID int64, expected string) model.WarmupJob {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		job, err := store.GetWarmupJob(t.Context(), jobID)
		if err != nil {
			t.Fatal(err)
		}
		if job.Status == expected {
			return job
		}
		time.Sleep(10 * time.Millisecond)
	}
	job, _ := store.GetWarmupJob(t.Context(), jobID)
	t.Fatalf("job status = %q, want %q", job.Status, expected)
	return model.WarmupJob{}
}

func warmupTestConfig(t *testing.T, serverURL string) config.Config {
	t.Helper()
	parsed, err := url.Parse(serverURL)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Server.UnixSocketEnabled = false
	cfg.Server.LocalAddress = parsed.Hostname()
	cfg.Server.LocalPort = port
	cfg.Warmup.BandwidthLimit = 0
	return cfg
}
