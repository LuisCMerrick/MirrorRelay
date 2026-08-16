// Package database handles persistent state and migrations.
package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

const AppearanceSettingsKey = "appearance_settings_v1"

type Store struct{ db *sql.DB }

func Open(path string) (*Store, error) {
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
	if err := s.migrate(ctx); err != nil {
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
