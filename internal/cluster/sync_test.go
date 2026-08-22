package cluster

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LuisCMerrick/MirrorRelay/internal/config"
	"github.com/LuisCMerrick/MirrorRelay/internal/model"
)

type mockClusterStore struct {
	mu    sync.Mutex
	nodes []model.ClusterNode
}

func (m *mockClusterStore) ListClusterNodes(ctx context.Context) ([]model.ClusterNode, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]model.ClusterNode(nil), m.nodes...), nil
}

func (m *mockClusterStore) GetClusterNode(ctx context.Context, id int64) (model.ClusterNode, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, n := range m.nodes {
		if n.ID == id {
			return n, nil
		}
	}
	return model.ClusterNode{}, ErrNoAvailableEdge
}

func (m *mockClusterStore) UpdateClusterNodeStatus(_ context.Context, node model.ClusterNode) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, n := range m.nodes {
		if n.ID == node.ID {
			m.nodes[i] = node
		}
	}
	return nil
}

func (m *mockClusterStore) ClusterSetting(ctx context.Context, key string) (string, bool, error) {
	return "", false, nil
}

func (m *mockClusterStore) PutClusterSetting(ctx context.Context, key, val string) error {
	return nil
}

func TestSyncManager_SyncNode(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == SyncApplyPath {
			var payload model.ClusterSyncRequest
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				http.Error(w, err.Error(), 400)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(model.ClusterSyncResponse{
				Status:             "applied",
				Fingerprint:        payload.Manifest.ConfigFingerprint,
				ProtocolVersion:    payload.Manifest.ProtocolVersion,
				ConfigGeneration:   payload.Manifest.ConfigGeneration,
				MirrorRelayVersion: "test-version",
				Capabilities:       payload.Manifest.Capabilities,
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()

	cfg := config.Config{}
	cfg.Security.AllowPrivateUpstream = true
	cfg.Distributed.AllowHTTP = true
	cfg.Distributed.Node.Name = "coordinator-1"

	store := &mockClusterStore{
		nodes: []model.ClusterNode{
			{
				ID:              1,
				Name:            "edge-1",
				URL:             ts.URL,
				Enabled:         true,
				MutationToken:   "edge-mutation-secret",
				ProtocolVersion: ClusterProtocolVersion,
				Capabilities:    []string{"apt"},
			},
		},
	}

	sm := NewSyncManager(cfg, store, "epoch-1")
	repositories := []model.Mirror{{Type: "apt"}}
	manifest := model.ClusterManifest{
		ProtocolVersion:   ClusterProtocolVersion,
		NodeID:            "coordinator-1",
		CoordinatorID:     "coordinator-1",
		CoordinatorEpoch:  "epoch-1",
		ConfigGeneration:  42,
		ConfigFingerprint: CanonicalClusterConfigFingerprint(repositories, []model.CustomConfig{}),
		Capabilities:      ExtractCapabilities(repositories),
	}
	payload := model.ClusterSyncRequest{Manifest: manifest, Repositories: repositories, CustomConfigs: []model.CustomConfig{}}

	results := sm.BroadcastSync(context.Background(), payload)
	if len(results) != 1 {
		t.Fatalf("expected 1 sync result, got %d", len(results))
	}
	if !results[0].Success {
		t.Fatalf("expected sync success, got error: %s", results[0].Error)
	}
	if results[0].Fingerprint != manifest.ConfigFingerprint {
		t.Fatalf("expected fingerprint %s, got %s", manifest.ConfigFingerprint, results[0].Fingerprint)
	}
	nodes, err := store.ListClusterNodes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if nodes[0].ProtocolVersion != ClusterProtocolVersion || len(nodes[0].Capabilities) != 1 || nodes[0].Capabilities[0] != "apt" {
		t.Fatalf("sync corrupted protocol metadata: %+v", nodes[0])
	}
	if nodes[0].LatencyMS != results[0].LatencyMS {
		t.Fatalf("sync latency was not persisted separately: node=%+v result=%+v", nodes[0], results[0])
	}
	router := NewRouter(cfg)
	router.SetNodes(nodes)
	if selected, err := router.SelectNode("203.0.113.10", model.Mirror{Slug: "packages", Type: "apt"}, manifest.ConfigFingerprint); err != ErrNoAvailableEdge || selected != nil {
		t.Fatalf("synchronized Edge was routed before a repository health probe: node=%+v err=%v", selected, err)
	}
}

func TestBroadcastSyncBoundsConcurrency(t *testing.T) {
	var active atomic.Int64
	var maximum atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		current := active.Add(1)
		defer active.Add(-1)
		for {
			previous := maximum.Load()
			if current <= previous || maximum.CompareAndSwap(previous, current) {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
		var payload model.ClusterSyncRequest
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(model.ClusterSyncResponse{
			Status:           "applied",
			Fingerprint:      payload.Manifest.ConfigFingerprint,
			ProtocolVersion:  payload.Manifest.ProtocolVersion,
			ConfigGeneration: payload.Manifest.ConfigGeneration,
			Capabilities:     payload.Manifest.Capabilities,
		})
	}))
	defer server.Close()

	nodes := make([]model.ClusterNode, clusterMutationWorkers*3)
	for index := range nodes {
		nodes[index] = model.ClusterNode{
			ID: int64(index + 1), URL: server.URL, Enabled: true, MutationToken: "edge-mutation-secret",
		}
	}
	cfg := config.Default()
	cfg.Security.AllowPrivateUpstream = true
	cfg.Distributed.AllowHTTP = true
	cfg.Distributed.Node.Name = "coordinator-1"
	manager := NewSyncManager(cfg, &mockClusterStore{nodes: nodes}, "epoch-1")
	results := manager.BroadcastSync(context.Background(), emptySyncPayload())
	for _, result := range results {
		if !result.Success {
			t.Fatalf("bounded sync failed: %+v", result)
		}
	}
	if got := maximum.Load(); got < 2 || got > clusterMutationWorkers {
		t.Fatalf("broadcast sync concurrency = %d, want 2..%d", got, clusterMutationWorkers)
	}
}

func TestSyncNodeRejectsInvalidSuccessResponse(t *testing.T) {
	payload := emptySyncPayload()
	valid := model.ClusterSyncResponse{
		Status: "applied", Fingerprint: payload.Manifest.ConfigFingerprint,
		ProtocolVersion: ClusterProtocolVersion, ConfigGeneration: payload.Manifest.ConfigGeneration,
		Capabilities: []string{},
	}
	cases := map[string]string{
		"malformed JSON":       "not-json",
		"missing fields":       `{}`,
		"wrong fingerprint":    `{"status":"applied","fingerprint":"sha256:wrong","protocol_version":1,"config_generation":1,"capabilities":[]}`,
		"multiple JSON values": `{"status":"applied"} {}`,
		"unknown field":        `{"status":"applied","unexpected":true}`,
	}
	wrongGeneration := valid
	wrongGeneration.ConfigGeneration = 2
	validBytes, err := json.Marshal(wrongGeneration)
	if err != nil {
		t.Fatal(err)
	}
	cases["wrong generation"] = string(validBytes)
	cases["oversized response"] = string(validBytes) + strings.Repeat(" ", 64<<10)
	for name, response := range cases {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(response))
			}))
			defer server.Close()
			cfg := config.Default()
			cfg.Distributed.AllowHTTP = true
			cfg.Distributed.Node.Name = "coordinator-1"
			cfg.Security.AllowPrivateUpstream = true
			manager := NewSyncManager(cfg, nil, "epoch-1")
			result := manager.SyncNode(context.Background(), model.ClusterNode{URL: server.URL, MutationToken: "edge-mutation-secret"}, payload)
			if result.Success || result.Error == "" {
				t.Fatalf("invalid HTTP 200 response was accepted: %+v", result)
			}
		})
	}
}

