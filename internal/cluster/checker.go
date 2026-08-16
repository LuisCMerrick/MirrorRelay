package cluster

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/LuisCMerrick/MirrorRelay/internal/config"
	"github.com/LuisCMerrick/MirrorRelay/internal/model"
)

type Store interface {
	ListClusterNodes(context.Context) ([]model.ClusterNode, error)
	GetClusterNode(context.Context, int64) (model.ClusterNode, error)
	UpdateClusterNodeStatus(context.Context, int64, string, string, string, string, int, []string, string, time.Time) error
	ClusterSetting(context.Context, string) (string, bool, error)
	PutClusterSetting(context.Context, string, string) error
}

type AuditRecorder interface {
	Record(user, action, object, detail string, ok bool)
}

type Checker struct {
	cfg          config.Config
	store        Store
	router       *Router
	metrics      *Metrics
	audit        AuditRecorder
	httpClient   *http.Client
	mu           sync.RWMutex
	fingerprint  string
	consecutiveF map[int64]int
	consecutiveS map[int64]int
}

func NewChecker(cfg config.Config, store Store, router *Router, metrics *Metrics, audit AuditRecorder) *Checker {
	timeout := cfg.Distributed.HealthCheck.Timeout
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	transport := &http.Transport{
		TLSClientConfig:       &tls.Config{InsecureSkipVerify: false},
		ResponseHeaderTimeout: timeout,
		DisableKeepAlives:     false,
	}
	client := &http.Client{
		Timeout:   timeout,
		Transport: transport,
	}

	c := &Checker{
		cfg:          cfg,
		store:        store,
		router:       router,
		metrics:      metrics,
		audit:        audit,
		httpClient:   client,
		consecutiveF: make(map[int64]int),
		consecutiveS: make(map[int64]int),
	}

	// Load cluster fingerprint from DB if exists
	if store != nil {
		if fp, ok, err := store.ClusterSetting(context.Background(), "cluster_fingerprint"); err == nil && ok {
			c.fingerprint = strings.TrimSpace(fp)
		}
	}

	return c
}

func (c *Checker) ClusterFingerprint() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.fingerprint
}

func (c *Checker) SetClusterFingerprint(ctx context.Context, fp string) error {
	c.mu.Lock()
	c.fingerprint = strings.TrimSpace(fp)
	c.mu.Unlock()

	if c.store != nil {
		return c.store.PutClusterSetting(ctx, "cluster_fingerprint", c.fingerprint)
	}
	return nil
}

func (c *Checker) CheckNode(ctx context.Context, node model.ClusterNode) (model.ClusterNode, error) {
	baseURL := strings.TrimRight(node.URL, "/")
	manifestURL := baseURL + "/api/v1/cluster/manifest"
	healthURL := baseURL + "/api/v1/cluster/health"

	token := c.cfg.Distributed.Token

	// 1. Fetch manifest
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, manifestURL, nil)
	if err != nil {
		return c.recordFailure(ctx, node, fmt.Sprintf("create manifest request: %v", err))
	}
	if token != "" {
		req.Header.Set("X-MirrorRelay-Cluster-Token", token)
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return c.recordFailure(ctx, node, fmt.Sprintf("fetch manifest: %v", err))
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return c.recordFailure(ctx, node, fmt.Sprintf("manifest returned HTTP %d: %s", resp.StatusCode, string(body)))
	}

	var manifest model.ClusterManifest
	if err := json.NewDecoder(resp.Body).Decode(&manifest); err != nil {
		return c.recordFailure(ctx, node, fmt.Sprintf("decode manifest: %v", err))
	}

	// 2. Fetch health
	reqH, err := http.NewRequestWithContext(ctx, http.MethodGet, healthURL, nil)
	if err != nil {
		return c.recordFailure(ctx, node, fmt.Sprintf("create health request: %v", err))
	}
	if token != "" {
		reqH.Header.Set("X-MirrorRelay-Cluster-Token", token)
		reqH.Header.Set("Authorization", "Bearer "+token)
	}

	respH, err := c.httpClient.Do(reqH)
	if err != nil {
		return c.recordFailure(ctx, node, fmt.Sprintf("fetch health: %v", err))
	}
	defer respH.Body.Close()

	if respH.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(respH.Body, 512))
		return c.recordFailure(ctx, node, fmt.Sprintf("health returned HTTP %d: %s", respH.StatusCode, string(body)))
	}

	var clusterHealth model.ClusterHealth
	if err := json.NewDecoder(respH.Body).Decode(&clusterHealth); err != nil {
		return c.recordFailure(ctx, node, fmt.Sprintf("decode health: %v", err))
	}

	return c.recordSuccess(ctx, node, manifest, clusterHealth)
}

