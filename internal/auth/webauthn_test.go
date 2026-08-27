package auth

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/json"
	"testing"
	"time"
)

func TestChallengeManager(t *testing.T) {
	cm := NewChallengeManager(100 * time.Millisecond)
	ch, err := cm.Create(1, "admin", "register", "example.com", "https://example.com")
	if err != nil {
		t.Fatal(err)
	}
	if ch == "" {
		t.Fatal("empty challenge")
	}

	// Action mismatch
	if _, err := cm.Consume(ch, "login"); err == nil {
		t.Fatal("expected error on action mismatch")
	}

	// Recreate
	ch2, err := cm.Create(1, "admin", "login", "example.com", "https://example.com")
	if err != nil {
		t.Fatal(err)
	}
	sess, err := cm.Consume(ch2, "login")
	if err != nil {
		t.Fatal(err)
	}
	if sess.Username != "admin" || sess.UserID != 1 {
		t.Fatalf("unexpected session: %+v", sess)
	}

	// Should not be reusable
	if _, err := cm.Consume(ch2, "login"); err == nil {
		t.Fatal("challenge should not be reusable")
	}
}

func TestChallengeManagerIsBounded(t *testing.T) {
	cm := NewChallengeManagerWithLimit(time.Minute, 2)
	first, err := cm.Create(1, "admin", "login", "example.com", "https://example.com")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cm.Create(1, "admin", "login", "example.com", "https://example.com"); err != nil {
		t.Fatal(err)
	}
	if _, err := cm.Create(1, "admin", "login", "example.com", "https://example.com"); err == nil {
		t.Fatal("challenge manager admitted a new session at the capacity limit")
	}
	if len(cm.sessions) != 2 {
		t.Fatalf("challenge manager retained %d sessions; want 2", len(cm.sessions))
	}
	if _, err := cm.Consume(first, "login"); err != nil {
		t.Fatalf("capacity pressure evicted an existing challenge: %v", err)
	}
}

func TestCOSEMapRejectsIntegerWraparound(t *testing.T) {
	// {negative(2^64-1): 1} previously wrapped the map key to zero.
	oversizedKey := []byte{0xa1, 0x3b, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x01}
	if _, err := parseCBORMap(oversizedKey); err == nil {
		t.Fatal("oversized negative map key was accepted through int64 wraparound")
	}
	// {1: unsigned(2^64-1)} previously wrapped the value to -1.
	oversizedValue := []byte{0xa1, 0x01, 0x1b, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}
	if _, err := parseCBORMap(oversizedValue); err == nil {
		t.Fatal("oversized unsigned map value was accepted through int64 wraparound")
	}
}

func TestVerifyOrigin(t *testing.T) {
	if err := VerifyOrigin("https://mirror.example.com", "https://mirror.example.com", nil); err != nil {
		t.Fatal(err)
	}
	if err := VerifyOrigin("https://alt.example.com", "https://mirror.example.com", []string{"https://alt.example.com"}); err != nil {
		t.Fatal(err)
	}
	if err := VerifyOrigin("http://localhost:8080", "http://localhost:8080", nil); err != nil {
		t.Fatal("explicitly expected localhost origin should be allowed")
	}
	if err := VerifyOrigin("http://localhost:8080", "https://mirror.example.com", nil); err == nil {
		t.Fatal("localhost must not bypass an expected production origin")
	}
	if err := VerifyOrigin("https://evil.com", "https://mirror.example.com", []string{"https://alt.example.com"}); err == nil {
		t.Fatal("evil.com should be rejected")
	}
}

func TestDefaultWebAuthnBindingUsesAdminRequestHost(t *testing.T) {
	if got := DefaultRPID("", "https://mirror.example.com", "admin.example.com:443"); got != "admin.example.com" {
		t.Fatalf("DefaultRPID = %q", got)
	}
	if got := DefaultOrigin(nil, "https://mirror.example.com", "admin.example.com", false); got != "https://admin.example.com" {
		t.Fatalf("DefaultOrigin = %q", got)
	}
	if got := DefaultOrigin(nil, "", "localhost:8080", false); got != "http://localhost:8080" {
		t.Fatalf("loopback DefaultOrigin = %q", got)
	}
	if got := DefaultOrigin([]string{"https://first.example.com", "https://admin.example.com"}, "", "admin.example.com", false); got != "https://admin.example.com" {
		t.Fatalf("configured request-matching DefaultOrigin = %q", got)
	}
}

