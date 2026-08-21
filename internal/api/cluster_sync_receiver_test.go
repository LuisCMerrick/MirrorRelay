package api

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LuisCMerrick/MirrorRelay/internal/buildinfo"
	"github.com/LuisCMerrick/MirrorRelay/internal/cachectl"
	"github.com/LuisCMerrick/MirrorRelay/internal/cluster"
	"github.com/LuisCMerrick/MirrorRelay/internal/config"
	"github.com/LuisCMerrick/MirrorRelay/internal/database"
	"github.com/LuisCMerrick/MirrorRelay/internal/mirror"
	"github.com/LuisCMerrick/MirrorRelay/internal/model"
	"github.com/LuisCMerrick/MirrorRelay/internal/upstreamnginx"
)

func TestClusterSyncAndPurgeUseRealEdgeHandler(t *testing.T) {
	ctx := context.Background()
	edgeStore, err := database.Open(filepath.Join(t.TempDir(), "edge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer edgeStore.Close()
	edgeConfig := config.Default()
	edgeConfig.Distributed.Enabled = true
	edgeConfig.Distributed.Role = "edge"
	edgeConfig.Distributed.Token = "shared-cluster-secret"
	edgeConfig.UpstreamNginx.Mode = "disabled"
	edgeRegistry := mirror.NewRegistry(edgeStore)
	edgeCache := cachectl.New(edgeConfig, edgeStore)
	if err := edgeCache.Load(ctx); err != nil {
		t.Fatal(err)
	}
	edgeController := upstreamnginx.NewController(edgeConfig, edgeStore)
	if err := edgeController.Start(ctx); err != nil {
		t.Fatal(err)
	}
	edgeServer, err := New(edgeConfig, edgeConfig, edgeStore, edgeRegistry, edgeCache, nil, nil, edgeController, nil, buildinfo.Info{Version: "test-version"})
	if err != nil {
		t.Fatal(err)
	}
	defer edgeServer.webhook.Stop()
	edgeHTTP := httptest.NewServer(edgeServer.Handler(http.NotFoundHandler()))
	defer edgeHTTP.Close()

	unauthenticated, err := http.Post(edgeHTTP.URL+cluster.SyncApplyPath, "application/json", bytes.NewBufferString("{}"))
	if err != nil {
		t.Fatal(err)
	}
	_ = unauthenticated.Body.Close()
	if unauthenticated.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated sync receiver returned %d", unauthenticated.StatusCode)
	}

	coordinatorStore, err := database.Open(filepath.Join(t.TempDir(), "coordinator.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer coordinatorStore.Close()
	coordinatorConfig := config.Default()
	coordinatorConfig.Distributed.Enabled = true
	coordinatorConfig.Distributed.Role = "coordinator"
	coordinatorConfig.Distributed.Token = edgeConfig.Distributed.Token
	coordinatorConfig.Distributed.AllowHTTP = true
	coordinatorConfig.Security.AllowPrivateUpstream = true
	coordinatorConfig.Security.AdminCIDRs = []string{"127.0.0.1/32"}
	coordinatorConfig.UpstreamNginx.Mode = "disabled"
	node, err := coordinatorStore.CreateClusterNode(ctx, model.ClusterNode{
		Name: "edge", URL: edgeHTTP.URL, Region: "test", Enabled: true, ProtocolVersion: cluster.ClusterProtocolVersion,
	})
	if err != nil {
		t.Fatal(err)
	}

	repository := model.Mirror{
		ID: 1, Name: "Packages", Slug: "packages", Type: "generic", Enabled: true,
		PublicMode: "path", PublicPath: "/packages/", CacheEnabled: true,
		Upstreams: []model.Upstream{{ID: 1, MirrorID: 1, URL: "https://8.8.8.8/", Enabled: true, Priority: 100, Weight: 100}},
	}
	if err := mirror.NormalizeAndValidate(&repository, false, false); err != nil {
		t.Fatal(err)
	}
	manifest := cluster.GenerateManifest(coordinatorConfig, []model.Mirror{repository}, buildinfo.Info{Version: "test-version"}, 7)
	payload := model.ClusterSyncRequest{
		Manifest: manifest, Repositories: []model.Mirror{repository}, CustomConfigs: []model.CustomConfig{},
	}
	manager := cluster.NewSyncManager(coordinatorConfig, coordinatorStore)
	result := manager.SyncNode(ctx, node, payload)
	if !result.Success {
		t.Fatalf("real handler sync failed: %+v", result)
	}
	edgeRepositories, err := edgeStore.ListMirrors(ctx)
	if err != nil || len(edgeRepositories) != 1 || edgeRepositories[0].Slug != repository.Slug {
		t.Fatalf("edge configuration was not applied: repositories=%+v err=%v", edgeRepositories, err)
	}
	updatedNode, err := coordinatorStore.GetClusterNode(ctx, node.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updatedNode.ProtocolVersion != cluster.ClusterProtocolVersion || len(updatedNode.Capabilities) != 1 || updatedNode.Capabilities[0] != "generic" {
		t.Fatalf("sync response metadata was not persisted correctly: %+v", updatedNode)
	}

	if err := coordinatorStore.ReplaceConfiguration(ctx, []model.Mirror{repository}, []model.CustomConfig{}); err != nil {
		t.Fatal(err)
	}
	coordinatorRegistry := mirror.NewRegistry(coordinatorStore)
	coordinatorRegistry.Replace([]model.Mirror{repository})
	coordinatorCache := cachectl.New(coordinatorConfig, coordinatorStore)
	if err := coordinatorCache.Load(ctx); err != nil {
		t.Fatal(err)
	}
	coordinatorController := upstreamnginx.NewController(coordinatorConfig, coordinatorStore)
	if err := coordinatorController.Start(ctx); err != nil {
		t.Fatal(err)
	}
	coordinatorServer, err := New(coordinatorConfig, coordinatorConfig, coordinatorStore, coordinatorRegistry,
		coordinatorCache, nil, nil, coordinatorController, nil, buildinfo.Info{Version: "test-version"})
	if err != nil {
		t.Fatal(err)
	}
	defer coordinatorServer.webhook.Stop()
	if err := coordinatorStore.CreateUser(ctx, "cluster-admin", "unused-hash", "admin"); err != nil {
		t.Fatal(err)
	}
	admin, err := coordinatorStore.UserByName(ctx, "cluster-admin")
	if err != nil {
		t.Fatal(err)
	}
	session, err := coordinatorServer.sessions.Create(admin.ID, admin.Username, admin.Role)
	if err != nil {
		t.Fatal(err)
	}
	coordinatorHandler := coordinatorServer.Handler(http.NotFoundHandler())
	purges := []struct {
		method string
		path   string
		body   string
	}{
		{method: http.MethodDelete, path: "/admin/api/v1/cache"},
		{method: http.MethodDelete, path: fmt.Sprintf("/admin/api/v1/mirrors/%d/cache", repository.ID)},
		{method: http.MethodPost, path: fmt.Sprintf("/admin/api/v1/mirrors/%d/cache/purge", repository.ID), body: `{"path":"/archive.tar.gz","query":""}`},
	}
	for _, purge := range purges {
		request := httptest.NewRequest(purge.method, "https://coordinator.example"+purge.path, strings.NewReader(purge.body))
		request.RemoteAddr = "127.0.0.1:12345"
		request.Header.Set("X-CSRF-Token", session.CSRFToken)
		request.AddCookie(&http.Cookie{Name: "mirrorrelay_session", Value: session.ID})
		recorder := httptest.NewRecorder()
		coordinatorHandler.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"targets":1`) || !strings.Contains(recorder.Body.String(), `"failed":0`) {
			t.Fatalf("Coordinator purge API %s %s did not propagate: status=%d body=%s", purge.method, purge.path, recorder.Code, recorder.Body.String())
		}
	}

	invalidUpdate := httptest.NewRequest(http.MethodPut, fmt.Sprintf("https://coordinator.example/admin/api/v1/cluster/nodes/%d", node.ID),
		strings.NewReader(`{"url":"https://user:pass@edge.example.com?token=secret"}`))
	invalidUpdate.RemoteAddr = "127.0.0.1:12345"
	invalidUpdate.Header.Set("X-CSRF-Token", session.CSRFToken)
	invalidUpdate.AddCookie(&http.Cookie{Name: "mirrorrelay_session", Value: session.ID})
	invalidUpdateRecorder := httptest.NewRecorder()
	coordinatorHandler.ServeHTTP(invalidUpdateRecorder, invalidUpdate)
	if invalidUpdateRecorder.Code != http.StatusBadRequest {
		t.Fatalf("cluster node update accepted a credentialed/query URL: status=%d body=%s", invalidUpdateRecorder.Code, invalidUpdateRecorder.Body.String())
	}
	persistedNode, err := coordinatorStore.GetClusterNode(ctx, node.ID)
	if err != nil || persistedNode.URL != node.URL {
		t.Fatalf("rejected cluster node URL changed persistent state: node=%+v err=%v", persistedNode, err)
	}

	objectID := cachectl.CanonicalObjectID(repository.ID, repository.Upstreams[0].URL, "/archive.tar.gz", "")
	generations, err := edgeStore.ListCacheGenerations(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]bool{}
	for _, generation := range generations {
		if generation.Generation <= 1 {
			continue
		}
		switch {
		case generation.Scope == "global":
			found["global"] = true
		case generation.Scope == "repository" && generation.RepositoryID == repository.ID:
			found["repository"] = true
		case generation.Scope == "object" && generation.RepositoryID == repository.ID && generation.ObjectID == objectID:
			found["object"] = true
		}
	}
	for _, scope := range []string{"global", "repository", "object"} {
		if !found[scope] {
			t.Fatalf("edge %s cache generation was not advanced: %+v", scope, generations)
		}
	}
}
