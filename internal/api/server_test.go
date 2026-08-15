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

	"github.com/LuisCMerrick/RepoGate/internal/auth"
	"github.com/LuisCMerrick/RepoGate/internal/config"
	"github.com/LuisCMerrick/RepoGate/internal/database"
	"github.com/LuisCMerrick/RepoGate/internal/mirror"
	"github.com/LuisCMerrick/RepoGate/internal/model"
	"github.com/LuisCMerrick/RepoGate/internal/security"
)

func TestWebHandlerServesConfiguredAdminIndexWithoutRedirect(t *testing.T) {
	cfg := config.Default()
	cfg.Admin.Path = "/private-console/"
	s := &Server{cfg: cfg, web: fstest.MapFS{
		"index.html": {Data: []byte("admin index")},
		"app.js":     {Data: []byte("console.log('ok')")},
	}}

	tests := []struct {
		path string
		body string
	}{
		{path: "/private-console/", body: "admin index"},
		{path: "/private-console/app.js", body: "console.log('ok')"},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			s.webHandler(recorder, httptest.NewRequest(http.MethodGet, tt.path, nil))
			response := recorder.Result()
			defer response.Body.Close()
			if response.StatusCode != http.StatusOK {
				t.Fatalf("status = %d, want %d; Location=%q", response.StatusCode, http.StatusOK, response.Header.Get("Location"))
			}
			if recorder.Body.String() != tt.body {
				t.Fatalf("body = %q, want %q", recorder.Body.String(), tt.body)
			}
		})
	}
}

func TestHandlerScopesUIAndAPIUnderConfiguredAdminPath(t *testing.T) {
	cfg := config.Default()
	cfg.Admin.Path = "/private-console/"
	server := &Server{
		cfg:      cfg,
		web:      fstest.MapFS{"index.html": {Data: []byte("admin index")}},
		sessions: auth.NewSessions(nil, time.Hour),
	}
	handler := server.Handler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("proxied"))
	}))

	ui := httptest.NewRecorder()
	handler.ServeHTTP(ui, httptest.NewRequest(http.MethodGet, "https://mirror.example/private-console/", nil))
	if ui.Code != http.StatusOK || ui.Body.String() != "admin index" {
		t.Fatalf("configured UI route: status=%d body=%q", ui.Code, ui.Body.String())
	}

	api := httptest.NewRecorder()
	handler.ServeHTTP(api, httptest.NewRequest(http.MethodGet, "https://mirror.example/private-console/api/v1/auth/session", nil))
	if api.Code != http.StatusUnauthorized {
		t.Fatalf("configured API route status = %d, want %d", api.Code, http.StatusUnauthorized)
	}

	legacy := httptest.NewRecorder()
	handler.ServeHTTP(legacy, httptest.NewRequest(http.MethodGet, "https://mirror.example/api/v1/auth/session", nil))
	if legacy.Body.String() != "proxied" {
		t.Fatalf("unconfigured legacy API route was retained: status=%d body=%q", legacy.Code, legacy.Body.String())
	}
}

func TestWebSettingsValidatePersistAndReset(t *testing.T) {
	store, err := database.Open(filepath.Join(t.TempDir(), "repogate.db"))
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
	store, err := database.Open(filepath.Join(t.TempDir(), "repogate.db"))
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
	tests := []struct {
		name      string
		upstreams []model.Upstream
		want      string
	}{
		{name: "healthy backup", upstreams: []model.Upstream{{Enabled: true, HealthStatus: "unhealthy"}, {Enabled: true, HealthStatus: "healthy"}}, want: "healthy"},
		{name: "unknown remains viable", upstreams: []model.Upstream{{Enabled: true, HealthStatus: "unhealthy"}, {Enabled: true, HealthStatus: "unknown"}}, want: "unknown"},
		{name: "all unhealthy", upstreams: []model.Upstream{{Enabled: true, HealthStatus: "unhealthy"}}, want: "unhealthy"},
		{name: "no enabled upstream", upstreams: []model.Upstream{{Enabled: false, HealthStatus: "healthy"}}, want: "unknown"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := repositoryHealthState(model.Mirror{Upstreams: test.upstreams}); got != test.want {
				t.Fatalf("state = %q, want %q", got, test.want)
			}
		})
	}
}

func TestPublicRootListsRepositoriesAndPreservesHostRoutes(t *testing.T) {
	registry := mirror.NewRegistry(nil)
	registry.Replace([]model.Mirror{
		{ID: 1, Name: "Debian & Stable", Slug: "debian", Type: "apt", Enabled: true, PublicMode: "path", PublicPath: "/debian/", Description: "Public <mirror>"},
		{ID: 2, Name: "Private", Slug: "private", Type: "generic", Enabled: true, PublicMode: "path", PublicPath: "/private/", AccessPolicy: "admin"},
		{ID: 3, Name: "Registry", Slug: "registry", Type: "oci-registry", Enabled: true, PublicMode: "host", PublicHost: "registry.example.com"},
	})
	adminCIDRs, err := security.ParseCIDRs([]string{"10.0.0.0/8"})
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{registry: registry, adminCIDRs: adminCIDRs}
	proxy := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("proxied"))
	})
	handler := server.publicHandler(proxy)

	request := httptest.NewRequest(http.MethodGet, "https://mirror.example.com/", nil)
	request.RemoteAddr = "192.0.2.10:1234"
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	body := recorder.Body.String()
	if recorder.Code != http.StatusOK || !strings.Contains(body, "Debian &amp; Stable") || !strings.Contains(body, "/debian/") || !strings.Contains(body, "registry.example.com/") {
		t.Fatalf("unexpected repository index: status=%d body=%s", recorder.Code, body)
	}
	if strings.Contains(body, "/private/") || strings.Contains(body, "<mirror>") {
		t.Fatalf("repository index disclosed or failed to escape content: %s", body)
	}
	if strings.Contains(body, "/admin/") || strings.Contains(body, "Administration") {
		t.Fatalf("repository index disclosed the administration path: %s", body)
	}

	hostRequest := httptest.NewRequest(http.MethodGet, "https://registry.example.com/", nil)
	hostRecorder := httptest.NewRecorder()
	handler.ServeHTTP(hostRecorder, hostRequest)
	if hostRecorder.Body.String() != "proxied" {
		t.Fatalf("host-mode root was not proxied: %q", hostRecorder.Body.String())
	}
}
