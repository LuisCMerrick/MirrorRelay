package warmup

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
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
	now := time.Now().UTC()
	if !shouldRunCron("@hourly", "", now) {
		t.Fatalf("expected first run with empty lastRun to be true")
	}
	oneHourAgo := now.Add(-65 * time.Minute).Format(time.RFC3339)
	if !shouldRunCron("@hourly", oneHourAgo, now) {
		t.Fatalf("expected run after 65 minutes to be true")
	}
	fiveMinsAgo := now.Add(-5 * time.Minute).Format(time.RFC3339)
	if shouldRunCron("@hourly", fiveMinsAgo, now) {
		t.Fatalf("expected run after 5 minutes to be false")
	}
}
