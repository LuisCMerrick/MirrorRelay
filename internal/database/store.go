// Package database handles persistent state and migrations.
package database

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

const AppearanceSettingsKey = "appearance_settings_v1"

const auxiliaryURLSigningKeySetting = "auxiliary_url_signing_key_v1"

type Store struct {
	db                 *sql.DB
	clusterTokenCipher *clusterMutationTokenCipher
}

type OpenOption func(*Store) error

func Open(path string, options ...OpenOption) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	for _, pragma := range []string{"PRAGMA journal_mode=WAL", "PRAGMA foreign_keys=ON", "PRAGMA busy_timeout=5000", "PRAGMA synchronous=NORMAL"} {
		if _, err := db.ExecContext(ctx, pragma); err != nil {
			db.Close()
			return nil, fmt.Errorf("sqlite %s: %w", pragma, err)
		}
	}
	s := &Store{db: db}
	for _, option := range options {
		if err := option(s); err != nil {
			db.Close()
			return nil, err
		}
	}
	if err := s.migrate(ctx); err != nil {
		db.Close()
		return nil, err
	}
	if err := s.migrateClusterMutationTokens(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) Setting(ctx context.Context, key string) (string, bool, error) {
	var value string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key=?`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return value, err == nil, err
}

func (s *Store) PutSetting(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO settings(key,value,updated_at) VALUES(?,?,?)
ON CONFLICT(key) DO UPDATE SET value=excluded.value,updated_at=excluded.updated_at`, key, value, nowText())
	return err
}

func (s *Store) DeleteSetting(ctx context.Context, key string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM settings WHERE key=?`, key)
	return err
}

func (s *Store) AuxiliaryURLSigningKey(ctx context.Context) ([]byte, error) {
	if raw, found, err := s.Setting(ctx, auxiliaryURLSigningKeySetting); err != nil {
		return nil, fmt.Errorf("read auxiliary URL signing key: %w", err)
	} else if found {
		decoded, decodeErr := base64.RawURLEncoding.DecodeString(raw)
		if decodeErr != nil || len(decoded) != 32 {
			return nil, errors.New("stored auxiliary URL signing key is invalid")
		}
		return decoded, nil
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("generate auxiliary URL signing key: %w", err)
	}
	if err := s.PutSetting(ctx, auxiliaryURLSigningKeySetting, base64.RawURLEncoding.EncodeToString(key)); err != nil {
		return nil, fmt.Errorf("persist auxiliary URL signing key: %w", err)
	}
	return key, nil
}
