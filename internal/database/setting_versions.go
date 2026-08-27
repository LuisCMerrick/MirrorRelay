package database

import (
	"context"
	"database/sql"
	"time"

	"github.com/LuisCMerrick/MirrorRelay/internal/model"
)

func (s *Store) AddSettingVersion(ctx context.Context, v model.SettingVersion, historyLimit int) (model.SettingVersion, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return v, err
	}
	defer tx.Rollback()
	v, err = addSettingVersionTx(ctx, tx, v, historyLimit)
	if err != nil {
		return v, err
	}
	return v, tx.Commit()
}

func (s *Store) PutSettingWithVersion(ctx context.Context, key, value string, v model.SettingVersion, historyLimit int) (model.SettingVersion, error) {
	return s.PutSettingsWithVersion(ctx, map[string]string{key: value}, v, historyLimit)
}

// PutSettingsWithVersion atomically persists a related set of settings and the
// redacted rollback snapshot that describes them.
func (s *Store) PutSettingsWithVersion(ctx context.Context, values map[string]string, v model.SettingVersion, historyLimit int) (model.SettingVersion, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return v, err
	}
	defer tx.Rollback()
	for key, value := range values {
		if _, err := tx.ExecContext(ctx, `INSERT INTO settings(key,value,updated_at) VALUES(?,?,?)
ON CONFLICT(key) DO UPDATE SET value=excluded.value,updated_at=excluded.updated_at`, key, value, nowText()); err != nil {
			return v, err
		}
	}
	v, err = addSettingVersionTx(ctx, tx, v, historyLimit)
	if err != nil {
		return v, err
	}
	return v, tx.Commit()
}

func (s *Store) DeleteSettingWithVersion(ctx context.Context, key string, v model.SettingVersion, historyLimit int) (model.SettingVersion, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return v, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM settings WHERE key=?`, key); err != nil {
		return v, err
	}
	v, err = addSettingVersionTx(ctx, tx, v, historyLimit)
	if err != nil {
		return v, err
	}
	return v, tx.Commit()
}

func addSettingVersionTx(ctx context.Context, tx *sql.Tx, v model.SettingVersion, historyLimit int) (model.SettingVersion, error) {
	if v.Version == 0 {
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(version),0)+1 FROM setting_versions`).Scan(&v.Version); err != nil {
			return v, err
		}
	}
	if v.CreatedAt.IsZero() {
		v.CreatedAt = time.Now()
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO setting_versions(version,created_at,operator,source,description,diff_summary,settings_json)
VALUES(?,?,?,?,?,?,?)`, v.Version, v.CreatedAt.UTC().Format(time.RFC3339Nano), v.Operator, v.Source, v.Description, v.DiffSummary, v.Settings)
	if err != nil {
		return v, err
	}
	v.ID, err = res.LastInsertId()
	if err != nil {
		return v, err
	}
	if historyLimit > 0 {
		if _, err := tx.ExecContext(ctx, `DELETE FROM setting_versions WHERE id NOT IN (
SELECT id FROM setting_versions ORDER BY id DESC LIMIT ?)`, historyLimit); err != nil {
			return v, err
		}
	}
	return v, nil
}

func (s *Store) ListSettingVersions(ctx context.Context, limit int) ([]model.SettingVersion, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,version,created_at,operator,source,description,diff_summary,settings_json
FROM setting_versions ORDER BY version DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]model.SettingVersion, 0)
	for rows.Next() {
		var v model.SettingVersion
		var created string
		if err := rows.Scan(&v.ID, &v.Version, &created, &v.Operator, &v.Source, &v.Description, &v.DiffSummary, &v.Settings); err != nil {
			return nil, err
		}
		v.CreatedAt = parseTime(created)
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *Store) GetSettingVersion(ctx context.Context, version int64) (model.SettingVersion, error) {
	var v model.SettingVersion
	var created string
	err := s.db.QueryRowContext(ctx, `SELECT id,version,created_at,operator,source,description,diff_summary,settings_json
FROM setting_versions WHERE version=?`, version).Scan(&v.ID, &v.Version, &created, &v.Operator, &v.Source, &v.Description, &v.DiffSummary, &v.Settings)
	if err != nil {
		return v, err
	}
	v.CreatedAt = parseTime(created)
	return v, nil
}