func TestExtractAttestationAuthDataParsesCBORStructure(t *testing.T) {
	authData := bytes.Repeat([]byte{0x42}, 37)
	attestation := append([]byte{0xa1, 0x68}, []byte("authData")...)
	attestation = append(attestation, 0x58, byte(len(authData)))
	attestation = append(attestation, authData...)
	got, err := ExtractAttestationAuthData(attestation)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, authData) {
		t.Fatal("extracted authData did not match")
	}

	decoy := append([]byte{0xa1, 0x63}, []byte("fmt")...)
	decoy = append(decoy, 0x68)
	decoy = append(decoy, []byte("authData")...)
	if _, err := ExtractAttestationAuthData(decoy); err == nil {
		t.Fatal("authData substring in a value was accepted as a map key")
	}

	// A COSE byte string declared near MaxInt must fail instead of overflowing
	// the reader's bounds check and panicking.
	oversized := []byte{0xa1, 0x01, 0x5b, 0x7f, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}
	if _, err := parseCBORMap(oversized); err == nil {
		t.Fatal("oversized CBOR string length was accepted")
	}
}

func TestRecoveryCodes(t *testing.T) {
	plain, hashes, err := GenerateRecoveryCodes(8)
	if err != nil {
		t.Fatal(err)
	}
	if len(plain) != 8 || len(hashes) != 8 {
		t.Fatalf("expected 8 codes, got %d, %d", len(plain), len(hashes))
	}
	for i, code := range plain {
		h := HashRecoveryCode(code)
		if h != hashes[i] {
			t.Fatalf("hash mismatch for code %s: got %s, expected %s", code, h, hashes[i])
		}
		// Formatted without dashes or lowercase should still match
		h2 := HashRecoveryCode(code)
		if h2 != hashes[i] {
			t.Fatalf("hash mismatch on normalized code: got %s", h2)
		}
	}
}

func TestECDSAAuthenticationSignature(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	authData := make([]byte, 37)
	clientData := []byte(`{"type":"webauthn.get","challenge":"abc","origin":"https://mirror.example.com"}`)
	clientHash := sha256.Sum256(clientData)
	signedData := append(authData, clientHash[:]...)
	hash := sha256.Sum256(signedData)

	sig, err := ecdsa.SignASN1(rand.Reader, key, hash[:])
	if err != nil {
		t.Fatal(err)
	}

	if err := VerifyAuthenticationSignature(&key.PublicKey, authData, clientData, sig); err != nil {
		t.Fatalf("VerifyAuthenticationSignature failed: %v", err)
	}

	// Corrupt signature
	sig[0] ^= 0xFF
	if err := VerifyAuthenticationSignature(&key.PublicKey, authData, clientData, sig); err == nil {
		t.Fatal("corrupt signature should fail")
	}
}

func TestRSAAuthenticationSignatureRequiresRS256(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}

	authData := make([]byte, 37)
	clientData := []byte(`{"type":"webauthn.get","challenge":"abc","origin":"https://mirror.example.com"}`)
	clientHash := sha256.Sum256(clientData)
	signedData := append(authData, clientHash[:]...)
	hash := sha256.Sum256(signedData)

	rs256Signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, hash[:])
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyAuthenticationSignature(&key.PublicKey, authData, clientData, rs256Signature); err != nil {
		t.Fatalf("valid RS256 signature failed verification: %v", err)
	}

	pssSignature, err := rsa.SignPSS(rand.Reader, key, crypto.SHA256, hash[:], nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyAuthenticationSignature(&key.PublicKey, authData, clientData, pssSignature); err == nil {
		t.Fatal("RSA-PSS signature was accepted for an RS256 credential")
	}
}

func TestVerifyClientData(t *testing.T) {
	cdata := ClientDataJSON{
		Type:      "webauthn.get",
		Challenge: "test-challenge-123",
		Origin:    "https://mirror.example.com",
	}
	raw, _ := json.Marshal(cdata)

	res, err := VerifyClientData(raw, "webauthn.get", "test-challenge-123", "https://mirror.example.com", nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Challenge != "test-challenge-123" {
		t.Fatalf("unexpected challenge %s", res.Challenge)
	}

	// Type mismatch
	if _, err := VerifyClientData(raw, "webauthn.create", "test-challenge-123", "https://mirror.example.com", nil); err == nil {
		t.Fatal("expected error on type mismatch")
	}
	cdata.CrossOrigin = true
	raw, _ = json.Marshal(cdata)
	if _, err := VerifyClientData(raw, "webauthn.get", "test-challenge-123", "https://mirror.example.com", nil); err == nil {
		t.Fatal("cross-origin client data was accepted")
	}
}
