// Package cluster provides distributed health checking and routing.
package cluster

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/LuisCMerrick/MirrorRelay/internal/config"
	"github.com/LuisCMerrick/MirrorRelay/internal/model"
	"github.com/LuisCMerrick/MirrorRelay/internal/security"
)

const (
	coordinatorExpectedStateKey = "coordinator_expected_state_v2"
	clusterCheckWorkers         = 8
)

type Store interface {
	ListClusterNodes(context.Context) ([]model.ClusterNode, error)
	GetClusterNode(context.Context, int64) (model.ClusterNode, error)
	UpdateClusterNodeStatus(context.Context, model.ClusterNode) error
	ClusterSetting(context.Context, string) (string, bool, error)
	PutClusterSetting(context.Context, string, string) error
}

type AuditRecorder interface {
	Record(user, action, object, detail string, ok bool)
}

type Checker struct {
	cfg        config.Config
	store      Store
	router     *Router
	metrics    *Metrics
	audit      AuditRecorder
	httpClient *http.Client

	mu               sync.RWMutex
	fingerprint      string
	coordinatorID    string
	coordinatorEpoch string
	configGeneration int64
	consecutiveF     map[int64]int
	consecutiveS     map[int64]int
}

func NewChecker(cfg config.Config, store Store, router *Router, metrics *Metrics, audit AuditRecorder) *Checker {
	timeout := cfg.Distributed.HealthCheck.Timeout
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	dialer := security.NewSafeDialer(timeout, timeout, cfg.Security.AllowPrivateUpstream)
	transport := &http.Transport{
		DialContext:           dialer.DialContext,
		TLSClientConfig:       &tls.Config{InsecureSkipVerify: false},
		ResponseHeaderTimeout: timeout,
		DisableKeepAlives:     false,
	}
	c := &Checker{
		cfg:     cfg,
		store:   store,
		router:  router,
		metrics: metrics,
		audit:   audit,
		httpClient: &http.Client{
			Timeout:   timeout,
			Transport: transport,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		consecutiveF: make(map[int64]int),
		consecutiveS: make(map[int64]int),
	}
	if store != nil {
		if raw, found, err := store.ClusterSetting(context.Background(), coordinatorExpectedStateKey); err == nil && found {
			var manifest model.ClusterManifest
			if json.Unmarshal([]byte(raw), &manifest) == nil {
				c.setExpectedInMemory(manifest)
			}
		} else if fingerprint, found, readErr := store.ClusterSetting(context.Background(), "cluster_fingerprint"); readErr == nil && found {
			c.fingerprint = strings.TrimSpace(fingerprint)
		}
	}
	return c
}

func (c *Checker) ClusterFingerprint() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.fingerprint
}

func (c *Checker) setExpectedInMemory(manifest model.ClusterManifest) {
	c.fingerprint = strings.TrimSpace(manifest.ConfigFingerprint)
	c.coordinatorID = strings.TrimSpace(manifest.CoordinatorID)
	c.coordinatorEpoch = strings.TrimSpace(manifest.CoordinatorEpoch)
	c.configGeneration = manifest.ConfigGeneration
}

func (c *Checker) SetExpectedConfiguration(ctx context.Context, manifest model.ClusterManifest) error {
	encoded, err := json.Marshal(manifest)
	if err != nil {
		return fmt.Errorf("encode Coordinator expected configuration: %w", err)
	}
	if c.store != nil {
		if err := c.store.PutClusterSetting(ctx, coordinatorExpectedStateKey, string(encoded)); err != nil {
			return err
		}
	}
	c.mu.Lock()
	c.setExpectedInMemory(manifest)
	c.mu.Unlock()
	if c.store != nil {
		if err := c.store.PutClusterSetting(ctx, "cluster_fingerprint", strings.TrimSpace(manifest.ConfigFingerprint)); err != nil {
			slog.Warn("persist legacy cluster fingerprint setting", "error", err)
		}
	}
	return nil
}

func (c *Checker) SetClusterFingerprint(ctx context.Context, fingerprint string) error {
	if c.store != nil {
		if err := c.store.PutClusterSetting(ctx, "cluster_fingerprint", strings.TrimSpace(fingerprint)); err != nil {
			return err
		}
	}
	c.mu.Lock()
	c.fingerprint = strings.TrimSpace(fingerprint)
	c.mu.Unlock()
	return nil
}

func (c *Checker) CheckNode(ctx context.Context, node model.ClusterNode) (model.ClusterNode, error) {
	started := time.Now()
	manifestURL, err := nodeEndpointURL(ctx, c.cfg, node.URL, "/api/v1/cluster/manifest")
	if err != nil {
		return c.recordFailure(ctx, node, fmt.Sprintf("validate cluster node URL: %v", err))
	}
	healthURL, err := nodeEndpointURL(ctx, c.cfg, node.URL, "/api/v1/cluster/health")
	if err != nil {
		return c.recordFailure(ctx, node, fmt.Sprintf("validate cluster node URL: %v", err))
	}
	var manifest model.ClusterManifest
	if err := c.fetchProbe(ctx, manifestURL, &manifest); err != nil {
		return c.recordFailure(ctx, node, "fetch manifest: "+err.Error())
	}
	var health model.ClusterHealth
	if err := c.fetchProbe(ctx, healthURL, &health); err != nil {
		return c.recordFailure(ctx, node, "fetch health: "+err.Error())
	}
	node.LatencyMS = time.Since(started).Milliseconds()
	return c.recordSuccess(ctx, node, manifest, health)
}

