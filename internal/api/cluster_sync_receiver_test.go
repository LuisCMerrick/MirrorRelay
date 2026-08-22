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
	edgeConfig.Distributed.MutationToken = "edge-only-mutation-secret"
	edgeConfig.Distributed.CoordinatorID = "coordinator-1"
	edgeConfig.Distributed.Node.Name = "edge-1"
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
	probeCannotMutate, err := http.NewRequest(http.MethodPost, edgeHTTP.URL+cluster.SyncApplyPath, bytes.NewBufferString("{}"))
	if err != nil {
		t.Fatal(err)
	}
	probeCannotMutate.Header.Set("Authorization", "Bearer "+edgeConfig.Distributed.Token)
	probeCannotMutateResponse, err := http.DefaultClient.Do(probeCannotMutate)
	if err != nil {
		t.Fatal(err)
	}
	_ = probeCannotMutateResponse.Body.Close()
	if probeCannotMutateResponse.StatusCode != http.StatusUnauthorized {
		t.Fatalf("read-only probe credential mutated Edge state: status=%d", probeCannotMutateResponse.StatusCode)
	}
	mutationCannotProbe, err := http.NewRequest(http.MethodGet, edgeHTTP.URL+"/api/v1/cluster/manifest", nil)
	if err != nil {
		t.Fatal(err)
	}
	mutationCannotProbe.Header.Set("X-MirrorRelay-Cluster-Token", edgeConfig.Distributed.MutationToken)
	mutationCannotProbeResponse, err := http.DefaultClient.Do(mutationCannotProbe)
	if err != nil {
		t.Fatal(err)
	}
	_ = mutationCannotProbeResponse.Body.Close()
	if mutationCannotProbeResponse.StatusCode != http.StatusUnauthorized {
		t.Fatalf("Edge mutation credential was accepted as a probe credential: status=%d", mutationCannotProbeResponse.StatusCode)
	}
	validProbe, err := http.NewRequest(http.MethodGet, edgeHTTP.URL+"/api/v1/cluster/manifest", nil)
	if err != nil {
		t.Fatal(err)
	}
	validProbe.Header.Set("X-MirrorRelay-Cluster-Token", edgeConfig.Distributed.Token)
	validProbeResponse, err := http.DefaultClient.Do(validProbe)
	if err != nil {
		t.Fatal(err)
	}
	_ = validProbeResponse.Body.Close()
	if validProbeResponse.StatusCode != http.StatusOK {
		t.Fatalf("valid probe credential was rejected: status=%d", validProbeResponse.StatusCode)
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
	coordinatorConfig.Distributed.Node.Name = "coordinator-1"
	coordinatorConfig.Distributed.AllowHTTP = true
	coordinatorConfig.Security.AllowPrivateUpstream = true
	coordinatorConfig.Security.AdminCIDRs = []string{"127.0.0.1/32"}
	coordinatorConfig.UpstreamNginx.Mode = "disabled"
	node, err := coordinatorStore.CreateClusterNode(ctx, model.ClusterNode{
		Name: "edge", URL: edgeHTTP.URL, MutationToken: edgeConfig.Distributed.MutationToken,
		Region: "test", Enabled: true, ProtocolVersion: cluster.ClusterProtocolVersion,
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
	coordinatorEpoch, err := cluster.EnsureCoordinatorEpoch(ctx, coordinatorStore)
	if err != nil {
		t.Fatal(err)
	}
	manifest := cluster.GenerateManifest(coordinatorConfig, []model.Mirror{repository}, []model.CustomConfig{},
		buildinfo.Info{Version: "test-version"}, 8, coordinatorConfig.Distributed.Node.Name, coordinatorEpoch)
	payload := model.ClusterSyncRequest{
		Manifest: manifest, Repositories: []model.Mirror{repository}, CustomConfigs: []model.CustomConfig{},
	}
	manager := cluster.NewSyncManager(coordinatorConfig, coordinatorStore, coordinatorEpoch)
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
	stale := payload
	stale.Manifest.ConfigGeneration = 7
	if staleResult := manager.SyncNode(ctx, node, stale); staleResult.Success || !strings.Contains(staleResult.Error, "HTTP 409") {
		t.Fatalf("Edge accepted a stale configuration generation: %+v", staleResult)
	}
	if idempotent := manager.SyncNode(ctx, node, payload); !idempotent.Success {
		t.Fatalf("Edge rejected an idempotent configuration replay: %+v", idempotent)
	}
	conflictingRepository := repository
	conflictingRepository.Description = "different effective configuration"
	conflicting := model.ClusterSyncRequest{
		Repositories: []model.Mirror{conflictingRepository}, CustomConfigs: []model.CustomConfig{},
	}
	conflicting.Manifest = cluster.GenerateManifest(coordinatorConfig, conflicting.Repositories, conflicting.CustomConfigs,
		buildinfo.Info{Version: "test-version"}, 8, coordinatorConfig.Distributed.Node.Name, coordinatorEpoch)
	if conflictResult := manager.SyncNode(ctx, node, conflicting); conflictResult.Success || !strings.Contains(conflictResult.Error, "HTTP 409") {
		t.Fatalf("Edge accepted conflicting content for an existing generation: %+v", conflictResult)
	}
	acceptedState, foundState, err := cluster.LoadEdgeSyncState(ctx, edgeStore)
	if err != nil || !foundState || acceptedState.Status != "applied" || acceptedState.ConfigGeneration != 8 ||
		acceptedState.ConfigFingerprint != payload.Manifest.ConfigFingerprint {
		t.Fatalf("Edge did not persist its accepted generation for restart recovery: state=%+v found=%v err=%v", acceptedState, foundState, err)
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
	listNodesRequest := httptest.NewRequest(http.MethodGet, "https://coordinator.example/admin/api/v1/cluster/nodes", nil)
	listNodesRequest.RemoteAddr = "127.0.0.1:12345"
	listNodesRequest.AddCookie(&http.Cookie{Name: "mirrorrelay_session", Value: session.ID})
	listNodesRecorder := httptest.NewRecorder()
	coordinatorHandler.ServeHTTP(listNodesRecorder, listNodesRequest)
	if listNodesRecorder.Code != http.StatusOK || strings.Contains(listNodesRecorder.Body.String(), edgeConfig.Distributed.MutationToken) ||
		!strings.Contains(listNodesRecorder.Body.String(), `"mutation_token_configured":true`) {
		t.Fatalf("cluster node API did not redact its mutation credential: status=%d body=%s", listNodesRecorder.Code, listNodesRecorder.Body.String())
	}
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

	probeCredentialUpdate := httptest.NewRequest(http.MethodPut, fmt.Sprintf("https://coordinator.example/admin/api/v1/cluster/nodes/%d", node.ID),
		strings.NewReader(fmt.Sprintf(`{"mutation_token":%q}`, coordinatorConfig.Distributed.Token)))
	probeCredentialUpdate.RemoteAddr = "127.0.0.1:12345"
	probeCredentialUpdate.Header.Set("X-CSRF-Token", session.CSRFToken)
	probeCredentialUpdate.AddCookie(&http.Cookie{Name: "mirrorrelay_session", Value: session.ID})
	probeCredentialRecorder := httptest.NewRecorder()
	coordinatorHandler.ServeHTTP(probeCredentialRecorder, probeCredentialUpdate)
	if probeCredentialRecorder.Code != http.StatusBadRequest {
		t.Fatalf("cluster node accepted the probe credential for mutations: status=%d body=%s", probeCredentialRecorder.Code, probeCredentialRecorder.Body.String())
	}
	secondNode, err := coordinatorStore.CreateClusterNode(ctx, model.ClusterNode{
		Name: "second-edge", URL: "http://127.0.0.1:1", MutationToken: "second-edge-mutation-secret", Region: "test", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	duplicateCredentialUpdate := httptest.NewRequest(http.MethodPut, fmt.Sprintf("https://coordinator.example/admin/api/v1/cluster/nodes/%d", node.ID),
		strings.NewReader(fmt.Sprintf(`{"mutation_token":%q}`, secondNode.MutationToken)))
	duplicateCredentialUpdate.RemoteAddr = "127.0.0.1:12345"
	duplicateCredentialUpdate.Header.Set("X-CSRF-Token", session.CSRFToken)
	duplicateCredentialUpdate.AddCookie(&http.Cookie{Name: "mirrorrelay_session", Value: session.ID})
	duplicateCredentialRecorder := httptest.NewRecorder()
	coordinatorHandler.ServeHTTP(duplicateCredentialRecorder, duplicateCredentialUpdate)
	if duplicateCredentialRecorder.Code != http.StatusBadRequest {
		t.Fatalf("two Edge nodes accepted one mutation credential: status=%d body=%s", duplicateCredentialRecorder.Code, duplicateCredentialRecorder.Body.String())
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

	replacementEpoch := strings.Repeat("2", 32)
	replacementPayload := payload
	replacementPayload.Manifest = cluster.GenerateManifest(coordinatorConfig, replacementPayload.Repositories,
		replacementPayload.CustomConfigs, buildinfo.Info{Version: "test-version"}, 1,
		coordinatorConfig.Distributed.Node.Name, replacementEpoch)
	replacementManager := cluster.NewSyncManager(coordinatorConfig, coordinatorStore, replacementEpoch)
	if replacement := replacementManager.SyncNode(ctx, node, replacementPayload); !replacement.Success {
		t.Fatalf("Edge rejected an authenticated Coordinator epoch transition: %+v", replacement)
	}
	retiredEpochReplay := payload
	retiredEpochReplay.Manifest.ConfigGeneration = 9
	if replay := manager.SyncNode(ctx, node, retiredEpochReplay); replay.Success || !strings.Contains(replay.Error, "HTTP 409") {
		t.Fatalf("Edge accepted a retired Coordinator epoch replay: %+v", replay)
	}
}
