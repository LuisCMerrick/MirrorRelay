package cluster

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/LuisCMerrick/MirrorRelay/internal/buildinfo"
	"github.com/LuisCMerrick/MirrorRelay/internal/config"
	"github.com/LuisCMerrick/MirrorRelay/internal/model"
)

func TestCanonicalFingerprintStability(t *testing.T) {
	m1 := model.Mirror{
		ID:          1,
		Name:        "Debian",
		Slug:        "debian",
		Type:        "apt",
		Enabled:     true,
		PublicMode:  "path",
		PublicHost:  "tokyo.repo.example.com", // Node-specific host
		PublicPath:  "/debian/",
		Upstreams:   []model.Upstream{{URL: "https://deb.debian.org/debian", Priority: 10, Weight: 100, Enabled: true}},
		HeaderAdd:   map[string]string{"b": "2", "a": "1"},
		Description: "Debian Mirror",
	}

	m2 := model.Mirror{
		ID:          99,
		Name:        "Debian",
		Slug:        "debian",
		Type:        "apt",
		Enabled:     true,
		PublicMode:  "path",
		PublicHost:  "sg.repo.example.com", // Different node-specific host
		PublicPath:  "/debian/",
		Upstreams:   []model.Upstream{{URL: "https://deb.debian.org/debian", Priority: 10, Weight: 100, Enabled: true}},
		HeaderAdd:   map[string]string{"a": "1", "b": "2"},
		Description: "Debian Mirror",
	}

	fp1 := CanonicalFingerprint([]model.Mirror{m1})
	fp2 := CanonicalFingerprint([]model.Mirror{m2})

	if fp1 == "" || fp1 != fp2 {
		t.Fatalf("expected identical fingerprints across node local hosts, got %q vs %q", fp1, fp2)
	}

	// Changing logical config should change fingerprint
	m3 := m1
	m3.Type = "rpm"
	fp3 := CanonicalFingerprint([]model.Mirror{m3})
	if fp3 == fp1 {
		t.Fatalf("changing repository type should alter fingerprint")
	}
}