func (c *Checker) fetchProbe(ctx context.Context, endpoint string, output any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	if token := c.cfg.Distributed.Token; token != "" {
		request.Header.Set("X-MirrorRelay-Cluster-Token", token)
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 512))
		return fmt.Errorf("HTTP %d: %s", response.StatusCode, string(body))
	}
	return decodeStrictJSONLimited(response.Body, 64<<10, output)
}

func (c *Checker) persistStatus(ctx context.Context, node model.ClusterNode) error {
	if c.store == nil {
		return nil
	}
	if err := c.store.UpdateClusterNodeStatus(ctx, node); err != nil {
		slog.Error("persist cluster node status", "node", node.Name, "url", node.URL, "error", err)
		if c.audit != nil {
			c.audit.Record("system", "node_status_persist_failed", "cluster_node", fmt.Sprintf("%s (%s): %v", node.Name, node.URL, err), false)
		}
		return fmt.Errorf("persist cluster node %q status: %w", node.Name, err)
	}
	return nil
}

func (c *Checker) recordFailure(ctx context.Context, node model.ClusterNode, message string) (model.ClusterNode, error) {
	c.mu.Lock()
	c.consecutiveF[node.ID]++
	c.consecutiveS[node.ID] = 0
	failures := c.consecutiveF[node.ID]
	c.mu.Unlock()
	threshold := c.cfg.Distributed.HealthCheck.UnhealthyThreshold
	if threshold <= 0 {
		threshold = 3
	}
	newHealth := node.HealthStatus
	if failures >= threshold {
		newHealth = "unhealthy"
		node.RepositoryHealth = nil
	}
	if c.metrics != nil {
		c.metrics.IncHealthCheckFailure(node.Name)
	}
	if node.HealthStatus == "healthy" && newHealth == "unhealthy" {
		slog.Warn("cluster node became unhealthy", "node", node.Name, "url", node.URL, "error", message)
		if c.audit != nil {
			c.audit.Record("system", "node_unhealthy", "cluster_node", fmt.Sprintf("%s (%s): %s", node.Name, node.URL, message), false)
		}
	}
	node.HealthStatus = newHealth
	node.LastError = message
	node.LastCheck = time.Now().UTC()
	return node, c.persistStatus(ctx, node)
}

func (c *Checker) recordInconsistent(ctx context.Context, node model.ClusterNode, manifest model.ClusterManifest, message string) (model.ClusterNode, error) {
	c.mu.Lock()
	c.consecutiveF[node.ID]++
	c.consecutiveS[node.ID] = 0
	c.mu.Unlock()
	node.HealthStatus = "unhealthy"
	node.ConfigStatus = "inconsistent"
	node.ConfigFingerprint = manifest.ConfigFingerprint
	node.ConfigGeneration = manifest.ConfigGeneration
	node.NodeID = manifest.NodeID
	node.CoordinatorID = manifest.CoordinatorID
	node.CoordinatorEpoch = manifest.CoordinatorEpoch
	node.ProtocolVersion = manifest.ProtocolVersion
	node.Capabilities = append([]string(nil), manifest.Capabilities...)
	node.RepositoryHealth = nil
	node.LastError = message
	node.LastCheck = time.Now().UTC()
	if c.metrics != nil {
		c.metrics.IncHealthCheckFailure(node.Name)
	}
	slog.Warn("cluster node returned an inconsistent control-plane snapshot", "node", node.Name, "url", node.URL, "error", message)
	return node, c.persistStatus(ctx, node)
}

