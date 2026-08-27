package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LuisCMerrick/MirrorRelay/internal/auth"
	"github.com/LuisCMerrick/MirrorRelay/internal/config"
	"github.com/LuisCMerrick/MirrorRelay/internal/database"
	"github.com/LuisCMerrick/MirrorRelay/internal/mirror"
	"github.com/LuisCMerrick/MirrorRelay/internal/model"
)

func TestAppearanceAPIAndSafeMode(t *testing.T) {
	store, err := database.Open(filepath.Join(t.TempDir(), "mirrorrelay.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	cfg := config.Default()
	server := &Server{
		cfg:        cfg,
		fileConfig: cfg,
		store:      store,
		sessions:   auth.NewSessionsWithPath(store, time.Hour, "/admin/"),
	}
	handler := server.Handler(http.NotFoundHandler())

	// Appearance used by the sign-in page is intentionally available before
	// authentication, but it excludes local custom-CSS paths and controls.
	publicAppearance := httptest.NewRecorder()
	handler.ServeHTTP(publicAppearance, httptest.NewRequest(http.MethodGet, "https://mirror.example/admin/api/v1/auth/appearance", nil))
	if publicAppearance.Code != http.StatusOK || strings.Contains(publicAppearance.Body.String(), "custom_css") {
		t.Fatalf("unexpected public appearance response: code=%d body=%s", publicAppearance.Code, publicAppearance.Body.String())
	}
	publicAppearancePost := httptest.NewRecorder()
	handler.ServeHTTP(publicAppearancePost, httptest.NewRequest(http.MethodPost, "https://mirror.example/admin/api/v1/auth/appearance", nil))
	if publicAppearancePost.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected public appearance POST to fail with 405, got %d", publicAppearancePost.Code)
	}

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

	// 1. GET /admin/api/v1/appearance
	rGet := httptest.NewRequest(http.MethodGet, "https://mirror.example/admin/api/v1/appearance", nil)
	rGet.AddCookie(&http.Cookie{Name: "mirrorrelay_session", Value: session.ID})
	recGet := httptest.NewRecorder()
	handler.ServeHTTP(recGet, rGet)
	if recGet.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", recGet.Code, recGet.Body.String())
	}

	// 2. GET /admin/api/v1/help/templates
	rTpl := httptest.NewRequest(http.MethodGet, "https://mirror.example/admin/api/v1/help/templates", nil)
	rTpl.AddCookie(&http.Cookie{Name: "mirrorrelay_session", Value: session.ID})
	recTpl := httptest.NewRecorder()
	handler.ServeHTTP(recTpl, rTpl)
	if recTpl.Code != http.StatusOK || !strings.Contains(recTpl.Body.String(), "debian") {
		t.Fatalf("expected templates list with debian, got %d: %s", recTpl.Code, recTpl.Body.String())
	}

	// 3. PUT /admin/api/v1/appearance
	newApp := model.UIEnhancementConfig{
		Enabled:           true,
		Theme:             "dark",
		AccentColor:       "#10b981",
		Branding:          model.BrandingConfig{Title: "MyMirror"},
		Login:             model.LoginBrandingConfig{Title: "MyMirror Login"},
		RepositoryBrowser: model.RepositoryBrowserConfig{Enabled: true},
	}
	bodyBytes, _ := json.Marshal(newApp)
	rPut := httptest.NewRequest(http.MethodPut, "https://mirror.example/admin/api/v1/appearance", bytes.NewReader(bodyBytes))
	rPut.AddCookie(&http.Cookie{Name: "mirrorrelay_session", Value: session.ID})
	rPut.Header.Set("X-CSRF-Token", session.CSRFToken)
	recPut := httptest.NewRecorder()
	handler.ServeHTTP(recPut, rPut)
	if recPut.Code != http.StatusOK {
		t.Fatalf("expected 200 PUT appearance, got %d: %s", recPut.Code, recPut.Body.String())
	}
	activeAppearance := server.appearanceConfig()
	if activeAppearance.Theme != "dark" || activeAppearance.Branding.Title != "MyMirror" {
		t.Fatalf("server appearance snapshot not updated: %+v", activeAppearance)
	}

	// 4. POST /admin/api/v1/appearance/reset
	rReset := httptest.NewRequest(http.MethodPost, "https://mirror.example/admin/api/v1/appearance/reset", nil)
	rReset.AddCookie(&http.Cookie{Name: "mirrorrelay_session", Value: session.ID})
	rReset.Header.Set("X-CSRF-Token", session.CSRFToken)
	recReset := httptest.NewRecorder()
	handler.ServeHTTP(recReset, rReset)
	if recReset.Code != http.StatusOK {
		t.Fatalf("expected 200 POST appearance reset, got %d: %s", recReset.Code, recReset.Body.String())
	}
	if activeAppearance = server.appearanceConfig(); activeAppearance.Enabled {
		t.Fatalf("expected reset to disabled, got: %+v", activeAppearance)
	}
}

