package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/LuisCMerrick/MirrorRelay/internal/auth"
	"github.com/LuisCMerrick/MirrorRelay/internal/config"
	"github.com/LuisCMerrick/MirrorRelay/internal/database"
	"github.com/LuisCMerrick/MirrorRelay/internal/security"
)

func newBootstrapTestHandler(t *testing.T, cidrs []string) (*database.Store, http.Handler) {
	t.Helper()
	store, err := database.Open(filepath.Join(t.TempDir(), "mirrorrelay.db"))
	if err != nil {
		t.Fatal(err)
	}
	allowed, err := security.ParseCIDRs(cidrs)
	if err != nil {
		store.Close()
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Security.AdminCIDRs = cidrs
	server := &Server{
		cfg:          cfg,
		store:        store,
		sessions:     auth.NewSessionsWithPath(store, time.Hour, cfg.Admin.Path),
		loginLimiter: auth.NewLoginLimiter(time.Minute, 5),
		adminCIDRs:   allowed,
	}
	return store, server.Handler(http.NotFoundHandler())
}

func TestInitialAdministratorRegistrationCreatesSessionOnce(t *testing.T) {
	store, handler := newBootstrapTestHandler(t, nil)
	defer store.Close()

	statusRequest := httptest.NewRequest(http.MethodGet, "https://mirror.example/admin/api/v1/auth/bootstrap", nil)
	statusRecorder := httptest.NewRecorder()
	handler.ServeHTTP(statusRecorder, statusRequest)
	if statusRecorder.Code != http.StatusOK || !bytes.Contains(statusRecorder.Body.Bytes(), []byte(`"required":true`)) {
		t.Fatalf("unexpected bootstrap status: %d %s", statusRecorder.Code, statusRecorder.Body.String())
	}

	body, err := json.Marshal(map[string]string{
		"username":              "site-admin",
		"password":              "correct horse battery staple",
		"password_confirmation": "correct horse battery staple",
	})
	if err != nil {
		t.Fatal(err)
	}
	registerRequest := httptest.NewRequest(http.MethodPost, "https://mirror.example/admin/api/v1/auth/bootstrap", bytes.NewReader(body))
	registerRecorder := httptest.NewRecorder()
	handler.ServeHTTP(registerRecorder, registerRequest)
	if registerRecorder.Code != http.StatusCreated {
		t.Fatalf("registration status=%d body=%s", registerRecorder.Code, registerRecorder.Body.String())
	}
	cookies := registerRecorder.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("initial administrator registration did not create a session cookie")
	}
	sessionRequest := httptest.NewRequest(http.MethodGet, "https://mirror.example/admin/api/v1/auth/session", nil)
	for _, cookie := range cookies {
		sessionRequest.AddCookie(cookie)
	}
	sessionRecorder := httptest.NewRecorder()
	handler.ServeHTTP(sessionRecorder, sessionRequest)
	if sessionRecorder.Code != http.StatusOK || !bytes.Contains(sessionRecorder.Body.Bytes(), []byte(`"username":"site-admin"`)) {
		t.Fatalf("registered administrator session status=%d body=%s", sessionRecorder.Code, sessionRecorder.Body.String())
	}
	user, err := store.UserByName(context.Background(), "site-admin")
	if err != nil || user.Role != "admin" || !auth.VerifyPassword(user.PasswordHash, "correct horse battery staple") {
		t.Fatalf("unexpected registered administrator: user=%+v err=%v", user, err)
	}

	secondRequest := httptest.NewRequest(http.MethodPost, "https://mirror.example/admin/api/v1/auth/bootstrap", bytes.NewReader(body))
	secondRecorder := httptest.NewRecorder()
	handler.ServeHTTP(secondRecorder, secondRequest)
	if secondRecorder.Code != http.StatusConflict {
		t.Fatalf("second registration status=%d body=%s", secondRecorder.Code, secondRecorder.Body.String())
	}

	statusRequest = httptest.NewRequest(http.MethodGet, "https://mirror.example/admin/api/v1/auth/bootstrap", nil)
	statusRecorder = httptest.NewRecorder()
	handler.ServeHTTP(statusRecorder, statusRequest)
	if statusRecorder.Code != http.StatusOK || !bytes.Contains(statusRecorder.Body.Bytes(), []byte(`"required":false`)) {
		t.Fatalf("unexpected initialized status: %d %s", statusRecorder.Code, statusRecorder.Body.String())
	}
}

func TestInitialAdministratorRegistrationValidatesConfirmationAndAdminCIDR(t *testing.T) {
	store, handler := newBootstrapTestHandler(t, []string{"10.0.0.0/8"})
	defer store.Close()

	body := []byte(`{"username":"site-admin","password":"correct horse battery staple","password_confirmation":"different password"}`)
	blockedRequest := httptest.NewRequest(http.MethodPost, "https://mirror.example/admin/api/v1/auth/bootstrap", bytes.NewReader(body))
	blockedRequest.RemoteAddr = "192.0.2.10:1234"
	blockedRecorder := httptest.NewRecorder()
	handler.ServeHTTP(blockedRecorder, blockedRequest)
	if blockedRecorder.Code != http.StatusForbidden {
		t.Fatalf("registration outside admin CIDR status=%d", blockedRecorder.Code)
	}

	mismatchRequest := httptest.NewRequest(http.MethodPost, "https://mirror.example/admin/api/v1/auth/bootstrap", bytes.NewReader(body))
	mismatchRequest.RemoteAddr = "10.1.2.3:1234"
	mismatchRecorder := httptest.NewRecorder()
	handler.ServeHTTP(mismatchRecorder, mismatchRequest)
	if mismatchRecorder.Code != http.StatusBadRequest {
		t.Fatalf("password mismatch status=%d body=%s", mismatchRecorder.Code, mismatchRecorder.Body.String())
	}
	count, err := store.CountUsers(context.Background())
	if err != nil || count != 0 {
		t.Fatalf("password mismatch created a user: count=%d err=%v", count, err)
	}
}
