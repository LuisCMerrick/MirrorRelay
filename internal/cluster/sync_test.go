package cluster

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/LuisCMerrick/MirrorRelay/internal/config"
	"github.com/LuisCMerrick/MirrorRelay/internal/model"
)

type mockClusterStore struct {
	nodes []model.ClusterNode
}

func (m *mockClusterStore) ListClusterNodes(ctx context.Context) ([]model.ClusterNode, error) {
	return m.nodes, nil
}

func (m *mockClusterStore) GetClusterNode(ctx context.Context, id int64) (model.ClusterNode, error) {
	for _, n := range m.nodes {
		if n.ID == id {
			return n, nil
		}
	}
	return model.ClusterNode{}, ErrNoAvailableEdge
}

func (m *mockClusterStore) UpdateClusterNodeStatus(ctx context.Context, id int64, health, config, fp, ver string, protocolVersion int, caps []string, latencyMS int64, lastErr string, t time.Time) error {
	for i, n := range m.nodes {
		if n.ID == id {
			m.nodes[i].HealthStatus = health
			m.nodes[i].ConfigStatus = config
			m.nodes[i].ConfigFingerprint = fp
			m.nodes[i].Version = ver
			m.nodes[i].ProtocolVersion = protocolVersion
			m.nodes[i].Capabilities = append([]string(nil), caps...)
			m.nodes[i].LatencyMS = latencyMS
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
	cfg.Distributed.Token = "cluster-secret"

	store := &mockClusterStore{
		nodes: []model.ClusterNode{
			{
				ID:              1,
				Name:            "edge-1",
				URL:             ts.URL,
				Enabled:         true,
				ProtocolVersion: 1,
				Capabilities:    []string{"apt"},
			},
		},
	}

	sm := NewSyncManager(cfg, store)
	repositories := []model.Mirror{{Type: "apt"}}
	manifest := model.ClusterManifest{
		ProtocolVersion:   ClusterProtocolVersion,
		ConfigGeneration:  42,
		ConfigFingerprint: CanonicalFingerprint(repositories),
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
	if store.nodes[0].ProtocolVersion != ClusterProtocolVersion || len(store.nodes[0].Capabilities) != 1 || store.nodes[0].Capabilities[0] != "apt" {
		t.Fatalf("sync corrupted protocol metadata: %+v", store.nodes[0])
	}
	if store.nodes[0].LatencyMS != results[0].LatencyMS {
		t.Fatalf("sync latency was not persisted separately: node=%+v result=%+v", store.nodes[0], results[0])
	}
	router := NewRouter(cfg)
	router.SetNodes(store.nodes)
	selected, err := router.SelectNode("203.0.113.10", model.Mirror{Slug: "packages", Type: "apt"}, manifest.ConfigFingerprint)
	if err != nil || selected.ID != store.nodes[0].ID {
		t.Fatalf("successfully synchronized edge became unroutable: node=%+v err=%v", selected, err)
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
	}
	wrongGeneration := valid
	wrongGeneration.ConfigGeneration = 2
	validBytes, err := json.Marshal(wrongGeneration)
	if err != nil {
		t.Fatal(err)
	}
	cases["wrong generation"] = string(validBytes)
	for name, response := range cases {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(response))
			}))
			defer server.Close()
			cfg := config.Default()
			cfg.Distributed.AllowHTTP = true
			cfg.Distributed.Token = "cluster-secret"
			cfg.Security.AllowPrivateUpstream = true
			manager := NewSyncManager(cfg, nil)
			result := manager.SyncNode(context.Background(), model.ClusterNode{URL: server.URL}, payload)
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
	cfg.Distributed.Token = "cluster-secret"
	cfg.Security.AllowPrivateUpstream = true
	store := &mockClusterStore{nodes: []model.ClusterNode{{ID: 1, URL: server.URL, Enabled: true}}}
	manager := NewSyncManager(cfg, store)
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
			ProtocolVersion: ClusterProtocolVersion, ConfigGeneration: 1,
			ConfigFingerprint: CanonicalFingerprint(repositories), Capabilities: []string{},
		},
		Repositories: repositories, CustomConfigs: []model.CustomConfig{},
	}
}