func TestRouterSelection(t *testing.T) {
	cfg := config.Default()
	cfg.Distributed.Enabled = true
	cfg.Distributed.Role = "coordinator"
	cfg.Distributed.Routing.ClientNetworks = []config.ClientNetworkMapping{
		{CIDR: "10.20.0.0/16", Region: "jp-tokyo"},
		{CIDR: "10.30.0.0/16", Region: "sg"},
	}

	router := NewRouter(cfg)

	fp := "sha256:1111"
	nodes := []model.ClusterNode{
		{
			ID:                1,
			Name:              "tokyo-01",
			URL:               "https://jp.repo.example.com",
			Region:            "jp-tokyo",
			Priority:          100,
			Weight:            100,
			Enabled:           true,
			HealthStatus:      "healthy",
			ConfigStatus:      "match",
			ConfigFingerprint: fp,
			ProtocolVersion:   1,
			Capabilities:      []string{"apt", "rpm"},
		},
		{
			ID:                2,
			Name:              "sg-01",
			URL:               "https://sg.repo.example.com",
			Region:            "sg",
			Priority:          100,
			Weight:            100,
			Enabled:           true,
			HealthStatus:      "healthy",
			ConfigStatus:      "match",
			ConfigFingerprint: fp,
			ProtocolVersion:   1,
			Capabilities:      []string{"apt", "rpm"},
		},
		{
			ID:                3,
			Name:              "us-01",
			URL:               "https://us.repo.example.com",
			Region:            "us",
			Priority:          200, // lower priority (fallback)
			Weight:            100,
			Enabled:           true,
			HealthStatus:      "healthy",
			ConfigStatus:      "match",
			ConfigFingerprint: fp,
			ProtocolVersion:   1,
			Capabilities:      []string{"apt", "rpm"},
		},
	}
	router.SetNodes(nodes)

	aptRepo := model.Mirror{Slug: "debian", Type: "apt"}

	// 1. Client in Tokyo CIDR 10.20.1.5 -> should pick tokyo-01
	nodeTok, err := router.SelectNode("10.20.1.5", aptRepo, fp)
	if err != nil || nodeTok.Name != "tokyo-01" {
		t.Fatalf("expected tokyo-01 for 10.20.1.5, got node=%+v err=%v", nodeTok, err)
	}

	// 2. Client in SG CIDR 10.30.5.10 -> should pick sg-01
	nodeSG, err := router.SelectNode("10.30.5.10", aptRepo, fp)
	if err != nil || nodeSG.Name != "sg-01" {
		t.Fatalf("expected sg-01 for 10.30.5.10, got node=%+v err=%v", nodeSG, err)
	}

	// 3. Client in other CIDR 192.168.1.1 -> should pick priority 100 nodes (tokyo-01 or sg-01, but not us-01 which is priority 200)
	nodeOther, err := router.SelectNode("192.168.1.1", aptRepo, fp)
	if err != nil || (nodeOther.Name != "tokyo-01" && nodeOther.Name != "sg-01") {
		t.Fatalf("expected priority 100 node, got node=%+v err=%v", nodeOther, err)
	}

	// 4. Stable selection: repeated calls with same client IP and slug must return identical node
	for i := 0; i < 20; i++ {
		repeat, err := router.SelectNode("192.168.1.1", aptRepo, fp)
		if err != nil || repeat.ID != nodeOther.ID {
			t.Fatalf("stable selection mismatch at iteration %d: got %s want %s", i, repeat.Name, nodeOther.Name)
		}
	}

	// 5. Docker / OCI registry repository -> must reject with ErrDistributedRegistryNotAllowed
	dockerRepo := model.Mirror{Slug: "docker", Type: "docker-registry"}
	_, err = router.SelectNode("10.20.1.5", dockerRepo, fp)
	if err != ErrDistributedRegistryNotAllowed {
		t.Fatalf("expected ErrDistributedRegistryNotAllowed for docker, got %v", err)
	}

	// 6. Capability filter: node does not support pypi
	pypiRepo := model.Mirror{Slug: "pypi", Type: "pypi"}
	_, err = router.SelectNode("10.20.1.5", pypiRepo, fp)
	if err != ErrNoAvailableEdge {
		t.Fatalf("expected ErrNoAvailableEdge for unsupported capability, got %v", err)
	}
}

type mockStore struct {
	nodes   map[int64]model.ClusterNode
	setting map[string]string
}

func (m *mockStore) ListClusterNodes(ctx context.Context) ([]model.ClusterNode, error) {
	var list []model.ClusterNode
	for _, n := range m.nodes {
		list = append(list, n)
	}
	return list, nil
}

func (m *mockStore) GetClusterNode(ctx context.Context, id int64) (model.ClusterNode, error) {
	return m.nodes[id], nil
}

func (m *mockStore) UpdateClusterNodeStatus(ctx context.Context, id int64, healthStatus, configStatus, fingerprint, version string, protoVer int, caps []string, lastError string, lastCheck time.Time) error {
	n := m.nodes[id]
	n.HealthStatus = healthStatus
	n.ConfigStatus = configStatus
	n.ConfigFingerprint = fingerprint
	n.Version = version
	n.ProtocolVersion = protoVer
	n.Capabilities = caps
	n.LastError = lastError
	n.LastCheck = lastCheck
	m.nodes[id] = n
	return nil
}

func (m *mockStore) ClusterSetting(ctx context.Context, key string) (string, bool, error) {
	v, ok := m.setting[key]
	return v, ok, nil
}

func (m *mockStore) PutClusterSetting(ctx context.Context, key, value string) error {
	if m.setting == nil {
		m.setting = make(map[string]string)
	}
	m.setting[key] = value
	return nil
}

type mockAudit struct {
	entries []string
}