func TestSyncNodeDoesNotSendTokenOverDisallowedHTTP(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	cfg := config.Default()
	cfg.Distributed.Node.Name = "coordinator-1"
	cfg.Security.AllowPrivateUpstream = true
	store := &mockClusterStore{nodes: []model.ClusterNode{{ID: 1, URL: server.URL, Enabled: true, MutationToken: "edge-mutation-secret"}}}
	manager := NewSyncManager(cfg, store, "epoch-1")
	result := manager.SyncNode(context.Background(), store.nodes[0], emptySyncPayload())
	if result.Success || requests != 0 {
		t.Fatalf("disallowed HTTP node received a token-bearing request: result=%+v requests=%d", result, requests)
	}
	purgeResults := manager.BroadcastPurge(context.Background(), model.ClusterPurgeRequest{Scope: "global"})
	if purgeResults[1] == nil || requests != 0 {
		t.Fatalf("disallowed HTTP node received a purge token: errors=%v requests=%d", purgeResults, requests)
	}
}

func emptySyncPayload() model.ClusterSyncRequest {
	repositories := []model.Mirror{}
	return model.ClusterSyncRequest{
		Manifest: model.ClusterManifest{
			ProtocolVersion: ClusterProtocolVersion, NodeID: "coordinator-1", CoordinatorID: "coordinator-1", CoordinatorEpoch: "epoch-1", ConfigGeneration: 1,
			ConfigFingerprint: CanonicalClusterConfigFingerprint(repositories, []model.CustomConfig{}), Capabilities: []string{},
		},
		Repositories: repositories, CustomConfigs: []model.CustomConfig{},
	}
}
