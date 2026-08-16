package database

import (
	"context"
	"database/sql"
	"time"

	"github.com/LuisCMerrick/MirrorRelay/internal/model"
)

func (s *Store) CountUsers(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM users").Scan(&n)
	return n, err
}

func (s *Store) CreateUser(ctx context.Context, username, passwordHash string) error {
	now := nowText()
	_, err := s.db.ExecContext(ctx, `INSERT INTO users(username,password_hash,created_at,updated_at) VALUES(?,?,?,?)`, username, passwordHash, now, now)
	return err
}

func (s *Store) ListUsers(ctx context.Context) ([]model.User, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,username,created_at,updated_at FROM users ORDER BY username`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var users []model.User
	for rows.Next() {
		var user model.User
		var created, updated string
		if err := rows.Scan(&user.ID, &user.Username, &created, &updated); err != nil {
			return nil, err
		}
		user.CreatedAt, user.UpdatedAt = parseTime(created), parseTime(updated)
		users = append(users, user)
	}
	return users, rows.Err()
}

func (s *Store) DeleteUser(ctx context.Context, id int64) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM users WHERE id=?`, id)
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) PutSession(ctx context.Context, idHash string, userID int64, username, csrf string, expires time.Time) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO sessions(id_hash,user_id,username,csrf_token,expires_at,updated_at) VALUES(?,?,?,?,?,?)
ON CONFLICT(id_hash) DO UPDATE SET user_id=excluded.user_id,username=excluded.username,csrf_token=excluded.csrf_token,expires_at=excluded.expires_at,updated_at=excluded.updated_at`,
		idHash, userID, username, csrf, expires.UTC().Format(time.RFC3339Nano), nowText())
	return err
}

func (s *Store) GetSession(ctx context.Context, idHash string) (int64, string, string, time.Time, error) {
	var userID int64
	var username, csrf, expires string
	err := s.db.QueryRowContext(ctx, `SELECT user_id,username,csrf_token,expires_at FROM sessions WHERE id_hash=?`, idHash).
		Scan(&userID, &username, &csrf, &expires)
	return userID, username, csrf, parseTime(expires), err
}

func (s *Store) DeleteSession(ctx context.Context, idHash string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE id_hash=?`, idHash)
	return err
}

func (s *Store) DeleteUserSessions(ctx context.Context, userID int64, exceptIDHash ...string) error {
	if len(exceptIDHash) > 0 && exceptIDHash[0] != "" {
		_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE user_id=? AND id_hash!=?`, userID, exceptIDHash[0])
		return err
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE user_id=?`, userID)
	return err
}

func (s *Store) UserByName(ctx context.Context, username string) (model.User, error) {
	var u model.User
	var created, updated string
	err := s.db.QueryRowContext(ctx, `SELECT id,username,password_hash,created_at,updated_at FROM users WHERE username=?`, username).
		Scan(&u.ID, &u.Username, &u.PasswordHash, &created, &updated)
	u.CreatedAt, u.UpdatedAt = parseTime(created), parseTime(updated)
	return u, err
}

func (s *Store) UpdatePassword(ctx context.Context, id int64, hash string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE users SET password_hash=?,updated_at=? WHERE id=?`, hash, nowText(), id)
	return err
}

func (s *Store) AddAudit(ctx context.Context, a model.AuditEntry) error {
	if a.Time.IsZero() {
		a.Time = time.Now()
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO audit_log(time,username,client_ip,action,object,detail,succeeded) VALUES(?,?,?,?,?,?,?)`, a.Time.UTC().Format(time.RFC3339Nano), a.Username, a.ClientIP, a.Action, a.Object, a.Detail, a.Succeeded)
	return err
}

func (s *Store) ListAudit(ctx context.Context, limit int) ([]model.AuditEntry, error) {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,time,username,client_ip,action,object,detail,succeeded FROM audit_log ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.AuditEntry
	for rows.Next() {
		var a model.AuditEntry
		var at string
		var ok int
		if err := rows.Scan(&a.ID, &at, &a.Username, &a.ClientIP, &a.Action, &a.Object, &a.Detail, &ok); err != nil {
			return nil, err
		}
		a.Time, a.Succeeded = parseTime(at), ok != 0
		out = append(out, a)
	}
	return out, rows.Err()
}
