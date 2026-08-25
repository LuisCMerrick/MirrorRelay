package database

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
)

const (
	clusterMutationTokenEnvelopePrefix = "mrenc:v1:"
	clusterMutationTokenAAD            = "MirrorRelay cluster node mutation token v1"
)

type clusterMutationTokenKey struct {
	id   string
	aead cipher.AEAD
}

type clusterMutationTokenCipher struct {
	primary clusterMutationTokenKey
	byID    map[string]clusterMutationTokenKey
}

// LoadClusterMutationTokenKeyFiles loads a rotation-capable keyring. The first
// file is the active encryption key and subsequent files are decrypt-only
// legacy keys. Each file must contain either exactly 32 raw bytes or one
// base64-encoded 32-byte key.
func LoadClusterMutationTokenKeyFiles(paths []string) ([][]byte, error) {
	keys := make([][]byte, 0, len(paths))
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			return nil, fmt.Errorf("stat cluster mutation-token key file %q: %w", path, err)
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("cluster mutation-token key file %q is not a regular file", path)
		}
		if info.Mode().Perm()&0o007 != 0 || info.Mode().Perm()&0o020 != 0 {
			return nil, fmt.Errorf("cluster mutation-token key file %q must not be accessible by other users or writable by its group", path)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read cluster mutation-token key file %q: %w", path, err)
		}
		key, err := decodeClusterMutationTokenKey(data)
		if err != nil {
			return nil, fmt.Errorf("decode cluster mutation-token key file %q: %w", path, err)
		}
		keys = append(keys, key)
	}
	return keys, nil
}

func decodeClusterMutationTokenKey(data []byte) ([]byte, error) {
	if len(data) == 32 {
		return append([]byte(nil), data...), nil
	}
	encoded := strings.TrimSpace(string(data))
	for _, encoding := range []*base64.Encoding{
		base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding,
	} {
		decoded, err := encoding.DecodeString(encoded)
		if err == nil && len(decoded) == 32 {
			return decoded, nil
		}
	}
	return nil, errors.New("key must contain exactly 32 raw bytes or a base64-encoded 32-byte value")
}

// WithClusterMutationTokenKeys configures transparent authenticated
// encryption for cluster_nodes mutation credentials.
func WithClusterMutationTokenKeys(keys ...[]byte) OpenOption {
	return func(store *Store) error {
		if len(keys) == 0 {
			return nil
		}
		keyring := &clusterMutationTokenCipher{byID: make(map[string]clusterMutationTokenKey, len(keys))}
		for index, raw := range keys {
			if len(raw) != 32 {
				return fmt.Errorf("cluster mutation-token encryption key %d must be exactly 32 bytes", index+1)
			}
			block, err := aes.NewCipher(raw)
			if err != nil {
				return fmt.Errorf("initialize cluster mutation-token encryption key %d: %w", index+1, err)
			}
			aead, err := cipher.NewGCM(block)
			if err != nil {
				return fmt.Errorf("initialize cluster mutation-token AEAD %d: %w", index+1, err)
			}
			digest := sha256.Sum256(raw)
			id := hex.EncodeToString(digest[:8])
			if _, duplicate := keyring.byID[id]; duplicate {
				return fmt.Errorf("cluster mutation-token encryption key %d duplicates an earlier key", index+1)
			}
			entry := clusterMutationTokenKey{id: id, aead: aead}
			keyring.byID[id] = entry
			if index == 0 {
				keyring.primary = entry
			}
		}
		store.clusterTokenCipher = keyring
		return nil
	}
}

func (c *clusterMutationTokenCipher) encrypt(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	nonce := make([]byte, c.primary.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("generate cluster mutation-token nonce: %w", err)
	}
	payload := c.primary.aead.Seal(nonce, nonce, []byte(value), []byte(clusterMutationTokenAAD))
	return clusterMutationTokenEnvelopePrefix + c.primary.id + ":" + base64.RawURLEncoding.EncodeToString(payload), nil
}

