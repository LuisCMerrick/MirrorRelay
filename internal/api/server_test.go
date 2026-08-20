package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/LuisCMerrick/MirrorRelay/internal/auth"
	"github.com/LuisCMerrick/MirrorRelay/internal/config"
	"github.com/LuisCMerrick/MirrorRelay/internal/database"
	"github.com/LuisCMerrick/MirrorRelay/internal/mirror"
	"github.com/LuisCMerrick/MirrorRelay/internal/model"
	"github.com/LuisCMerrick/MirrorRelay/internal/security"
)

func TestWebHandlerServesConfiguredAdminIndexWithoutRedirect(t *testing.T) {
	cfg := config.Default()
	cfg.Admin.Path = "/private-console/"

	server := &Server{
		cfg: cfg,
		web: fstest.MapFS{
			"index.html": {Data: []byte("<!doctype html><title>Admin</title>")},
			"app.js":     {Data: []byte("console.log('ok')")},
		},
	}

	for _, target := range []string{"/private-console/", "/private-console/app.js"} {
		t.Run(target, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, target, nil)

			server.webHandler(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("expected status 200, got %d with Location %q", rec.Code, rec.Header().Get("Location"))
			}
			if target == "/private-console/" && !strings.Contains(rec.Body.String(), "<!doctype html>") {
				t.Fatalf("expected admin HTML body, got %q", rec.Body.String())
			}
			if target == "/private-console/app.js" && !strings.Contains(rec.Body.String(), "console.log('ok')") {
				t.Fatalf("expected app.js body, got %q", rec.Body.String())
			}
		})
	}
}

func TestHandlerScopesUIAndAPIUnderConfiguredAdminPath(t *testing.T) {
	cfg := config.Default()
	cfg.Admin.Path = "/custom-admin/"
	cfg.Security.AdminCIDRs = []string{"10.0.0.0/8"}
	cidrs, err := security.ParseCIDRs(cfg.Security.AdminCIDRs)
	if err != nil {
		t.Fatal(err)
	}

	server := &Server{
		cfg:        cfg,
		adminCIDRs: cidrs,
		web: fstest.MapFS{
			"index.html": {Data: []byte("custom admin index")},
		},
	}

	handler := server.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/custom-admin/", nil)
	req.RemoteAddr = "10.1.2.3:4321"
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "custom admin index") {
		t.Fatalf("expected 200 with admin UI body, got status %d body %q", rec.Code, rec.Body.String())
	}
}

