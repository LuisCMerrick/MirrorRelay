package auth

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/url"
	"strings"
	"sync"
	"time"
)

// WebAuthnSession represents an ongoing registration or login challenge.
type WebAuthnSession struct {
	Challenge string
	UserID    int64
	Username  string
	Action    string // "register" or "login"
	RPID      string
	Origin    string
	ExpiresAt time.Time
}

// ChallengeManager manages active WebAuthn challenges.
type ChallengeManager struct {
	mu       sync.Mutex
	sessions map[string]WebAuthnSession
	ttl      time.Duration
}

func NewChallengeManager(ttl time.Duration) *ChallengeManager {
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	return &ChallengeManager{
		sessions: make(map[string]WebAuthnSession),
		ttl:      ttl,
	}
}

func (cm *ChallengeManager) Create(userID int64, username, action, rpID, origin string) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	challenge := base64.RawURLEncoding.EncodeToString(b)
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.pruneLocked(time.Now())
	cm.sessions[challenge] = WebAuthnSession{
		Challenge: challenge,
		UserID:    userID,
		Username:  username,
		Action:    action,
		RPID:      rpID,
		Origin:    origin,
		ExpiresAt: time.Now().Add(cm.ttl),
	}
	return challenge, nil
}

func (cm *ChallengeManager) Consume(challenge, action string) (WebAuthnSession, error) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.pruneLocked(time.Now())
	sess, ok := cm.sessions[challenge]
	if !ok {
		return WebAuthnSession{}, errors.New("challenge expired or invalid")
	}
	delete(cm.sessions, challenge)
	if time.Now().After(sess.ExpiresAt) {
		return WebAuthnSession{}, errors.New("challenge expired")
	}
	if sess.Action != action {
		return WebAuthnSession{}, fmt.Errorf("challenge action mismatch: expected %s got %s", sess.Action, action)
	}
	return sess, nil
}

func (cm *ChallengeManager) pruneLocked(now time.Time) {
	for k, v := range cm.sessions {
		if now.After(v.ExpiresAt) {
			delete(cm.sessions, k)
		}
	}
}

// ClientDataJSON matches the standard WebAuthn client data representation.
type ClientDataJSON struct {
	Type      string `json:"type"`
	Challenge string `json:"challenge"`
	Origin    string `json:"origin"`
}

// ParsedAuthData contains decoded authenticator data fields.
type ParsedAuthData struct {
	RPIDHash       [32]byte
	Flags          byte
	UserPresent    bool
	UserVerified   bool
	AttestedData   bool
	ExtensionData  bool
	SignCount      uint32
	AAGUID         string
	CredentialID   []byte
	PublicKeyPKIX  []byte
	PublicKey      crypto.PublicKey
	BackupEligible bool
	BackupState    bool
}

// ParseAuthenticatorData extracts flags, sign count, and optional attested credential data.
func ParseAuthenticatorData(authData []byte) (*ParsedAuthData, error) {
	if len(authData) < 37 {
		return nil, errors.New("authenticator data is too short")
	}
	res := &ParsedAuthData{}
	copy(res.RPIDHash[:], authData[:32])
	res.Flags = authData[32]
	res.UserPresent = (res.Flags & 0x01) != 0
	res.UserVerified = (res.Flags & 0x04) != 0
	res.BackupEligible = (res.Flags & 0x08) != 0
	res.BackupState = (res.Flags & 0x10) != 0
	res.AttestedData = (res.Flags & 0x40) != 0
	res.ExtensionData = (res.Flags & 0x80) != 0
	res.SignCount = binary.BigEndian.Uint32(authData[33:37])

	if res.AttestedData {
		if len(authData) < 55 {
			return nil, errors.New("authenticator data with attested data is too short")
		}
		res.AAGUID = fmt.Sprintf("%x-%x-%x-%x-%x",
			authData[37:41], authData[41:43], authData[43:45], authData[45:47], authData[47:53])
		credIDLen := binary.BigEndian.Uint16(authData[53:55])
		if len(authData) < 55+int(credIDLen) {
			return nil, errors.New("authenticator data truncated before credential ID")
		}
		res.CredentialID = authData[55 : 55+credIDLen]
		coseBytes := authData[55+credIDLen:]
		pubKey, pkix, err := parseCOSEPublicKey(coseBytes)
		if err != nil {
			return nil, fmt.Errorf("parse COSE public key: %w", err)
		}
		res.PublicKey = pubKey
		res.PublicKeyPKIX = pkix
	}
	return res, nil
}

