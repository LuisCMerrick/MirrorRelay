package api

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LuisCMerrick/MirrorRelay/internal/auth"
	"github.com/LuisCMerrick/MirrorRelay/internal/buildinfo"
	"github.com/LuisCMerrick/MirrorRelay/internal/config"
	"github.com/LuisCMerrick/MirrorRelay/internal/database"
)

func newTestPasskeyServer(t *testing.T, enablePasskey bool) (*Server, *database.Store, func()) {
	t.Helper()
	dir := t.TempDir()
	store, err := database.Open(filepath.Join(dir, "mirrorrelay.db"))
	if err != nil {
		t.Fatal(err)
	}

	cfg := config.Default()
	cfg.Admin.Path = "/admin/"
	cfg.Security.AdminCIDRs = []string{"127.0.0.1/32", "::1/128"}
	cfg.Admin.Passkey.Enabled = enablePasskey
	cfg.Admin.Passkey.RPName = "MirrorRelay"
	cfg.Admin.Passkey.RPID = "localhost"
	cfg.Admin.Passkey.Origins = []string{"http://localhost:8080"}

	srv, err := New(cfg, cfg, store, nil, nil, nil, nil, nil, nil, buildinfo.Info{Version: "0.0.21"})
	if err != nil {
		t.Fatal(err)
	}

	return srv, store, func() { store.Close() }
}

func TestPasskeyStatusEndpoint(t *testing.T) {
	srv, _, cleanup := newTestPasskeyServer(t, true)
	defer cleanup()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/api/v1/auth/passkey/status", nil)
	srv.apiHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var res map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if res["enabled"] != true || res["rp_name"] != "MirrorRelay" || res["rp_id"] != "localhost" {
		t.Fatalf("unexpected passkey status: %+v", res)
	}
}

