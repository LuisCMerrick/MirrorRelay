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
	"net"
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
	mu          sync.Mutex
	sessions    map[string]WebAuthnSession
	ttl         time.Duration
	maxSessions int
}

const defaultMaxWebAuthnChallenges = 10_000

func NewChallengeManager(ttl time.Duration) *ChallengeManager {
	return NewChallengeManagerWithLimit(ttl, defaultMaxWebAuthnChallenges)
}

// NewChallengeManagerWithLimit creates a bounded challenge manager. The
// explicit limit is primarily useful for focused tests; production callers use
// NewChallengeManager's conservative global bound.
func NewChallengeManagerWithLimit(ttl time.Duration, maxSessions int) *ChallengeManager {
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	if maxSessions <= 0 {
		maxSessions = defaultMaxWebAuthnChallenges
	}
	return &ChallengeManager{
		sessions:    make(map[string]WebAuthnSession),
		ttl:         ttl,
		maxSessions: maxSessions,
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
	now := time.Now()
	cm.pruneLocked(now)
	if len(cm.sessions) >= cm.maxSessions {
		return "", errors.New("too many active WebAuthn challenges")
	}
	cm.sessions[challenge] = WebAuthnSession{
		Challenge: challenge,
		UserID:    userID,
		Username:  username,
		Action:    action,
		RPID:      rpID,
		Origin:    origin,
		ExpiresAt: now.Add(cm.ttl),
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
	Type        string `json:"type"`
	Challenge   string `json:"challenge"`
	Origin      string `json:"origin"`
	CrossOrigin bool   `json:"crossOrigin,omitempty"`
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
	if res.BackupState && !res.BackupEligible {
		return nil, errors.New("authenticator backup state is set without backup eligibility")
	}

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

// VerifyOrigin verifies that the origin in clientDataJSON exactly matches an
// expected, canonical HTTP(S) origin.
func VerifyOrigin(clientOrigin string, expectedOrigin string, allowedOrigins []string) error {
	clientCanonical, err := canonicalWebAuthnOrigin(clientOrigin)
	if err != nil {
		return fmt.Errorf("invalid client origin: %w", err)
	}
	expectedCanonical, err := canonicalWebAuthnOrigin(expectedOrigin)
	if err == nil && clientCanonical == expectedCanonical {
		return nil
	}
	for _, o := range allowedOrigins {
		allowedCanonical, err := canonicalWebAuthnOrigin(o)
		if err == nil && clientCanonical == allowedCanonical {
			return nil
		}
	}
	return fmt.Errorf("origin %q not allowed (expected %q)", clientCanonical, expectedOrigin)
}

func canonicalWebAuthnOrigin(value string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(value))
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("%q is not an HTTP(S) origin", value)
	}
	hostname := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	if hostname == "" {
		return "", fmt.Errorf("%q has no hostname", value)
	}
	port := u.Port()
	if (u.Scheme == "https" && port == "443") || (u.Scheme == "http" && port == "80") {
		port = ""
	}
	host := hostname
	if strings.Contains(hostname, ":") {
		host = "[" + hostname + "]"
	}
	if port != "" {
		host = net.JoinHostPort(hostname, port)
	}
	return strings.ToLower(u.Scheme) + "://" + host, nil
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
	if cdata.CrossOrigin {
		return nil, errors.New("cross-origin WebAuthn ceremonies are not allowed")
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
		if err := rsa.VerifyPKCS1v15(key, crypto.SHA256, hash[:], signature); err != nil {
			return fmt.Errorf("invalid RS256 signature: %w", err)
		}
		return nil
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
		if !elliptic.P256().IsOnCurve(x, y) {
			return nil, errors.New("raw ECDSA point is not on P-256")
		}
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
		if alg, ok := m[3].(int64); !ok || alg != -7 {
			return nil, nil, errors.New("EC2 COSE key must use ES256")
		}
		if curve, ok := m[-1].(int64); !ok || curve != 1 {
			return nil, nil, errors.New("EC2 COSE key must use P-256")
		}
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
		if !pub.Curve.IsOnCurve(pub.X, pub.Y) {
			return nil, nil, errors.New("EC2 coordinates are not on P-256")
		}
		pkix, err := x509.MarshalPKIXPublicKey(pub)
		if err != nil {
			return nil, nil, err
		}
		return pub, pkix, nil

	case 3: // RSA
		if alg, ok := m[3].(int64); !ok || alg != -257 {
			return nil, nil, errors.New("RSA COSE key must use RS256")
		}
		nBytes, okN := m[-1].([]byte)
		eBytes, okE := m[-2].([]byte)
		if !okN || !okE || len(eBytes) == 0 || len(eBytes) > 4 {
			return nil, nil, errors.New("invalid RSA parameters in COSE key")
		}
		e := int(binary.BigEndian.Uint32(padLeft(eBytes, 4)))
		modulus := new(big.Int).SetBytes(nBytes)
		if modulus.BitLen() < 2048 || e < 3 || e%2 == 0 {
			return nil, nil, errors.New("RSA key must have a 2048-bit modulus and valid odd exponent")
		}
		pub := &rsa.PublicKey{
			N: modulus,
			E: e,
		}
		pkix, err := x509.MarshalPKIXPublicKey(pub)
		if err != nil {
			return nil, nil, err
		}
		return pub, pkix, nil

	case 1: // OKP (Ed25519)
		if alg, ok := m[3].(int64); !ok || alg != -8 {
			return nil, nil, errors.New("OKP COSE key must use EdDSA")
		}
		if curve, ok := m[-1].(int64); !ok || curve != 6 {
			return nil, nil, errors.New("OKP COSE key must use Ed25519")
		}
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
	if val > 64 {
		return nil, errors.New("COSE map has too many entries")
	}
	count := int(val)
	out := make(map[int64]any, count)
	const maxInt64Value = uint64(^uint64(0) >> 1)
	for i := 0; i < count; i++ {
		// Read Key (must be integer: major 0 or major 1)
		kMajor, kVal, err := r.readHeader()
		if err != nil {
			return nil, err
		}
		var key int64
		if kMajor == 0 {
			if kVal > maxInt64Value {
				return nil, errors.New("COSE map key exceeds signed integer range")
			}
			key = int64(kVal)
		} else if kMajor == 1 {
			if kVal > maxInt64Value {
				return nil, errors.New("COSE map key exceeds signed integer range")
			}
			key = -1 - int64(kVal)
		} else {
			return nil, fmt.Errorf("unsupported map key type %d", kMajor)
		}
		if _, exists := out[key]; exists {
			return nil, fmt.Errorf("duplicate COSE map key %d", key)
		}

		// Read Value
		vMajor, vVal, err := r.readHeader()
		if err != nil {
			return nil, err
		}
		switch vMajor {
		case 0: // Unsigned int
			if vVal > maxInt64Value {
				return nil, fmt.Errorf("COSE integer value exceeds signed range for key %d", key)
			}
			out[key] = int64(vVal)
		case 1: // Negative int
			if vVal > maxInt64Value {
				return nil, fmt.Errorf("COSE integer value exceeds signed range for key %d", key)
			}
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
			return nil, fmt.Errorf("unsupported COSE value type %d for key %d", vMajor, key)
		}
	}
	return out, nil
}

// ExtractAttestationAuthData parses a definite-length CBOR attestation object
// and returns its top-level authData byte string. It deliberately parses map
// structure instead of searching for attacker-controlled byte substrings.
func ExtractAttestationAuthData(data []byte) ([]byte, error) {
	if len(data) == 0 || len(data) > 1<<20 {
		return nil, errors.New("attestation object must be between 1 byte and 1 MiB")
	}
	r := &cborReader{data: data}
	major, count, err := r.readHeader()
	if err != nil {
		return nil, err
	}
	if major != 5 || count > 32 {
		return nil, errors.New("attestation object must be a bounded CBOR map")
	}
	var authData []byte
	for i := uint64(0); i < count; i++ {
		keyMajor, keyLength, err := r.readHeader()
		if err != nil {
			return nil, err
		}
		if keyMajor != 3 || keyLength > 128 {
			return nil, errors.New("attestation object contains an invalid map key")
		}
		keyBytes, err := r.readBytes(int(keyLength))
		if err != nil {
			return nil, err
		}
		valueMajor, valueLength, err := r.readHeader()
		if err != nil {
			return nil, err
		}
		if string(keyBytes) == "authData" {
			if authData != nil {
				return nil, errors.New("attestation object contains duplicate authData")
			}
			if valueMajor != 2 || valueLength < 37 || valueLength > 64<<10 {
				return nil, errors.New("attestation authData must be a bounded byte string")
			}
			value, err := r.readBytes(int(valueLength))
			if err != nil {
				return nil, err
			}
			authData = append([]byte{}, value...)
			continue
		}
		if err := r.skipValue(valueMajor, valueLength, 0); err != nil {
			return nil, err
		}
	}
	if authData == nil {
		return nil, errors.New("authData key not found in attestation object")
	}
	if r.pos != len(r.data) {
		return nil, errors.New("trailing data after attestation object")
	}
	return authData, nil
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
	if n < 0 || r.pos < 0 || r.pos > len(r.data) || n > len(r.data)-r.pos {
		return nil, ioEOF()
	}
	b := r.data[r.pos : r.pos+n]
	r.pos += n
	return b, nil
}

func (r *cborReader) skipValue(major byte, value uint64, depth int) error {
	if depth > 16 {
		return errors.New("CBOR nesting exceeds limit")
	}
	switch major {
	case 0, 1, 7:
		return nil
	case 2, 3:
		if value > uint64(len(r.data)-r.pos) {
			return ioEOF()
		}
		_, err := r.readBytes(int(value))
		return err
	case 4:
		if value > 1024 {
			return errors.New("CBOR array exceeds limit")
		}
		for i := uint64(0); i < value; i++ {
			childMajor, childValue, err := r.readHeader()
			if err != nil {
				return err
			}
			if err := r.skipValue(childMajor, childValue, depth+1); err != nil {
				return err
			}
		}
		return nil
	case 5:
		if value > 1024 {
			return errors.New("CBOR map exceeds limit")
		}
		for i := uint64(0); i < value*2; i++ {
			childMajor, childValue, err := r.readHeader()
			if err != nil {
				return err
			}
			if err := r.skipValue(childMajor, childValue, depth+1); err != nil {
				return err
			}
		}
		return nil
	case 6:
		childMajor, childValue, err := r.readHeader()
		if err != nil {
			return err
		}
		return r.skipValue(childMajor, childValue, depth+1)
	default:
		return fmt.Errorf("unsupported CBOR major type %d", major)
	}
}

func ioEOF() error {
	return errors.New("unexpected EOF in CBOR data")
}

// GenerateRecoveryCodes produces count crypto-random recovery codes in format xxxx-xxxx-xxxx and returns plaintext and SHA-256 hashes.
func GenerateRecoveryCodes(count int) (plaintext []string, hashes []string, err error) {
	if count <= 0 {
		count = 8
	}
	if count > 100 {
		return nil, nil, errors.New("recovery code count exceeds 100")
	}
	const charset = "23456789ABCDEFGHJKLMNPQRSTUVWXYZ" // base32 without confusing chars (0,1,O,I)
	charsetSize := big.NewInt(int64(len(charset)))
	for i := 0; i < count; i++ {
		var code strings.Builder
		for j := 0; j < 12; j++ {
			if j > 0 && j%4 == 0 {
				code.WriteByte('-')
			}
			index, err := rand.Int(rand.Reader, charsetSize)
			if err != nil {
				return nil, nil, err
			}
			code.WriteByte(charset[index.Int64()])
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

// DefaultRPID defaults the RP ID to the administration request host. The public
// mirror hostname can differ from the dedicated administration hostname and is
// therefore only a fallback.
func DefaultRPID(cfgRPID string, publicBaseURL string, requestHost string) string {
	if value := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(cfgRPID), ".")); value != "" {
		return value
	}
	if host := requestHostname(requestHost); host != "" {
		return host
	}
	if publicBaseURL != "" {
		if u, err := url.Parse(publicBaseURL); err == nil && u.Hostname() != "" {
			return strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
		}
	}
	return "localhost"
}

func requestHostname(requestHost string) string {
	requestHost = strings.TrimSpace(requestHost)
	if requestHost == "" {
		return ""
	}
	u, err := url.Parse("//" + requestHost)
	if err != nil {
		return ""
	}
	return strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
}

func isLoopbackWebAuthnHost(host string) bool {
	host = strings.Trim(strings.ToLower(host), "[]")
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// DefaultOrigin defaults to the administration request authority. External
// Shared Nginx commonly terminates TLS, so non-loopback hosts use HTTPS even
// when the Go request itself arrives over loopback HTTP.
func DefaultOrigin(cfgOrigins []string, publicBaseURL string, rHost string, isTLS bool) string {
	requestOrigin := ""
	if host := requestHostname(rHost); host != "" {
		scheme := "https"
		if !isTLS && isLoopbackWebAuthnHost(host) {
			scheme = "http"
		}
		if origin, err := canonicalWebAuthnOrigin(scheme + "://" + rHost); err == nil {
			requestOrigin = origin
		}
	}
	firstConfiguredOrigin := ""
	for _, configured := range cfgOrigins {
		origin, err := canonicalWebAuthnOrigin(configured)
		if err != nil {
			continue
		}
		if firstConfiguredOrigin == "" {
			firstConfiguredOrigin = origin
		}
		if requestOrigin != "" && origin == requestOrigin {
			return origin
		}
	}
	if firstConfiguredOrigin != "" {
		return firstConfiguredOrigin
	}
	if requestOrigin != "" {
		return requestOrigin
	}
	if publicBaseURL != "" {
		if origin, err := canonicalWebAuthnOrigin(publicBaseURL); err == nil {
			return origin
		}
	}
	return "http://localhost"
}
