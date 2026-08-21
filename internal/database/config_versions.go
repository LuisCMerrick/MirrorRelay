package database

import (
	"context"
	"database/sql"
	"time"

	"github.com/LuisCMerrick/MirrorRelay/internal/model"
)

type scanner interface {
	Scan(dest ...any) error
}

func (s *Store) AddConfigVersion(ctx context.Context, v model.ConfigVersion, historyLimit int) (model.ConfigVersion, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return v, err
	}
	defer tx.Rollback()
	if v.Version == 0 {
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(version),0)+1 FROM config_versions`).Scan(&v.Version); err != nil {
			return v, err
		}
	}
	if v.CreatedAt.IsZero() {
		v.CreatedAt = time.Now()
	}
	if v.Active {
		if _, err := tx.ExecContext(ctx, `UPDATE config_versions SET active=0`); err != nil {
			return v, err
		}
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO config_versions(version,created_at,operator,description,configuration_hash,
validation_ok,validation_result,active,snapshot,configuration) VALUES(?,?,?,?,?,?,?,?,?,?)`, v.Version,
		v.CreatedAt.UTC().Format(time.RFC3339Nano), v.Operator, v.Description, v.ConfigurationHash, v.ValidationOK,
		v.ValidationResult, v.Active, v.Snapshot, v.Configuration)
	if err != nil {
		return v, err
	}
	v.ID, err = res.LastInsertId()
	if err != nil {
		return v, err
	}
	if historyLimit > 0 {
		if _, err := tx.ExecContext(ctx, `DELETE FROM config_versions WHERE active=0 AND id NOT IN (
SELECT id FROM config_versions ORDER BY id DESC LIMIT ?)`, historyLimit); err != nil {
			return v, err
		}
	}
	return v, tx.Commit()
}

func (s *Store) ListConfigVersions(ctx context.Context, limit int) ([]model.ConfigVersion, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,version,created_at,operator,description,configuration_hash,
validation_ok,validation_result,active,snapshot,configuration FROM config_versions ORDER BY version DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]model.ConfigVersion, 0)
	for rows.Next() {
		v, err := scanConfigVersion(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *Store) ConfigVersion(ctx context.Context, version int64) (model.ConfigVersion, error) {
	return scanConfigVersion(s.db.QueryRowContext(ctx, `SELECT id,version,created_at,operator,description,configuration_hash,
validation_ok,validation_result,active,snapshot,configuration FROM config_versions WHERE version=?`, version))
}

func (s *Store) ActiveConfigVersion(ctx context.Context) (model.ConfigVersion, error) {
	return scanConfigVersion(s.db.QueryRowContext(ctx, `SELECT id,version,created_at,operator,description,configuration_hash,
validation_ok,validation_result,active,snapshot,configuration FROM config_versions WHERE active=1 ORDER BY version DESC LIMIT 1`))
}

func scanConfigVersion(row scanner) (model.ConfigVersion, error) {
	var v model.ConfigVersion
	var created string
	var valid, active int
	err := row.Scan(&v.ID, &v.Version, &created, &v.Operator, &v.Description, &v.ConfigurationHash, &valid,
		&v.ValidationResult, &active, &v.Snapshot, &v.Configuration)
	v.CreatedAt, v.ValidationOK, v.Active = parseTime(created), valid != 0, active != 0
	return v, err
}

func (s *Store) SetActiveConfigVersion(ctx context.Context, version int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE config_versions SET active=0`); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `UPDATE config_versions SET active=1 WHERE version=?`, version)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return tx.Commit()
}

func (s *Store) ListCustomConfigs(ctx context.Context) ([]model.CustomConfig, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,context,repository_id,enabled,content,last_validation_result,created_at,updated_at FROM custom_configs ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]model.CustomConfig, 0)
	for rows.Next() {
		value, err := scanCustomConfig(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, value)
	}
	return out, rows.Err()
}

func (s *Store) CustomConfig(ctx context.Context, id int64) (model.CustomConfig, error) {
	return scanCustomConfig(s.db.QueryRowContext(ctx, `SELECT id,name,context,repository_id,enabled,content,last_validation_result,created_at,updated_at FROM custom_configs WHERE id=?`, id))
}

func scanCustomConfig(row scanner) (model.CustomConfig, error) {
	var c model.CustomConfig
	var enabled int
	var created, updated string
	err := row.Scan(&c.ID, &c.Name, &c.Context, &c.RepositoryID, &enabled, &c.Content, &c.LastResult, &created, &updated)
	c.Enabled = enabled != 0
	c.CreatedAt, c.UpdatedAt = parseTime(created), parseTime(updated)
	return c, err
}

func (s *Store) CreateCustomConfig(ctx context.Context, c model.CustomConfig) (model.CustomConfig, error) {
	now := nowText()
	res, err := s.db.ExecContext(ctx, `INSERT INTO custom_configs(name,context,repository_id,enabled,content,last_validation_result,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?)`,
		c.Name, c.Context, c.RepositoryID, c.Enabled, c.Content, c.LastResult, now, now)
	if err != nil {
		return c, err
	}
	c.ID, err = res.LastInsertId()
	if err != nil {
		return c, err
	}
	return s.CustomConfig(ctx, c.ID)
}

func (s *Store) UpdateCustomConfig(ctx context.Context, c model.CustomConfig) (model.CustomConfig, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE custom_configs SET name=?,context=?,repository_id=?,enabled=?,content=?,last_validation_result=?,updated_at=? WHERE id=?`,
		c.Name, c.Context, c.RepositoryID, c.Enabled, c.Content, c.LastResult, nowText(), c.ID)
	if err != nil {
		return c, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return c, sql.ErrNoRows
	}
	return s.CustomConfig(ctx, c.ID)
}

func (s *Store) DeleteCustomConfig(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM custom_configs WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) ReplaceCustomConfigs(ctx context.Context, values []model.CustomConfig) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM custom_configs`); err != nil {
		return err
	}
	for _, value := range values {
		created, updated := value.CreatedAt, value.UpdatedAt
		if created.IsZero() {
			created = time.Now()
		}
		if updated.IsZero() {
			updated = created
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO custom_configs(id,name,context,repository_id,enabled,content,last_validation_result,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?)`,
			value.ID, value.Name, value.Context, value.RepositoryID, value.Enabled, value.Content, value.LastResult,
			created.UTC().Format(time.RFC3339Nano), updated.UTC().Format(time.RFC3339Nano)); err != nil {
			return err
		}
	}
	return tx.Commit()
}
