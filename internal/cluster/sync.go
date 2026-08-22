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
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/LuisCMerrick/MirrorRelay/internal/config"
	"github.com/LuisCMerrick/MirrorRelay/internal/model"
	"github.com/LuisCMerrick/MirrorRelay/internal/security"
)

const clusterMutationWorkers = 8

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
	cfg              config.Config
	store            Store
	coordinatorEpoch string
	httpClient       *http.Client
}

func NewSyncManager(cfg config.Config, store Store, coordinatorEpoch string) *SyncManager {
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
		cfg:              cfg,
		store:            store,
		coordinatorEpoch: coordinatorEpoch,
		httpClient: &http.Client{
			Timeout:   timeout,
			Transport: transport,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

// SyncNode pushes one complete, active configuration snapshot to an edge.
func (sm *SyncManager) SyncNode(ctx context.Context, node model.ClusterNode, payload model.ClusterSyncRequest) SyncResult {
	start := time.Now()
	res := SyncResult{NodeID: node.ID, NodeURL: node.URL, SyncedAt: start.UTC()}
	finish := func(message string) SyncResult {
		res.LatencyMS = time.Since(start).Milliseconds()
		res.Error = message
		return res
	}
	if node.MutationToken == "" {
		return finish("edge mutation token is empty")
	}
	if payload.Manifest.ProtocolVersion != ClusterProtocolVersion || payload.Manifest.ConfigFingerprint == "" ||
		payload.Manifest.CoordinatorID != strings.TrimSpace(sm.cfg.Distributed.Node.Name) ||
		payload.Manifest.CoordinatorEpoch == "" || payload.Manifest.CoordinatorEpoch != sm.coordinatorEpoch {
		return finish("invalid local cluster sync manifest")
	}
	if payload.Repositories == nil || payload.CustomConfigs == nil ||
		CanonicalClusterConfigFingerprint(payload.Repositories, payload.CustomConfigs) != payload.Manifest.ConfigFingerprint ||
		!slices.Equal(ExtractCapabilities(payload.Repositories), payload.Manifest.Capabilities) {
		return finish("local cluster sync payload does not match its manifest")
	}
	targetURL, err := nodeEndpointURL(ctx, sm.cfg, node.URL, SyncApplyPath)
	if err != nil {
		return finish(fmt.Sprintf("validate node URL: %v", err))
	}
	payloadData, err := json.Marshal(payload)
	if err != nil {
		return finish(fmt.Sprintf("marshal sync payload: %v", err))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewReader(payloadData))
	if err != nil {
		return finish(fmt.Sprintf("build request: %v", err))
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "MirrorRelay-ClusterSync/2.0")
	req.Header.Set("X-MirrorRelay-Cluster-Mutation-Token", node.MutationToken)
	req.Header.Set("Authorization", "Bearer "+node.MutationToken)

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

	var edgeResp model.ClusterSyncResponse
	if err := decodeStrictJSONLimited(resp.Body, 64<<10, &edgeResp); err != nil {
		res.Error = fmt.Sprintf("decode edge response: %v", err)
		return res
	}
	if edgeResp.Status != "applied" {
		res.Error = fmt.Sprintf("edge returned invalid sync status %q", edgeResp.Status)
		return res
	}
	if edgeResp.Fingerprint == "" || edgeResp.Fingerprint != payload.Manifest.ConfigFingerprint {
		res.Error = fmt.Sprintf("edge fingerprint mismatch: got %q, want %q", edgeResp.Fingerprint, payload.Manifest.ConfigFingerprint)
		return res
	}
	if edgeResp.ProtocolVersion != payload.Manifest.ProtocolVersion {
		res.Error = fmt.Sprintf("edge protocol version mismatch: got %d, want %d", edgeResp.ProtocolVersion, payload.Manifest.ProtocolVersion)
		return res
	}
	if edgeResp.ConfigGeneration != payload.Manifest.ConfigGeneration {
		res.Error = fmt.Sprintf("edge configuration generation mismatch: got %d, want %d", edgeResp.ConfigGeneration, payload.Manifest.ConfigGeneration)
		return res
	}
	if !slices.Equal(edgeResp.Capabilities, payload.Manifest.Capabilities) {
		res.Error = fmt.Sprintf("edge capabilities mismatch: got %v, want %v", edgeResp.Capabilities, payload.Manifest.Capabilities)
		return res
	}

	res.Fingerprint = edgeResp.Fingerprint
	if sm.store != nil {
		version := edgeResp.MirrorRelayVersion
		if version == "" {
			version = node.Version
		}
		node.HealthStatus = "unknown"
		node.ConfigStatus = "match"
		node.ConfigFingerprint = res.Fingerprint
		node.ConfigGeneration = edgeResp.ConfigGeneration
		node.CoordinatorID = payload.Manifest.CoordinatorID
		node.CoordinatorEpoch = payload.Manifest.CoordinatorEpoch
		node.Version = version
		node.ProtocolVersion = edgeResp.ProtocolVersion
		node.Capabilities = append([]string(nil), edgeResp.Capabilities...)
		node.RepositoryHealth = nil
		node.LatencyMS = res.LatencyMS
		node.LastError = ""
		node.LastCheck = time.Now().UTC()
		if err := sm.store.UpdateClusterNodeStatus(ctx, node); err != nil {
			return finish(fmt.Sprintf("persist synchronized edge status: %v", err))
		}
	}
	res.Success = true
	return res
}

// BroadcastSync synchronizes all enabled edge nodes concurrently.
func (sm *SyncManager) BroadcastSync(ctx context.Context, payload model.ClusterSyncRequest) []SyncResult {
	if sm.store == nil {
		return nil
	}
	nodes, err := sm.store.ListClusterNodes(ctx)
	if err != nil {
		slog.Error("ListClusterNodes failed during broadcast sync", "err", err)
		return []SyncResult{{Error: err.Error(), SyncedAt: time.Now().UTC()}}
	}
	targets := make([]model.ClusterNode, 0, len(nodes))
	for _, node := range nodes {
		if node.Enabled {
			targets = append(targets, node)
		}
	}

	results := make([]SyncResult, len(targets))
	workerCount := clusterMutationWorkers
	if len(targets) < workerCount {
		workerCount = len(targets)
	}
	jobs := make(chan int)
	var wg sync.WaitGroup
	for worker := 0; worker < workerCount; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for index := range jobs {
				results[index] = sm.SyncNode(ctx, targets[index], payload)
			}
		}()
	}
	for index := range targets {
		jobs <- index
	}
	close(jobs)
	wg.Wait()
	return results
}