func TestPublicHelpAndStaticRoutes(t *testing.T) {
	cfg := config.Default()
	cfg.HTTP.PublicBaseURL = "https://mirror.example.com"
	reg := mirror.NewRegistry(nil)
	reg.Replace([]model.Mirror{
		{
			ID:         1,
			Name:       "Debian",
			Slug:       "debian",
			Type:       "apt",
			Enabled:    true,
			PublicMode: "path",
			PublicPath: "/debian/",
			Upstreams:  []model.Upstream{{URL: "https://deb.debian.org/debian/", Enabled: true}},
			Help: model.HelpConfig{
				Enabled:  true,
				Template: "debian",
				Title:    "Debian",
				Summary:  "Debian APT mirror",
			},
		},
		{
			ID:         2,
			Name:       "Private Repo",
			Slug:       "private-repo",
			Type:       "generic",
			Enabled:    true,
			PublicMode: "path",
			PublicPath: "/private-repo/",
			Help: model.HelpConfig{
				Enabled: false,
			},
		},
	})

	server := &Server{
		cfg:      cfg,
		registry: reg,
	}
	handler := server.Handler(http.NotFoundHandler())

	// 1. GET /help/ -> overview
	recHelp := httptest.NewRecorder()
	handler.ServeHTTP(recHelp, httptest.NewRequest(http.MethodGet, "https://mirror.example.com/help/", nil))
	if recHelp.Code != http.StatusOK || !strings.Contains(recHelp.Body.String(), "Debian") {
		t.Fatalf("expected 200 help overview containing Debian, got %d: %s", recHelp.Code, recHelp.Body.String())
	}

	// 2. GET /help/debian/ -> detail
	recDeb := httptest.NewRecorder()
	handler.ServeHTTP(recDeb, httptest.NewRequest(http.MethodGet, "https://mirror.example.com/help/debian/", nil))
	if recDeb.Code != http.StatusOK || !strings.Contains(recDeb.Body.String(), "mirror.example.com/debian") {
		t.Fatalf("expected 200 debian detail, got %d: %s", recDeb.Code, recDeb.Body.String())
	}

	// 3. GET /help/private-repo/ -> 404 (help disabled)
	recPriv := httptest.NewRecorder()
	handler.ServeHTTP(recPriv, httptest.NewRequest(http.MethodGet, "https://mirror.example.com/help/private-repo/", nil))
	if recPriv.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for repo without help, got %d", recPriv.Code)
	}

	// 4. GET /ui/icons/folder.svg
	recIcon := httptest.NewRecorder()
	handler.ServeHTTP(recIcon, httptest.NewRequest(http.MethodGet, "https://mirror.example.com/ui/icons/folder.svg", nil))
	if recIcon.Code != http.StatusOK || recIcon.Header().Get("Content-Type") != "image/svg+xml" {
		t.Fatalf("expected 200 SVG icon, got %d Content-Type=%q", recIcon.Code, recIcon.Header().Get("Content-Type"))
	}
}