func TestRecoveryLoginRejectsOversizedCredentials(t *testing.T) {
	srv, _, cleanup := newTestPasskeyServer(t, true)
	defer cleanup()

	for name, payload := range map[string]map[string]string{
		"username": {
			"username": strings.Repeat("u", 65), "recovery_code": "ABCD-EFGH-JKLM",
		},
		"recovery code": {
			"username": "admin", "recovery_code": strings.Repeat("A", 65),
		},
	} {
		t.Run(name, func(t *testing.T) {
			body, err := json.Marshal(payload)
			if err != nil {
				t.Fatal(err)
			}
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/admin/api/v1/auth/recovery/login", bytes.NewReader(body))
			srv.apiHandler(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestPasskeyRegistrationAndAuthenticationFlow(t *testing.T) {
	srv, store, cleanup := newTestPasskeyServer(t, true)
	defer cleanup()

	ctx := t.Context()
	passHash, err := auth.HashPassword("adminPassword123")
	if err != nil {
		t.Fatal(err)
	}
	user, ok, err := store.CreateInitialAdmin(ctx, "admin", passHash)
	if err != nil || !ok {
		t.Fatal("failed to create admin")
	}

	// 1. Log in via session
	session, err := srv.sessions.Create(user.ID, user.Username, user.Role)
	if err != nil {
		t.Fatal(err)
	}

	// 2. Request registration options
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/admin/api/v1/account/passkeys/register/options", nil)
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: session.ID})
	req.Header.Set("X-CSRF-Token", session.CSRFToken)
	srv.apiHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("register options failed (%d): %s", rec.Code, rec.Body.String())
	}

	var regOptions map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &regOptions); err != nil {
		t.Fatal(err)
	}
	challenge := regOptions["challenge"].(string)

	// 3. Generate mock WebAuthn credential (ECDSA P-256)
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	clientData := auth.ClientDataJSON{
		Type:      "webauthn.create",
		Challenge: challenge,
		Origin:    "http://localhost:8080",
	}
	clientDataBytes, _ := json.Marshal(clientData)

	// Construct authenticatorData
	rpHash := sha256.Sum256([]byte("localhost"))
	authData := make([]byte, 37)
	copy(authData[:32], rpHash[:])
	authData[32] = 0x45 // UP (0x01) + UV (0x04) + AT (0x40)

	aaguid := make([]byte, 16)
	credID := []byte("test-cred-id-123")
	var credIDLen [2]byte
	credIDLen[0] = 0
	credIDLen[1] = byte(len(credID))

	// Mock CBOR COSE key (map: 1:2, 3:-7, -1:1, -2:x, -3:y)
	xBytes := key.X.Bytes()
	yBytes := key.Y.Bytes()
	if len(xBytes) < 32 {
		pad := make([]byte, 32-len(xBytes))
		xBytes = append(pad, xBytes...)
	}
	if len(yBytes) < 32 {
		pad := make([]byte, 32-len(yBytes))
		yBytes = append(pad, yBytes...)
	}

	var coseBuf bytes.Buffer
	coseBuf.WriteByte(0xA5) // Map of 5 items
	// 1: 2 (kty: EC2)
	coseBuf.WriteByte(0x01)
	coseBuf.WriteByte(0x02)
	// 3: -7 (alg: ES256 -> major 1, val 6)
	coseBuf.WriteByte(0x03)
	coseBuf.WriteByte(0x26)
	// -1: 1 (crv: P-256)
	coseBuf.WriteByte(0x20)
	coseBuf.WriteByte(0x01)
	// -2: x (major 1 val 1 -> key -2; major 2 val 32 -> 32 bytes)
	coseBuf.WriteByte(0x21)
	coseBuf.WriteByte(0x58)
	coseBuf.WriteByte(0x20)
	coseBuf.Write(xBytes)
	// -3: y (major 1 val 2 -> key -3; major 2 val 32 -> 32 bytes)
	coseBuf.WriteByte(0x22)
	coseBuf.WriteByte(0x58)
	coseBuf.WriteByte(0x20)
	coseBuf.Write(yBytes)

	fullAuthData := append(authData, aaguid...)
	fullAuthData = append(fullAuthData, credIDLen[:]...)
	fullAuthData = append(fullAuthData, credID...)
	fullAuthData = append(fullAuthData, coseBuf.Bytes()...)

	// Mock attestation object containing authData
	var attBuf bytes.Buffer
	attBuf.WriteByte(0xA3)                // Map of 3 items
	attBuf.WriteString("\x63fmt\x64none") // "fmt": "none"
	attBuf.WriteString("\x67attStmt\xA0") // "attStmt": {}
	attBuf.WriteString("\x68authData")    // "authData"
	attBuf.WriteByte(0x58)
	attBuf.WriteByte(byte(len(fullAuthData)))
	attBuf.Write(fullAuthData)

	verifyBody, _ := json.Marshal(map[string]any{
		"display_name": "Test Key",
		"id":           base64.RawURLEncoding.EncodeToString(credID),
		"rawId":        base64.RawURLEncoding.EncodeToString(credID),
		"type":         "public-key",
		"transports":   []string{"usb", "internal"},
		"response": map[string]string{
			"clientDataJSON":    base64.RawURLEncoding.EncodeToString(clientDataBytes),
			"attestationObject": base64.RawURLEncoding.EncodeToString(attBuf.Bytes()),
		},
	})

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/admin/api/v1/account/passkeys/register/verify", bytes.NewReader(verifyBody))
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: session.ID})
	req.Header.Set("X-CSRF-Token", session.CSRFToken)
	srv.apiHandler(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("register verify failed (%d): %s", rec.Code, rec.Body.String())
	}

	// 4. List Account Passkeys
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/admin/api/v1/account/passkeys", nil)
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: session.ID})
	srv.apiHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("list account passkeys failed: %s", rec.Body.String())
	}
	var accountRes map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &accountRes); err != nil {
		t.Fatal(err)
	}
	passkeys := accountRes["passkeys"].([]any)
	if len(passkeys) != 1 {
		t.Fatalf("expected 1 passkey, got %d", len(passkeys))
	}
	pkObj := passkeys[0].(map[string]any)
	passkeyID := int64(pkObj["id"].(float64))

	// 5. Test Passkey Login Flow
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/admin/api/v1/auth/passkey/login/options", strings.NewReader(`{"username":"admin"}`))
	srv.apiHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("passkey login options failed: %s", rec.Body.String())
	}
	var loginOptions map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &loginOptions); err != nil {
		t.Fatal(err)
	}
	loginChallenge := loginOptions["challenge"].(string)

	loginClientData := auth.ClientDataJSON{
		Type:      "webauthn.get",
		Challenge: loginChallenge,
		Origin:    "http://localhost:8080",
	}
	loginClientDataBytes, _ := json.Marshal(loginClientData)

	loginAuthData := make([]byte, 37)
	copy(loginAuthData[:32], rpHash[:])
	loginAuthData[32] = 0x05 // UP + UV
	loginAuthData[36] = 1    // SignCount = 1

	loginClientHash := sha256.Sum256(loginClientDataBytes)
	signedData := append(loginAuthData, loginClientHash[:]...)
	signedHash := sha256.Sum256(signedData)

	sig, err := ecdsa.SignASN1(rand.Reader, key, signedHash[:])
	if err != nil {
		t.Fatal(err)
	}

	loginVerifyBody, _ := json.Marshal(map[string]any{
		"id":    base64.RawURLEncoding.EncodeToString(credID),
		"rawId": base64.RawURLEncoding.EncodeToString(credID),
		"type":  "public-key",
		"response": map[string]string{
			"clientDataJSON":    base64.RawURLEncoding.EncodeToString(loginClientDataBytes),
			"authenticatorData": base64.RawURLEncoding.EncodeToString(loginAuthData),
			"signature":         base64.RawURLEncoding.EncodeToString(sig),
		},
	})

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/admin/api/v1/auth/passkey/login/verify", bytes.NewReader(loginVerifyBody))
	srv.apiHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("passkey login verify failed (%d): %s", rec.Code, rec.Body.String())
	}

	// 6. Test Recovery Codes Generation & Login
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/admin/api/v1/account/recovery/generate", nil)
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: session.ID})
	req.Header.Set("X-CSRF-Token", session.CSRFToken)
	srv.apiHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("recovery generate failed: %s", rec.Body.String())
	}
	var recoveryRes map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &recoveryRes); err != nil {
		t.Fatal(err)
	}
	codes := recoveryRes["recovery_codes"].([]any)
	if len(codes) != 8 {
		t.Fatalf("expected 8 recovery codes, got %d", len(codes))
	}
	firstCode := codes[0].(string)

	// Test Recovery Code Login
	recoveryLoginBody, _ := json.Marshal(map[string]string{
		"username":      "admin",
		"recovery_code": firstCode,
	})
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/admin/api/v1/auth/recovery/login", bytes.NewReader(recoveryLoginBody))
	srv.apiHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("recovery login failed (%d): %s", rec.Code, rec.Body.String())
	}

	// Reusing same recovery code should fail
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/admin/api/v1/auth/recovery/login", bytes.NewReader(recoveryLoginBody))
	srv.apiHandler(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 on reused recovery code, got %d", rec.Code)
	}

	// 7. Test Password Login Policy & Protection against lockout
	// Disabling password login when passkeys and recovery codes are present should succeed
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPut, "/admin/api/v1/account/security/password-login", strings.NewReader(`{"disabled": true}`))
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: session.ID})
	req.Header.Set("X-CSRF-Token", session.CSRFToken)
	srv.apiHandler(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("disable password login failed: %s", rec.Body.String())
	}

	// Password login should now be rejected
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/admin/api/v1/auth/login", strings.NewReader(`{"username":"admin","password":"adminPassword123"}`))
	srv.apiHandler(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 when password login is disabled, got %d", rec.Code)
	}

	// Deleting the ONLY passkey when password login is disabled should fail
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/admin/api/v1/account/passkeys/%d", passkeyID), nil)
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: session.ID})
	req.Header.Set("X-CSRF-Token", session.CSRFToken)
	srv.apiHandler(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 on deleting last passkey while password login is disabled, got %d", rec.Code)
	}
}