func (c *clusterMutationTokenCipher) decrypt(value string) (string, bool, error) {
	if value == "" {
		return "", true, nil
	}
	if !strings.HasPrefix(value, clusterMutationTokenEnvelopePrefix) {
		return "", false, errors.New("cluster mutation-token ciphertext has an unsupported format")
	}
	remainder := strings.TrimPrefix(value, clusterMutationTokenEnvelopePrefix)
	keyID, encoded, found := strings.Cut(remainder, ":")
	if !found || keyID == "" || encoded == "" {
		return "", false, errors.New("cluster mutation-token ciphertext is malformed")
	}
	key, ok := c.byID[keyID]
	if !ok {
		return "", false, fmt.Errorf("cluster mutation-token ciphertext requires unavailable key %s", keyID)
	}
	payload, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil || len(payload) < key.aead.NonceSize()+key.aead.Overhead() {
		return "", false, errors.New("cluster mutation-token ciphertext payload is malformed")
	}
	nonce := payload[:key.aead.NonceSize()]
	plaintext, err := key.aead.Open(nil, nonce, payload[key.aead.NonceSize():], []byte(clusterMutationTokenAAD))
	if err != nil {
		return "", false, errors.New("cluster mutation-token ciphertext authentication failed")
	}
	return string(plaintext), keyID == c.primary.id, nil
}

func (s *Store) encryptClusterMutationToken(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	if s.clusterTokenCipher == nil {
		return "", errors.New("cluster mutation-token encryption is not configured")
	}
	return s.clusterTokenCipher.encrypt(value)
}

func (s *Store) decryptClusterMutationToken(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	if s.clusterTokenCipher == nil {
		return "", errors.New("cluster mutation-token encryption is not configured")
	}
	plaintext, _, err := s.clusterTokenCipher.decrypt(value)
	return plaintext, err
}

func (s *Store) migrateClusterMutationTokens(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `SELECT id,mutation_token,mutation_token_ciphertext FROM cluster_nodes WHERE mutation_token<>'' OR mutation_token_ciphertext<>'' ORDER BY id`)
	if err != nil {
		return fmt.Errorf("load cluster mutation tokens for encryption: %w", err)
	}
	type storedToken struct {
		id         int64
		plaintext  string
		ciphertext string
	}
	var values []storedToken
	for rows.Next() {
		var value storedToken
		if err := rows.Scan(&value.id, &value.plaintext, &value.ciphertext); err != nil {
			rows.Close()
			return fmt.Errorf("scan cluster mutation token for encryption: %w", err)
		}
		values = append(values, value)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close cluster mutation-token migration rows: %w", err)
	}
	if len(values) == 0 {
		return nil
	}
	if s.clusterTokenCipher == nil {
		return errors.New("cluster mutation-token key files are required to encrypt existing node credentials")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin cluster mutation-token encryption: %w", err)
	}
	defer tx.Rollback()
	for _, value := range values {
		plaintext := value.plaintext
		currentKey := false
		if value.ciphertext != "" {
			decrypted, current, err := s.clusterTokenCipher.decrypt(value.ciphertext)
			if err != nil {
				return fmt.Errorf("decrypt cluster node %d mutation token: %w", value.id, err)
			}
			if plaintext != "" && plaintext != decrypted {
				return fmt.Errorf("cluster node %d has conflicting plaintext and encrypted mutation tokens", value.id)
			}
			plaintext = decrypted
			currentKey = current
		}
		if value.plaintext == "" && value.ciphertext != "" && currentKey {
			continue
		}
		ciphertext, err := s.clusterTokenCipher.encrypt(plaintext)
		if err != nil {
			return fmt.Errorf("encrypt cluster node %d mutation token: %w", value.id, err)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE cluster_nodes SET mutation_token='',mutation_token_ciphertext=? WHERE id=?`, ciphertext, value.id); err != nil {
			return fmt.Errorf("persist encrypted cluster node %d mutation token: %w", value.id, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit cluster mutation-token encryption: %w", err)
	}
	return nil
}