func (c *Checker) recordSuccess(ctx context.Context, node model.ClusterNode, manifest model.ClusterManifest, health model.ClusterHealth) (model.ClusterNode, error) {
	reportedHealth := strings.ToLower(strings.TrimSpace(health.Status))
	if reportedHealth != "healthy" && reportedHealth != "degraded" {
		return c.recordInconsistent(ctx, node, manifest, "edge returned an invalid cluster health status")
	}
	if health.ConfigFingerprint == "" || health.ConfigFingerprint != manifest.ConfigFingerprint {
		return c.recordInconsistent(ctx, node, manifest, "manifest and health fingerprints differ")
	}
	if health.ConfigGeneration != manifest.ConfigGeneration {
		return c.recordInconsistent(ctx, node, manifest, "manifest and health configuration generations differ")
	}

	c.mu.Lock()
	c.consecutiveS[node.ID]++
	c.consecutiveF[node.ID] = 0
	successes := c.consecutiveS[node.ID]
	expectedFingerprint := c.fingerprint
	expectedCoordinatorID := c.coordinatorID
	expectedCoordinatorEpoch := c.coordinatorEpoch
	expectedGeneration := c.configGeneration
	c.mu.Unlock()
	threshold := c.cfg.Distributed.HealthCheck.HealthyThreshold
	if threshold <= 0 {
		threshold = 2
	}
	for _, viable := range health.Repositories {
		if !viable {
			reportedHealth = "degraded"
			break
		}
	}
	newHealth := reportedHealth
	if node.HealthStatus == "unhealthy" && successes < threshold {
		newHealth = "unhealthy"
	}

	newConfigStatus := "match"
	switch {
	case manifest.ProtocolVersion != ClusterProtocolVersion:
		newConfigStatus = "version_incompatible"
	case expectedFingerprint == "" || manifest.ConfigFingerprint != expectedFingerprint:
		newConfigStatus = "mismatch"
	case expectedCoordinatorID == "" || manifest.CoordinatorID != expectedCoordinatorID:
		newConfigStatus = "coordinator_mismatch"
	case expectedCoordinatorEpoch == "" || manifest.CoordinatorEpoch != expectedCoordinatorEpoch:
		newConfigStatus = "epoch_mismatch"
	case expectedGeneration <= 0 || manifest.ConfigGeneration != expectedGeneration:
		newConfigStatus = "generation_mismatch"
	}

	if node.HealthStatus == "unhealthy" && (newHealth == "healthy" || newHealth == "degraded") {
		slog.Info("cluster node recovered", "node", node.Name, "url", node.URL, "health", newHealth)
		if c.audit != nil {
			c.audit.Record("system", "node_recovered", "cluster_node", fmt.Sprintf("%s (%s)", node.Name, node.URL), true)
		}
	}
	if node.ConfigStatus == "match" && newConfigStatus != "match" {
		slog.Warn("cluster node configuration drifted", "node", node.Name, "url", node.URL, "expected_fingerprint", expectedFingerprint, "node_fingerprint", manifest.ConfigFingerprint, "status", newConfigStatus)
		if c.audit != nil {
			c.audit.Record("system", "config_drift", "cluster_node", fmt.Sprintf("%s (%s): %s", node.Name, node.URL, newConfigStatus), false)
		}
	}

	repositoryHealth := make(map[string]bool, len(health.Repositories))
	for slug, viable := range health.Repositories {
		repositoryHealth[strings.ToLower(strings.TrimSpace(slug))] = viable
	}
	node.HealthStatus = newHealth
	node.ConfigStatus = newConfigStatus
	node.ConfigFingerprint = manifest.ConfigFingerprint
	node.ConfigGeneration = manifest.ConfigGeneration
	node.NodeID = manifest.NodeID
	node.CoordinatorID = manifest.CoordinatorID
	node.CoordinatorEpoch = manifest.CoordinatorEpoch
	node.Version = manifest.MirrorRelayVersion
	node.ProtocolVersion = manifest.ProtocolVersion
	node.Capabilities = append([]string(nil), manifest.Capabilities...)
	node.RepositoryHealth = repositoryHealth
	node.LastError = ""
	node.LastCheck = time.Now().UTC()
	return node, c.persistStatus(ctx, node)
}

func (c *Checker) CheckAll(ctx context.Context) error {
	if c.store == nil {
		return nil
	}
	nodes, err := c.store.ListClusterNodes(ctx)
	if err != nil {
		return err
	}
	workerCount := clusterCheckWorkers
	if len(nodes) < workerCount {
		workerCount = len(nodes)
	}
	jobs := make(chan int)
	var wait sync.WaitGroup
	var errorMu sync.Mutex
	var scanErrors []error
	for worker := 0; worker < workerCount; worker++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for index := range jobs {
				_, checkErr := c.CheckNode(ctx, nodes[index])
				if checkErr != nil {
					errorMu.Lock()
					scanErrors = append(scanErrors, checkErr)
					errorMu.Unlock()
					continue
				}
			}
		}()
	}
	for index := range nodes {
		jobs <- index
	}
	close(jobs)
	wait.Wait()
	durableNodes, reloadErr := c.store.ListClusterNodes(ctx)
	if reloadErr != nil {
		scanErrors = append(scanErrors, fmt.Errorf("reload durable cluster routing snapshot: %w", reloadErr))
	} else {
		if c.router != nil {
			c.router.SetNodes(durableNodes)
		}
		if c.metrics != nil {
			c.metrics.UpdateNodeStats(durableNodes, c.ClusterFingerprint())
		}
	}
	return errors.Join(scanErrors...)
}

func (c *Checker) Start(ctx context.Context) {
	interval := c.cfg.Distributed.HealthCheck.Interval
	if interval <= 0 {
		interval = 10 * time.Second
	}
	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		if err := c.CheckAll(ctx); err != nil {
			slog.Error("cluster health scan failed", "error", err)
		}
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := c.CheckAll(ctx); err != nil {
					slog.Error("cluster health scan failed", "error", err)
				}
			}
		}
	}()
}
