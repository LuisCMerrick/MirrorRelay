package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/LuisCMerrick/MirrorRelay/internal/model"
)

func IsConflict(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "unique constraint") || strings.Contains(s, "constraint failed")
}

func IsNotFound(err error) bool { return errors.Is(err, sql.ErrNoRows) }

func (s *Store) ListClusterNodes(ctx context.Context) ([]model.ClusterNode, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,url,region,country,priority,weight,enabled,health_status,config_status,config_fingerprint,version,protocol_version,capabilities,latency_ms,last_check,last_error,created_at,updated_at FROM cluster_nodes ORDER BY priority ASC, id ASC`)
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
		if err := rows.Scan(&n.ID, &n.Name, &n.URL, &n.Region, &n.Country, &n.Priority, &n.Weight, &enabled, &n.HealthStatus, &n.ConfigStatus, &n.ConfigFingerprint, &n.Version, &n.ProtocolVersion, &capsJSON, &n.LatencyMS, &lastCheckStr, &n.LastError, &createdStr, &updatedStr); err != nil {
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
	err := s.db.QueryRowContext(ctx, `SELECT id,name,url,region,country,priority,weight,enabled,health_status,config_status,config_fingerprint,version,protocol_version,capabilities,latency_ms,last_check,last_error,created_at,updated_at FROM cluster_nodes WHERE id=?`, id).Scan(
		&n.ID, &n.Name, &n.URL, &n.Region, &n.Country, &n.Priority, &n.Weight, &enabled, &n.HealthStatus, &n.ConfigStatus, &n.ConfigFingerprint, &n.Version, &n.ProtocolVersion, &capsJSON, &n.LatencyMS, &lastCheckStr, &n.LastError, &createdStr, &updatedStr)
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
	err := s.db.QueryRowContext(ctx, `SELECT id,name,url,region,country,priority,weight,enabled,health_status,config_status,config_fingerprint,version,protocol_version,capabilities,latency_ms,last_check,last_error,created_at,updated_at FROM cluster_nodes WHERE url=?`, rawURL).Scan(
		&n.ID, &n.Name, &n.URL, &n.Region, &n.Country, &n.Priority, &n.Weight, &enabled, &n.HealthStatus, &n.ConfigStatus, &n.ConfigFingerprint, &n.Version, &n.ProtocolVersion, &capsJSON, &n.LatencyMS, &lastCheckStr, &n.LastError, &createdStr, &updatedStr)
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
	res, err := s.db.ExecContext(ctx, `INSERT INTO cluster_nodes(name,url,region,country,priority,weight,enabled,health_status,config_status,config_fingerprint,version,protocol_version,capabilities,latency_ms,last_check,last_error,created_at,updated_at)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, node.Name, node.URL, node.Region, node.Country, node.Priority, node.Weight, enabledInt, node.HealthStatus, node.ConfigStatus, node.ConfigFingerprint, node.Version, node.ProtocolVersion, string(capsBytes), node.LatencyMS, "", node.LastError, nowStr, nowStr)
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

func (s *Store) UpdateClusterNodeStatus(ctx context.Context, id int64, healthStatus, configStatus, fingerprint, version string, protoVer int, caps []string, latencyMS int64, lastError string, lastCheck time.Time) error {
	nowStr := time.Now().UTC().Format(time.RFC3339Nano)
	var checkStr string
	if !lastCheck.IsZero() {
		checkStr = lastCheck.UTC().Format(time.RFC3339Nano)
	}
	capsBytes, _ := json.Marshal(caps)
	_, err := s.db.ExecContext(ctx, `UPDATE cluster_nodes SET health_status=?,config_status=?,config_fingerprint=?,version=?,protocol_version=?,capabilities=?,latency_ms=?,last_error=?,last_check=?,updated_at=? WHERE id=?`,
		healthStatus, configStatus, fingerprint, version, protoVer, string(capsBytes), latencyMS, lastError, checkStr, nowStr, id)
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
