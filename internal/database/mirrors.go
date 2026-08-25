package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/LuisCMerrick/MirrorRelay/internal/model"
)

const mirrorColumns = `id,name,slug,type,enabled,description,public_mode,public_host,public_path,proxy_mode,
cache_enabled,cache_profile,rewrite_enabled,html_rewrite_enabled,rewrite_profile,rewrite_hosts,health_check_enabled,health_check_path,
	health_interval_sec,health_timeout_sec,health_method,health_expected,redirect_mode,profile_name,profile_version,
	rate_limit_profile,access_policy,strip_prefix,add_prefix,host_rewrite,header_add,header_remove,connect_timeout_sec,read_timeout_sec,send_timeout_sec,
	metadata_rewrite_limit_bytes,metadata_ttl_sec,package_ttl_sec,immutable_ttl_sec,blob_ttl_sec,cache_authenticated,
	auth_mode,token_upstream,blob_redirect_mode,pull_only,config_state,config_error,
	allow_http,allow_private,insecure_tls,bandwidth_limit_bps,max_concurrency,help_enabled,help_json,blocked_packages,allowed_packages,created_at,updated_at`

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

func scanMirror(row scanner) (model.Mirror, error) {
	var m model.Mirror
	var enabled, cacheEnabled, rewriteEnabled, htmlRewriteEnabled, healthEnabled, pullOnly, cacheAuthenticated, allowHTTP, allowPrivate, insecure, helpEnabled int
	var created, updated, rewriteHosts, headerAdd, headerRemove, helpJSON, blockedPkg, allowedPkg string
	err := row.Scan(&m.ID, &m.Name, &m.Slug, &m.Type, &enabled, &m.Description, &m.PublicMode, &m.PublicHost, &m.PublicPath,
		&m.ProxyMode, &cacheEnabled, &m.CacheProfile, &rewriteEnabled, &htmlRewriteEnabled, &m.RewriteProfile, &rewriteHosts, &healthEnabled,
		&m.HealthCheckPath, &m.HealthIntervalSec, &m.HealthTimeoutSec, &m.HealthMethod, &m.HealthExpected,
		&m.RedirectMode, &m.ProfileName, &m.ProfileVersion, &m.RateLimitProfile, &m.AccessPolicy, &m.StripPrefix, &m.AddPrefix, &m.HostRewrite,
		&headerAdd, &headerRemove, &m.ConnectTimeoutSec, &m.ReadTimeoutSec, &m.SendTimeoutSec, &m.MetadataLimitBytes,
		&m.MetadataTTLSec, &m.PackageTTLSec, &m.ImmutableTTLSec, &m.BlobTTLSec, &cacheAuthenticated, &m.AuthMode,
		&m.TokenUpstream, &m.BlobRedirectMode, &pullOnly, &m.ConfigState, &m.ConfigError, &allowHTTP, &allowPrivate, &insecure,
		&m.BandwidthLimitBPS, &m.MaxConcurrency, &helpEnabled, &helpJSON, &blockedPkg, &allowedPkg, &created, &updated)
	if err != nil {
		return m, err
	}
	m.Enabled, m.CacheEnabled, m.RewriteEnabled, m.HTMLRewriteEnabled, m.HealthCheckEnabled, m.PullOnly = enabled != 0, cacheEnabled != 0, rewriteEnabled != 0, htmlRewriteEnabled != 0, healthEnabled != 0, pullOnly != 0
	if err := json.Unmarshal([]byte(rewriteHosts), &m.RewriteHosts); err != nil {
		return m, fmt.Errorf("decode repository %d rewrite_hosts: %w", m.ID, err)
	}
	if err := json.Unmarshal([]byte(headerAdd), &m.HeaderAdd); err != nil {
		return m, fmt.Errorf("decode repository %d header_add: %w", m.ID, err)
	}
	if err := json.Unmarshal([]byte(headerRemove), &m.HeaderRemove); err != nil {
		return m, fmt.Errorf("decode repository %d header_remove: %w", m.ID, err)
	}
	if helpJSON != "" && helpJSON != "{}" {
		if err := json.Unmarshal([]byte(helpJSON), &m.Help); err != nil {
			return m, fmt.Errorf("decode repository %d help_json: %w", m.ID, err)
		}
	}
	m.Help.Enabled = helpEnabled != 0
	if err := json.Unmarshal([]byte(blockedPkg), &m.BlockedPackages); err != nil {
		return m, fmt.Errorf("decode repository %d blocked_packages: %w", m.ID, err)
	}
	if err := json.Unmarshal([]byte(allowedPkg), &m.AllowedPackages); err != nil {
		return m, fmt.Errorf("decode repository %d allowed_packages: %w", m.ID, err)
	}
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
		max_concurrency,help_enabled,help_json,blocked_packages,allowed_packages,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		m.Name, m.Slug, m.Type, m.Enabled, m.Description, m.PublicMode, m.PublicHost, m.PublicPath, m.ProxyMode,
		m.CacheEnabled, m.CacheProfile, m.RewriteEnabled, m.HTMLRewriteEnabled, m.RewriteProfile, encodeStrings(m.RewriteHosts), m.HealthCheckEnabled, m.HealthCheckPath,
		m.HealthIntervalSec, m.HealthTimeoutSec, m.HealthMethod, m.HealthExpected, m.RedirectMode, m.ProfileName,
		m.ProfileVersion, m.RateLimitProfile, m.AccessPolicy, m.StripPrefix, m.AddPrefix, m.HostRewrite, encodeMap(m.HeaderAdd), encodeStrings(m.HeaderRemove),
		m.ConnectTimeoutSec, m.ReadTimeoutSec, m.SendTimeoutSec, m.MetadataLimitBytes, m.MetadataTTLSec, m.PackageTTLSec, m.ImmutableTTLSec, m.BlobTTLSec, m.CacheAuthenticated,
		m.AuthMode, m.TokenUpstream, m.BlobRedirectMode,
		m.PullOnly, m.ConfigState, m.ConfigError, m.AllowHTTP, m.AllowPrivate, m.InsecureTLS, m.BandwidthLimitBPS, m.MaxConcurrency,
		boolToInt(m.Help.Enabled), encodeHelp(m.Help), encodeStrings(m.BlockedPackages), encodeStrings(m.AllowedPackages), now, now)
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
	config_state=?,config_error=?,allow_http=?,allow_private=?,insecure_tls=?,bandwidth_limit_bps=?,max_concurrency=?,help_enabled=?,help_json=?,blocked_packages=?,allowed_packages=?,updated_at=? WHERE id=?`,
		m.Name, m.Slug, m.Type, m.Enabled, m.Description, m.PublicMode, m.PublicHost, m.PublicPath, m.ProxyMode,
		m.CacheEnabled, m.CacheProfile, m.RewriteEnabled, m.HTMLRewriteEnabled, m.RewriteProfile, encodeStrings(m.RewriteHosts), m.HealthCheckEnabled, m.HealthCheckPath,
		m.HealthIntervalSec, m.HealthTimeoutSec, m.HealthMethod, m.HealthExpected, m.RedirectMode, m.ProfileName,
		m.ProfileVersion, m.RateLimitProfile, m.AccessPolicy, m.StripPrefix, m.AddPrefix, m.HostRewrite, encodeMap(m.HeaderAdd), encodeStrings(m.HeaderRemove),
		m.ConnectTimeoutSec, m.ReadTimeoutSec, m.SendTimeoutSec, m.MetadataLimitBytes, m.MetadataTTLSec, m.PackageTTLSec, m.ImmutableTTLSec, m.BlobTTLSec, m.CacheAuthenticated,
		m.AuthMode, m.TokenUpstream, m.BlobRedirectMode,
		m.PullOnly, m.ConfigState, m.ConfigError, m.AllowHTTP, m.AllowPrivate, m.InsecureTLS, m.BandwidthLimitBPS, m.MaxConcurrency,
		boolToInt(m.Help.Enabled), encodeHelp(m.Help), encodeStrings(m.BlockedPackages), encodeStrings(m.AllowedPackages), nowText(), m.ID)
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
		max_concurrency,help_enabled,help_json,blocked_packages,allowed_packages,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			m.ID, m.Name, m.Slug, m.Type, m.Enabled, m.Description, m.PublicMode, m.PublicHost, m.PublicPath, m.ProxyMode,
			m.CacheEnabled, m.CacheProfile, m.RewriteEnabled, m.HTMLRewriteEnabled, m.RewriteProfile, encodeStrings(m.RewriteHosts), m.HealthCheckEnabled, m.HealthCheckPath,
			m.HealthIntervalSec, m.HealthTimeoutSec, m.HealthMethod, m.HealthExpected, m.RedirectMode, m.ProfileName,
			m.ProfileVersion, m.RateLimitProfile, m.AccessPolicy, m.StripPrefix, m.AddPrefix, m.HostRewrite, encodeMap(m.HeaderAdd), encodeStrings(m.HeaderRemove),
			m.ConnectTimeoutSec, m.ReadTimeoutSec, m.SendTimeoutSec, m.MetadataLimitBytes, m.MetadataTTLSec, m.PackageTTLSec, m.ImmutableTTLSec, m.BlobTTLSec, m.CacheAuthenticated,
			m.AuthMode, m.TokenUpstream, m.BlobRedirectMode,
			m.PullOnly, m.ConfigState, m.ConfigError, m.AllowHTTP, m.AllowPrivate, m.InsecureTLS, m.BandwidthLimitBPS, m.MaxConcurrency,
			boolToInt(m.Help.Enabled), encodeHelp(m.Help), encodeStrings(m.BlockedPackages), encodeStrings(m.AllowedPackages),
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
		max_concurrency,help_enabled,help_json,blocked_packages,allowed_packages,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			m.ID, m.Name, m.Slug, m.Type, m.Enabled, m.Description, m.PublicMode, m.PublicHost, m.PublicPath, m.ProxyMode,
			m.CacheEnabled, m.CacheProfile, m.RewriteEnabled, m.HTMLRewriteEnabled, m.RewriteProfile, encodeStrings(m.RewriteHosts), m.HealthCheckEnabled, m.HealthCheckPath,
			m.HealthIntervalSec, m.HealthTimeoutSec, m.HealthMethod, m.HealthExpected, m.RedirectMode, m.ProfileName,
			m.ProfileVersion, m.RateLimitProfile, m.AccessPolicy, m.StripPrefix, m.AddPrefix, m.HostRewrite, encodeMap(m.HeaderAdd), encodeStrings(m.HeaderRemove),
			m.ConnectTimeoutSec, m.ReadTimeoutSec, m.SendTimeoutSec, m.MetadataLimitBytes, m.MetadataTTLSec, m.PackageTTLSec, m.ImmutableTTLSec, m.BlobTTLSec, m.CacheAuthenticated,
			m.AuthMode, m.TokenUpstream, m.BlobRedirectMode,
			m.PullOnly, m.ConfigState, m.ConfigError, m.AllowHTTP, m.AllowPrivate, m.InsecureTLS, m.BandwidthLimitBPS, m.MaxConcurrency,
			boolToInt(m.Help.Enabled), encodeHelp(m.Help), encodeStrings(m.BlockedPackages), encodeStrings(m.AllowedPackages),
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

func encodeHelp(h model.HelpConfig) string {
	encoded, _ := json.Marshal(h)
	return string(encoded)
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