// VerifyOrigin verifies that the origin in clientDataJSON matches expected origin or allowed origins.
func VerifyOrigin(clientOrigin string, expectedOrigin string, allowedOrigins []string) error {
	clientOrigin = strings.TrimRight(strings.TrimSpace(clientOrigin), "/")
	expectedOrigin = strings.TrimRight(strings.TrimSpace(expectedOrigin), "/")
	if expectedOrigin != "" && strings.EqualFold(clientOrigin, expectedOrigin) {
		return nil
	}
	for _, o := range allowedOrigins {
		if strings.EqualFold(clientOrigin, strings.TrimRight(strings.TrimSpace(o), "/")) {
			return nil
		}
	}
	// Allow localhost / 127.0.0.1 origins for local development/testing
	u, err := url.Parse(clientOrigin)
	if err == nil {
		host := u.Hostname()
		if host == "localhost" || host == "127.0.0.1" || host == "::1" {
			return nil
		}
	}
	return fmt.Errorf("origin %q not allowed (expected %q or allowed list)", clientOrigin, expectedOrigin)
}

// VerifyRPID ensures the RPIDHash in authenticatorData matches sha256(rpID).
func VerifyRPID(rpIDHash [32]byte, expectedRPID string) error {
	expected := sha256.Sum256([]byte(expectedRPID))
	if !bytes.Equal(rpIDHash[:], expected[:]) {
		return fmt.Errorf("RP ID hash mismatch for %q", expectedRPID)
	}
	return nil
}

// VerifyClientData parses and verifies type, challenge, and origin.
func VerifyClientData(rawClientData []byte, expectedType, expectedChallenge, expectedOrigin string, allowedOrigins []string) (*ClientDataJSON, error) {
	var cdata ClientDataJSON
	if err := json.Unmarshal(rawClientData, &cdata); err != nil {
		return nil, fmt.Errorf("decode clientDataJSON: %w", err)
	}
	if cdata.Type != expectedType {
		return nil, fmt.Errorf("clientData type mismatch: expected %q, got %q", expectedType, cdata.Type)
	}
	if cdata.Challenge != expectedChallenge {
		return nil, errors.New("clientData challenge mismatch")
	}
	if err := VerifyOrigin(cdata.Origin, expectedOrigin, allowedOrigins); err != nil {
		return nil, err
	}
	return &cdata, nil
}

// VerifyAuthenticationSignature verifies that signature over (authData || sha256(clientDataJSON)) is valid for pubKey.
func VerifyAuthenticationSignature(pubKey crypto.PublicKey, authData, rawClientData, signature []byte) error {
	clientHash := sha256.Sum256(rawClientData)
	signedData := append(authData, clientHash[:]...)

	switch key := pubKey.(type) {
	case *ecdsa.PublicKey:
		hash := sha256.Sum256(signedData)
		if !ecdsa.VerifyASN1(key, hash[:], signature) {
			return errors.New("invalid ECDSA signature")
		}
		return nil
	case *rsa.PublicKey:
		hash := sha256.Sum256(signedData)
		err := rsa.VerifyPKCS1v15(key, crypto.SHA256, hash[:], signature)
		if err != nil {
			// Try PSS
			err = rsa.VerifyPSS(key, crypto.SHA256, hash[:], signature, nil)
		}
		return err
	case ed25519.PublicKey:
		if !ed25519.Verify(key, signedData, signature) {
			return errors.New("invalid Ed25519 signature")
		}
		return nil
	default:
		return fmt.Errorf("unsupported public key algorithm %T", pubKey)
	}
}

// ParsePKIXPublicKey decodes a standard PKIX DER/PEM or raw base64 string to crypto.PublicKey.
func ParsePKIXPublicKey(pubKeyBytes []byte) (crypto.PublicKey, error) {
	pub, err := x509.ParsePKIXPublicKey(pubKeyBytes)
	if err == nil {
		return pub, nil
	}
	// Try parsing raw ECDSA uncompressed point (0x04 || X || Y)
	if len(pubKeyBytes) == 65 && pubKeyBytes[0] == 0x04 {
		x := new(big.Int).SetBytes(pubKeyBytes[1:33])
		y := new(big.Int).SetBytes(pubKeyBytes[33:65])
		return &ecdsa.PublicKey{Curve: elliptic.P256(), X: x, Y: y}, nil
	}
	// Try parsing raw Ed25519 32-byte key
	if len(pubKeyBytes) == 32 {
		return ed25519.PublicKey(pubKeyBytes), nil
	}
	return nil, err
}

