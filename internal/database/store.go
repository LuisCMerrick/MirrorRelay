// Package database handles persistent state and migrations.
package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/LuisCMerrick/MirrorRelay/internal/model"
	"github.com/LuisCMerrick/MirrorRelay/internal/stats"
)

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

func (s *Store) migrate(ctx context.Context) error {
	const schema = `
CREATE TABLE IF NOT EXISTS users (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 username TEXT NOT NULL UNIQUE COLLATE NOCASE,
 password_hash TEXT NOT NULL,
 created_at TEXT NOT NULL,
 updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS sessions (
 id_hash TEXT PRIMARY KEY,
 user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 username TEXT NOT NULL,
 csrf_token TEXT NOT NULL,
 expires_at TEXT NOT NULL,
 updated_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_sessions_expires ON sessions(expires_at);
CREATE TABLE IF NOT EXISTS mirrors (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 name TEXT NOT NULL,
 slug TEXT NOT NULL UNIQUE COLLATE NOCASE,

 type TEXT NOT NULL DEFAULT 'generic',
 enabled INTEGER NOT NULL DEFAULT 1,
 description TEXT NOT NULL DEFAULT '',
 public_mode TEXT NOT NULL DEFAULT 'path',
 public_host TEXT NOT NULL DEFAULT '',
 public_path TEXT NOT NULL DEFAULT '',
 proxy_mode TEXT NOT NULL DEFAULT 'transparent',
 cache_enabled INTEGER NOT NULL DEFAULT 0,
 cache_profile TEXT NOT NULL DEFAULT 'standard',
 rewrite_enabled INTEGER NOT NULL DEFAULT 0,
 html_rewrite_enabled INTEGER NOT NULL DEFAULT 0,
 rewrite_profile TEXT NOT NULL DEFAULT '',
 rewrite_hosts TEXT NOT NULL DEFAULT '[]',
 health_check_enabled INTEGER NOT NULL DEFAULT 1,
 health_check_path TEXT NOT NULL DEFAULT '',
 health_interval_sec INTEGER NOT NULL DEFAULT 60,
 health_timeout_sec INTEGER NOT NULL DEFAULT 5,
 health_method TEXT NOT NULL DEFAULT 'HEAD',
 health_expected INTEGER NOT NULL DEFAULT 200,
 redirect_mode TEXT NOT NULL DEFAULT 'rewrite',
	profile_name TEXT NOT NULL DEFAULT 'Custom',
	profile_version TEXT NOT NULL DEFAULT '1.0.0',
	rate_limit_profile TEXT NOT NULL DEFAULT '',
	access_policy TEXT NOT NULL DEFAULT 'public',
 strip_prefix TEXT NOT NULL DEFAULT '',
 add_prefix TEXT NOT NULL DEFAULT '',
 host_rewrite TEXT NOT NULL DEFAULT '',
 header_add TEXT NOT NULL DEFAULT '{}',
 header_remove TEXT NOT NULL DEFAULT '[]',
 connect_timeout_sec INTEGER NOT NULL DEFAULT 10,
 read_timeout_sec INTEGER NOT NULL DEFAULT 3600,
 send_timeout_sec INTEGER NOT NULL DEFAULT 3600,
 metadata_rewrite_limit_bytes INTEGER NOT NULL DEFAULT 0,
 metadata_ttl_sec INTEGER NOT NULL DEFAULT 0,
 package_ttl_sec INTEGER NOT NULL DEFAULT 0,
 immutable_ttl_sec INTEGER NOT NULL DEFAULT 0,
 blob_ttl_sec INTEGER NOT NULL DEFAULT 0,
 cache_authenticated INTEGER NOT NULL DEFAULT 0,
 auth_mode TEXT NOT NULL DEFAULT 'direct',
 token_upstream TEXT NOT NULL DEFAULT '',
 blob_redirect_mode TEXT NOT NULL DEFAULT 'full_proxy',
 pull_only INTEGER NOT NULL DEFAULT 1,
	config_state TEXT NOT NULL DEFAULT 'pending',
	config_error TEXT NOT NULL DEFAULT '',
	allow_http INTEGER NOT NULL DEFAULT 0,
	allow_private INTEGER NOT NULL DEFAULT 0,
 insecure_tls INTEGER NOT NULL DEFAULT 0,
 bandwidth_limit_bps INTEGER NOT NULL DEFAULT 0,
 max_concurrency INTEGER NOT NULL DEFAULT 0,
 created_at TEXT NOT NULL,
 updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS upstreams (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 mirror_id INTEGER NOT NULL REFERENCES mirrors(id) ON DELETE CASCADE,
 url TEXT NOT NULL,
 host TEXT NOT NULL DEFAULT '',
 priority INTEGER NOT NULL DEFAULT 100,
 weight INTEGER NOT NULL DEFAULT 1,
 enabled INTEGER NOT NULL DEFAULT 1,
 health_status TEXT NOT NULL DEFAULT 'unknown',
 last_check TEXT NOT NULL DEFAULT '',
 latency_ms INTEGER NOT NULL DEFAULT 0,
 last_error TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_upstreams_mirror_priority ON upstreams(mirror_id, priority, id);
CREATE TABLE IF NOT EXISTS audit_log (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 time TEXT NOT NULL,
 username TEXT NOT NULL,
 client_ip TEXT NOT NULL,
 action TEXT NOT NULL,
 object TEXT NOT NULL,
 detail TEXT NOT NULL,
 succeeded INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_audit_time ON audit_log(time DESC);
CREATE TABLE IF NOT EXISTS settings (
 key TEXT PRIMARY KEY,
 value TEXT NOT NULL,
 updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS stats_hourly (
 hour TEXT NOT NULL,
 mirror_id INTEGER NOT NULL,
 requests INTEGER NOT NULL,
 bytes INTEGER NOT NULL,
 upstream_bytes INTEGER NOT NULL DEFAULT 0,
 cache_bytes INTEGER NOT NULL DEFAULT 0,
	cache_hits INTEGER NOT NULL,
	cache_misses INTEGER NOT NULL,
	upstream_errors INTEGER NOT NULL DEFAULT 0,
	status_2xx INTEGER NOT NULL DEFAULT 0,
	status_3xx INTEGER NOT NULL DEFAULT 0,
	status_4xx INTEGER NOT NULL DEFAULT 0,
	status_5xx INTEGER NOT NULL DEFAULT 0,
 PRIMARY KEY(hour, mirror_id)
);
CREATE TABLE IF NOT EXISTS config_versions (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 version INTEGER NOT NULL UNIQUE,
 created_at TEXT NOT NULL,
 operator TEXT NOT NULL,
 description TEXT NOT NULL,
 configuration_hash TEXT NOT NULL,
 validation_ok INTEGER NOT NULL,
 validation_result TEXT NOT NULL,
 active INTEGER NOT NULL DEFAULT 0,
 snapshot TEXT NOT NULL,
 configuration TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS custom_configs (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 name TEXT NOT NULL UNIQUE COLLATE NOCASE,
 context TEXT NOT NULL,
 repository_id INTEGER NOT NULL DEFAULT 0,
 enabled INTEGER NOT NULL DEFAULT 1,
 content TEXT NOT NULL,
 last_validation_result TEXT NOT NULL DEFAULT '',
 created_at TEXT NOT NULL,
 updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS cache_generations (
 scope TEXT NOT NULL,
 repository_id INTEGER NOT NULL DEFAULT 0,
 object_id TEXT NOT NULL DEFAULT '',
 generation INTEGER NOT NULL DEFAULT 1,
 updated_at TEXT NOT NULL,
 PRIMARY KEY(scope,repository_id,object_id)
);
CREATE TABLE IF NOT EXISTS purge_jobs (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 scope TEXT NOT NULL,
 repository_id INTEGER NOT NULL DEFAULT 0,
 object_id TEXT NOT NULL DEFAULT '',
 old_generation INTEGER NOT NULL,
 new_generation INTEGER NOT NULL,
 reclaim_state TEXT NOT NULL,
 reclaimed_bytes INTEGER NOT NULL DEFAULT 0,
 error TEXT NOT NULL DEFAULT '',
 operator TEXT NOT NULL,
 created_at TEXT NOT NULL,
 updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS cluster_nodes (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL,
  url TEXT NOT NULL UNIQUE,
  region TEXT NOT NULL,
  country TEXT NOT NULL DEFAULT '',
  priority INTEGER NOT NULL DEFAULT 100,
  weight INTEGER NOT NULL DEFAULT 100,
  enabled INTEGER NOT NULL DEFAULT 1,
  health_status TEXT NOT NULL DEFAULT 'unknown',
  config_status TEXT NOT NULL DEFAULT 'unknown',
  config_fingerprint TEXT NOT NULL DEFAULT '',
  version TEXT NOT NULL DEFAULT '',
  protocol_version INTEGER NOT NULL DEFAULT 0,
  capabilities TEXT NOT NULL DEFAULT '[]',
  last_check TEXT NOT NULL DEFAULT '',
  last_error TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS cluster_settings (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL,
  updated_at TEXT NOT NULL
);`
	_, err := s.db.ExecContext(ctx, schema)
	if err != nil {
		return fmt.Errorf("migrate database: %w", err)
	}
	columns := map[string]string{
		"type": "TEXT NOT NULL DEFAULT 'generic'", "public_mode": "TEXT NOT NULL DEFAULT 'path'", "public_host": "TEXT NOT NULL DEFAULT ''",
		"public_path": "TEXT NOT NULL DEFAULT ''", "proxy_mode": "TEXT NOT NULL DEFAULT 'transparent'", "cache_profile": "TEXT NOT NULL DEFAULT 'standard'",
		"rewrite_enabled": "INTEGER NOT NULL DEFAULT 0", "html_rewrite_enabled": "INTEGER NOT NULL DEFAULT 0", "rewrite_profile": "TEXT NOT NULL DEFAULT ''", "rewrite_hosts": "TEXT NOT NULL DEFAULT '[]'", "profile_name": "TEXT NOT NULL DEFAULT 'Custom'",
		"profile_version": "TEXT NOT NULL DEFAULT '1.0.0'", "strip_prefix": "TEXT NOT NULL DEFAULT ''", "add_prefix": "TEXT NOT NULL DEFAULT ''",
		"rate_limit_profile": "TEXT NOT NULL DEFAULT ''", "access_policy": "TEXT NOT NULL DEFAULT 'public'", "allow_http": "INTEGER NOT NULL DEFAULT 0",
		"host_rewrite": "TEXT NOT NULL DEFAULT ''", "auth_mode": "TEXT NOT NULL DEFAULT 'direct'", "token_upstream": "TEXT NOT NULL DEFAULT ''",
		"header_add": "TEXT NOT NULL DEFAULT '{}'", "header_remove": "TEXT NOT NULL DEFAULT '[]'", "connect_timeout_sec": "INTEGER NOT NULL DEFAULT 10",
		"read_timeout_sec": "INTEGER NOT NULL DEFAULT 3600", "send_timeout_sec": "INTEGER NOT NULL DEFAULT 3600", "metadata_rewrite_limit_bytes": "INTEGER NOT NULL DEFAULT 0",
		"metadata_ttl_sec": "INTEGER NOT NULL DEFAULT 0", "package_ttl_sec": "INTEGER NOT NULL DEFAULT 0", "immutable_ttl_sec": "INTEGER NOT NULL DEFAULT 0",
		"blob_ttl_sec": "INTEGER NOT NULL DEFAULT 0", "cache_authenticated": "INTEGER NOT NULL DEFAULT 0",
		"blob_redirect_mode": "TEXT NOT NULL DEFAULT 'full_proxy'", "pull_only": "INTEGER NOT NULL DEFAULT 1", "config_state": "TEXT NOT NULL DEFAULT 'pending'",
		"config_error": "TEXT NOT NULL DEFAULT ''",
	}
	for name, definition := range columns {
		if err := s.ensureColumn(ctx, "mirrors", name, definition); err != nil {
			return err
		}
	}
	if err := s.ensureColumn(ctx, "upstreams", "host", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	for name, definition := range map[string]string{
		"upstream_bytes":  "INTEGER NOT NULL DEFAULT 0",
		"cache_bytes":     "INTEGER NOT NULL DEFAULT 0",
		"upstream_errors": "INTEGER NOT NULL DEFAULT 0",
		"status_2xx":      "INTEGER NOT NULL DEFAULT 0",
		"status_3xx":      "INTEGER NOT NULL DEFAULT 0",
		"status_4xx":      "INTEGER NOT NULL DEFAULT 0",
		"status_5xx":      "INTEGER NOT NULL DEFAULT 0",
	} {
		if err := s.ensureColumn(ctx, "stats_hourly", name, definition); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) LoadStatsHourly(ctx context.Context, since string) ([]stats.PersistentRecord, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT hour,mirror_id,requests,bytes,upstream_bytes,cache_bytes,cache_hits,cache_misses,upstream_errors,status_2xx,status_3xx,status_4xx,status_5xx FROM stats_hourly WHERE hour>=? ORDER BY hour,mirror_id`, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var records []stats.PersistentRecord
	for rows.Next() {
		var record stats.PersistentRecord
		if err := rows.Scan(&record.Hour, &record.MirrorID, &record.Counters.Requests, &record.Counters.Bytes,
			&record.Counters.UpstreamBytes, &record.Counters.CacheBytes, &record.Counters.CacheHits,
			&record.Counters.CacheMisses, &record.Counters.UpstreamErrors, &record.Counters.Status2xx,
			&record.Counters.Status3xx, &record.Counters.Status4xx, &record.Counters.Status5xx); err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

func (s *Store) SaveStatsHourly(ctx context.Context, records []stats.PersistentRecord, before string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, record := range records {
		counters := record.Counters
		if _, err := tx.ExecContext(ctx, `INSERT INTO stats_hourly(hour,mirror_id,requests,bytes,cache_hits,cache_misses,upstream_bytes,cache_bytes,upstream_errors,status_2xx,status_3xx,status_4xx,status_5xx)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(hour,mirror_id) DO UPDATE SET requests=excluded.requests,bytes=excluded.bytes,cache_hits=excluded.cache_hits,cache_misses=excluded.cache_misses,upstream_bytes=excluded.upstream_bytes,cache_bytes=excluded.cache_bytes,upstream_errors=excluded.upstream_errors,status_2xx=excluded.status_2xx,status_3xx=excluded.status_3xx,status_4xx=excluded.status_4xx,status_5xx=excluded.status_5xx`,
			record.Hour, record.MirrorID, counters.Requests, counters.Bytes, counters.CacheHits, counters.CacheMisses,
			counters.UpstreamBytes, counters.CacheBytes, counters.UpstreamErrors, counters.Status2xx,
			counters.Status3xx, counters.Status4xx, counters.Status5xx); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM stats_hourly WHERE hour<?`, before); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) CacheGenerations(ctx context.Context, repositoryID int64, objectID string) (int64, int64, int64, error) {
	global, err := s.cacheGeneration(ctx, "global", 0, "")
	if err != nil {
		return 0, 0, 0, err
	}
	repository, err := s.cacheGeneration(ctx, "repository", repositoryID, "")
	if err != nil {
		return 0, 0, 0, err
	}
	object, err := s.cacheGeneration(ctx, "object", repositoryID, objectID)
	return global, repository, object, err
}

func (s *Store) ListCacheGenerations(ctx context.Context) ([]model.CacheGeneration, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT scope,repository_id,object_id,generation FROM cache_generations`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var generations []model.CacheGeneration
	for rows.Next() {
		var generation model.CacheGeneration
		if err := rows.Scan(&generation.Scope, &generation.RepositoryID, &generation.ObjectID, &generation.Generation); err != nil {
			return nil, err
		}
		generations = append(generations, generation)
	}
	return generations, rows.Err()
}

func (s *Store) ListPurgeJobs(ctx context.Context, limit int) ([]model.PurgeJob, error) {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,scope,repository_id,object_id,old_generation,new_generation,reclaim_state,reclaimed_bytes,error,operator,created_at,updated_at FROM purge_jobs ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var jobs []model.PurgeJob
	for rows.Next() {
		var job model.PurgeJob
		var created, updated string
		if err := rows.Scan(&job.ID, &job.Scope, &job.RepositoryID, &job.ObjectID, &job.OldGeneration, &job.NewGeneration,
			&job.ReclaimState, &job.ReclaimedBytes, &job.Error, &job.Operator, &created, &updated); err != nil {
			return nil, err
		}
		job.CreatedAt, job.UpdatedAt = parseTime(created), parseTime(updated)
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}

func (s *Store) cacheGeneration(ctx context.Context, scope string, repositoryID int64, objectID string) (int64, error) {
	var generation int64
	err := s.db.QueryRowContext(ctx, `SELECT generation FROM cache_generations WHERE scope=? AND repository_id=? AND object_id=?`, scope, repositoryID, objectID).Scan(&generation)
	if errors.Is(err, sql.ErrNoRows) {
		return 1, nil
	}
	return generation, err
}

func (s *Store) PurgeCache(ctx context.Context, scope string, repositoryID int64, objectID, operator string) (model.PurgeJob, error) {
	if scope != "global" && scope != "repository" && scope != "object" {
		return model.PurgeJob{}, errors.New("invalid cache purge scope")
	}
	if scope == "global" {
		repositoryID, objectID = 0, ""
	}
	if scope == "repository" {
		objectID = ""
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.PurgeJob{}, err
	}
	defer tx.Rollback()
	old := int64(1)
	err = tx.QueryRowContext(ctx, `SELECT generation FROM cache_generations WHERE scope=? AND repository_id=? AND object_id=?`, scope, repositoryID, objectID).Scan(&old)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return model.PurgeJob{}, err
	}
	next := old + 1
	now := nowText()
	_, err = tx.ExecContext(ctx, `INSERT INTO cache_generations(scope,repository_id,object_id,generation,updated_at) VALUES(?,?,?,?,?)
ON CONFLICT(scope,repository_id,object_id) DO UPDATE SET generation=excluded.generation,updated_at=excluded.updated_at`, scope, repositoryID, objectID, next, now)
	if err != nil {
		return model.PurgeJob{}, err
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO purge_jobs(scope,repository_id,object_id,old_generation,new_generation,reclaim_state,operator,created_at,updated_at) VALUES(?,?,?,?,?,'pending',?,?,?)`, scope, repositoryID, objectID, old, next, operator, now, now)
	if err != nil {
		return model.PurgeJob{}, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return model.PurgeJob{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.PurgeJob{}, err
	}
	return s.PurgeJob(ctx, id)
}

func (s *Store) PurgeJob(ctx context.Context, id int64) (model.PurgeJob, error) {
	var job model.PurgeJob
	var created, updated string
	err := s.db.QueryRowContext(ctx, `SELECT id,scope,repository_id,object_id,old_generation,new_generation,reclaim_state,reclaimed_bytes,error,operator,created_at,updated_at FROM purge_jobs WHERE id=?`, id).
		Scan(&job.ID, &job.Scope, &job.RepositoryID, &job.ObjectID, &job.OldGeneration, &job.NewGeneration, &job.ReclaimState, &job.ReclaimedBytes, &job.Error, &job.Operator, &created, &updated)
	job.CreatedAt, job.UpdatedAt = parseTime(created), parseTime(updated)
	return job, err
}

func (s *Store) PendingPurgeJobs(ctx context.Context, limit int) ([]model.PurgeJob, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,scope,repository_id,object_id,old_generation,new_generation,reclaim_state,reclaimed_bytes,error,operator,created_at,updated_at FROM purge_jobs WHERE reclaim_state IN ('pending','running') ORDER BY id LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var jobs []model.PurgeJob
	for rows.Next() {
		var job model.PurgeJob
		var created, updated string
		if err := rows.Scan(&job.ID, &job.Scope, &job.RepositoryID, &job.ObjectID, &job.OldGeneration, &job.NewGeneration, &job.ReclaimState, &job.ReclaimedBytes, &job.Error, &job.Operator, &created, &updated); err != nil {
			return nil, err
		}
		job.CreatedAt, job.UpdatedAt = parseTime(created), parseTime(updated)
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}

func (s *Store) UpdatePurgeJob(ctx context.Context, id int64, state string, reclaimed int64, message string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE purge_jobs SET reclaim_state=?,reclaimed_bytes=?,error=?,updated_at=? WHERE id=?`, state, reclaimed, message, nowText(), id)
	return err
}

func (s *Store) ensureColumn(ctx context.Context, table, column, definition string) error {
	rows, err := s.db.QueryContext(ctx, "PRAGMA table_info("+table+")")
	if err != nil {
		return err
	}
	found := false
	for rows.Next() {
		var cid int
		var name, typ string
		var notnull, pk int
		var defaultValue any
		if err := rows.Scan(&cid, &name, &typ, &notnull, &defaultValue, &pk); err != nil {
			rows.Close()
			return err
		}
		if name == column {
			found = true
		}
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if found {
		return nil
	}
	if _, err := s.db.ExecContext(ctx, "ALTER TABLE "+table+" ADD COLUMN "+column+" "+definition); err != nil {
		return fmt.Errorf("add %s.%s: %w", table, column, err)
	}
	return nil
}

func nowText() string { return time.Now().UTC().Format(time.RFC3339Nano) }

func encodeStrings(values []string) string {
	encoded, _ := json.Marshal(values)
	return string(encoded)
}

func encodeMap(values map[string]string) string {
	if values == nil {
		values = map[string]string{}
	}
	encoded, _ := json.Marshal(values)
	return string(encoded)
}

func parseTime(v string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, v)
	return t
}

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

const mirrorColumns = `id,name,slug,type,enabled,description,public_mode,public_host,public_path,proxy_mode,
cache_enabled,cache_profile,rewrite_enabled,html_rewrite_enabled,rewrite_profile,rewrite_hosts,health_check_enabled,health_check_path,
	health_interval_sec,health_timeout_sec,health_method,health_expected,redirect_mode,profile_name,profile_version,
	rate_limit_profile,access_policy,strip_prefix,add_prefix,host_rewrite,header_add,header_remove,connect_timeout_sec,read_timeout_sec,send_timeout_sec,
	metadata_rewrite_limit_bytes,metadata_ttl_sec,package_ttl_sec,immutable_ttl_sec,blob_ttl_sec,cache_authenticated,
	auth_mode,token_upstream,blob_redirect_mode,pull_only,config_state,config_error,
	allow_http,allow_private,insecure_tls,bandwidth_limit_bps,max_concurrency,created_at,updated_at`

func (s *Store) ListMirrors(ctx context.Context) ([]model.Mirror, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+mirrorColumns+` FROM mirrors ORDER BY slug`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	mirrors := make([]model.Mirror, 0)
	for rows.Next() {
		m, err := scanMirror(rows)
		if err != nil {
			return nil, err
		}
		mirrors = append(mirrors, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range mirrors {
		mirrors[i].Upstreams, err = s.listUpstreams(ctx, mirrors[i].ID)
		if err != nil {
			return nil, err
		}
	}
	return mirrors, nil
}

type scanner interface{ Scan(...any) error }

func scanMirror(row scanner) (model.Mirror, error) {
	var m model.Mirror
	var enabled, cacheEnabled, rewriteEnabled, htmlRewriteEnabled, healthEnabled, pullOnly, cacheAuthenticated, allowHTTP, allowPrivate, insecure int
	var created, updated, rewriteHosts, headerAdd, headerRemove string
	err := row.Scan(&m.ID, &m.Name, &m.Slug, &m.Type, &enabled, &m.Description, &m.PublicMode, &m.PublicHost, &m.PublicPath,
		&m.ProxyMode, &cacheEnabled, &m.CacheProfile, &rewriteEnabled, &htmlRewriteEnabled, &m.RewriteProfile, &rewriteHosts, &healthEnabled,
		&m.HealthCheckPath, &m.HealthIntervalSec, &m.HealthTimeoutSec, &m.HealthMethod, &m.HealthExpected,
		&m.RedirectMode, &m.ProfileName, &m.ProfileVersion, &m.RateLimitProfile, &m.AccessPolicy, &m.StripPrefix, &m.AddPrefix, &m.HostRewrite,
		&headerAdd, &headerRemove, &m.ConnectTimeoutSec, &m.ReadTimeoutSec, &m.SendTimeoutSec, &m.MetadataLimitBytes,
		&m.MetadataTTLSec, &m.PackageTTLSec, &m.ImmutableTTLSec, &m.BlobTTLSec, &cacheAuthenticated, &m.AuthMode,
		&m.TokenUpstream, &m.BlobRedirectMode, &pullOnly, &m.ConfigState, &m.ConfigError, &allowHTTP, &allowPrivate, &insecure,
		&m.BandwidthLimitBPS, &m.MaxConcurrency, &created, &updated)
	if err != nil {
		return m, err
	}
	m.Enabled, m.CacheEnabled, m.RewriteEnabled, m.HTMLRewriteEnabled, m.HealthCheckEnabled, m.PullOnly = enabled != 0, cacheEnabled != 0, rewriteEnabled != 0, htmlRewriteEnabled != 0, healthEnabled != 0, pullOnly != 0
	_ = json.Unmarshal([]byte(rewriteHosts), &m.RewriteHosts)
	_ = json.Unmarshal([]byte(headerAdd), &m.HeaderAdd)
	_ = json.Unmarshal([]byte(headerRemove), &m.HeaderRemove)
	m.CacheAuthenticated = cacheAuthenticated != 0
	m.AllowHTTP, m.AllowPrivate, m.InsecureTLS = allowHTTP != 0, allowPrivate != 0, insecure != 0
	m.CreatedAt, m.UpdatedAt = parseTime(created), parseTime(updated)
	return m, nil
}

func (s *Store) Mirror(ctx context.Context, id int64) (model.Mirror, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+mirrorColumns+` FROM mirrors WHERE id=?`, id)
	m, err := scanMirror(row)
	if err != nil {
		return m, err
	}
	m.Upstreams, err = s.listUpstreams(ctx, m.ID)
	return m, err
}

func (s *Store) listUpstreams(ctx context.Context, mirrorID int64) ([]model.Upstream, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,mirror_id,url,host,priority,weight,enabled,health_status,last_check,latency_ms,last_error FROM upstreams WHERE mirror_id=? ORDER BY priority,id`, mirrorID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Upstream
	for rows.Next() {
		var u model.Upstream
		var enabled int
		var lastCheck string
		if err := rows.Scan(&u.ID, &u.MirrorID, &u.URL, &u.Host, &u.Priority, &u.Weight, &enabled, &u.HealthStatus, &lastCheck, &u.LatencyMS, &u.LastError); err != nil {
			return nil, err
		}
		u.Enabled = enabled != 0
		u.LastCheck = parseTime(lastCheck)
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *Store) CreateMirror(ctx context.Context, m model.Mirror) (model.Mirror, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return m, err
	}
	defer tx.Rollback()
	now := nowText()
	res, err := tx.ExecContext(ctx, `INSERT INTO mirrors(name,slug,type,enabled,description,public_mode,public_host,public_path,proxy_mode,
cache_enabled,cache_profile,rewrite_enabled,html_rewrite_enabled,rewrite_profile,rewrite_hosts,health_check_enabled,health_check_path,health_interval_sec,
	health_timeout_sec,health_method,health_expected,redirect_mode,profile_name,profile_version,rate_limit_profile,access_policy,strip_prefix,add_prefix,host_rewrite,
		header_add,header_remove,connect_timeout_sec,read_timeout_sec,send_timeout_sec,metadata_rewrite_limit_bytes,metadata_ttl_sec,package_ttl_sec,immutable_ttl_sec,blob_ttl_sec,cache_authenticated,
		auth_mode,token_upstream,blob_redirect_mode,pull_only,config_state,config_error,allow_http,allow_private,insecure_tls,bandwidth_limit_bps,
		max_concurrency,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		m.Name, m.Slug, m.Type, m.Enabled, m.Description, m.PublicMode, m.PublicHost, m.PublicPath, m.ProxyMode,
		m.CacheEnabled, m.CacheProfile, m.RewriteEnabled, m.HTMLRewriteEnabled, m.RewriteProfile, encodeStrings(m.RewriteHosts), m.HealthCheckEnabled, m.HealthCheckPath,
		m.HealthIntervalSec, m.HealthTimeoutSec, m.HealthMethod, m.HealthExpected, m.RedirectMode, m.ProfileName,
		m.ProfileVersion, m.RateLimitProfile, m.AccessPolicy, m.StripPrefix, m.AddPrefix, m.HostRewrite, encodeMap(m.HeaderAdd), encodeStrings(m.HeaderRemove),
		m.ConnectTimeoutSec, m.ReadTimeoutSec, m.SendTimeoutSec, m.MetadataLimitBytes, m.MetadataTTLSec, m.PackageTTLSec, m.ImmutableTTLSec, m.BlobTTLSec, m.CacheAuthenticated,
		m.AuthMode, m.TokenUpstream, m.BlobRedirectMode,
		m.PullOnly, m.ConfigState, m.ConfigError, m.AllowHTTP, m.AllowPrivate, m.InsecureTLS, m.BandwidthLimitBPS, m.MaxConcurrency, now, now)
	if err != nil {
		return m, err
	}
	m.ID, err = res.LastInsertId()
	if err != nil {
		return m, err
	}
	if err := replaceUpstreams(ctx, tx, m.ID, m.Upstreams); err != nil {
		return m, err
	}
	if err := tx.Commit(); err != nil {
		return m, err
	}
	return s.Mirror(ctx, m.ID)
}

func (s *Store) UpdateMirror(ctx context.Context, m model.Mirror) (model.Mirror, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return m, err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `UPDATE mirrors SET name=?,slug=?,type=?,enabled=?,description=?,public_mode=?,public_host=?,
public_path=?,proxy_mode=?,cache_enabled=?,cache_profile=?,rewrite_enabled=?,html_rewrite_enabled=?,rewrite_profile=?,rewrite_hosts=?,health_check_enabled=?,
health_check_path=?,health_interval_sec=?,health_timeout_sec=?,health_method=?,health_expected=?,redirect_mode=?,profile_name=?,
	profile_version=?,rate_limit_profile=?,access_policy=?,strip_prefix=?,add_prefix=?,host_rewrite=?,header_add=?,header_remove=?,connect_timeout_sec=?,read_timeout_sec=?,send_timeout_sec=?,
	metadata_rewrite_limit_bytes=?,metadata_ttl_sec=?,package_ttl_sec=?,immutable_ttl_sec=?,blob_ttl_sec=?,cache_authenticated=?,auth_mode=?,token_upstream=?,blob_redirect_mode=?,pull_only=?,
	config_state=?,config_error=?,allow_http=?,allow_private=?,insecure_tls=?,bandwidth_limit_bps=?,max_concurrency=?,updated_at=? WHERE id=?`,
		m.Name, m.Slug, m.Type, m.Enabled, m.Description, m.PublicMode, m.PublicHost, m.PublicPath, m.ProxyMode,
		m.CacheEnabled, m.CacheProfile, m.RewriteEnabled, m.HTMLRewriteEnabled, m.RewriteProfile, encodeStrings(m.RewriteHosts), m.HealthCheckEnabled, m.HealthCheckPath,
		m.HealthIntervalSec, m.HealthTimeoutSec, m.HealthMethod, m.HealthExpected, m.RedirectMode, m.ProfileName,
		m.ProfileVersion, m.RateLimitProfile, m.AccessPolicy, m.StripPrefix, m.AddPrefix, m.HostRewrite, encodeMap(m.HeaderAdd), encodeStrings(m.HeaderRemove),
		m.ConnectTimeoutSec, m.ReadTimeoutSec, m.SendTimeoutSec, m.MetadataLimitBytes, m.MetadataTTLSec, m.PackageTTLSec, m.ImmutableTTLSec, m.BlobTTLSec, m.CacheAuthenticated,
		m.AuthMode, m.TokenUpstream, m.BlobRedirectMode,
		m.PullOnly, m.ConfigState, m.ConfigError, m.AllowHTTP, m.AllowPrivate, m.InsecureTLS, m.BandwidthLimitBPS, m.MaxConcurrency,
		nowText(), m.ID)
	if err != nil {
		return m, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return m, sql.ErrNoRows
	}
	if err := replaceUpstreams(ctx, tx, m.ID, m.Upstreams); err != nil {
		return m, err
	}
	if err := tx.Commit(); err != nil {
		return m, err
	}
	return s.Mirror(ctx, m.ID)
}

func (s *Store) ReplaceMirrors(ctx context.Context, mirrors []model.Mirror) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM upstreams`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM mirrors`); err != nil {
		return err
	}
	for _, m := range mirrors {
		created, updated := m.CreatedAt, m.UpdatedAt
		if created.IsZero() {
			created = time.Now()
		}
		if updated.IsZero() {
			updated = created
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO mirrors(id,name,slug,type,enabled,description,public_mode,public_host,public_path,proxy_mode,
cache_enabled,cache_profile,rewrite_enabled,html_rewrite_enabled,rewrite_profile,rewrite_hosts,health_check_enabled,health_check_path,health_interval_sec,
	health_timeout_sec,health_method,health_expected,redirect_mode,profile_name,profile_version,rate_limit_profile,access_policy,strip_prefix,add_prefix,host_rewrite,
		header_add,header_remove,connect_timeout_sec,read_timeout_sec,send_timeout_sec,metadata_rewrite_limit_bytes,metadata_ttl_sec,package_ttl_sec,immutable_ttl_sec,blob_ttl_sec,cache_authenticated,
		auth_mode,token_upstream,blob_redirect_mode,pull_only,config_state,config_error,allow_http,allow_private,insecure_tls,bandwidth_limit_bps,
		max_concurrency,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			m.ID, m.Name, m.Slug, m.Type, m.Enabled, m.Description, m.PublicMode, m.PublicHost, m.PublicPath, m.ProxyMode,
			m.CacheEnabled, m.CacheProfile, m.RewriteEnabled, m.HTMLRewriteEnabled, m.RewriteProfile, encodeStrings(m.RewriteHosts), m.HealthCheckEnabled, m.HealthCheckPath,
			m.HealthIntervalSec, m.HealthTimeoutSec, m.HealthMethod, m.HealthExpected, m.RedirectMode, m.ProfileName,
			m.ProfileVersion, m.RateLimitProfile, m.AccessPolicy, m.StripPrefix, m.AddPrefix, m.HostRewrite, encodeMap(m.HeaderAdd), encodeStrings(m.HeaderRemove),
			m.ConnectTimeoutSec, m.ReadTimeoutSec, m.SendTimeoutSec, m.MetadataLimitBytes, m.MetadataTTLSec, m.PackageTTLSec, m.ImmutableTTLSec, m.BlobTTLSec, m.CacheAuthenticated,
			m.AuthMode, m.TokenUpstream, m.BlobRedirectMode,
			m.PullOnly, m.ConfigState, m.ConfigError, m.AllowHTTP, m.AllowPrivate, m.InsecureTLS, m.BandwidthLimitBPS, m.MaxConcurrency,
			created.UTC().Format(time.RFC3339Nano), updated.UTC().Format(time.RFC3339Nano))
		if err != nil {
			return err
		}
		if err := replaceUpstreams(ctx, tx, m.ID, m.Upstreams); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func replaceUpstreams(ctx context.Context, tx *sql.Tx, mirrorID int64, upstreams []model.Upstream) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM upstreams WHERE mirror_id=?`, mirrorID); err != nil {
		return err
	}
	for _, u := range upstreams {
		if _, err := tx.ExecContext(ctx, `INSERT INTO upstreams(mirror_id,url,host,priority,weight,enabled,health_status,last_check,latency_ms,last_error) VALUES(?,?,?,?,?,?,'unknown','',0,'')`, mirrorID, u.URL, u.Host, u.Priority, u.Weight, u.Enabled); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) DeleteMirror(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM mirrors WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) SetMirrorEnabled(ctx context.Context, id int64, enabled bool) error {
	res, err := s.db.ExecContext(ctx, `UPDATE mirrors SET enabled=?,updated_at=? WHERE id=?`, enabled, nowText(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) SetConfigState(ctx context.Context, ids []int64, state, detail string) error {
	if len(ids) == 0 {
		_, err := s.db.ExecContext(ctx, `UPDATE mirrors SET config_state=?,config_error=?,updated_at=?`, state, detail, nowText())
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, id := range ids {
		if _, err := tx.ExecContext(ctx, `UPDATE mirrors SET config_state=?,config_error=?,updated_at=? WHERE id=?`, state, detail, nowText(), id); err != nil {
			return err
		}
	}
	return tx.Commit()
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

func (s *Store) ReplaceConfiguration(ctx context.Context, mirrors []model.Mirror, configs []model.CustomConfig) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM upstreams`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM mirrors`); err != nil {
		return err
	}
	for _, m := range mirrors {
		created, updated := m.CreatedAt, m.UpdatedAt
		if created.IsZero() {
			created = time.Now()
		}
		if updated.IsZero() {
			updated = created
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO mirrors(id,name,slug,type,enabled,description,public_mode,public_host,public_path,proxy_mode,
cache_enabled,cache_profile,rewrite_enabled,html_rewrite_enabled,rewrite_profile,rewrite_hosts,health_check_enabled,health_check_path,health_interval_sec,
	health_timeout_sec,health_method,health_expected,redirect_mode,profile_name,profile_version,rate_limit_profile,access_policy,strip_prefix,add_prefix,host_rewrite,
		header_add,header_remove,connect_timeout_sec,read_timeout_sec,send_timeout_sec,metadata_rewrite_limit_bytes,metadata_ttl_sec,package_ttl_sec,immutable_ttl_sec,blob_ttl_sec,cache_authenticated,
		auth_mode,token_upstream,blob_redirect_mode,pull_only,config_state,config_error,allow_http,allow_private,insecure_tls,bandwidth_limit_bps,
		max_concurrency,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			m.ID, m.Name, m.Slug, m.Type, m.Enabled, m.Description, m.PublicMode, m.PublicHost, m.PublicPath, m.ProxyMode,
			m.CacheEnabled, m.CacheProfile, m.RewriteEnabled, m.HTMLRewriteEnabled, m.RewriteProfile, encodeStrings(m.RewriteHosts), m.HealthCheckEnabled, m.HealthCheckPath,
			m.HealthIntervalSec, m.HealthTimeoutSec, m.HealthMethod, m.HealthExpected, m.RedirectMode, m.ProfileName,
			m.ProfileVersion, m.RateLimitProfile, m.AccessPolicy, m.StripPrefix, m.AddPrefix, m.HostRewrite, encodeMap(m.HeaderAdd), encodeStrings(m.HeaderRemove),
			m.ConnectTimeoutSec, m.ReadTimeoutSec, m.SendTimeoutSec, m.MetadataLimitBytes, m.MetadataTTLSec, m.PackageTTLSec, m.ImmutableTTLSec, m.BlobTTLSec, m.CacheAuthenticated,
			m.AuthMode, m.TokenUpstream, m.BlobRedirectMode,
			m.PullOnly, m.ConfigState, m.ConfigError, m.AllowHTTP, m.AllowPrivate, m.InsecureTLS, m.BandwidthLimitBPS, m.MaxConcurrency,
			created.UTC().Format(time.RFC3339Nano), updated.UTC().Format(time.RFC3339Nano))
		if err != nil {
			return err
		}
		if err := replaceUpstreams(ctx, tx, m.ID, m.Upstreams); err != nil {
			return err
		}
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM custom_configs`); err != nil {
		return err
	}
	for _, value := range configs {
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

func (s *Store) UpdateUpstreamHealth(ctx context.Context, upstreamID int64, status string, latency int64, detail string, at time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE upstreams SET health_status=?,last_check=?,latency_ms=?,last_error=? WHERE id=?`, status, at.UTC().Format(time.RFC3339Nano), latency, detail, upstreamID)
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

func IsConflict(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "unique constraint") || strings.Contains(s, "constraint failed")
}

func IsNotFound(err error) bool { return errors.Is(err, sql.ErrNoRows) }

func (s *Store) ListClusterNodes(ctx context.Context) ([]model.ClusterNode, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,url,region,country,priority,weight,enabled,health_status,config_status,config_fingerprint,version,protocol_version,capabilities,last_check,last_error,created_at,updated_at FROM cluster_nodes ORDER BY priority ASC, id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var nodes []model.ClusterNode
	for rows.Next() {
		var n model.ClusterNode
		var enabled int
		var capsJSON string
		var lastCheckStr, createdStr, updatedStr string
		if err := rows.Scan(&n.ID, &n.Name, &n.URL, &n.Region, &n.Country, &n.Priority, &n.Weight, &enabled, &n.HealthStatus, &n.ConfigStatus, &n.ConfigFingerprint, &n.Version, &n.ProtocolVersion, &capsJSON, &lastCheckStr, &n.LastError, &createdStr, &updatedStr); err != nil {
			return nil, err
		}
		n.Enabled = enabled != 0
		if capsJSON != "" {
			_ = json.Unmarshal([]byte(capsJSON), &n.Capabilities)
		}
		if lastCheckStr != "" {
			n.LastCheck = parseTime(lastCheckStr)
		}
		n.CreatedAt = parseTime(createdStr)
		n.UpdatedAt = parseTime(updatedStr)
		nodes = append(nodes, n)
	}
	return nodes, rows.Err()
}

func (s *Store) GetClusterNode(ctx context.Context, id int64) (model.ClusterNode, error) {
	var n model.ClusterNode
	var enabled int
	var capsJSON string
	var lastCheckStr, createdStr, updatedStr string
	err := s.db.QueryRowContext(ctx, `SELECT id,name,url,region,country,priority,weight,enabled,health_status,config_status,config_fingerprint,version,protocol_version,capabilities,last_check,last_error,created_at,updated_at FROM cluster_nodes WHERE id=?`, id).Scan(
		&n.ID, &n.Name, &n.URL, &n.Region, &n.Country, &n.Priority, &n.Weight, &enabled, &n.HealthStatus, &n.ConfigStatus, &n.ConfigFingerprint, &n.Version, &n.ProtocolVersion, &capsJSON, &lastCheckStr, &n.LastError, &createdStr, &updatedStr)
	if err != nil {
		return n, err
	}
	n.Enabled = enabled != 0
	if capsJSON != "" {
		_ = json.Unmarshal([]byte(capsJSON), &n.Capabilities)
	}
	if lastCheckStr != "" {
		n.LastCheck = parseTime(lastCheckStr)
	}
	n.CreatedAt = parseTime(createdStr)
	n.UpdatedAt = parseTime(updatedStr)
	return n, nil
}

func (s *Store) GetClusterNodeByURL(ctx context.Context, rawURL string) (model.ClusterNode, error) {
	var n model.ClusterNode
	var enabled int
	var capsJSON string
	var lastCheckStr, createdStr, updatedStr string
	err := s.db.QueryRowContext(ctx, `SELECT id,name,url,region,country,priority,weight,enabled,health_status,config_status,config_fingerprint,version,protocol_version,capabilities,last_check,last_error,created_at,updated_at FROM cluster_nodes WHERE url=?`, rawURL).Scan(
		&n.ID, &n.Name, &n.URL, &n.Region, &n.Country, &n.Priority, &n.Weight, &enabled, &n.HealthStatus, &n.ConfigStatus, &n.ConfigFingerprint, &n.Version, &n.ProtocolVersion, &capsJSON, &lastCheckStr, &n.LastError, &createdStr, &updatedStr)
	if err != nil {
		return n, err
	}
	n.Enabled = enabled != 0
	if capsJSON != "" {
		_ = json.Unmarshal([]byte(capsJSON), &n.Capabilities)
	}
	if lastCheckStr != "" {
		n.LastCheck = parseTime(lastCheckStr)
	}
	n.CreatedAt = parseTime(createdStr)
	n.UpdatedAt = parseTime(updatedStr)
	return n, nil
}

func (s *Store) CreateClusterNode(ctx context.Context, node model.ClusterNode) (model.ClusterNode, error) {
	now := time.Now()
	nowStr := now.UTC().Format(time.RFC3339Nano)
	if node.Priority <= 0 {
		node.Priority = 100
	}
	if node.Weight <= 0 {
		node.Weight = 100
	}
	if node.HealthStatus == "" {
		node.HealthStatus = "unknown"
	}
	if node.ConfigStatus == "" {
		node.ConfigStatus = "unknown"
	}
	capsBytes, _ := json.Marshal(node.Capabilities)
	var enabledInt int
	if node.Enabled {
		enabledInt = 1
	}
	res, err := s.db.ExecContext(ctx, `INSERT INTO cluster_nodes(name,url,region,country,priority,weight,enabled,health_status,config_status,config_fingerprint,version,protocol_version,capabilities,last_check,last_error,created_at,updated_at)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, node.Name, node.URL, node.Region, node.Country, node.Priority, node.Weight, enabledInt, node.HealthStatus, node.ConfigStatus, node.ConfigFingerprint, node.Version, node.ProtocolVersion, string(capsBytes), "", node.LastError, nowStr, nowStr)
	if err != nil {
		return node, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return node, err
	}
	node.ID = id
	node.CreatedAt = now
	node.UpdatedAt = now
	return node, nil
}

func (s *Store) UpdateClusterNode(ctx context.Context, node model.ClusterNode) (model.ClusterNode, error) {
	now := time.Now()
	nowStr := now.UTC().Format(time.RFC3339Nano)
	if node.Priority <= 0 {
		node.Priority = 100
	}
	if node.Weight <= 0 {
		node.Weight = 100
	}
	var enabledInt int
	if node.Enabled {
		enabledInt = 1
	}
	_, err := s.db.ExecContext(ctx, `UPDATE cluster_nodes SET name=?,url=?,region=?,country=?,priority=?,weight=?,enabled=?,updated_at=? WHERE id=?`,
		node.Name, node.URL, node.Region, node.Country, node.Priority, node.Weight, enabledInt, nowStr, node.ID)
	if err != nil {
		return node, err
	}
	node.UpdatedAt = now
	return node, nil
}

func (s *Store) UpdateClusterNodeStatus(ctx context.Context, id int64, healthStatus, configStatus, fingerprint, version string, protoVer int, caps []string, lastError string, lastCheck time.Time) error {
	nowStr := time.Now().UTC().Format(time.RFC3339Nano)
	var checkStr string
	if !lastCheck.IsZero() {
		checkStr = lastCheck.UTC().Format(time.RFC3339Nano)
	}
	capsBytes, _ := json.Marshal(caps)
	_, err := s.db.ExecContext(ctx, `UPDATE cluster_nodes SET health_status=?,config_status=?,config_fingerprint=?,version=?,protocol_version=?,capabilities=?,last_error=?,last_check=?,updated_at=? WHERE id=?`,
		healthStatus, configStatus, fingerprint, version, protoVer, string(capsBytes), lastError, checkStr, nowStr, id)
	return err
}

func (s *Store) DeleteClusterNode(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM cluster_nodes WHERE id=?`, id)
	return err
}

func (s *Store) SetClusterNodeEnabled(ctx context.Context, id int64, enabled bool) error {
	var enabledInt int
	if enabled {
		enabledInt = 1
	}
	nowStr := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.ExecContext(ctx, `UPDATE cluster_nodes SET enabled=?,updated_at=? WHERE id=?`, enabledInt, nowStr, id)
	return err
}

func (s *Store) ClusterSetting(ctx context.Context, key string) (string, bool, error) {
	var value string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM cluster_settings WHERE key=?`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return value, err == nil, err
}

func (s *Store) PutClusterSetting(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO cluster_settings(key,value,updated_at) VALUES(?,?,?)
ON CONFLICT(key) DO UPDATE SET value=excluded.value,updated_at=excluded.updated_at`, key, value, nowText())
	return err
}
