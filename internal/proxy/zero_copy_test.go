package proxy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/LuisCMerrick/MirrorRelay/internal/cachectl"
	"github.com/LuisCMerrick/MirrorRelay/internal/config"
	"github.com/LuisCMerrick/MirrorRelay/internal/database"
	"github.com/LuisCMerrick/MirrorRelay/internal/mirror"
	"github.com/LuisCMerrick/MirrorRelay/internal/model"
	"github.com/LuisCMerrick/MirrorRelay/internal/stats"
)

func TestZeroCopyXAccelRedirectBypass(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "mirrorrelay.db")
	store, err := database.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ctx := context.Background()
	repoID, err := store.CreateMirror(ctx, model.Mirror{
		Name:       "Debian",
		Slug:       "debian",
		Type:       "apt",
		PublicMode: "path",
		PublicPath: "/debian/",
		Enabled:    true,
		Upstreams: []model.Upstream{
			{URL: "https://deb.debian.org/debian", Enabled: true, Priority: 100},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	cfg := config.Default()
	cfg.Performance.ZeroCopyBypass = true
	cfg.Security.AdminCIDRs = []string{"0.0.0.0/0", "::/0"}

	registry := mirror.NewRegistry(store)
	if err := registry.Reload(ctx); err != nil {
		t.Fatal(err)
	}
	cacheManager := cachectl.New(cfg, store)
	if err := cacheManager.Load(ctx); err != nil {
		t.Fatal(err)
	}
	metric := stats.New()

	engine := New(cfg, registry, cacheManager, metric, nil, testAuxiliarySigningKey)
	defer engine.CloseIdleConnections()

	// 1. Binary package request with X-Accel-Supported -> returns X-Accel-Redirect
	rPkg := httptest.NewRequest(http.MethodGet, "http://localhost/debian/pool/main/v/vim.deb", nil)
	rPkg.Header.Set("X-Accel-Supported", "1")
	wPkg := httptest.NewRecorder()
	engine.ServeHTTP(wPkg, rPkg)

	if wPkg.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", wPkg.Code)
	}
	accelHeader := wPkg.Header().Get("X-Accel-Redirect")
	if accelHeader == "" {
		t.Fatalf("expected X-Accel-Redirect header on binary package request")
	}
	if repoHeader := wPkg.Header().Get("X-Mirror-Internal-Repository-ID"); repoHeader == "" {
		t.Fatalf("expected X-Mirror-Internal-Repository-ID header")
	}

	// 2. Metadata request (InRelease) with X-Accel-Supported -> should NOT use X-Accel-Redirect (Go handles metadata)
	rMeta := httptest.NewRequest(http.MethodGet, "http://localhost/debian/dists/bookworm/InRelease", nil)
	rMeta.Header.Set("X-Accel-Supported", "1")
	wMeta := httptest.NewRecorder()
	engine.ServeHTTP(wMeta, rMeta)

	if wMeta.Header().Get("X-Accel-Redirect") != "" {
		t.Fatalf("metadata should not be accelerated with X-Accel-Redirect")
	}

	// 3. Binary package when ZeroCopyBypass is disabled -> should NOT return X-Accel-Redirect
	cfgDisabled := cfg
	cfgDisabled.Performance.ZeroCopyBypass = false
	engineDisabled := New(cfgDisabled, registry, cacheManager, metric, nil, testAuxiliarySigningKey)
	defer engineDisabled.CloseIdleConnections()

	wDisabled := httptest.NewRecorder()
	engineDisabled.ServeHTTP(wDisabled, rPkg)
	if wDisabled.Header().Get("X-Accel-Redirect") != "" {
		t.Fatalf("disabled zero copy should not emit X-Accel-Redirect")
	}

	// 4. Adaptive header detection (X-Accel-Supported: true, X-Accel-Mapping, X-Sendfile-Type)
	for _, headerCase := range []struct {
		key   string
		value string
	}{
		{"X-Accel-Supported", "true"},
		{"X-Accel-Mapping", "/var/cache=/uri"},
		{"X-Sendfile-Type", "X-Accel-Redirect"},
	} {
		rAdaptive := httptest.NewRequest(http.MethodGet, "http://localhost/debian/pool/main/v/vim.deb", nil)
		rAdaptive.Header.Set(headerCase.key, headerCase.value)
		wAdaptive := httptest.NewRecorder()
		engine.ServeHTTP(wAdaptive, rAdaptive)
		if wAdaptive.Header().Get("X-Accel-Redirect") == "" {
			t.Fatalf("expected X-Accel-Redirect for header %s: %s", headerCase.key, headerCase.value)
		}
	}

	_ = repoID
}
