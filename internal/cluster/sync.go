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
	httpClient *http.Client
}

func NewSyncManager(cfg config.Config, store Store) *SyncManager {
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
		cfg:   cfg,
		store: store,
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
	if sm.cfg.Distributed.Token == "" {
		return finish("cluster token is empty")
	}
	if payload.Manifest.ProtocolVersion != ClusterProtocolVersion || payload.Manifest.ConfigFingerprint == "" {
		return finish("invalid local cluster sync manifest")
	}
	if payload.Repositories == nil || payload.CustomConfigs == nil ||
		CanonicalFingerprint(payload.Repositories) != payload.Manifest.ConfigFingerprint ||
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
	req.Header.Set("User-Agent", "MirrorRelay-ClusterSync/1.0")
	req.Header.Set("Authorization", "Bearer "+sm.cfg.Distributed.Token)

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
	decoder := json.NewDecoder(io.LimitReader(resp.Body, 64<<10))
	if err := decoder.Decode(&edgeResp); err != nil {
		res.Error = fmt.Sprintf("decode edge response: %v", err)
		return res
	}
	if err := requireJSONEOF(decoder); err != nil {
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
	res.Success = true
	if sm.store != nil {
		version := edgeResp.MirrorRelayVersion
		if version == "" {
			version = node.Version
		}
		_ = sm.store.UpdateClusterNodeStatus(ctx, node.ID, "healthy", "match", res.Fingerprint,
			version, edgeResp.ProtocolVersion, edgeResp.Capabilities, res.LatencyMS, "", time.Now().UTC())
	}
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
	var wg sync.WaitGroup
	for index, node := range targets {
		wg.Add(1)
		go func(index int, node model.ClusterNode) {
			defer wg.Done()
			results[index] = sm.SyncNode(ctx, node, payload)
		}(index, node)
	}
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
	payloadData, err := json.Marshal(payload)
	if err != nil {
		results[0] = err
		return results
	}

	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, node := range nodes {
		if !node.Enabled {
			continue
		}
		wg.Add(1)
		go func(node model.ClusterNode) {
			defer wg.Done()
			result := sm.purgeNode(ctx, node, payload, payloadData)
			mu.Lock()
			results[node.ID] = result
			mu.Unlock()
		}(node)
	}
	wg.Wait()
	return results
}

func (sm *SyncManager) purgeNode(ctx context.Context, node model.ClusterNode, payload model.ClusterPurgeRequest, payloadData []byte) error {
	if sm.cfg.Distributed.Token == "" {
		return errors.New("cluster token is empty")
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
	req.Header.Set("User-Agent", "MirrorRelay-ClusterSync/1.0")
	req.Header.Set("Authorization", "Bearer "+sm.cfg.Distributed.Token)
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
	decoder := json.NewDecoder(io.LimitReader(resp.Body, 64<<10))
	if err := decoder.Decode(&edgeResp); err != nil {
		return fmt.Errorf("decode edge response: %w", err)
	}
	if err := requireJSONEOF(decoder); err != nil {
		return fmt.Errorf("decode edge response: %w", err)
	}
	if edgeResp.Status != "applied" || edgeResp.Scope != payload.Scope || edgeResp.RepositorySlug != payload.RepositorySlug || edgeResp.ObjectID != payload.ObjectID || edgeResp.Generation <= 0 {
		return errors.New("edge returned an invalid purge acknowledgement")
	}
	return nil
}

func requireJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); errors.Is(err, io.EOF) {
		return nil
	} else if err != nil {
		return err
	}
	return errors.New("multiple JSON values are not allowed")
}
