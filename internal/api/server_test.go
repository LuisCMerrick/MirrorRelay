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
	"github.com/LuisCMerrick/RepoGate/internal/cluster"
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

	// 4. Request to /metrics from unauthorized IP -> should return 403 Forbidden
	r4 := httptest.NewRequest(http.MethodGet, "https://admin.example.com/metrics", nil)
	r4.RemoteAddr = "192.168.1.100:1234"
	rec4 := httptest.NewRecorder()
	handler.ServeHTTP(rec4, r4)
	if rec4.Code != http.StatusForbidden {
		t.Fatalf("/metrics from non-admin CIDR should return 403, got %d", rec4.Code)
	}
}

func TestDebianSecurityClientExamples(t *testing.T) {
	cfg := config.Default()
	cfg.HTTP.PublicBaseURL = "https://mirror.example.com"

	debian := model.Mirror{Type: "apt", ProfileName: "Debian", PublicPath: "/debian/"}
	exDebian := clientExamples(cfg, debian)
	if len(exDebian) == 0 || !strings.Contains(exDebian[0].Command, "bookworm main") {
		t.Fatalf("unexpected debian example: %+v", exDebian)
	}

	debSec := model.Mirror{Type: "apt", ProfileName: "Debian Security", PublicPath: "/debian-security/"}
	exDebSec := clientExamples(cfg, debSec)
	if len(exDebSec) == 0 || !strings.Contains(exDebSec[0].Command, "bookworm-security") {
		t.Fatalf("unexpected debian-security example: %+v", exDebSec)
	}
}

func TestClusterManifestAndHealthEndpoints(t *testing.T) {
	cfg := config.Default()
	cfg.Distributed.Enabled = true
	cfg.Distributed.Role = "edge"
	cfg.Distributed.Token = "secret-token-123"
	cfg.Distributed.Node.Name = "tokyo-01"

	registry := mirror.NewRegistry(nil)
	registry.Replace([]model.Mirror{
		{ID: 1, Name: "Debian", Slug: "debian", Type: "apt", Enabled: true, PublicPath: "/debian/"},
	})

	server := &Server{
		cfg:      cfg,
		registry: registry,
		web:      fstest.MapFS{"index.html": {Data: []byte("admin index")}},
	}
	handler := server.Handler(http.NotFoundHandler())

	// 1. Unauthenticated request to /api/v1/cluster/manifest -> 401
	r1 := httptest.NewRequest(http.MethodGet, "/api/v1/cluster/manifest", nil)
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, r1)
	if rec1.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for unauthenticated manifest request, got %d", rec1.Code)
	}

	// 2. Authenticated request with header -> 200
	r2 := httptest.NewRequest(http.MethodGet, "/api/v1/cluster/manifest", nil)
	r2.Header.Set("X-RepoGate-Cluster-Token", "secret-token-123")
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, r2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("expected 200 for authenticated manifest request, got %d", rec2.Code)
	}
	var manifest model.ClusterManifest
	if err := json.NewDecoder(rec2.Body).Decode(&manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.NodeID != "tokyo-01" || manifest.ProtocolVersion != 1 || len(manifest.Capabilities) != 1 || manifest.Capabilities[0] != "apt" {
		t.Fatalf("unexpected manifest: %+v", manifest)
	}

	// 3. Authenticated request to /api/v1/cluster/health -> 200
	r3 := httptest.NewRequest(http.MethodGet, "/api/v1/cluster/health", nil)
	r3.Header.Set("Authorization", "Bearer secret-token-123")
	rec3 := httptest.NewRecorder()
	handler.ServeHTTP(rec3, r3)
	if rec3.Code != http.StatusOK {
		t.Fatalf("expected 200 for authenticated health request, got %d", rec3.Code)
	}
}

func TestCoordinatorDistributed307Redirect(t *testing.T) {
	cfg := config.Default()
	cfg.Distributed.Enabled = true
	cfg.Distributed.Role = "coordinator"
	cfg.Distributed.Routing.ClientNetworks = []config.ClientNetworkMapping{
		{CIDR: "10.20.0.0/16", Region: "jp-tokyo"},
	}

	registry := mirror.NewRegistry(nil)
	registry.Replace([]model.Mirror{
		{ID: 1, Name: "Debian", Slug: "debian", Type: "apt", Enabled: true, PublicPath: "/debian/"},
		{ID: 2, Name: "Docker Hub", Slug: "docker", Type: "docker-registry", Enabled: true, PublicPath: "/docker/"},
	})

	router := cluster.NewRouter(cfg)
	fp := cluster.CanonicalFingerprint(registry.List())
	router.SetNodes([]model.ClusterNode{
		{
			ID:                1,
			Name:              "tokyo-01",
			URL:               "https://jp.repo.example.com",
			Region:            "jp-tokyo",
			Priority:          100,
			Weight:            100,
			Enabled:           true,
			HealthStatus:      "healthy",
			ConfigStatus:      "match",
			ConfigFingerprint: fp,
			ProtocolVersion:   1,
			Capabilities:      []string{"apt"},
		},
	})

	metrics := cluster.NewMetrics()
	checker := cluster.NewChecker(cfg, nil, router, metrics, nil)
	_ = checker.SetClusterFingerprint(context.Background(), fp)

	server := &Server{
		cfg:            cfg,
		registry:       registry,
		clusterRouter:  router,
		clusterChecker: checker,
		clusterMetrics: metrics,
		web:            fstest.MapFS{"index.html": {Data: []byte("admin index")}},
	}
	handler := server.Handler(http.NotFoundHandler())

	// 1. Client request to /debian/dists/bookworm/InRelease?arch=amd64 from Tokyo CIDR 10.20.1.5 -> 307 to Tokyo Edge
	r1 := httptest.NewRequest(http.MethodGet, "https://repo.example.com/debian/dists/bookworm/InRelease?arch=amd64", nil)
	r1.RemoteAddr = "10.20.1.5:1234"
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, r1)

	if rec1.Code != http.StatusTemporaryRedirect {
		t.Fatalf("expected HTTP 307 Temporary Redirect, got %d; body=%s", rec1.Code, rec1.Body.String())
	}
	loc := rec1.Header().Get("Location")
	expectedLoc := "https://jp.repo.example.com/debian/dists/bookworm/InRelease?arch=amd64"
	if loc != expectedLoc {
		t.Fatalf("Location mismatch: got %q, want %q", loc, expectedLoc)
	}

	// 2. Client request for docker-registry -> 501 / Not implemented
	r2 := httptest.NewRequest(http.MethodGet, "https://repo.example.com/docker/v2/", nil)
	r2.RemoteAddr = "10.20.1.5:1234"
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, r2)
	if rec2.Code != http.StatusNotImplemented {
		t.Fatalf("expected 501 for distributed docker registry, got %d", rec2.Code)
	}

	// 3. When no healthy nodes exist -> 503 Service Unavailable
	router.SetNodes([]model.ClusterNode{})
	r3 := httptest.NewRequest(http.MethodGet, "https://repo.example.com/debian/dists/bookworm/InRelease", nil)
	r3.RemoteAddr = "10.20.1.5:1234"
	rec3 := httptest.NewRecorder()
	handler.ServeHTTP(rec3, r3)
	if rec3.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 when no healthy edge is available, got %d", rec3.Code)
	}
}