// BroadcastPurge propagates one explicit cache invalidation scope to every
// enabled edge. A nil map value records a successful receiver acknowledgement.
func (sm *SyncManager) BroadcastPurge(ctx context.Context, payload model.ClusterPurgeRequest) map[int64]error {
	results := make(map[int64]error)
	if sm.store == nil {
		return results
	}
	nodes, err := sm.store.ListClusterNodes(ctx)
	if err != nil {
		results[0] = err
		return results
	}
	payload.CoordinatorID = strings.TrimSpace(sm.cfg.Distributed.Node.Name)
	payload.CoordinatorEpoch = sm.coordinatorEpoch
	payloadData, err := json.Marshal(payload)
	if err != nil {
		results[0] = err
		return results
	}

	targets := make([]model.ClusterNode, 0, len(nodes))
	for _, node := range nodes {
		if node.Enabled {
			targets = append(targets, node)
		}
	}
	workerCount := clusterMutationWorkers
	if len(targets) < workerCount {
		workerCount = len(targets)
	}
	jobs := make(chan model.ClusterNode)
	var mu sync.Mutex
	var wg sync.WaitGroup
	for worker := 0; worker < workerCount; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for node := range jobs {
				result := sm.purgeNode(ctx, node, payload, payloadData)
				mu.Lock()
				results[node.ID] = result
				mu.Unlock()
			}
		}()
	}
	for _, node := range targets {
		jobs <- node
	}
	close(jobs)
	wg.Wait()
	return results
}

func (sm *SyncManager) purgeNode(ctx context.Context, node model.ClusterNode, payload model.ClusterPurgeRequest, payloadData []byte) error {
	if node.MutationToken == "" {
		return errors.New("edge mutation token is empty")
	}
	targetURL, err := nodeEndpointURL(ctx, sm.cfg, node.URL, SyncPurgePath)
	if err != nil {
		return fmt.Errorf("validate node URL: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewReader(payloadData))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "MirrorRelay-ClusterSync/2.0")
	req.Header.Set("X-MirrorRelay-Cluster-Mutation-Token", node.MutationToken)
	req.Header.Set("Authorization", "Bearer "+node.MutationToken)
	resp, err := sm.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("edge returned HTTP %d: %s", resp.StatusCode, string(body))
	}
	var edgeResp model.ClusterPurgeResponse
	if err := decodeStrictJSONLimited(resp.Body, 64<<10, &edgeResp); err != nil {
		return fmt.Errorf("decode edge response: %w", err)
	}
	if edgeResp.Status != "applied" || edgeResp.Scope != payload.Scope || edgeResp.RepositorySlug != payload.RepositorySlug || edgeResp.ObjectID != payload.ObjectID || edgeResp.Generation <= 0 {
		return errors.New("edge returned an invalid purge acknowledgement")
	}
	return nil
}
