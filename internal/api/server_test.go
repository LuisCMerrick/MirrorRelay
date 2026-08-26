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

func TestAdminAccessRejectsSpoofedClientIPFromUntrustedPeer(t *testing.T) {
	cfg := config.Default()
	cfg.Security.AdminCIDRs = []string{"203.0.113.0/24"}
	adminCIDRs, err := security.ParseCIDRs(cfg.Security.AdminCIDRs)
	if err != nil {
		t.Fatal(err)
	}
	trustedProxies, err := security.ParseCIDRs(cfg.Security.TrustedProxyCIDRs)
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{
		cfg:            cfg,
		adminCIDRs:     adminCIDRs,
		trustedProxies: trustedProxies,
		web: fstest.MapFS{
			"index.html": {Data: []byte("admin index")},
		},
	}
	handler := server.Handler(http.NotFoundHandler())

	spoofed := httptest.NewRequest(http.MethodGet, "/admin/", nil)
	spoofed.RemoteAddr = "198.51.100.20:4321"
	spoofed.Header.Set("X-Real-IP", "203.0.113.10")
	spoofedRecorder := httptest.NewRecorder()
	handler.ServeHTTP(spoofedRecorder, spoofed)
	if spoofedRecorder.Code != http.StatusForbidden {
		t.Fatalf("untrusted peer spoofed an administrative client address: status=%d", spoofedRecorder.Code)
	}

	trusted := httptest.NewRequest(http.MethodGet, "/admin/", nil)
	trusted.RemoteAddr = "127.0.0.1:4321"
	trusted.Header.Set("X-Real-IP", "203.0.113.10")
	trustedRecorder := httptest.NewRecorder()
	handler.ServeHTTP(trustedRecorder, trusted)
	if trustedRecorder.Code != http.StatusOK {
		t.Fatalf("trusted ingress address was ignored: status=%d", trustedRecorder.Code)
	}
}

func TestRequestPublicBaseDefaultsToValidatedHTTPSAuthority(t *testing.T) {
	server := &Server{cfg: config.Default()}
	request := httptest.NewRequest(http.MethodGet, "http://mirror.example/help/", nil)
	request.Header.Set("X-Forwarded-Proto", "http")
	base, err := server.requestPublicBase(request)
	if err != nil || base != "https://mirror.example" {
		t.Fatalf("public help base = %q, %v; want validated HTTPS origin", base, err)
	}

	request.Host = "unsafe.example;return"
	if _, err := server.requestPublicBase(request); err == nil {
		t.Fatal("unsafe request authority was accepted for public help URLs")
	}
}

