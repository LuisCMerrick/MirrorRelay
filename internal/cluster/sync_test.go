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

func (m *mockClusterStore) UpdateClusterNodeStatus(ctx context.Context, id int64, health, config, fp, ver string, lat int, caps []string, lastErr string, t time.Time) error {
	for i, n := range m.nodes {
		if n.ID == id {
			m.nodes[i].HealthStatus = health
			m.nodes[i].ConfigStatus = config
			m.nodes[i].ConfigFingerprint = fp
			m.nodes[i].LatencyMS = int64(lat)
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
		if r.URL.Path == "/admin/api/v1/cluster/apply-sync" {
			var m model.ClusterManifest
			if err := json.NewDecoder(r.Body).Decode(&m); err != nil {
				http.Error(w, err.Error(), 400)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{
				"status":      "applied",
				"fingerprint": m.ConfigFingerprint,
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()

	cfg := config.Config{}
	cfg.Security.AllowPrivateUpstream = true

	store := &mockClusterStore{
		nodes: []model.ClusterNode{
			{
				ID:      1,
				Name:    "edge-1",
				URL:     ts.URL,
				Enabled: true,
			},
		},
	}

	sm := NewSyncManager(cfg, store, nil, nil, nil)
	manifest := model.ClusterManifest{
		ProtocolVersion:   1,
		ConfigFingerprint: "sha256:testfingerprint",
	}

	results := sm.BroadcastSync(context.Background(), manifest)
	if len(results) != 1 {
		t.Fatalf("expected 1 sync result, got %d", len(results))
	}
	if !results[0].Success {
		t.Fatalf("expected sync success, got error: %s", results[0].Error)
	}
	if results[0].Fingerprint != "sha256:testfingerprint" {
		t.Fatalf("expected fingerprint sha256:testfingerprint, got %s", results[0].Fingerprint)
	}
}
