package database

import (
	"context"
	"fmt"
	"time"
)

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
 help_enabled INTEGER NOT NULL DEFAULT 0,
 help_json TEXT NOT NULL DEFAULT '{}',
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
  mutation_token TEXT NOT NULL DEFAULT '',
  health_status TEXT NOT NULL DEFAULT 'unknown',
  config_status TEXT NOT NULL DEFAULT 'unknown',
  config_fingerprint TEXT NOT NULL DEFAULT '',
  config_generation INTEGER NOT NULL DEFAULT 0,
  node_id TEXT NOT NULL DEFAULT '',
  coordinator_id TEXT NOT NULL DEFAULT '',
  coordinator_epoch TEXT NOT NULL DEFAULT '',
  version TEXT NOT NULL DEFAULT '',
  protocol_version INTEGER NOT NULL DEFAULT 0,
  capabilities TEXT NOT NULL DEFAULT '[]',
  repository_health TEXT NOT NULL DEFAULT '{}',
  latency_ms INTEGER NOT NULL DEFAULT 0,
  last_check TEXT NOT NULL DEFAULT '',
  last_error TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS cluster_settings (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS warmup_jobs (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  mirror_id INTEGER NOT NULL REFERENCES mirrors(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  cron_expression TEXT NOT NULL DEFAULT '',
  url_patterns TEXT NOT NULL DEFAULT '[]',
  status TEXT NOT NULL DEFAULT 'idle',
  total_items INTEGER NOT NULL DEFAULT 0,
  completed_items INTEGER NOT NULL DEFAULT 0,
  failed_items INTEGER NOT NULL DEFAULT 0,
  bytes_downloaded INTEGER NOT NULL DEFAULT 0,
  error_message TEXT NOT NULL DEFAULT '',
  last_run_at TEXT NOT NULL DEFAULT '',
  next_run_at TEXT NOT NULL DEFAULT '',
  enabled INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_warmup_mirror ON warmup_jobs(mirror_id);`
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
		"config_error": "TEXT NOT NULL DEFAULT ''", "help_enabled": "INTEGER NOT NULL DEFAULT 0", "help_json": "TEXT NOT NULL DEFAULT '{}'",
		"blocked_packages": "TEXT NOT NULL DEFAULT '[]'", "allowed_packages": "TEXT NOT NULL DEFAULT '[]'",
	}
	for name, definition := range columns {
		if err := s.ensureColumn(ctx, "mirrors", name, definition); err != nil {
			return err
		}
	}
	if err := s.ensureColumn(ctx, "users", "role", "TEXT NOT NULL DEFAULT 'admin'"); err != nil {
		return err
	}
	if err := s.ensureColumn(ctx, "sessions", "role", "TEXT NOT NULL DEFAULT 'admin'"); err != nil {
		return err
	}
	if err := s.ensureColumn(ctx, "upstreams", "host", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.ensureColumn(ctx, "cluster_nodes", "latency_ms", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	for name, definition := range map[string]string{
		"mutation_token":    "TEXT NOT NULL DEFAULT ''",
		"config_generation": "INTEGER NOT NULL DEFAULT 0",
		"node_id":           "TEXT NOT NULL DEFAULT ''",
		"coordinator_id":    "TEXT NOT NULL DEFAULT ''",
		"coordinator_epoch": "TEXT NOT NULL DEFAULT ''",
		"repository_health": "TEXT NOT NULL DEFAULT '{}'",
	} {
		if err := s.ensureColumn(ctx, "cluster_nodes", name, definition); err != nil {
			return err
		}
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

func (s *Store) ensureColumn(ctx context.Context, table, column, definition string) error {
	rows, err := s.db.QueryContext(ctx, "PRAGMA table_info("+table+")")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, typ string
		var notnull, pk int
		var defaultValue any
		if err := rows.Scan(&cid, &name, &typ, &notnull, &defaultValue, &pk); err != nil {
			return err
		}
		if name == column {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, "ALTER TABLE "+table+" ADD COLUMN "+column+" "+definition)
	return err
}

func nowText() string { return timeText(time.Now()) }

func timeText(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}

func parseTime(text string) time.Time {
	if text == "" {
		return time.Time{}
	}
	parsed, err := time.Parse(time.RFC3339Nano, text)
	if err != nil {
		parsed, _ = time.Parse(time.RFC3339, text)
	}
	return parsed.UTC()
}