// Minimal CBOR/COSE decoder for WebAuthn public keys (ES256, RS256, Ed25519).
func parseCOSEPublicKey(data []byte) (crypto.PublicKey, []byte, error) {
	// Standard WebAuthn COSE map:
	// 1: kty (2=EC2, 3=RSA, 1=OKP)
	// 3: alg (-7=ES256, -257=RS256, -8=EdDSA)
	// -1: crv (1=P-256, 6=Ed25519)
	// -2: x coordinate
	// -3: y coordinate / d
	// -1: n (modulus for RSA)
	// -2: e (exponent for RSA)
	m, err := parseCBORMap(data)
	if err != nil {
		return nil, nil, err
	}

	kty, ok := m[1].(int64)
	if !ok {
		return nil, nil, errors.New("COSE key missing kty")
	}

	switch kty {
	case 2: // EC2 (e.g. ES256)
		xBytes, okX := m[-2].([]byte)
		yBytes, okY := m[-3].([]byte)
		if !okX || !okY || len(xBytes) != 32 || len(yBytes) != 32 {
			return nil, nil, errors.New("invalid EC2 coordinates in COSE key")
		}
		pub := &ecdsa.PublicKey{
			Curve: elliptic.P256(),
			X:     new(big.Int).SetBytes(xBytes),
			Y:     new(big.Int).SetBytes(yBytes),
		}
		pkix, err := x509.MarshalPKIXPublicKey(pub)
		if err != nil {
			return nil, nil, err
		}
		return pub, pkix, nil

	case 3: // RSA
		nBytes, okN := m[-1].([]byte)
		eBytes, okE := m[-2].([]byte)
		if !okN || !okE {
			return nil, nil, errors.New("invalid RSA parameters in COSE key")
		}
		e := int(binary.BigEndian.Uint32(padLeft(eBytes, 4)))
		pub := &rsa.PublicKey{
			N: new(big.Int).SetBytes(nBytes),
			E: e,
		}
		pkix, err := x509.MarshalPKIXPublicKey(pub)
		if err != nil {
			return nil, nil, err
		}
		return pub, pkix, nil

	case 1: // OKP (Ed25519)
		xBytes, okX := m[-2].([]byte)
		if !okX || len(xBytes) != 32 {
			return nil, nil, errors.New("invalid OKP key in COSE key")
		}
		pub := ed25519.PublicKey(xBytes)
		pkix, err := x509.MarshalPKIXPublicKey(pub)
		if err != nil {
			return nil, nil, err
		}
		return pub, pkix, nil

	default:
		return nil, nil, fmt.Errorf("unsupported COSE key type %d", kty)
	}
}

func padLeft(b []byte, size int) []byte {
	if len(b) >= size {
		return b
	}
	res := make([]byte, size)
	copy(res[size-len(b):], b)
	return res
}

// Simple and safe parser for COSE CBOR map with integer keys.
func parseCBORMap(data []byte) (map[int64]any, error) {
	if len(data) == 0 {
		return nil, errors.New("empty CBOR data")
	}
	r := &cborReader{data: data, pos: 0}
	major, val, err := r.readHeader()
	if err != nil {
		return nil, err
	}
	if major != 5 { // Major type 5 is Map
		return nil, fmt.Errorf("expected CBOR map (major 5), got major %d", major)
	}
	count := int(val)
	out := make(map[int64]any, count)
	for i := 0; i < count; i++ {
		// Read Key (must be integer: major 0 or major 1)
		kMajor, kVal, err := r.readHeader()
		if err != nil {
			return nil, err
		}
		var key int64
		if kMajor == 0 {
			key = int64(kVal)
		} else if kMajor == 1 {
			key = -1 - int64(kVal)
		} else {
			return nil, fmt.Errorf("unsupported map key type %d", kMajor)
		}

		// Read Value
		vMajor, vVal, err := r.readHeader()
		if err != nil {
			return nil, err
		}
		switch vMajor {
		case 0: // Unsigned int
			out[key] = int64(vVal)
		case 1: // Negative int
			out[key] = -1 - int64(vVal)
		case 2: // Byte string
			b, err := r.readBytes(int(vVal))
			if err != nil {
				return nil, err
			}
			out[key] = b
		case 3: // Text string
			b, err := r.readBytes(int(vVal))
			if err != nil {
				return nil, err
			}
			out[key] = string(b)
		default:
			// Skip unknown values
			_ = vVal
		}
	}
	return out, nil
}

