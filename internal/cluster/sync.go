package cluster

import (
	"bytes"
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

type SyncResult struct {
	NodeID      int64     `json:"node_id"`
	NodeURL     string    `json:"node_url"`
	Fingerprint string    `json:"fingerprint"`
	Success     bool      `json:"success"`
	LatencyMS   int64     `json:"latency_ms"`
	Error       string    `json:"error,omitempty"`
	SyncedAt    time.Time `json:"synced_at"`
}

type SyncManager struct {
	cfg        config.Config
	store      Store
	router     *Router
	checker    *Checker
	audit      AuditRecorder
	httpClient *http.Client
}

func NewSyncManager(cfg config.Config, store Store, router *Router, checker *Checker, audit AuditRecorder) *SyncManager {
	timeout := 10 * time.Second
	dialer := security.NewSafeDialer(timeout, timeout, cfg.Security.AllowPrivateUpstream)

	transport := &http.Transport{
		DialContext:           dialer.DialContext,
		TLSClientConfig:       &tls.Config{InsecureSkipVerify: false},
		ResponseHeaderTimeout: timeout,
		MaxIdleConns:          50,
		MaxIdleConnsPerHost:   10,
	}

	return &SyncManager{
		cfg:     cfg,
		store:   store,
		router:  router,
		checker: checker,
		audit:   audit,
		httpClient: &http.Client{
			Timeout:   timeout,
			Transport: transport,
		},
	}
}

// SyncNode pushes the canonical manifest to a single edge node.
func (sm *SyncManager) SyncNode(ctx context.Context, node model.ClusterNode, manifest model.ClusterManifest) SyncResult {
	start := time.Now()
	res := SyncResult{
		NodeID:   node.ID,
		NodeURL:  node.URL,
		SyncedAt: start.UTC(),
	}

	manifestData, err := json.Marshal(manifest)
	if err != nil {
		res.Error = fmt.Sprintf("marshal manifest: %v", err)
		return res
	}

	targetURL := strings.TrimRight(node.URL, "/") + "/admin/api/v1/cluster/apply-sync"
	req, err := http.NewRequestWithContext(ctx, "POST", targetURL, bytes.NewReader(manifestData))
	if err != nil {
		res.Error = fmt.Sprintf("build request: %v", err)
		return res
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "MirrorRelay-ClusterSync/1.0")
	if sm.cfg.Distributed.Token != "" {
		req.Header.Set("Authorization", "Bearer "+sm.cfg.Distributed.Token)
	}

	resp, err := sm.httpClient.Do(req)
	res.LatencyMS = time.Since(start).Milliseconds()
	if err != nil {
		res.Error = fmt.Sprintf("request failed: %v", err)
		return res
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		res.Error = fmt.Sprintf("edge returned HTTP %d: %s", resp.StatusCode, string(body))
		return res
	}

	var edgeResp struct {
		Fingerprint string `json:"fingerprint"`
		Status      string `json:"status"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&edgeResp); err == nil {
		res.Fingerprint = edgeResp.Fingerprint
	} else {
		res.Fingerprint = manifest.ConfigFingerprint
	}

	res.Success = true
	if sm.store != nil {
		_ = sm.store.UpdateClusterNodeStatus(
			ctx, node.ID, "healthy", "in_sync", res.Fingerprint,
			node.Version, int(res.LatencyMS), nil, "", time.Now().UTC(),
		)
	}
	return res
}

// BroadcastSync synchronizes all enabled edge nodes concurrently.
func (sm *SyncManager) BroadcastSync(ctx context.Context, manifest model.ClusterManifest) []SyncResult {
	if sm.store == nil {
		return nil
	}

	nodes, err := sm.store.ListClusterNodes(ctx)
	if err != nil {
		slog.Error("ListClusterNodes failed during broadcast sync", "err", err)
		return nil
	}

	var wg sync.WaitGroup
	results := make([]SyncResult, len(nodes))

	for i, node := range nodes {
		if !node.Enabled {
			results[i] = SyncResult{
				NodeID:   node.ID,
				NodeURL:  node.URL,
				Success:  false,
				Error:    "Node is disabled",
				SyncedAt: time.Now().UTC(),
			}
			continue
		}

		wg.Add(1)
		go func(idx int, n model.ClusterNode) {
			defer wg.Done()
			results[idx] = sm.SyncNode(ctx, n, manifest)
		}(i, node)
	}

	wg.Wait()
	if sm.audit != nil {
		sm.audit.Record("system", "cluster_sync", "broadcast", fmt.Sprintf("Synchronized %d edge nodes", len(nodes)), true)
	}
	return results
}

// BroadcastPurge propagates a cache purge event to all active edge nodes.
func (sm *SyncManager) BroadcastPurge(ctx context.Context, repoSlug, objectPath string) map[int64]error {
	if sm.store == nil {
		return nil
	}

	nodes, err := sm.store.ListClusterNodes(ctx)
	if err != nil {
		return nil
	}

	results := make(map[int64]error)
	var mu sync.Mutex
	var wg sync.WaitGroup

	payload, _ := json.Marshal(map[string]string{
		"repository_slug": repoSlug,
		"object_path":     objectPath,
	})

	for _, node := range nodes {
		if !node.Enabled || node.HealthStatus != "healthy" {
			continue
		}

		wg.Add(1)
		go func(n model.ClusterNode) {
			defer wg.Done()

			targetURL := strings.TrimRight(n.URL, "/") + "/admin/api/v1/cluster/sync/purge"
			req, err := http.NewRequestWithContext(ctx, "POST", targetURL, bytes.NewReader(payload))
			if err != nil {
				mu.Lock()
				results[n.ID] = err
				mu.Unlock()
				return
			}

			req.Header.Set("Content-Type", "application/json")
			if sm.cfg.Distributed.Token != "" {
				req.Header.Set("Authorization", "Bearer "+sm.cfg.Distributed.Token)
			}

			resp, err := sm.httpClient.Do(req)
			if err != nil {
				mu.Lock()
				results[n.ID] = err
				mu.Unlock()
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode >= 400 {
				mu.Lock()
				results[n.ID] = errors.New(resp.Status)
				mu.Unlock()
			}
		}(node)
	}

	wg.Wait()
	return results
}