func (c *Checker) recordFailure(ctx context.Context, node model.ClusterNode, errMsg string) (model.ClusterNode, error) {
	c.mu.Lock()
	c.consecutiveF[node.ID]++
	c.consecutiveS[node.ID] = 0
	failCount := c.consecutiveF[node.ID]
	c.mu.Unlock()

	unhealthyThreshold := c.cfg.Distributed.HealthCheck.UnhealthyThreshold
	if unhealthyThreshold <= 0 {
		unhealthyThreshold = 3
	}

	newHealth := node.HealthStatus
	if failCount >= unhealthyThreshold {
		newHealth = "unhealthy"
	}

	if c.metrics != nil {
		c.metrics.IncHealthCheckFailure(node.Name)
	}

	if node.HealthStatus == "healthy" && newHealth == "unhealthy" {
		slog.Warn("cluster node became unhealthy", "node", node.Name, "url", node.URL, "error", errMsg)
		if c.audit != nil {
			c.audit.Record("system", "node_unhealthy", "cluster_node", fmt.Sprintf("%s (%s): %s", node.Name, node.URL, errMsg), false)
		}
	}

	now := time.Now()
	node.HealthStatus = newHealth
	node.LastError = errMsg
	node.LastCheck = now

	if c.store != nil {
		_ = c.store.UpdateClusterNodeStatus(ctx, node.ID, newHealth, node.ConfigStatus, node.ConfigFingerprint, node.Version, node.ProtocolVersion, node.Capabilities, errMsg, now)
	}

	return node, nil
}

func (c *Checker) recordSuccess(ctx context.Context, node model.ClusterNode, manifest model.ClusterManifest, health model.ClusterHealth) (model.ClusterNode, error) {
	c.mu.Lock()
	c.consecutiveS[node.ID]++
	c.consecutiveF[node.ID] = 0
	succCount := c.consecutiveS[node.ID]

	// Establish cluster fingerprint if empty
	if c.fingerprint == "" && manifest.ConfigFingerprint != "" {
		c.fingerprint = manifest.ConfigFingerprint
		if c.store != nil {
			_ = c.store.PutClusterSetting(ctx, "cluster_fingerprint", c.fingerprint)
		}
		slog.Info("cluster fingerprint initialized", "fingerprint", c.fingerprint, "source_node", node.Name)
		if c.audit != nil {
			c.audit.Record("system", "cluster_fingerprint_init", "cluster", fmt.Sprintf("initialized from %s to %s", node.Name, c.fingerprint), true)
		}
	}
	clusterFP := c.fingerprint
	c.mu.Unlock()

	healthyThreshold := c.cfg.Distributed.HealthCheck.HealthyThreshold
	if healthyThreshold <= 0 {
		healthyThreshold = 2
	}

	newHealth := node.HealthStatus
	if succCount >= healthyThreshold || node.HealthStatus == "unknown" {
		newHealth = "healthy"
	}

	var newConfigStatus string
	if manifest.ProtocolVersion != ClusterProtocolVersion {
		newConfigStatus = "version_incompatible"
	} else if clusterFP == "" || manifest.ConfigFingerprint == clusterFP {
		newConfigStatus = "match"
	} else {
		newConfigStatus = "mismatch"
	}

	if node.HealthStatus != "healthy" && newHealth == "healthy" {
		slog.Info("cluster node recovered", "node", node.Name, "url", node.URL)
		if c.audit != nil {
			c.audit.Record("system", "node_recovered", "cluster_node", fmt.Sprintf("%s (%s)", node.Name, node.URL), true)
		}
	}

	if node.ConfigStatus == "match" && newConfigStatus == "mismatch" {
		slog.Warn("cluster node configuration drifted", "node", node.Name, "url", node.URL, "cluster_fp", clusterFP, "node_fp", manifest.ConfigFingerprint)
		if c.audit != nil {
			c.audit.Record("system", "config_drift", "cluster_node", fmt.Sprintf("%s (%s): expected %s, got %s", node.Name, node.URL, clusterFP, manifest.ConfigFingerprint), false)
		}
	}

	now := time.Now()
	node.HealthStatus = newHealth
	node.ConfigStatus = newConfigStatus
	node.ConfigFingerprint = manifest.ConfigFingerprint
	node.Version = manifest.MirrorRelayVersion
	node.ProtocolVersion = manifest.ProtocolVersion
	node.Capabilities = manifest.Capabilities
	node.LastError = ""
	node.LastCheck = now

	if c.store != nil {
		_ = c.store.UpdateClusterNodeStatus(ctx, node.ID, newHealth, newConfigStatus, manifest.ConfigFingerprint, manifest.MirrorRelayVersion, manifest.ProtocolVersion, manifest.Capabilities, "", now)
	}

	return node, nil
}

func (c *Checker) CheckAll(ctx context.Context) error {
	if c.store == nil {
		return nil
	}
	nodes, err := c.store.ListClusterNodes(ctx)
	if err != nil {
		return err
	}

	var updated []model.ClusterNode
	for _, n := range nodes {
		res, _ := c.CheckNode(ctx, n)
		updated = append(updated, res)
	}

	if c.router != nil {
		c.router.SetNodes(updated)
	}

	if c.metrics != nil {
		c.metrics.UpdateNodeStats(updated, c.ClusterFingerprint())
	}

	return nil
}

func (c *Checker) Start(ctx context.Context) {
	interval := c.cfg.Distributed.HealthCheck.Interval
	if interval <= 0 {
		interval = 10 * time.Second
	}
	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		_ = c.CheckAll(ctx)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_ = c.CheckAll(ctx)
			}
		}
	}()
}
