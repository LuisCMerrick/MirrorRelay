package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
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

const clusterNodeColumns = `id,name,url,region,country,priority,weight,enabled,mutation_token,health_status,config_status,config_fingerprint,config_generation,node_id,coordinator_id,coordinator_epoch,version,protocol_version,capabilities,repository_health,latency_ms,last_check,last_error,created_at,updated_at`

type rowScanner interface {
	Scan(...any) error
}

func scanClusterNode(scanner rowScanner) (model.ClusterNode, error) {
	var node model.ClusterNode
	var enabled int
	var capabilitiesJSON, repositoryHealthJSON string
	var lastCheck, created, updated string
	err := scanner.Scan(&node.ID, &node.Name, &node.URL, &node.Region, &node.Country, &node.Priority, &node.Weight,
		&enabled, &node.MutationToken, &node.HealthStatus, &node.ConfigStatus, &node.ConfigFingerprint,
		&node.ConfigGeneration, &node.NodeID, &node.CoordinatorID, &node.CoordinatorEpoch, &node.Version,
		&node.ProtocolVersion, &capabilitiesJSON, &repositoryHealthJSON, &node.LatencyMS, &lastCheck,
		&node.LastError, &created, &updated)
	if err != nil {
		return node, err
	}
	node.Enabled = enabled != 0
	node.MutationTokenConfigured = node.MutationToken != ""
	if capabilitiesJSON != "" {
		if err := json.Unmarshal([]byte(capabilitiesJSON), &node.Capabilities); err != nil {
			return node, fmt.Errorf("decode cluster node capabilities: %w", err)
		}
	}
	if repositoryHealthJSON != "" {
		if err := json.Unmarshal([]byte(repositoryHealthJSON), &node.RepositoryHealth); err != nil {
			return node, fmt.Errorf("decode cluster node repository health: %w", err)
		}
	}
	if lastCheck != "" {
		node.LastCheck = parseTime(lastCheck)
	}
	node.CreatedAt = parseTime(created)
	node.UpdatedAt = parseTime(updated)
	return node, nil
}

func (s *Store) ListClusterNodes(ctx context.Context) ([]model.ClusterNode, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+clusterNodeColumns+` FROM cluster_nodes ORDER BY priority ASC, id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var nodes []model.ClusterNode
	for rows.Next() {
		node, err := scanClusterNode(rows)
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, node)
	}
	return nodes, rows.Err()
}

func (s *Store) GetClusterNode(ctx context.Context, id int64) (model.ClusterNode, error) {
	return scanClusterNode(s.db.QueryRowContext(ctx, `SELECT `+clusterNodeColumns+` FROM cluster_nodes WHERE id=?`, id))
}

func (s *Store) GetClusterNodeByURL(ctx context.Context, rawURL string) (model.ClusterNode, error) {
	return scanClusterNode(s.db.QueryRowContext(ctx, `SELECT `+clusterNodeColumns+` FROM cluster_nodes WHERE url=?`, rawURL))
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
	repositoryHealthBytes, _ := json.Marshal(node.RepositoryHealth)
	res, err := s.db.ExecContext(ctx, `INSERT INTO cluster_nodes(name,url,region,country,priority,weight,enabled,mutation_token,health_status,config_status,config_fingerprint,config_generation,node_id,coordinator_id,coordinator_epoch,version,protocol_version,capabilities,repository_health,latency_ms,last_check,last_error,created_at,updated_at)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, node.Name, node.URL, node.Region, node.Country, node.Priority, node.Weight, enabledInt, node.MutationToken, node.HealthStatus, node.ConfigStatus, node.ConfigFingerprint, node.ConfigGeneration, node.NodeID, node.CoordinatorID, node.CoordinatorEpoch, node.Version, node.ProtocolVersion, string(capsBytes), string(repositoryHealthBytes), node.LatencyMS, "", node.LastError, nowStr, nowStr)
	if err != nil {
		return node, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return node, err
	}
	node.ID = id
	node.MutationTokenConfigured = node.MutationToken != ""
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
	result, err := s.db.ExecContext(ctx, `UPDATE cluster_nodes SET name=?,url=?,region=?,country=?,priority=?,weight=?,enabled=?,mutation_token=?,updated_at=? WHERE id=?`,
		node.Name, node.URL, node.Region, node.Country, node.Priority, node.Weight, enabledInt, node.MutationToken, nowStr, node.ID)
	if err != nil {
		return node, err
	}
	if affected, err := result.RowsAffected(); err != nil {
		return node, err
	} else if affected == 0 {
		return node, sql.ErrNoRows
	}
	node.UpdatedAt = now
	node.MutationTokenConfigured = node.MutationToken != ""
	return node, nil
}

func (s *Store) UpdateClusterNodeStatus(ctx context.Context, node model.ClusterNode) error {
	nowStr := time.Now().UTC().Format(time.RFC3339Nano)
	var checkStr string
	if !node.LastCheck.IsZero() {
		checkStr = node.LastCheck.UTC().Format(time.RFC3339Nano)
	}
	capsBytes, _ := json.Marshal(node.Capabilities)
	repositoryHealthBytes, _ := json.Marshal(node.RepositoryHealth)
	result, err := s.db.ExecContext(ctx, `UPDATE cluster_nodes SET health_status=?,config_status=?,config_fingerprint=?,config_generation=?,node_id=?,coordinator_id=?,coordinator_epoch=?,version=?,protocol_version=?,capabilities=?,repository_health=?,latency_ms=?,last_error=?,last_check=?,updated_at=? WHERE id=?`,
		node.HealthStatus, node.ConfigStatus, node.ConfigFingerprint, node.ConfigGeneration, node.NodeID, node.CoordinatorID,
		node.CoordinatorEpoch, node.Version, node.ProtocolVersion, string(capsBytes), string(repositoryHealthBytes), node.LatencyMS,
		node.LastError, checkStr, nowStr, node.ID)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) DeleteClusterNode(ctx context.Context, id int64) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM cluster_nodes WHERE id=?`, id)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) SetClusterNodeEnabled(ctx context.Context, id int64, enabled bool) error {
	var enabledInt int
	if enabled {
		enabledInt = 1
	}
	nowStr := time.Now().UTC().Format(time.RFC3339Nano)
	result, err := s.db.ExecContext(ctx, `UPDATE cluster_nodes SET enabled=?,updated_at=? WHERE id=?`, enabledInt, nowStr, id)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
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