type cborReader struct {
	data []byte
	pos  int
}

func (r *cborReader) readHeader() (byte, uint64, error) {
	if r.pos >= len(r.data) {
		return 0, 0, ioEOF()
	}
	b := r.data[r.pos]
	r.pos++
	major := b >> 5
	info := b & 0x1F
	if info < 24 {
		return major, uint64(info), nil
	}
	switch info {
	case 24:
		if r.pos >= len(r.data) {
			return 0, 0, ioEOF()
		}
		v := uint64(r.data[r.pos])
		r.pos++
		return major, v, nil
	case 25:
		if r.pos+2 > len(r.data) {
			return 0, 0, ioEOF()
		}
		v := uint64(binary.BigEndian.Uint16(r.data[r.pos:]))
		r.pos += 2
		return major, v, nil
	case 26:
		if r.pos+4 > len(r.data) {
			return 0, 0, ioEOF()
		}
		v := uint64(binary.BigEndian.Uint32(r.data[r.pos:]))
		r.pos += 4
		return major, v, nil
	case 27:
		if r.pos+8 > len(r.data) {
			return 0, 0, ioEOF()
		}
		v := binary.BigEndian.Uint64(r.data[r.pos:])
		r.pos += 8
		return major, v, nil
	default:
		return 0, 0, fmt.Errorf("unsupported CBOR info %d", info)
	}
}

func (r *cborReader) readBytes(n int) ([]byte, error) {
	if n < 0 || r.pos+n > len(r.data) {
		return nil, ioEOF()
	}
	b := r.data[r.pos : r.pos+n]
	r.pos += n
	return b, nil
}

func ioEOF() error {
	return errors.New("unexpected EOF in CBOR data")
}

// GenerateRecoveryCodes produces count crypto-random recovery codes in format xxxx-xxxx-xxxx and returns plaintext and SHA-256 hashes.
func GenerateRecoveryCodes(count int) (plaintext []string, hashes []string, err error) {
	if count <= 0 {
		count = 8
	}
	const charset = "23456789ABCDEFGHJKLMNPQRSTUVWXYZ" // base32 without confusing chars (0,1,O,I)
	for i := 0; i < count; i++ {
		b := make([]byte, 12)
		if _, err := rand.Read(b); err != nil {
			return nil, nil, err
		}
		var code strings.Builder
		for j := 0; j < 12; j++ {
			if j > 0 && j%4 == 0 {
				code.WriteByte('-')
			}
			code.WriteByte(charset[int(b[j])%len(charset)])
		}
		codeStr := code.String()
		h := HashRecoveryCode(codeStr)
		plaintext = append(plaintext, codeStr)
		hashes = append(hashes, h)
	}
	return plaintext, hashes, nil
}

// HashRecoveryCode standardizes and hashes a recovery code string.
func HashRecoveryCode(code string) string {
	cleaned := strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(code), "-", ""))
	sum := sha256.Sum256([]byte(cleaned))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// DefaultRPID extracts or defaults RP ID from public base URL or host.
func DefaultRPID(cfgRPID string, publicBaseURL string, requestHost string) string {
	if cfgRPID != "" {
		return cfgRPID
	}
	if publicBaseURL != "" {
		if u, err := url.Parse(publicBaseURL); err == nil && u.Hostname() != "" {
			return u.Hostname()
		}
	}
	if requestHost != "" {
		h := requestHost
		if idx := strings.Index(h, ":"); idx != -1 {
			h = h[:idx]
		}
		h = strings.Trim(strings.TrimSuffix(h, "."), "[]")
		if h != "" {
			return h
		}
	}
	return "localhost"
}

// DefaultOrigin extracts or defaults Origin from public base URL or request scheme/host.
func DefaultOrigin(cfgOrigins []string, publicBaseURL string, rHost string, isTLS bool) string {
	if len(cfgOrigins) > 0 && cfgOrigins[0] != "" {
		return strings.TrimRight(cfgOrigins[0], "/")
	}
	if publicBaseURL != "" {
		return strings.TrimRight(publicBaseURL, "/")
	}
	scheme := "http"
	if isTLS {
		scheme = "https"
	}
	return scheme + "://" + rHost
}
