package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/LuisCMerrick/MirrorRelay/internal/model"
)

func (s *Store) CreatePasskey(ctx context.Context, p model.PasskeyCredential) error {
	now := nowText()
	transportsJSON, _ := json.Marshal(p.Transports)
	if p.Transports == nil {
		transportsJSON = []byte("[]")
	}
	be := 0
	if p.BackupEligible {
		be = 1
	}
	bs := 0
	if p.BackupState {
		bs = 1
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO passkey_credentials(
		user_id, credential_id, public_key, sign_count, aaguid, transports, backup_eligible, backup_state, display_name, created_at
	) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.UserID, p.CredentialID, p.PublicKey, p.SignCount, p.AAGUID, string(transportsJSON), be, bs, p.DisplayName, now,
	)
	return err
}

func (s *Store) GetPasskeyByCredentialID(ctx context.Context, credID string) (model.PasskeyCredential, error) {
	var p model.PasskeyCredential
	var transportsStr, createdAt string
	var lastUsedStr sql.NullString
	var be, bs int
	err := s.db.QueryRowContext(ctx, `SELECT id, user_id, credential_id, public_key, sign_count, aaguid, transports, backup_eligible, backup_state, display_name, created_at, last_used_at
FROM passkey_credentials WHERE credential_id=?`, credID).
		Scan(&p.ID, &p.UserID, &p.CredentialID, &p.PublicKey, &p.SignCount, &p.AAGUID, &transportsStr, &be, &bs, &p.DisplayName, &createdAt, &lastUsedStr)
	if err != nil {
		return p, err
	}
	p.BackupEligible = be != 0
	p.BackupState = bs != 0
	_ = json.Unmarshal([]byte(transportsStr), &p.Transports)
	p.CreatedAt = parseTime(createdAt)
	if lastUsedStr.Valid && lastUsedStr.String != "" {
		t := parseTime(lastUsedStr.String)
		p.LastUsedAt = &t
	}
	return p, nil
}

func (s *Store) ListPasskeysByUserID(ctx context.Context, userID int64) ([]model.PasskeyCredential, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, user_id, credential_id, public_key, sign_count, aaguid, transports, backup_eligible, backup_state, display_name, created_at, last_used_at
FROM passkey_credentials WHERE user_id=? ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []model.PasskeyCredential
	for rows.Next() {
		var p model.PasskeyCredential
		var transportsStr, createdAt string
		var lastUsedStr sql.NullString
		var be, bs int
		if err := rows.Scan(&p.ID, &p.UserID, &p.CredentialID, &p.PublicKey, &p.SignCount, &p.AAGUID, &transportsStr, &be, &bs, &p.DisplayName, &createdAt, &lastUsedStr); err != nil {
			return nil, err
		}
		p.BackupEligible = be != 0
		p.BackupState = bs != 0
		_ = json.Unmarshal([]byte(transportsStr), &p.Transports)
		p.CreatedAt = parseTime(createdAt)
		if lastUsedStr.Valid && lastUsedStr.String != "" {
			t := parseTime(lastUsedStr.String)
			p.LastUsedAt = &t
		}
		result = append(result, p)
	}
	return result, rows.Err()
}

func (s *Store) CountPasskeysByUserID(ctx context.Context, userID int64) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM passkey_credentials WHERE user_id=?`, userID).Scan(&count)
	return count, err
}

func (s *Store) UpdatePasskeySignCount(ctx context.Context, credID string, signCount uint32) error {
	now := nowText()
	_, err := s.db.ExecContext(ctx, `UPDATE passkey_credentials SET sign_count=?, last_used_at=? WHERE credential_id=?`, signCount, now, credID)
	return err
}

func (s *Store) UpdatePasskeyDisplayName(ctx context.Context, id, userID int64, name string) error {
	result, err := s.db.ExecContext(ctx, `UPDATE passkey_credentials SET display_name=? WHERE id=? AND user_id=?`, name, id, userID)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) DeletePasskey(ctx context.Context, id, userID int64) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM passkey_credentials WHERE id=? AND user_id=?`, id, userID)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) DeleteAllPasskeysByUserID(ctx context.Context, userID int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM passkey_credentials WHERE user_id=?`, userID)
	return err
}

func (s *Store) SaveRecoveryCodes(ctx context.Context, userID int64, hashes []string) error {
	now := nowText()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Invalidate previous recovery codes
	if _, err := tx.ExecContext(ctx, `DELETE FROM admin_recovery_codes WHERE user_id=?`, userID); err != nil {
		return err
	}

	for _, h := range hashes {
		if _, err := tx.ExecContext(ctx, `INSERT INTO admin_recovery_codes(user_id, code_hash, created_at) VALUES(?, ?, ?)`, userID, h, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) VerifyAndUseRecoveryCode(ctx context.Context, userID int64, codeHash string) (bool, error) {
	now := nowText()
	result, err := s.db.ExecContext(ctx, `UPDATE admin_recovery_codes SET used_at=? WHERE user_id=? AND code_hash=? AND used_at IS NULL`, now, userID, codeHash)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	if n == 0 {
		return false, errors.New("invalid or already used recovery code")
	}
	return true, nil
}

func (s *Store) CountValidRecoveryCodes(ctx context.Context, userID int64) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_recovery_codes WHERE user_id=? AND used_at IS NULL`, userID).Scan(&count)
	return count, err
}
