package cluster

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
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
	m4 := m1
	m4.BlockedPackages = []string{"malicious-*"}
	if CanonicalFingerprint([]model.Mirror{m4}) == fp1 {
		t.Fatal("changing package guard policy should alter fingerprint")
	}
	custom := []model.CustomConfig{{Name: "headers", Context: "server", Enabled: true, Content: "add_header X-Test one;\r\n"}}
	customFingerprint := CanonicalClusterConfigFingerprint([]model.Mirror{m1}, custom)
	custom[0].Content = "add_header X-Test one;\n"
	if normalized := CanonicalClusterConfigFingerprint([]model.Mirror{m1}, custom); normalized != customFingerprint {
		t.Fatalf("line-ending-only custom config change altered fingerprint: %q vs %q", customFingerprint, normalized)
	}
	custom[0].Content = "add_header X-Test two;\n"
	if CanonicalClusterConfigFingerprint([]model.Mirror{m1}, custom) == customFingerprint {
		t.Fatal("changing custom Managed Upstream Nginx configuration should alter fingerprint")
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
			ProtocolVersion:   ClusterProtocolVersion,
			Capabilities:      []string{"apt", "rpm"},
			RepositoryHealth:  map[string]bool{"debian": true},
			MutationToken:     "must-not-enter-routing-snapshot",
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
			ProtocolVersion:   ClusterProtocolVersion,
			Capabilities:      []string{"apt", "rpm"},
			RepositoryHealth:  map[string]bool{"debian": true},
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
			ProtocolVersion:   ClusterProtocolVersion,
			Capabilities:      []string{"apt", "rpm"},
			RepositoryHealth:  map[string]bool{"debian": true},
		},
	}
	router.SetNodes(nodes)
	if snapshot := router.Nodes(); snapshot[0].MutationToken != "" {
		t.Fatal("Router retained an Edge mutation credential")
	}

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

func TestRouterUsesPerRepositoryHealth(t *testing.T) {
	router := NewRouter(config.Default())
	fingerprint := "sha256:cluster"
	router.SetNodes([]model.ClusterNode{{
		ID: 1, Name: "edge", Enabled: true, HealthStatus: "degraded", ConfigStatus: "match",
		ConfigFingerprint: fingerprint, ProtocolVersion: ClusterProtocolVersion, Capabilities: []string{"apt"},
		RepositoryHealth: map[string]bool{"debian": false, "ubuntu": true},
	}})
	if _, err := router.SelectNode("203.0.113.10", model.Mirror{Slug: "debian", Type: "apt"}, fingerprint); !errors.Is(err, ErrNoAvailableEdge) {
		t.Fatalf("unhealthy repository remained routable: %v", err)
	}
	if node, err := router.SelectNode("203.0.113.10", model.Mirror{Slug: "ubuntu", Type: "apt"}, fingerprint); err != nil || node.ID != 1 {
		t.Fatalf("healthy repository on degraded Edge was excluded: node=%+v err=%v", node, err)
	}
}

type mockStore struct {
	mu      sync.Mutex
	nodes   map[int64]model.ClusterNode
	setting map[string]string
	persist error
}

func (m *mockStore) ListClusterNodes(ctx context.Context) ([]model.ClusterNode, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var list []model.ClusterNode
	for _, n := range m.nodes {
		list = append(list, n)
	}
	return list, nil
}

func (m *mockStore) GetClusterNode(ctx context.Context, id int64) (model.ClusterNode, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.nodes[id], nil
}

func (m *mockStore) UpdateClusterNodeStatus(_ context.Context, node model.ClusterNode) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.persist != nil {
		return m.persist
	}
	if _, exists := m.nodes[node.ID]; !exists {
		return sql.ErrNoRows
	}
	m.nodes[node.ID] = node
	return nil
}

func (m *mockStore) ClusterSetting(ctx context.Context, key string) (string, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.setting[key]
	return v, ok, nil
}

