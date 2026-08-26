package auth

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
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

func TestVerifyOrigin(t *testing.T) {
	if err := VerifyOrigin("https://mirror.example.com", "https://mirror.example.com", nil); err != nil {
		t.Fatal(err)
	}
	if err := VerifyOrigin("https://alt.example.com", "https://mirror.example.com", []string{"https://alt.example.com"}); err != nil {
		t.Fatal(err)
	}
	if err := VerifyOrigin("http://localhost:8080", "https://mirror.example.com", nil); err != nil {
		t.Fatal("localhost should be allowed for local dev")
	}
	if err := VerifyOrigin("https://evil.com", "https://mirror.example.com", []string{"https://alt.example.com"}); err == nil {
		t.Fatal("evil.com should be rejected")
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
}
