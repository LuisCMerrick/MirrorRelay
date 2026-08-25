package api

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
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

	var configuredRequests atomic.Int64
	webhookServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		configuredRequests.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer webhookServer.Close()
	var overrideRequests atomic.Int64
	var overrideSignatureOK atomic.Bool
	var overrideUnsigned atomic.Bool
	overrideServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		overrideRequests.Add(1)
		body, _ := io.ReadAll(r.Body)
		mac := hmac.New(sha256.New, []byte("override-secret"))
		_, _ = mac.Write(body)
		signature := r.Header.Get("X-MirrorRelay-Signature")
		if signature == "" {
			overrideUnsigned.Store(true)
		} else if hmac.Equal([]byte(signature), []byte(fmt.Sprintf("sha256=%x", mac.Sum(nil)))) {
			overrideSignatureOK.Store(true)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer overrideServer.Close()

	cfg := config.Default()
	cfg.Security.AdminCIDRs = []string{"0.0.0.0/0", "::/0"}
	cfg.Webhook = model.WebhookConfig{
		Enabled:      true,
		URL:          webhookServer.URL,
		Secret:       "configured-secret",
		Timeout:      2 * time.Second,
		AllowHTTP:    true,
		AllowPrivate: true,
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

	viewerNginxTest := httptest.NewRequest(http.MethodPost, "https://mirror.example/admin/api/v1/upstream-nginx/test", nil)
	viewerNginxTest.AddCookie(&http.Cookie{Name: "mirrorrelay_session", Value: viewerSession.ID})
	viewerNginxTest.Header.Set("X-CSRF-Token", viewerSession.CSRFToken)
	viewerNginxRecorder := httptest.NewRecorder()
	handler.ServeHTTP(viewerNginxRecorder, viewerNginxTest)
	if viewerNginxRecorder.Code != http.StatusForbidden {
		t.Fatalf("viewer should be forbidden from invoking Nginx validation, got %d", viewerNginxRecorder.Code)
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
	operatorUsers := httptest.NewRequest(http.MethodGet, "https://mirror.example/admin/api/v1/users", nil)
	operatorUsers.AddCookie(&http.Cookie{Name: "mirrorrelay_session", Value: operatorSession.ID})
	operatorUsersRecorder := httptest.NewRecorder()
	handler.ServeHTTP(operatorUsersRecorder, operatorUsers)
	if operatorUsersRecorder.Code != http.StatusForbidden {
		t.Fatalf("operator should be forbidden from listing users, got %d", operatorUsersRecorder.Code)
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
		"url": overrideServer.URL, "secret": "override-secret",
	})
	rWebhook := httptest.NewRequest(http.MethodPost, "https://mirror.example/admin/api/v1/webhooks/test", bytes.NewReader(testWebhookReq))
	rWebhook.AddCookie(&http.Cookie{Name: "mirrorrelay_session", Value: adminSession.ID})
	rWebhook.Header.Set("X-CSRF-Token", adminSession.CSRFToken)
	wWebhook := httptest.NewRecorder()
	handler.ServeHTTP(wWebhook, rWebhook)
	if wWebhook.Code != http.StatusOK {
		t.Fatalf("webhook test should succeed, got %d: %s", wWebhook.Code, wWebhook.Body.String())
	}
	if configuredRequests.Load() != 0 || overrideRequests.Load() != 1 || !overrideSignatureOK.Load() {
		t.Fatalf("webhook URL/secret override was not honored: configured=%d override=%d signature_ok=%v", configuredRequests.Load(), overrideRequests.Load(), overrideSignatureOK.Load())
	}

	// A one-time URL without a one-time secret must not inherit the live
	// destination's secret.
	unsignedWebhookReq, _ := json.Marshal(map[string]any{"url": overrideServer.URL})
	rUnsignedWebhook := httptest.NewRequest(http.MethodPost, "https://mirror.example/admin/api/v1/webhooks/test", bytes.NewReader(unsignedWebhookReq))
	rUnsignedWebhook.AddCookie(&http.Cookie{Name: "mirrorrelay_session", Value: adminSession.ID})
	rUnsignedWebhook.Header.Set("X-CSRF-Token", adminSession.CSRFToken)
	wUnsignedWebhook := httptest.NewRecorder()
	handler.ServeHTTP(wUnsignedWebhook, rUnsignedWebhook)
	if wUnsignedWebhook.Code != http.StatusOK || overrideRequests.Load() != 2 || !overrideUnsigned.Load() {
		t.Fatalf("temporary webhook unexpectedly inherited configured secret: status=%d requests=%d unsigned=%v", wUnsignedWebhook.Code, overrideRequests.Load(), overrideUnsigned.Load())
	}

	malformed := httptest.NewRequest(http.MethodPost, "https://mirror.example/admin/api/v1/webhooks/test", bytes.NewBufferString("{"))
	malformed.AddCookie(&http.Cookie{Name: "mirrorrelay_session", Value: adminSession.ID})
	malformed.Header.Set("X-CSRF-Token", adminSession.CSRFToken)
	malformedRecorder := httptest.NewRecorder()
	handler.ServeHTTP(malformedRecorder, malformed)
	if malformedRecorder.Code != http.StatusBadRequest || configuredRequests.Load() != 0 || overrideRequests.Load() != 2 {
		t.Fatalf("malformed webhook test had side effects: status=%d configured=%d override=%d", malformedRecorder.Code, configuredRequests.Load(), overrideRequests.Load())
	}

	secretMirror, err := store.CreateMirror(ctx, model.Mirror{
		Name: "Credentialed", Slug: "credentialed", Type: "generic", Enabled: true,
		HeaderAdd:     map[string]string{"Authorization": "Bearer repository-secret", "X-Repo": "header-secret"},
		TokenUpstream: "https://tokens.example/token/path-secret/%65ncoded-secret?access_token=token-secret",
		Upstreams:     []model.Upstream{{URL: "https://packages.example/archive?signature=query-secret", Enabled: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	viewerMirrors := httptest.NewRequest(http.MethodGet, "https://mirror.example/admin/api/v1/mirrors", nil)
	viewerMirrors.AddCookie(&http.Cookie{Name: "mirrorrelay_session", Value: viewerSession.ID})
	viewerMirrorsRecorder := httptest.NewRecorder()
	handler.ServeHTTP(viewerMirrorsRecorder, viewerMirrors)
	for _, secret := range []string{"repository-secret", "header-secret", "token-secret", "path-secret", "%65ncoded-secret", "encoded-secret", "query-secret"} {
		if strings.Contains(viewerMirrorsRecorder.Body.String(), secret) {
			t.Fatalf("viewer mirror response leaked %q: status=%d body=%s", secret, viewerMirrorsRecorder.Code, viewerMirrorsRecorder.Body.String())
		}
	}
	if viewerMirrorsRecorder.Code != http.StatusOK || !strings.Contains(viewerMirrorsRecorder.Body.String(), redactedValue) {
		t.Fatalf("viewer mirror response leaked static credentials: status=%d body=%s", viewerMirrorsRecorder.Code, viewerMirrorsRecorder.Body.String())
	}
	operatorMirrors := httptest.NewRequest(http.MethodGet, "https://mirror.example/admin/api/v1/mirrors", nil)
	operatorMirrors.AddCookie(&http.Cookie{Name: "mirrorrelay_session", Value: operatorSession.ID})
	operatorMirrorsRecorder := httptest.NewRecorder()
	handler.ServeHTTP(operatorMirrorsRecorder, operatorMirrors)
	for _, secret := range []string{"repository-secret", "header-secret", "token-secret", "path-secret", "%65ncoded-secret", "encoded-secret", "query-secret"} {
		if strings.Contains(operatorMirrorsRecorder.Body.String(), secret) {
			t.Fatalf("operator mirror response leaked %q: status=%d body=%s", secret, operatorMirrorsRecorder.Code, operatorMirrorsRecorder.Body.String())
		}
	}
	if operatorMirrorsRecorder.Code != http.StatusOK || !strings.Contains(operatorMirrorsRecorder.Body.String(), redactedValue) {
		t.Fatalf("operator mirror response did not redact credentials: status=%d body=%s", operatorMirrorsRecorder.Code, operatorMirrorsRecorder.Body.String())
	}
	operatorCheck := httptest.NewRequest(http.MethodPost, fmt.Sprintf("https://mirror.example/admin/api/v1/mirrors/%d/check", secretMirror.ID), nil)
	operatorCheck.AddCookie(&http.Cookie{Name: "mirrorrelay_session", Value: operatorSession.ID})
	operatorCheck.Header.Set("X-CSRF-Token", operatorSession.CSRFToken)
	operatorCheckRecorder := httptest.NewRecorder()
	handler.ServeHTTP(operatorCheckRecorder, operatorCheck)
	if operatorCheckRecorder.Code != http.StatusOK {
		t.Fatalf("operator repository health check failed: status=%d body=%s", operatorCheckRecorder.Code, operatorCheckRecorder.Body.String())
	}
	for _, secret := range []string{"query-secret", cfg.UpstreamNginx.UpstreamSocket, "permission denied"} {
		if secret != "" && strings.Contains(operatorCheckRecorder.Body.String(), secret) {
			t.Fatalf("operator repository health result leaked %q: %s", secret, operatorCheckRecorder.Body.String())
		}
	}
	operatorCredentialCreate := httptest.NewRequest(http.MethodPost, "https://mirror.example/admin/api/v1/mirrors", strings.NewReader(`{"name":"Credentialed","slug":"operator-secret","token_upstream":"https://tokens.example/secret"}`))
	operatorCredentialCreate.AddCookie(&http.Cookie{Name: "mirrorrelay_session", Value: operatorSession.ID})
	operatorCredentialCreate.Header.Set("X-CSRF-Token", operatorSession.CSRFToken)
	operatorCredentialCreateRecorder := httptest.NewRecorder()
	handler.ServeHTTP(operatorCredentialCreateRecorder, operatorCredentialCreate)
	if operatorCredentialCreateRecorder.Code != http.StatusForbidden {
		t.Fatalf("operator should not configure repository credentials, got %d: %s", operatorCredentialCreateRecorder.Code, operatorCredentialCreateRecorder.Body.String())
	}
	viewerAccess := httptest.NewRequest(http.MethodGet, "https://mirror.example/admin/api/v1/access", nil)
	viewerAccess.AddCookie(&http.Cookie{Name: "mirrorrelay_session", Value: viewerSession.ID})
	viewerAccessRecorder := httptest.NewRecorder()
	handler.ServeHTTP(viewerAccessRecorder, viewerAccess)
	if viewerAccessRecorder.Code != http.StatusForbidden {
		t.Fatalf("viewer should not receive Managed Upstream Nginx access records, got %d", viewerAccessRecorder.Code)
	}
	viewerConfig := httptest.NewRequest(http.MethodGet, fmt.Sprintf("https://mirror.example/admin/api/v1/mirrors/%d/config", secretMirror.ID), nil)
	viewerConfig.AddCookie(&http.Cookie{Name: "mirrorrelay_session", Value: viewerSession.ID})
	viewerConfigRecorder := httptest.NewRecorder()
	handler.ServeHTTP(viewerConfigRecorder, viewerConfig)
	if viewerConfigRecorder.Code != http.StatusForbidden {
		t.Fatalf("viewer should not receive generated repository config, got %d", viewerConfigRecorder.Code)
	}
	operatorConfig := httptest.NewRequest(http.MethodGet, fmt.Sprintf("https://mirror.example/admin/api/v1/mirrors/%d/config", secretMirror.ID), nil)
	operatorConfig.AddCookie(&http.Cookie{Name: "mirrorrelay_session", Value: operatorSession.ID})
	operatorConfigRecorder := httptest.NewRecorder()
	handler.ServeHTTP(operatorConfigRecorder, operatorConfig)
	if operatorConfigRecorder.Code != http.StatusForbidden {
		t.Fatalf("operator should not receive generated repository config, got %d", operatorConfigRecorder.Code)
	}
	viewerEffective := httptest.NewRequest(http.MethodGet, "https://mirror.example/admin/api/v1/upstream-nginx/config", nil)
	viewerEffective.AddCookie(&http.Cookie{Name: "mirrorrelay_session", Value: viewerSession.ID})
	viewerEffectiveRecorder := httptest.NewRecorder()
	handler.ServeHTTP(viewerEffectiveRecorder, viewerEffective)
	if viewerEffectiveRecorder.Code != http.StatusForbidden {
		t.Fatalf("viewer should not receive effective Nginx config, got %d", viewerEffectiveRecorder.Code)
	}
	operatorEffective := httptest.NewRequest(http.MethodGet, "https://mirror.example/admin/api/v1/upstream-nginx/config", nil)
	operatorEffective.AddCookie(&http.Cookie{Name: "mirrorrelay_session", Value: operatorSession.ID})
	operatorEffectiveRecorder := httptest.NewRecorder()
	handler.ServeHTTP(operatorEffectiveRecorder, operatorEffective)
	if operatorEffectiveRecorder.Code != http.StatusForbidden {
		t.Fatalf("operator should not receive effective Nginx config, got %d", operatorEffectiveRecorder.Code)
	}
	if _, err := store.CreateCustomConfig(ctx, model.CustomConfig{Name: "credentialed", Context: "http", Enabled: true, Content: "proxy_set_header Authorization secret-in-custom-config;"}); err != nil {
		t.Fatal(err)
	}
	viewerCustom := httptest.NewRequest(http.MethodGet, "https://mirror.example/admin/api/v1/custom-configs", nil)
	viewerCustom.AddCookie(&http.Cookie{Name: "mirrorrelay_session", Value: viewerSession.ID})
	viewerCustomRecorder := httptest.NewRecorder()
	handler.ServeHTTP(viewerCustomRecorder, viewerCustom)
	if viewerCustomRecorder.Code != http.StatusForbidden || strings.Contains(viewerCustomRecorder.Body.String(), "secret-in-custom-config") {
		t.Fatalf("viewer should not receive custom Nginx configuration: status=%d body=%s", viewerCustomRecorder.Code, viewerCustomRecorder.Body.String())
	}
	operatorCustom := httptest.NewRequest(http.MethodGet, "https://mirror.example/admin/api/v1/custom-configs", nil)
	operatorCustom.AddCookie(&http.Cookie{Name: "mirrorrelay_session", Value: operatorSession.ID})
	operatorCustomRecorder := httptest.NewRecorder()
	handler.ServeHTTP(operatorCustomRecorder, operatorCustom)
	if operatorCustomRecorder.Code != http.StatusForbidden || strings.Contains(operatorCustomRecorder.Body.String(), "secret-in-custom-config") {
		t.Fatalf("operator should not receive custom Nginx configuration: status=%d body=%s", operatorCustomRecorder.Code, operatorCustomRecorder.Body.String())
	}
	for _, restricted := range []struct {
		name      string
		path      string
		sessionID string
	}{
		{name: "viewer settings", path: "/settings", sessionID: viewerSession.ID},
		{name: "operator settings", path: "/settings", sessionID: operatorSession.ID},
		{name: "viewer ingress configuration", path: "/ingress/snippet", sessionID: viewerSession.ID},
		{name: "operator ingress configuration", path: "/ingress/snippet", sessionID: operatorSession.ID},
	} {
		req := httptest.NewRequest(http.MethodGet, "https://mirror.example/admin/api/v1"+restricted.path, nil)
		req.AddCookie(&http.Cookie{Name: "mirrorrelay_session", Value: restricted.sessionID})
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)
		if recorder.Code != http.StatusForbidden {
			t.Fatalf("%s should be forbidden, got %d: %s", restricted.name, recorder.Code, recorder.Body.String())
		}
	}

	operatorNodeCreate := httptest.NewRequest(http.MethodPost, "https://mirror.example/admin/api/v1/cluster/nodes", strings.NewReader(`{}`))
	operatorNodeCreate.AddCookie(&http.Cookie{Name: "mirrorrelay_session", Value: operatorSession.ID})
	operatorNodeCreate.Header.Set("X-CSRF-Token", operatorSession.CSRFToken)
	operatorNodeCreateRecorder := httptest.NewRecorder()
	handler.ServeHTTP(operatorNodeCreateRecorder, operatorNodeCreate)
	if operatorNodeCreateRecorder.Code != http.StatusForbidden {
		t.Fatalf("operator should not manage cluster node credentials, got %d", operatorNodeCreateRecorder.Code)
	}

	viewerSystem := httptest.NewRequest(http.MethodGet, "https://mirror.example/admin/api/v1/system", nil)
	viewerSystem.AddCookie(&http.Cookie{Name: "mirrorrelay_session", Value: viewerSession.ID})
	viewerSystemRecorder := httptest.NewRecorder()
	handler.ServeHTTP(viewerSystemRecorder, viewerSystem)
	if viewerSystemRecorder.Code != http.StatusForbidden {
		t.Fatalf("viewer should not receive system details, got %d", viewerSystemRecorder.Code)
	}
	operatorSystem := httptest.NewRequest(http.MethodGet, "https://mirror.example/admin/api/v1/system", nil)
	operatorSystem.AddCookie(&http.Cookie{Name: "mirrorrelay_session", Value: operatorSession.ID})
	operatorSystemRecorder := httptest.NewRecorder()
	handler.ServeHTTP(operatorSystemRecorder, operatorSystem)
	if operatorSystemRecorder.Code != http.StatusOK {
		t.Fatalf("operator should receive operational system status, got %d", operatorSystemRecorder.Code)
	}
	for _, sensitiveField := range []string{"tls_private_key", "tls_certificate", "frontend_address", "upstream_address", cfg.TLS.PrivateKey, cfg.Server.FrontendSocket} {
		if strings.Contains(operatorSystemRecorder.Body.String(), sensitiveField) {
			t.Fatalf("operator system response leaked %q: %s", sensitiveField, operatorSystemRecorder.Body.String())
		}
	}
	operatorHealth := httptest.NewRequest(http.MethodGet, "https://mirror.example/admin/api/v1/health", nil)
	operatorHealth.AddCookie(&http.Cookie{Name: "mirrorrelay_session", Value: operatorSession.ID})
	operatorHealthRecorder := httptest.NewRecorder()
	handler.ServeHTTP(operatorHealthRecorder, operatorHealth)
	if operatorHealthRecorder.Code != http.StatusOK {
		t.Fatalf("operator should receive health state, got %d", operatorHealthRecorder.Code)
	}
	for _, sensitive := range []string{"frontend_address", "upstream_address", "frontend_network", "upstream_network", cfg.UpstreamNginx.UpstreamSocket} {
		if strings.Contains(operatorHealthRecorder.Body.String(), sensitive) {
			t.Fatalf("operator health response leaked %q: %s", sensitive, operatorHealthRecorder.Body.String())
		}
	}
}