func TestPublicUIRoutesAreBoundedAndReservedForHostRepositories(t *testing.T) {
	cssPath := filepath.Join(t.TempDir(), "custom.css")
	if err := os.WriteFile(cssPath, []byte("body { color: red; }"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.HTTP.PublicBaseURL = "https://mirror.example.com"
	cfg.UIEnhancement.Enabled = true
	cfg.UIEnhancement.CustomCSS = model.CustomCSSConfig{Enabled: true, File: cssPath}
	reg := mirror.NewRegistry(nil)
	reg.Replace([]model.Mirror{{
		ID: 1, Name: "Host Repo", Slug: "host-repo", Type: "generic", Enabled: true,
		PublicMode: "host", PublicHost: "packages.example.com",
		Help: model.HelpConfig{Enabled: true, Template: "generic"},
	}})
	server := &Server{cfg: cfg, registry: reg}
	handler := server.Handler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "origin", http.StatusTeapot)
	}))

	cssHead := httptest.NewRecorder()
	handler.ServeHTTP(cssHead, httptest.NewRequest(http.MethodHead, "https://packages.example.com/ui/custom.css", nil))
	if cssHead.Code != http.StatusOK || cssHead.Body.Len() != 0 || cssHead.Header().Get("Content-Length") == "" {
		t.Fatalf("expected bounded host-route CSS HEAD response, got code=%d length=%q body=%q", cssHead.Code, cssHead.Header().Get("Content-Length"), cssHead.Body.String())
	}

	cssPost := httptest.NewRecorder()
	handler.ServeHTTP(cssPost, httptest.NewRequest(http.MethodPost, "https://packages.example.com/ui/custom.css", nil))
	if cssPost.Code != http.StatusMethodNotAllowed || cssPost.Header().Get("Allow") != "GET, HEAD" {
		t.Fatalf("expected CSS POST to be rejected, got code=%d Allow=%q", cssPost.Code, cssPost.Header().Get("Allow"))
	}

	helpRec := httptest.NewRecorder()
	handler.ServeHTTP(helpRec, httptest.NewRequest(http.MethodGet, "https://packages.example.com/help/", nil))
	if helpRec.Code != http.StatusOK || strings.Contains(helpRec.Body.String(), "origin") || helpRec.Header().Get("Content-Security-Policy") == "" {
		t.Fatalf("expected help to remain an instance route on a host repository, got code=%d body=%q", helpRec.Code, helpRec.Body.String())
	}
	helpPost := httptest.NewRecorder()
	handler.ServeHTTP(helpPost, httptest.NewRequest(http.MethodPost, "https://packages.example.com/help/", nil))
	if helpPost.Code != http.StatusMethodNotAllowed || helpPost.Header().Get("Allow") != "GET, HEAD" {
		t.Fatalf("expected help POST to be rejected, got code=%d Allow=%q", helpPost.Code, helpPost.Header().Get("Allow"))
	}

	symlinkPath := filepath.Join(filepath.Dir(cssPath), "linked.css")
	if err := os.Symlink(cssPath, symlinkPath); err != nil {
		t.Fatal(err)
	}
	symlinkAppearance := server.appearanceConfig()
	symlinkAppearance.CustomCSS.File = symlinkPath
	server.appearance.Store(symlinkAppearance)
	symlinkCSS := httptest.NewRecorder()
	handler.ServeHTTP(symlinkCSS, httptest.NewRequest(http.MethodGet, "https://packages.example.com/ui/custom.css", nil))
	if symlinkCSS.Code != http.StatusNotFound {
		t.Fatalf("expected symlinked custom CSS to be rejected, got %d", symlinkCSS.Code)
	}
	symlinkAppearance.CustomCSS.File = cssPath
	server.appearance.Store(symlinkAppearance)

	if err := os.WriteFile(cssPath, bytes.Repeat([]byte("x"), (1<<20)+1), 0o600); err != nil {
		t.Fatal(err)
	}
	tooLarge := httptest.NewRecorder()
	handler.ServeHTTP(tooLarge, httptest.NewRequest(http.MethodGet, "https://packages.example.com/ui/custom.css", nil))
	if tooLarge.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected oversized CSS to be rejected, got %d", tooLarge.Code)
	}
}
