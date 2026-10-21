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

func TestLoginUnknownUserPerformsPasswordHashWork(t *testing.T) {
	store, handler := newBootstrapTestHandler(t, nil)
	defer store.Close()
	hash, err := auth.HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	auth.VerifyPassword(hash, "wrong password value")
	verifyCost := time.Since(started)

	body := []byte(`{"username":"no-such-user","password":"wrong password value"}`)
	request := httptest.NewRequest(http.MethodPost, "https://mirror.example/admin/api/v1/auth/login", bytes.NewReader(body))
	recorder := httptest.NewRecorder()
	started = time.Now()
	handler.ServeHTTP(recorder, request)
	elapsed := time.Since(started)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if minimum := 250*time.Millisecond + verifyCost/2; elapsed < minimum {
		t.Fatalf("unknown-user login took %s, want at least %s so usernames cannot be enumerated by timing", elapsed, minimum)
	}
}

func TestDecodeJSONRejectsOversizedBodyWith413(t *testing.T) {
	body := append([]byte(`{"username":"`), bytes.Repeat([]byte("a"), 2<<20)...)
	body = append(body, []byte(`"}`)...)
	request := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	recorder := httptest.NewRecorder()
	var decoded loginRequest
	if err := decodeJSON(recorder, request, &decoded); err == nil {
		t.Fatal("oversized body was accepted")
	}
	if recorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status=%d body=%s, want 413", recorder.Code, recorder.Body.String())
	}
}

func TestMissingMirrorAndUserAreNotFound(t *testing.T) {
	store, handler := newBootstrapTestHandler(t, nil)
	defer store.Close()
	body := []byte(`{"username":"site-admin","password":"correct horse battery staple","password_confirmation":"correct horse battery staple"}`)
	register := httptest.NewRecorder()
	handler.ServeHTTP(register, httptest.NewRequest(http.MethodPost, "https://mirror.example/admin/api/v1/auth/bootstrap", bytes.NewReader(body)))
	if register.Code != http.StatusCreated {
		t.Fatalf("registration status=%d body=%s", register.Code, register.Body.String())
	}
	var session struct {
		CSRF string `json:"csrf_token"`
	}
	if err := json.Unmarshal(register.Body.Bytes(), &session); err != nil {
		t.Fatal(err)
	}
	do := func(method, path string) int {
		request := httptest.NewRequest(method, "https://mirror.example/admin/api/v1"+path, nil)
		request.Header.Set("X-CSRF-Token", session.CSRF)
		for _, cookie := range register.Result().Cookies() {
			request.AddCookie(cookie)
		}
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		return recorder.Code
	}
	if code := do(http.MethodGet, "/mirrors/4242"); code != http.StatusNotFound {
		t.Fatalf("missing mirror status=%d, want 404", code)
	}
	if code := do(http.MethodDelete, "/users/4242"); code != http.StatusNotFound {
		t.Fatalf("missing user status=%d, want 404", code)
	}
}