func (m *mockStore) PutClusterSetting(ctx context.Context, key, value string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
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
		ProtocolVersion:    ClusterProtocolVersion,
		MirrorRelayVersion: "0.0.1",
		NodeID:             "tokyo-01",
		CoordinatorID:      "coordinator-1",
		CoordinatorEpoch:   "epoch-1",
		ConfigGeneration:   1,
		ConfigFingerprint:  "sha256:valid_fingerprint",
		Capabilities:       []string{"apt", "rpm"},
	}
	health := model.ClusterHealth{
		Status:            "healthy",
		Version:           "0.0.1",
		ConfigGeneration:  1,
		ConfigFingerprint: "sha256:valid_fingerprint",
		Repositories:      map[string]bool{"debian": true, "rpm": true},
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
	cfg.Distributed.Node.Name = "coordinator-1"
	cfg.Distributed.HealthCheck.Interval = time.Second
	cfg.Distributed.HealthCheck.Timeout = time.Second
	cfg.Distributed.HealthCheck.HealthyThreshold = 1
	cfg.Distributed.HealthCheck.UnhealthyThreshold = 1
	cfg.Distributed.AllowHTTP = true
	cfg.Security.AllowHTTPUpstream = true
	cfg.Security.AllowPrivateUpstream = true

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
	uninitialized := NewChecker(cfg, store, router, metrics, audit)
	firstReport, err := uninitialized.recordSuccess(context.Background(), store.nodes[1], manifest, health)
	if err != nil {
		t.Fatal(err)
	}
	if uninitialized.ClusterFingerprint() != "" || firstReport.ConfigStatus != "mismatch" {
		t.Fatalf("first Edge report became authoritative: fingerprint=%q node=%+v", uninitialized.ClusterFingerprint(), firstReport)
	}

	checker := NewChecker(cfg, store, router, metrics, audit)
	if err := checker.SetExpectedConfiguration(context.Background(), manifest); err != nil {
		t.Fatal(err)
	}

	// 1. Initial check: compare against the Coordinator-owned fingerprint.
	node, err := checker.CheckNode(context.Background(), store.nodes[1])
	if err != nil {
		t.Fatal(err)
	}
	if node.HealthStatus != "healthy" || node.ConfigStatus != "match" || node.ConfigFingerprint != "sha256:valid_fingerprint" {
		t.Fatalf("unexpected node status after check: %+v", node)
	}
	if checker.ClusterFingerprint() != "sha256:valid_fingerprint" {
		t.Fatalf("cluster fingerprint changed unexpectedly: %s", checker.ClusterFingerprint())
	}

	// 2. Config drift: modify manifest returned by edge
	manifest.ConfigFingerprint = "sha256:drifted_fingerprint"
	health.ConfigFingerprint = manifest.ConfigFingerprint
	node2, err := checker.CheckNode(context.Background(), node)
	if err != nil {
		t.Fatal(err)
	}
	if node2.ConfigStatus != "mismatch" {
		t.Fatalf("expected config status mismatch after drift, got %s", node2.ConfigStatus)
	}

	// 3. Manifest and health must describe the same atomic snapshot.
	health.ConfigFingerprint = "sha256:other"
	inconsistent, err := checker.CheckNode(context.Background(), node2)
	if err != nil {
		t.Fatal(err)
	}
	if inconsistent.HealthStatus != "unhealthy" || inconsistent.ConfigStatus != "inconsistent" {
		t.Fatalf("inconsistent probe response remained routable: %+v", inconsistent)
	}
}

func TestCheckerReportsPersistenceFailure(t *testing.T) {
	store := &mockStore{nodes: map[int64]model.ClusterNode{1: {ID: 1, Name: "edge"}}, persist: errors.New("disk unavailable")}
	checker := NewChecker(config.Default(), store, nil, nil, nil)
	manifest := model.ClusterManifest{
		ProtocolVersion: ClusterProtocolVersion, NodeID: "edge", CoordinatorID: "coordinator", CoordinatorEpoch: "epoch",
		ConfigGeneration: 1, ConfigFingerprint: "sha256:one", Capabilities: []string{"apt"},
	}
	health := model.ClusterHealth{Status: "healthy", ConfigGeneration: 1, ConfigFingerprint: "sha256:one", Repositories: map[string]bool{"debian": true}}
	if _, err := checker.recordSuccess(context.Background(), store.nodes[1], manifest, health); err == nil {
		t.Fatal("cluster node status persistence failure was ignored")
	}
}

func TestCheckerAppliesRecoveryThresholdToDegradedNode(t *testing.T) {
	store := &mockStore{nodes: map[int64]model.ClusterNode{
		1: {ID: 1, Name: "edge", HealthStatus: "unhealthy", ConfigStatus: "match"},
	}, setting: make(map[string]string)}
	cfg := config.Default()
	cfg.Distributed.HealthCheck.HealthyThreshold = 2
	checker := NewChecker(cfg, store, nil, nil, nil)
	manifest := model.ClusterManifest{
		ProtocolVersion: ClusterProtocolVersion, NodeID: "edge", CoordinatorID: "coordinator", CoordinatorEpoch: "epoch",
		ConfigGeneration: 3, ConfigFingerprint: "sha256:three", Capabilities: []string{"apt"},
	}
	if err := checker.SetExpectedConfiguration(context.Background(), manifest); err != nil {
		t.Fatal(err)
	}
	health := model.ClusterHealth{
		Status: "degraded", ConfigGeneration: 3, ConfigFingerprint: manifest.ConfigFingerprint,
		Repositories: map[string]bool{"debian": false, "ubuntu": true},
	}
	first, err := checker.recordSuccess(context.Background(), store.nodes[1], manifest, health)
	if err != nil {
		t.Fatal(err)
	}
	if first.HealthStatus != "unhealthy" {
		t.Fatalf("degraded Edge bypassed recovery threshold after one success: %+v", first)
	}
	second, err := checker.recordSuccess(context.Background(), first, manifest, health)
	if err != nil {
		t.Fatal(err)
	}
	if second.HealthStatus != "degraded" || !second.RepositoryHealth["ubuntu"] {
		t.Fatalf("degraded Edge did not recover after threshold: %+v", second)
	}
}