func (a *mockAudit) Record(user, action, object, detail string, ok bool) {
	a.entries = append(a.entries, action+":"+object)
}

func TestCheckerProbeAndDrift(t *testing.T) {
	// Mock Edge server
	manifest := model.ClusterManifest{
		ProtocolVersion:    1,
		MirrorRelayVersion: "0.0.1",
		NodeID:             "tokyo-01",
		ConfigGeneration:   1,
		ConfigFingerprint:  "sha256:valid_fingerprint",
		Capabilities:       []string{"apt", "rpm"},
	}
	health := model.ClusterHealth{
		Status:            "healthy",
		Version:           "0.0.1",
		ConfigFingerprint: "sha256:valid_fingerprint",
	}

	edgeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/cluster/manifest" {
			_ = json.NewEncoder(w).Encode(manifest)
			return
		}
		if r.URL.Path == "/api/v1/cluster/health" {
			_ = json.NewEncoder(w).Encode(health)
			return
		}
		http.NotFound(w, r)
	}))
	defer edgeServer.Close()

	cfg := config.Default()
	cfg.Distributed.Enabled = true
	cfg.Distributed.Role = "coordinator"
	cfg.Distributed.HealthCheck.Interval = time.Second
	cfg.Distributed.HealthCheck.Timeout = time.Second
	cfg.Distributed.HealthCheck.HealthyThreshold = 1
	cfg.Distributed.HealthCheck.UnhealthyThreshold = 1

	store := &mockStore{
		nodes: map[int64]model.ClusterNode{
			1: {
				ID:           1,
				Name:         "tokyo-01",
				URL:          edgeServer.URL,
				Region:       "jp-tokyo",
				Enabled:      true,
				HealthStatus: "unknown",
				ConfigStatus: "unknown",
			},
		},
		setting: make(map[string]string),
	}

	metrics := NewMetrics()
	audit := &mockAudit{}
	router := NewRouter(cfg)
	checker := NewChecker(cfg, store, router, metrics, audit)

	// 1. Initial check: should succeed and initialize cluster fingerprint
	node, err := checker.CheckNode(context.Background(), store.nodes[1])
	if err != nil {
		t.Fatal(err)
	}
	if node.HealthStatus != "healthy" || node.ConfigStatus != "match" || node.ConfigFingerprint != "sha256:valid_fingerprint" {
		t.Fatalf("unexpected node status after check: %+v", node)
	}
	if checker.ClusterFingerprint() != "sha256:valid_fingerprint" {
		t.Fatalf("cluster fingerprint not initialized: %s", checker.ClusterFingerprint())
	}

	// 2. Config drift: modify manifest returned by edge
	manifest.ConfigFingerprint = "sha256:drifted_fingerprint"
	node2, err := checker.CheckNode(context.Background(), node)
	if err != nil {
		t.Fatal(err)
	}
	if node2.ConfigStatus != "mismatch" {
		t.Fatalf("expected config status mismatch after drift, got %s", node2.ConfigStatus)
	}
}

func TestManifestGeneration(t *testing.T) {
	cfg := config.Default()
	cfg.Distributed.Node.Name = "edge-node-1"
	build := buildinfo.New("0.0.1", "abcd", "2026-08-16", "build1")
	repos := []model.Mirror{
		{Slug: "npm", Type: "npm", Enabled: true},
		{Slug: "debian", Type: "apt", Enabled: true},
	}

	manifest := GenerateManifest(cfg, repos, build, 5)
	if manifest.NodeID != "edge-node-1" || manifest.ProtocolVersion != 1 || manifest.ConfigGeneration != 5 {
		t.Fatalf("unexpected manifest: %+v", manifest)
	}
	if len(manifest.Capabilities) != 2 || manifest.Capabilities[0] != "apt" || manifest.Capabilities[1] != "npm" {
		t.Fatalf("unexpected capabilities: %+v", manifest.Capabilities)
	}
}
