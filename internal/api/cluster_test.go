package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"github.com/LuisCMerrick/MirrorRelay/internal/cluster"
	"github.com/LuisCMerrick/MirrorRelay/internal/config"
	"github.com/LuisCMerrick/MirrorRelay/internal/mirror"
	"github.com/LuisCMerrick/MirrorRelay/internal/model"
)

func TestClusterManifestAndHealthEndpoints(t *testing.T) {
	cfg := config.Default()
	cfg.Distributed.Enabled = true
	cfg.Distributed.Role = "edge"
	cfg.Distributed.Token = "secret-token-123"
	cfg.Distributed.Node.Name = "tokyo-01"

	registry := mirror.NewRegistry(nil)
	registry.Replace([]model.Mirror{
		{ID: 1, Name: "Debian", Slug: "debian", Type: "apt", Enabled: true, PublicPath: "/debian/"},
	})

	server := &Server{
		cfg:      cfg,
		registry: registry,
		web:      fstest.MapFS{"index.html": {Data: []byte("admin index")}},
	}
	handler := server.Handler(http.NotFoundHandler())

	// 1. Unauthenticated request to /api/v1/cluster/manifest -> 401
	r1 := httptest.NewRequest(http.MethodGet, "/api/v1/cluster/manifest", nil)
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, r1)
	if rec1.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for unauthenticated manifest request, got %d", rec1.Code)
	}

	// 2. Authenticated request with header -> 200
	r2 := httptest.NewRequest(http.MethodGet, "/api/v1/cluster/manifest", nil)
	r2.Header.Set("X-MirrorRelay-Cluster-Token", "secret-token-123")
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, r2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("expected 200 for authenticated manifest request, got %d", rec2.Code)
	}
	var manifest model.ClusterManifest
	if err := json.NewDecoder(rec2.Body).Decode(&manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.NodeID != "tokyo-01" || manifest.ProtocolVersion != 1 || len(manifest.Capabilities) != 1 || manifest.Capabilities[0] != "apt" {
		t.Fatalf("unexpected manifest: %+v", manifest)
	}

	// 3. Authenticated request to /api/v1/cluster/health -> 200
	r3 := httptest.NewRequest(http.MethodGet, "/api/v1/cluster/health", nil)
	r3.Header.Set("Authorization", "Bearer secret-token-123")
	rec3 := httptest.NewRecorder()
	handler.ServeHTTP(rec3, r3)
	if rec3.Code != http.StatusOK {
		t.Fatalf("expected 200 for authenticated health request, got %d", rec3.Code)
	}
}

func TestCoordinatorDistributed307Redirect(t *testing.T) {
	cfg := config.Default()
	cfg.Distributed.Enabled = true
	cfg.Distributed.Role = "coordinator"
	cfg.Distributed.Routing.ClientNetworks = []config.ClientNetworkMapping{
		{CIDR: "10.20.0.0/16", Region: "jp-tokyo"},
	}

	registry := mirror.NewRegistry(nil)
	registry.Replace([]model.Mirror{
		{ID: 1, Name: "Debian", Slug: "debian", Type: "apt", Enabled: true, PublicPath: "/debian/"},
		{ID: 2, Name: "Docker Hub", Slug: "docker", Type: "docker-registry", Enabled: true, PublicPath: "/docker/"},
	})

	router := cluster.NewRouter(cfg)
	fp := cluster.CanonicalFingerprint(registry.List())
	router.SetNodes([]model.ClusterNode{
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
			Capabilities:      []string{"apt"},
		},
	})

	metrics := cluster.NewMetrics()
	checker := cluster.NewChecker(cfg, nil, router, metrics, nil)
	_ = checker.SetClusterFingerprint(context.Background(), fp)

	server := &Server{
		cfg:            cfg,
		registry:       registry,
		clusterRouter:  router,
		clusterChecker: checker,
		clusterMetrics: metrics,
		web:            fstest.MapFS{"index.html": {Data: []byte("admin index")}},
	}
	handler := server.Handler(http.NotFoundHandler())

	// 1. Client request to /debian/dists/bookworm/InRelease?arch=amd64 from Tokyo CIDR 10.20.1.5 -> 307 to Tokyo Edge
	r1 := httptest.NewRequest(http.MethodGet, "https://repo.example.com/debian/dists/bookworm/InRelease?arch=amd64", nil)
	r1.RemoteAddr = "10.20.1.5:1234"
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, r1)

	if rec1.Code != http.StatusTemporaryRedirect {
		t.Fatalf("expected HTTP 307 Temporary Redirect, got %d; body=%s", rec1.Code, rec1.Body.String())
	}
	loc := rec1.Header().Get("Location")
	expectedLoc := "https://jp.repo.example.com/debian/dists/bookworm/InRelease?arch=amd64"
	if loc != expectedLoc {
		t.Fatalf("Location mismatch: got %q, want %q", loc, expectedLoc)
	}

	// Preserve escaped separators, percent signs, Unicode, duplicate slashes and query bytes.
	escapedURL := "https://repo.example.com/debian/pool/a%2Fb%25c/%E4%B8%AD//pkg?x=%2F&y=%25"
	escapedRequest := httptest.NewRequest(http.MethodGet, escapedURL, nil)
	escapedRequest.RemoteAddr = "10.20.1.5:1234"
	escapedRecorder := httptest.NewRecorder()
	handler.ServeHTTP(escapedRecorder, escapedRequest)
	if escapedRecorder.Code != http.StatusTemporaryRedirect {
		t.Fatalf("expected encoded request redirect, got %d: %s", escapedRecorder.Code, escapedRecorder.Body.String())
	}
	wantEscapedLocation := "https://jp.repo.example.com/debian/pool/a%2Fb%25c/%E4%B8%AD//pkg?x=%2F&y=%25"
	if got := escapedRecorder.Header().Get("Location"); got != wantEscapedLocation {
		t.Fatalf("encoded Location changed semantics: got %q, want %q", got, wantEscapedLocation)
	}

	// 2. Client request for docker-registry -> 501 / Not implemented
	r2 := httptest.NewRequest(http.MethodGet, "https://repo.example.com/docker/v2/", nil)
	r2.RemoteAddr = "10.20.1.5:1234"
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, r2)
	if rec2.Code != http.StatusNotImplemented {
		t.Fatalf("expected 501 for distributed docker registry, got %d", rec2.Code)
	}

	// 3. When no healthy nodes exist -> 503 Service Unavailable
	router.SetNodes([]model.ClusterNode{})
	r3 := httptest.NewRequest(http.MethodGet, "https://repo.example.com/debian/dists/bookworm/InRelease", nil)
	r3.RemoteAddr = "10.20.1.5:1234"
	rec3 := httptest.NewRecorder()
	handler.ServeHTTP(rec3, r3)
	if rec3.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 when no healthy edge is available, got %d", rec3.Code)
	}
}