func TestCheckerScansNodesWithBoundedConcurrency(t *testing.T) {
	manifest := model.ClusterManifest{
		ProtocolVersion: ClusterProtocolVersion, NodeID: "edge", CoordinatorID: "coordinator", CoordinatorEpoch: "epoch",
		ConfigGeneration: 7, ConfigFingerprint: "sha256:seven", Capabilities: []string{"apt"},
	}
	health := model.ClusterHealth{Status: "healthy", ConfigGeneration: 7, ConfigFingerprint: manifest.ConfigFingerprint, Repositories: map[string]bool{"debian": true}}
	edge := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		time.Sleep(30 * time.Millisecond)
		if request.URL.Path == "/api/v1/cluster/manifest" {
			_ = json.NewEncoder(w).Encode(manifest)
			return
		}
		_ = json.NewEncoder(w).Encode(health)
	}))
	defer edge.Close()

	cfg := config.Default()
	cfg.Distributed.AllowHTTP = true
	cfg.Distributed.HealthCheck.Timeout = 2 * time.Second
	cfg.Distributed.HealthCheck.HealthyThreshold = 1
	cfg.Security.AllowPrivateUpstream = true
	store := &mockStore{nodes: make(map[int64]model.ClusterNode), setting: make(map[string]string)}
	for id := int64(1); id <= 20; id++ {
		store.nodes[id] = model.ClusterNode{ID: id, Name: "edge", URL: edge.URL, Enabled: true, HealthStatus: "unknown"}
	}
	checker := NewChecker(cfg, store, NewRouter(cfg), nil, nil)
	if err := checker.SetExpectedConfiguration(context.Background(), manifest); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	if err := checker.CheckAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed >= 800*time.Millisecond {
		t.Fatalf("20-node health scan was effectively serial: %s", elapsed)
	}
}

func TestCheckerDoesNotRestoreNodeDeletedDuringScan(t *testing.T) {
	manifest := model.ClusterManifest{
		ProtocolVersion: ClusterProtocolVersion, NodeID: "edge", CoordinatorID: "coordinator", CoordinatorEpoch: "epoch",
		ConfigGeneration: 4, ConfigFingerprint: "sha256:four", Capabilities: []string{"apt"},
	}
	health := model.ClusterHealth{
		Status: "healthy", ConfigGeneration: 4, ConfigFingerprint: manifest.ConfigFingerprint,
		Repositories: map[string]bool{"debian": true},
	}
	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	edge := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		once.Do(func() { close(started) })
		<-release
		if request.URL.Path == "/api/v1/cluster/manifest" {
			_ = json.NewEncoder(w).Encode(manifest)
			return
		}
		_ = json.NewEncoder(w).Encode(health)
	}))
	defer edge.Close()

	cfg := config.Default()
	cfg.Distributed.AllowHTTP = true
	cfg.Distributed.HealthCheck.Timeout = 2 * time.Second
	cfg.Distributed.HealthCheck.HealthyThreshold = 1
	cfg.Security.AllowPrivateUpstream = true
	store := &mockStore{
		nodes:   map[int64]model.ClusterNode{1: {ID: 1, Name: "edge", URL: edge.URL, Enabled: true}},
		setting: make(map[string]string),
	}
	router := NewRouter(cfg)
	router.SetNodes([]model.ClusterNode{{ID: 1, Name: "edge", Enabled: true}})
	checker := NewChecker(cfg, store, router, nil, nil)
	if err := checker.SetExpectedConfiguration(context.Background(), manifest); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- checker.CheckAll(context.Background()) }()
	<-started
	store.mu.Lock()
	delete(store.nodes, 1)
	store.mu.Unlock()
	close(release)
	if err := <-done; err == nil {
		t.Fatal("scan did not report status persistence for a concurrently deleted node")
	}
	if nodes := router.Nodes(); len(nodes) != 0 {
		t.Fatalf("deleted node was restored from a stale health snapshot: %+v", nodes)
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

	manifest := GenerateManifest(cfg, repos, []model.CustomConfig{}, build, 5, "coordinator-1", "epoch-1")
	if manifest.NodeID != "edge-node-1" || manifest.ProtocolVersion != ClusterProtocolVersion || manifest.ConfigGeneration != 5 ||
		manifest.CoordinatorID != "coordinator-1" || manifest.CoordinatorEpoch != "epoch-1" {
		t.Fatalf("unexpected manifest: %+v", manifest)
	}
	if len(manifest.Capabilities) != 2 || manifest.Capabilities[0] != "apt" || manifest.Capabilities[1] != "npm" {
		t.Fatalf("unexpected capabilities: %+v", manifest.Capabilities)
	}
}