func TestWebSettingsValidatePersistAndReset(t *testing.T) {
	store, err := database.Open(filepath.Join(t.TempDir(), "mirrorrelay.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	cfg := config.Default()
	server := &Server{cfg: cfg, fileConfig: cfg, store: store}
	settings := config.WebSettingsFrom(cfg)
	settings.Server.UnixSocketEnabled = false
	settings.Server.LocalPort = 19081
	encoded, err := json.Marshal(settings)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPut, "/admin/api/v1/settings", bytes.NewReader(encoded))
	recorder := httptest.NewRecorder()
	server.updateWebSettings(recorder, request, auth.Session{Username: "admin"})
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"restart_required":true`) {
		t.Fatalf("settings update: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	stored, found, err := store.Setting(request.Context(), config.WebSettingsKey)
	if err != nil || !found || !strings.Contains(stored, `"local_port":19081`) {
		t.Fatalf("stored settings: found=%v value=%s err=%v", found, stored, err)
	}

	getRecorder := httptest.NewRecorder()
	server.webSettings(getRecorder, httptest.NewRequest(http.MethodGet, "/admin/api/v1/settings", nil))
	if getRecorder.Code != http.StatusOK || !strings.Contains(getRecorder.Body.String(), `"source":"web_ui"`) {
		t.Fatalf("settings read: status=%d body=%s", getRecorder.Code, getRecorder.Body.String())
	}

	resetRecorder := httptest.NewRecorder()
	server.resetWebSettings(resetRecorder, httptest.NewRequest(http.MethodDelete, "/admin/api/v1/settings", nil), auth.Session{Username: "admin"})
	if resetRecorder.Code != http.StatusOK {
		t.Fatalf("settings reset: status=%d body=%s", resetRecorder.Code, resetRecorder.Body.String())
	}
	if _, found, err := store.Setting(request.Context(), config.WebSettingsKey); err != nil || found {
		t.Fatalf("settings override survived reset: found=%v err=%v", found, err)
	}
}

func TestWebSettingsResetReportsRestartAfterAppliedOverride(t *testing.T) {
	store, err := database.Open(filepath.Join(t.TempDir(), "mirrorrelay.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	fileConfig := config.Default()
	running := fileConfig
	running.Logging.KeepDays++
	server := &Server{cfg: running, fileConfig: fileConfig, store: store}
	settings := config.WebSettingsFrom(running)
	encoded, err := json.Marshal(settings)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.PutSetting(context.Background(), config.WebSettingsKey, string(encoded)); err != nil {
		t.Fatal(err)
	}

	before := httptest.NewRecorder()
	server.webSettings(before, httptest.NewRequest(http.MethodGet, "/admin/api/v1/settings", nil))
	if before.Code != http.StatusOK || !strings.Contains(before.Body.String(), `"source":"web_ui"`) || strings.Contains(before.Body.String(), `"restart_required":true`) {
		t.Fatalf("applied override state: status=%d body=%s", before.Code, before.Body.String())
	}

	reset := httptest.NewRecorder()
	server.resetWebSettings(reset, httptest.NewRequest(http.MethodDelete, "/admin/api/v1/settings", nil), auth.Session{Username: "admin"})
	if reset.Code != http.StatusOK || !strings.Contains(reset.Body.String(), `"restart_required":true`) || !strings.Contains(reset.Body.String(), fmt.Sprintf(`"keep_days":%d`, fileConfig.Logging.KeepDays)) {
		t.Fatalf("reset state: status=%d body=%s", reset.Code, reset.Body.String())
	}

	after := httptest.NewRecorder()
	server.webSettings(after, httptest.NewRequest(http.MethodGet, "/admin/api/v1/settings", nil))
	if after.Code != http.StatusOK || !strings.Contains(after.Body.String(), `"source":"configuration_file"`) || !strings.Contains(after.Body.String(), `"restart_required":true`) {
		t.Fatalf("pending YAML restart state: status=%d body=%s", after.Code, after.Body.String())
	}
}

func TestRepositoryHealthStateUsesAnyViableUpstream(t *testing.T) {
	t.Run("healthy backup", func(t *testing.T) {
		repo := model.Mirror{
			Enabled:            true,
			HealthCheckEnabled: true,
			Upstreams: []model.Upstream{
				{Enabled: true, HealthStatus: "unhealthy"},
				{Enabled: true, HealthStatus: "healthy"},
			},
		}
		if state := repositoryHealthState(repo); state != "healthy" {
			t.Fatalf("expected healthy when a healthy upstream exists, got %q", state)
		}
	})
	t.Run("unknown remains viable", func(t *testing.T) {
		repo := model.Mirror{
			Enabled:            true,
			HealthCheckEnabled: true,
			Upstreams: []model.Upstream{
				{Enabled: true, HealthStatus: "unhealthy"},
				{Enabled: true, HealthStatus: "unknown"},
			},
		}
		if state := repositoryHealthState(repo); state != "unknown" {
			t.Fatalf("expected unknown when an unprobed upstream remains, got %q", state)
		}
	})
	t.Run("all unhealthy", func(t *testing.T) {
		repo := model.Mirror{
			Enabled:            true,
			HealthCheckEnabled: true,
			Upstreams: []model.Upstream{
				{Enabled: true, HealthStatus: "unhealthy"},
				{Enabled: false, HealthStatus: "healthy"},
			},
		}
		if state := repositoryHealthState(repo); state != "unhealthy" {
			t.Fatalf("expected unhealthy when every enabled upstream fails, got %q", state)
		}
	})
	t.Run("no enabled upstream", func(t *testing.T) {
		repo := model.Mirror{
			Enabled:            true,
			HealthCheckEnabled: true,
			Upstreams: []model.Upstream{
				{Enabled: false, HealthStatus: "healthy"},
			},
		}
		if state := repositoryHealthState(repo); state != "unknown" {
			t.Fatalf("expected unknown when no upstreams are enabled, got %q", state)
		}
	})
	t.Run("disabled repository", func(t *testing.T) {
		repo := model.Mirror{
			Enabled:            false,
			HealthCheckEnabled: true,
			Upstreams: []model.Upstream{
				{Enabled: true, HealthStatus: "healthy"},
			},
		}
		if state := repositoryHealthState(repo); state != "disabled" {
			t.Fatalf("expected disabled when repository is disabled, got %q", state)
		}
	})
}

func TestPublicRootListsRepositoriesAndPreservesHostRoutes(t *testing.T) {
	cfg := config.Default()
	cidrs, err := security.ParseCIDRs(cfg.Security.AdminCIDRs)
	if err != nil {
		t.Fatal(err)
	}
	reg := mirror.NewRegistry(nil)
	reg.Replace([]model.Mirror{
		{
			ID:           1,
			Name:         "Debian Main",
			Slug:         "debian",
			Type:         "apt",
			Enabled:      true,
			PublicMode:   "path",
			PublicPath:   "/debian/",
			AccessPolicy: "public",
		},
		{
			ID:           2,
			Name:         "Docker Host Route",
			Slug:         "docker",
			Type:         "docker-registry",
			Enabled:      true,
			PublicMode:   "host",
			PublicHost:   "docker.mirror.local",
			AccessPolicy: "public",
		},
	})
	server := &Server{
		cfg:        cfg,
		adminCIDRs: cidrs,
		registry:   reg,
	}
	handler := server.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))

	recRoot := httptest.NewRecorder()
	handler.ServeHTTP(recRoot, httptest.NewRequest(http.MethodGet, "https://mirror.example.org/", nil))
	if recRoot.Code != http.StatusOK || !strings.Contains(recRoot.Body.String(), "Debian Main") || !strings.Contains(recRoot.Body.String(), "docker.mirror.local") {
		t.Fatalf("expected public root index with both repositories, got code=%d body=%s", recRoot.Code, recRoot.Body.String())
	}

	recHost := httptest.NewRecorder()
	handler.ServeHTTP(recHost, httptest.NewRequest(http.MethodGet, "https://docker.mirror.local/v2/", nil))
	if recHost.Code != http.StatusTeapot {
		t.Fatalf("expected host-mode repository request to reach proxy handler, got %d", recHost.Code)
	}
}

func TestOriginIsolationAndMetricsSecurity(t *testing.T) {
	cfg := config.Default()
	cfg.Admin.Host = "admin.example.com"
	cfg.Admin.Path = "/admin/"
	adminCIDRs, _ := security.ParseCIDRs([]string{"127.0.0.1/32"})
	server := &Server{
		cfg:        cfg,
		adminCIDRs: adminCIDRs,
		web:        fstest.MapFS{"index.html": {Data: []byte("admin index")}},
		registry:   mirror.NewRegistry(nil),
	}
	proxy := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("proxied package data"))
	})
	handler := server.Handler(proxy)

	// 1. Request to admin.example.com for /admin/ -> should succeed
	r1 := httptest.NewRequest(http.MethodGet, "https://admin.example.com/admin/", nil)
	r1.RemoteAddr = "127.0.0.1:1234"
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, r1)
	if rec1.Code != http.StatusOK || rec1.Body.String() != "admin index" {
		t.Fatalf("admin host admin access failed: code=%d body=%s", rec1.Code, rec1.Body.String())
	}

	// 2. Request to admin.example.com for package proxy route -> should return 404
	r2 := httptest.NewRequest(http.MethodGet, "https://admin.example.com/debian/pool/main/a.deb", nil)
	r2.RemoteAddr = "127.0.0.1:1234"
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, r2)
	if rec2.Code != http.StatusNotFound {
		t.Fatalf("admin host should reject package proxy routes with 404, got %d", rec2.Code)
	}

	// 3. Request to data plane host mirror.example.com for /admin/ -> should return 404
	r3 := httptest.NewRequest(http.MethodGet, "https://mirror.example.com/admin/", nil)
	r3.RemoteAddr = "127.0.0.1:1234"
	rec3 := httptest.NewRecorder()
	handler.ServeHTTP(rec3, r3)
	if rec3.Code != http.StatusNotFound {
		t.Fatalf("data plane host should reject admin routes with 404, got %d", rec3.Code)
	}
}

func TestDebianSecurityClientExamples(t *testing.T) {
	cfg := config.Default()
	cfg.HTTP.PublicBaseURL = "https://mirror.example.com"

	repoDebSec := model.Mirror{
		Type:        "apt",
		ProfileName: "Debian Security",
		PublicMode:  "path",
		PublicPath:  "/debian-security/",
	}

	exDebSec := clientExamples(cfg, repoDebSec)
	if len(exDebSec) == 0 || exDebSec[0].Command != "deb https://mirror.example.com/debian-security bookworm-security main contrib non-free-firmware" {
		t.Fatalf("unexpected debian-security example: %+v", exDebSec)
	}
}

func TestSystemRestartEndpoint(t *testing.T) {
	store, err := database.Open(filepath.Join(t.TempDir(), "mirrorrelay.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	triggered := make(chan struct{}, 1)
	server := &Server{
		cfg:      config.Default(),
		store:    store,
		sessions: auth.NewSessionsWithPath(store, time.Hour, "/admin/"),
		web:      fstest.MapFS{"index.html": {Data: []byte("admin index")}},
	}
	server.SetRestartTrigger(func() {
		triggered <- struct{}{}
	})

	handler := server.Handler(http.NotFoundHandler())

	// 1. Unauthenticated request -> 401
	r1 := httptest.NewRequest(http.MethodPost, "https://mirror.example/admin/api/v1/system/restart", nil)
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, r1)
	if rec1.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for unauthenticated restart, got %d", rec1.Code)
	}

	// 2. Authenticated request with CSRF token -> 200 and triggers restart
	if err := store.CreateUser(context.Background(), "admin", "hash", "admin"); err != nil {
		t.Fatal(err)
	}
	user, err := store.UserByName(context.Background(), "admin")
	if err != nil {
		t.Fatal(err)
	}
	session, err := server.sessions.Create(user.ID, user.Username, user.Role)
	if err != nil {
		t.Fatal(err)
	}

	r2 := httptest.NewRequest(http.MethodPost, "https://mirror.example/admin/api/v1/system/restart", nil)
	r2.AddCookie(&http.Cookie{Name: "mirrorrelay_session", Value: session.ID})
	r2.Header.Set("X-CSRF-Token", session.CSRFToken)
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, r2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("expected 200 restarting, got %d: %s", rec2.Code, rec2.Body.String())
	}
	select {
	case <-triggered:
	case <-time.After(2 * time.Second):
		t.Fatal("restart trigger was not called")
	}
}