func TestWebSettingsValidatePersistAndReset(t *testing.T) {
	store, err := database.Open(filepath.Join(t.TempDir(), "mirrorrelay.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	cfg := config.Default()
	cfg.Webhook.URL = "https://hooks.example.test/services/token-in-path?access_token=viewer-must-not-see-url"
	cfg.Webhook.Secret = "viewer-must-not-see-this"
	server := &Server{cfg: cfg, fileConfig: cfg, store: store}
	settings := config.WebSettingsFrom(cfg)
	settings.Server.UnixSocketEnabled = false
	settings.Server.LocalAddress = "127.0.0.2"
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
	if err != nil || !found || !strings.Contains(stored, `"local_address":"127.0.0.2"`) || !strings.Contains(stored, `"local_port":19081`) {
		t.Fatalf("stored settings: found=%v value=%s err=%v", found, stored, err)
	}

	getRecorder := httptest.NewRecorder()
	server.webSettings(getRecorder, httptest.NewRequest(http.MethodGet, "/admin/api/v1/settings", nil), auth.Session{Role: "admin"})
	if getRecorder.Code != http.StatusOK || !strings.Contains(getRecorder.Body.String(), `"source":"web_ui"`) {
		t.Fatalf("settings read: status=%d body=%s", getRecorder.Code, getRecorder.Body.String())
	}
	if !strings.Contains(getRecorder.Body.String(), cfg.Webhook.Secret) {
		t.Fatalf("admin settings response unexpectedly redacted webhook secret: %s", getRecorder.Body.String())
	}
	viewerRecorder := httptest.NewRecorder()
	server.webSettings(viewerRecorder, httptest.NewRequest(http.MethodGet, "/admin/api/v1/settings", nil), auth.Session{Role: "viewer"})
	if viewerRecorder.Code != http.StatusOK || strings.Contains(viewerRecorder.Body.String(), cfg.Webhook.Secret) || strings.Contains(viewerRecorder.Body.String(), "viewer-must-not-see-url") || strings.Contains(viewerRecorder.Body.String(), "token-in-path") {
		t.Fatalf("viewer settings leaked webhook credentials: status=%d body=%s", viewerRecorder.Code, viewerRecorder.Body.String())
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
	server.webSettings(before, httptest.NewRequest(http.MethodGet, "/admin/api/v1/settings", nil), auth.Session{Role: "admin"})
	if before.Code != http.StatusOK || !strings.Contains(before.Body.String(), `"source":"web_ui"`) || strings.Contains(before.Body.String(), `"restart_required":true`) {
		t.Fatalf("applied override state: status=%d body=%s", before.Code, before.Body.String())
	}

	reset := httptest.NewRecorder()
	server.resetWebSettings(reset, httptest.NewRequest(http.MethodDelete, "/admin/api/v1/settings", nil), auth.Session{Username: "admin"})
	if reset.Code != http.StatusOK || !strings.Contains(reset.Body.String(), `"restart_required":true`) || !strings.Contains(reset.Body.String(), fmt.Sprintf(`"keep_days":%d`, fileConfig.Logging.KeepDays)) {
		t.Fatalf("reset state: status=%d body=%s", reset.Code, reset.Body.String())
	}

	after := httptest.NewRecorder()
	server.webSettings(after, httptest.NewRequest(http.MethodGet, "/admin/api/v1/settings", nil), auth.Session{Role: "admin"})
	if after.Code != http.StatusOK || !strings.Contains(after.Body.String(), `"source":"configuration_file"`) || !strings.Contains(after.Body.String(), `"restart_required":true`) {
		t.Fatalf("pending YAML restart state: status=%d body=%s", after.Code, after.Body.String())
	}
}

func TestWebSettingsEqualIgnoresAppearanceAndNormalizesDurations(t *testing.T) {
	cfg := config.Default()
	left := config.WebSettingsFrom(cfg)
	right := config.WebSettingsFrom(cfg)

	// Mutate dynamic appearance config on one side
	left.UIEnhancement.Enabled = true
	left.UIEnhancement.Theme = "dark"
	left.UIEnhancement.AccentColor = "#123456"

	// Express durations in different formats
	left.HTTP.ReadTimeout = "20s"
	right.HTTP.ReadTimeout = "20000ms"
	left.HTTP.WriteTimeout = "1h"
	right.HTTP.WriteTimeout = "1h0m0s"

	// Nil vs empty slice
	left.Security.AdminCIDRs = nil
	right.Security.AdminCIDRs = []string{}

	if !webSettingsEqual(left, right) {
		t.Fatalf("expected webSettingsEqual to be true for matching operational settings despite appearance/duration differences")
	}

	// Changing an operational setting should report mismatch
	right.Logging.KeepDays += 7
	if webSettingsEqual(left, right) {
		t.Fatalf("expected webSettingsEqual to be false when operational settings differ")
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
		Slug:        "debian-security",
		ProfileName: "Debian Security",
		PublicMode:  "path",
		PublicPath:  "/debian-security/",
	}

	exDebSec := clientExamples(cfg, repoDebSec)
	if len(exDebSec) != 2 || exDebSec[0].Format != "sources.list" || exDebSec[1].Format != "deb822" {
		t.Fatalf("expected both APT formats: %+v", exDebSec)
	}
	if exDebSec[0].FilePath != "/etc/apt/sources.list.d/mirrorrelay-debian-security.list" ||
		exDebSec[1].FilePath != "/etc/apt/sources.list.d/mirrorrelay-debian-security.sources" {
		t.Fatalf("unexpected repository-scoped APT paths: %+v", exDebSec)
	}
	if !strings.Contains(exDebSec[0].Command, "https://mirror.example.com/debian-security/ bookworm-security") ||
		strings.Contains(exDebSec[0].Command, "bookworm-security-security") ||
		!strings.Contains(exDebSec[1].Command, "Suites: bookworm-security") {
		t.Fatalf("unexpected debian-security example: %+v", exDebSec)
	}
}

func TestUbuntuClientExamplesUseUbuntuKeyringInBothFormats(t *testing.T) {
	cfg := config.Default()
	cfg.HTTP.PublicBaseURL = "https://mirror.example.com"
	repository := model.Mirror{
		Name: "Ubuntu", Slug: "ubuntu", Type: "apt", ProfileName: "Ubuntu",
		PublicMode: "path", PublicPath: "/ubuntu/",
	}

	examples := clientExamples(cfg, repository)
	if len(examples) != 2 || examples[0].Format != "sources.list" || examples[1].Format != "deb822" {
		t.Fatalf("expected both Ubuntu APT formats: %+v", examples)
	}
	for _, example := range examples {
		if !strings.Contains(example.Command, "/usr/share/keyrings/ubuntu-archive-keyring.gpg") ||
			!strings.Contains(example.Command, "noble-security") ||
			strings.Contains(example.Command, "debian-archive-keyring") {
			t.Fatalf("unexpected Ubuntu APT example: %+v", example)
		}
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

func TestSettingsExportImportAndHistoryRollback(t *testing.T) {
	store, err := database.Open(filepath.Join(t.TempDir(), "mirrorrelay.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	cfg := config.Default()
	cfg.HTTP.PublicBaseURL = "https://original-host.example.com"
	cfg.Distributed.Token = "secret-token"
	server := &Server{cfg: cfg, fileConfig: cfg, store: store}

	// 1. Test Export standard vs full backup
	expRec := httptest.NewRecorder()
	server.exportSettings(expRec, httptest.NewRequest(http.MethodGet, "/admin/api/v1/settings/export", nil), auth.Session{Role: "admin", Username: "admin"})
	if expRec.Code != http.StatusOK {
		t.Fatalf("export standard failed: code=%d body=%s", expRec.Code, expRec.Body.String())
	}
	if strings.Contains(expRec.Body.String(), "secret-token") || strings.Contains(expRec.Body.String(), "original-host.example.com") {
		t.Fatal("standard export contained secret or public base URL")
	}

	expFullRec := httptest.NewRecorder()
	server.exportSettings(expFullRec, httptest.NewRequest(http.MethodGet, "/admin/api/v1/settings/export?full_backup=true", nil), auth.Session{Role: "admin", Username: "admin"})
	if expFullRec.Code != http.StatusOK || !strings.Contains(expFullRec.Body.String(), "secret-token") {
		t.Fatalf("full export failed to include token: code=%d body=%s", expFullRec.Code, expFullRec.Body.String())
	}

	// 2. Test Import Preview
	importYAML := `
server:
  local_port: 19088
security:
  login_max_failures: 9
`
	previewPayload, _ := json.Marshal(map[string]string{"yaml": importYAML})
	prevRec := httptest.NewRecorder()
	server.previewImportSettings(prevRec, httptest.NewRequest(http.MethodPost, "/admin/api/v1/settings/import/preview", bytes.NewReader(previewPayload)), auth.Session{Role: "admin", Username: "admin"})
	if prevRec.Code != http.StatusOK {
		t.Fatalf("import preview failed: code=%d body=%s", prevRec.Code, prevRec.Body.String())
	}
	var previewRes struct {
		Valid           bool                     `json:"valid"`
		Diff            []model.SettingDiffEntry `json:"diff"`
		RestartRequired bool                     `json:"restart_required"`
	}
	if err := json.Unmarshal(prevRec.Body.Bytes(), &previewRes); err != nil || !previewRes.Valid || len(previewRes.Diff) == 0 {
		t.Fatalf("invalid preview result: %+v, err=%v", previewRes, err)
	}

	// 3. Test Apply Import
	applyRec := httptest.NewRecorder()
	server.applyImportSettings(applyRec, httptest.NewRequest(http.MethodPost, "/admin/api/v1/settings/import", bytes.NewReader(previewPayload)), auth.Session{Role: "admin", Username: "admin"})
	if applyRec.Code != http.StatusOK {
		t.Fatalf("apply import failed: code=%d body=%s", applyRec.Code, applyRec.Body.String())
	}

	// Verify imported settings in DB preserved public base URL and secrets
	stored, found, err := store.Setting(context.Background(), config.WebSettingsKey)
	if err != nil || !found {
		t.Fatalf("stored setting not found after import: %v", err)
	}
	ws, err := config.DecodeWebSettings([]byte(stored))
	if err != nil {
		t.Fatalf("decode stored settings failed: %v", err)
	}
	if ws.Server.LocalPort != 19088 || ws.Security.LoginMaxFailures != 9 {
		t.Fatalf("imported fields were not applied: %+v", ws)
	}
	if ws.HTTP.PublicBaseURL != "https://original-host.example.com" {
		t.Fatalf("local instance base URL was wiped on import: %s", ws.HTTP.PublicBaseURL)
	}

	// 4. Test History and Rollback
	histRec := httptest.NewRecorder()
	server.listSettingsHistory(histRec, httptest.NewRequest(http.MethodGet, "/admin/api/v1/settings/history", nil), auth.Session{Role: "admin", Username: "admin"})
	if histRec.Code != http.StatusOK {
		t.Fatalf("history failed: code=%d body=%s", histRec.Code, histRec.Body.String())
	}
	var versions []model.SettingVersion
	if err := json.Unmarshal(histRec.Body.Bytes(), &versions); err != nil || len(versions) == 0 {
		t.Fatalf("expected version records: %+v, err=%v", versions, err)
	}

	// Make another change
	ws.Server.LocalPort = 19099
	encodedWS, _ := json.Marshal(ws)
	updRec := httptest.NewRecorder()
	server.updateWebSettings(updRec, httptest.NewRequest(http.MethodPut, "/admin/api/v1/settings", bytes.NewReader(encodedWS)), auth.Session{Role: "admin", Username: "admin"})
	if updRec.Code != http.StatusOK {
		t.Fatalf("update settings failed: %s", updRec.Body.String())
	}

	// Rollback to version 1 (which was the import)
	rbRec := httptest.NewRecorder()
	server.rollbackSettingsHistory(rbRec, httptest.NewRequest(http.MethodPost, "/admin/api/v1/settings/history/1/rollback", nil), auth.Session{Role: "admin", Username: "admin"}, "1")
	if rbRec.Code != http.StatusOK {
		t.Fatalf("rollback failed: code=%d body=%s", rbRec.Code, rbRec.Body.String())
	}

	// Check that port was rolled back to 19088
	storedAfterRB, _, _ := store.Setting(context.Background(), config.WebSettingsKey)
	wsAfterRB, _ := config.DecodeWebSettings([]byte(storedAfterRB))
	if wsAfterRB.Server.LocalPort != 19088 {
		t.Fatalf("rollback did not restore previous value: got port %d", wsAfterRB.Server.LocalPort)
	}
}

func TestPublicRepositoryIndexAndGitHubLinks(t *testing.T) {
	cfg := config.Default()
	registry := mirror.NewRegistry(nil)
	registry.Replace([]model.Mirror{
		{
			ID:           1,
			Name:         "Ubuntu",
			Slug:         "ubuntu",
			Type:         "apt",
			PublicPath:   "/ubuntu/",
			Description:  "Ubuntu Archive Mirror",
			Enabled:      true,
			AccessPolicy: "public",
		},
	})

	srv := &Server{
		cfg:      cfg,
		registry: registry,
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	srv.repositoryIndex(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "https://github.com/LuisCMerrick/MirrorRelay") {
		t.Fatalf("expected GitHub link in repository index HTML, got:\n%s", body)
	}
	if !strings.Contains(body, "theme-select") {
		t.Fatalf("expected custom theme switcher in repository index HTML, got:\n%s", body)
	}
	if !strings.Contains(body, "Ubuntu") || !strings.Contains(body, "/ubuntu/") {
		t.Fatalf("expected Ubuntu repository listing in HTML, got:\n%s", body)
	}
}
