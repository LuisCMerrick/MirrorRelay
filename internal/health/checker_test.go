package health

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/LuisCMerrick/MirrorRelay/internal/config"
	"github.com/LuisCMerrick/MirrorRelay/internal/mirror"
	"github.com/LuisCMerrick/MirrorRelay/internal/model"
)

type dummyStore struct{}

func (dummyStore) UpdateUpstreamHealth(context.Context, int64, string, int64, string, time.Time) error {
	return nil
}

func TestCheckerInFlightAndWorker(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := config.Default()
	cfg.Server.UnixSocketEnabled = false
	cfg.UpstreamNginx.UpstreamSocketEnabled = false
	store := dummyStore{}
	registry := mirror.NewRegistry(nil)
	repo := model.Mirror{
		ID:                 1,
		Name:               "test",
		Slug:               "test",
		Enabled:            true,
		HealthCheckEnabled: true,
		HealthIntervalSec:  1,
		HealthTimeoutSec:   2,
		Upstreams: []model.Upstream{
			{ID: 10, URL: server.URL, Enabled: true},
		},
	}
	registry.Replace([]model.Mirror{repo})
	checker := New(cfg, store, registry)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	checker.Start(ctx)
	results, err := checker.CheckMirror(ctx, repo)
	if err != nil && len(results) == 0 {
		t.Fatalf("CheckMirror failed: %v", err)
	}
}
