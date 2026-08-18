package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/LuisCMerrick/MirrorRelay/internal/buildinfo"
	"github.com/LuisCMerrick/MirrorRelay/internal/cachectl"
	"github.com/LuisCMerrick/MirrorRelay/internal/config"
	"github.com/LuisCMerrick/MirrorRelay/internal/database"
	"github.com/LuisCMerrick/MirrorRelay/internal/health"
	"github.com/LuisCMerrick/MirrorRelay/internal/mirror"
	"github.com/LuisCMerrick/MirrorRelay/internal/model"
	"github.com/LuisCMerrick/MirrorRelay/internal/stats"
	"github.com/LuisCMerrick/MirrorRelay/internal/upstreamnginx"
)

type dummyFS struct{}

func (dummyFS) Open(name string) (fs.File, error) { return nil, fs.ErrNotExist }

func TestRBACPermissionsAndWebhookTestEndpoint(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "mirrorrelay.db")
	store, err := database.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	webhookServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer webhookServer.Close()

	cfg := config.Default()
	cfg.Security.AdminCIDRs = []string{"0.0.0.0/0", "::/0"}
	cfg.Webhook = model.WebhookConfig{
		Enabled: true,
		URL:     webhookServer.URL,
		Timeout: 2 * time.Second,
	}

	registry := mirror.NewRegistry(store)
	cacheManager := cachectl.New(cfg, store)
	metric := stats.New()
	checker := health.New(cfg, store, registry)
	upstreamNginx := upstreamnginx.NewController(cfg, store)

	srv, err := New(cfg, cfg, store, registry, cacheManager, metric, checker, upstreamNginx, dummyFS{}, buildinfo.Info{Version: "0.0.5"})
	if err != nil {
		t.Fatal(err)
	}
	handler := srv.Handler(http.NotFoundHandler())

	ctx := context.Background()
	// Create admin, operator, viewer users
	if err := store.CreateUser(ctx, "u_admin", "hash", "admin"); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateUser(ctx, "u_operator", "hash", "operator"); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateUser(ctx, "u_viewer", "hash", "viewer"); err != nil {
		t.Fatal(err)
	}

	adminUser, _ := store.UserByName(ctx, "u_admin")
	operatorUser, _ := store.UserByName(ctx, "u_operator")
	viewerUser, _ := store.UserByName(ctx, "u_viewer")

	adminSession, _ := srv.sessions.Create(adminUser.ID, adminUser.Username, adminUser.Role)
	operatorSession, _ := srv.sessions.Create(operatorUser.ID, operatorUser.Username, operatorUser.Role)
	viewerSession, _ := srv.sessions.Create(viewerUser.ID, viewerUser.Username, viewerUser.Role)

	// 1. Viewer trying to create repository -> 403 Forbidden
	mirrorReq, _ := json.Marshal(map[string]any{
		"name": "Debian", "slug": "debian", "type": "apt",
		"upstreams": []map[string]any{{"url": "https://deb.debian.org/debian"}},
	})
	rViewer := httptest.NewRequest(http.MethodPost, "https://mirror.example/admin/api/v1/mirrors", bytes.NewReader(mirrorReq))
	rViewer.AddCookie(&http.Cookie{Name: "mirrorrelay_session", Value: viewerSession.ID})
	rViewer.Header.Set("X-CSRF-Token", viewerSession.CSRFToken)
	wViewer := httptest.NewRecorder()
	handler.ServeHTTP(wViewer, rViewer)
	if wViewer.Code != http.StatusForbidden {
		t.Fatalf("viewer should be forbidden from creating mirror, got %d", wViewer.Code)
	}

	// 2. Operator trying to create user -> 403 Forbidden
	userReq, _ := json.Marshal(map[string]any{
		"username": "new_user", "password": "password12345", "role": "viewer",
	})
	rOperator := httptest.NewRequest(http.MethodPost, "https://mirror.example/admin/api/v1/users", bytes.NewReader(userReq))
	rOperator.AddCookie(&http.Cookie{Name: "mirrorrelay_session", Value: operatorSession.ID})
	rOperator.Header.Set("X-CSRF-Token", operatorSession.CSRFToken)
	wOperator := httptest.NewRecorder()
	handler.ServeHTTP(wOperator, rOperator)
	if wOperator.Code != http.StatusForbidden {
		t.Fatalf("operator should be forbidden from creating users, got %d", wOperator.Code)
	}

	// 3. Admin creating user with role -> 201 Created
	rAdmin := httptest.NewRequest(http.MethodPost, "https://mirror.example/admin/api/v1/users", bytes.NewReader(userReq))
	rAdmin.AddCookie(&http.Cookie{Name: "mirrorrelay_session", Value: adminSession.ID})
	rAdmin.Header.Set("X-CSRF-Token", adminSession.CSRFToken)
	wAdmin := httptest.NewRecorder()
	handler.ServeHTTP(wAdmin, rAdmin)
	if wAdmin.Code != http.StatusCreated {
		t.Fatalf("admin should be able to create users, got %d: %s", wAdmin.Code, wAdmin.Body.String())
	}

	// 4. Admin testing webhook endpoint -> 200 OK
	testWebhookReq, _ := json.Marshal(map[string]any{
		"url": webhookServer.URL,
	})
	rWebhook := httptest.NewRequest(http.MethodPost, "https://mirror.example/admin/api/v1/webhooks/test", bytes.NewReader(testWebhookReq))
	rWebhook.AddCookie(&http.Cookie{Name: "mirrorrelay_session", Value: adminSession.ID})
	rWebhook.Header.Set("X-CSRF-Token", adminSession.CSRFToken)
	wWebhook := httptest.NewRecorder()
	handler.ServeHTTP(wWebhook, rWebhook)
	if wWebhook.Code != http.StatusOK {
		t.Fatalf("webhook test should succeed, got %d: %s", wWebhook.Code, wWebhook.Body.String())
	}
}
