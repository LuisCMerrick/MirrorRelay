package warmup

import (
	"compress/gzip"
	"context"
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
	urls := engine.expandTargetURLs(context.Background(), repository, []string{"/dists/bookworm/main/binary-amd64/Packages.gz"})
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
