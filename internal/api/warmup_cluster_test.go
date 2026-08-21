package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/LuisCMerrick/MirrorRelay/internal/buildinfo"
	"github.com/LuisCMerrick/MirrorRelay/internal/config"
	"github.com/LuisCMerrick/MirrorRelay/internal/database"
	"github.com/LuisCMerrick/MirrorRelay/internal/mirror"
	"github.com/LuisCMerrick/MirrorRelay/internal/model"
	"github.com/LuisCMerrick/MirrorRelay/internal/upstreamnginx"
)

func TestWarmupAPI_CRUD(t *testing.T) {
	dbPath := t.TempDir() + "/test.db"
	store, err := database.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer store.Close()

	// Seed user and mirror
	_ = store.CreateUser(context.Background(), "admin", "adminadmin", "admin")
	user, _ := store.UserByName(context.Background(), "admin")
	mir, _ := store.CreateMirror(context.Background(), model.Mirror{
		Name:       "Test Debian",
		Slug:       "debian",
		Type:       "apt",
		PublicPath: "/debian",
	})

	cfg, _ := config.Load("", true)
	cfg.Security.AdminCIDRs = []string{"127.0.0.1/32"}
	registry := mirror.NewRegistry(store)

	srv, err := New(cfg, cfg, store, registry, nil, nil, nil, nil, nil, buildinfo.Info{})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}

	session, err := srv.sessions.Create(user.ID, user.Username, user.Role)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	handler := srv.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	// 1. Create Warmup Job
	jobPayload := map[string]any{
		"mirror_id":       mir.ID,
		"name":            "Debian Core Warmup",
		"cron_expression": "@hourly",
		"url_patterns":    []string{"/debian/dists/bookworm/Release"},
		"enabled":         true,
	}
	body, _ := json.Marshal(jobPayload)
	req := httptest.NewRequest("POST", "/admin/api/v1/warmup/jobs", bytes.NewReader(body))
	req.Header.Set("X-CSRF-Token", session.CSRFToken)
	req.AddCookie(&http.Cookie{Name: "mirrorrelay_session", Value: session.ID})
	req.RemoteAddr = "127.0.0.1:12345"

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created, got %d: %s", w.Code, w.Body.String())
	}

	var created model.WarmupJob
	_ = json.NewDecoder(w.Body).Decode(&created)
	if created.ID <= 0 || created.Name != "Debian Core Warmup" || created.NextRunAt == "" {
		t.Fatalf("unexpected created job: %+v", created)
	}

	invalidPayload := map[string]any{
		"mirror_id": mir.ID, "name": "Invalid schedule", "cron_expression": "not a cron",
		"url_patterns": []string{"/debian/dists/bookworm/Release"}, "enabled": true,
	}
	invalidBody, _ := json.Marshal(invalidPayload)
	invalidRequest := httptest.NewRequest(http.MethodPost, "/admin/api/v1/warmup/jobs", bytes.NewReader(invalidBody))
	invalidRequest.Header.Set("X-CSRF-Token", session.CSRFToken)
	invalidRequest.AddCookie(&http.Cookie{Name: "mirrorrelay_session", Value: session.ID})
	invalidRequest.RemoteAddr = "127.0.0.1:12345"
	invalidRecorder := httptest.NewRecorder()
	handler.ServeHTTP(invalidRecorder, invalidRequest)
	if invalidRecorder.Code != http.StatusBadRequest {
		t.Fatalf("invalid cron expression was accepted: status=%d body=%s", invalidRecorder.Code, invalidRecorder.Body.String())
	}

	// 2. List Warmup Jobs
	reqList := httptest.NewRequest("GET", "/admin/api/v1/warmup/jobs", nil)
	reqList.AddCookie(&http.Cookie{Name: "mirrorrelay_session", Value: session.ID})
	reqList.RemoteAddr = "127.0.0.1:12345"

	wList := httptest.NewRecorder()
	handler.ServeHTTP(wList, reqList)
	if wList.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", wList.Code)
	}

	var jobs []model.WarmupJob
	_ = json.NewDecoder(wList.Body).Decode(&jobs)
	if len(jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(jobs))
	}

	// 3. Warmup Status
	reqStatus := httptest.NewRequest("GET", "/admin/api/v1/warmup/status", nil)
	reqStatus.AddCookie(&http.Cookie{Name: "mirrorrelay_session", Value: session.ID})
	reqStatus.RemoteAddr = "127.0.0.1:12345"

	wStatus := httptest.NewRecorder()
	handler.ServeHTTP(wStatus, reqStatus)
	if wStatus.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", wStatus.Code)
	}
}

func TestClusterSyncAPI(t *testing.T) {
	dbPath := t.TempDir() + "/cluster_test.db"
	store, err := database.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer store.Close()

	_ = store.CreateUser(context.Background(), "admin", "adminadmin", "admin")
	user, _ := store.UserByName(context.Background(), "admin")

	cfg, _ := config.Load("", true)
	cfg.Distributed.Enabled = true
	cfg.Distributed.Role = "coordinator"
	cfg.Distributed.Token = "secret-token"
	cfg.Security.AdminCIDRs = []string{"127.0.0.1/32"}
	cfg.UpstreamNginx.Mode = "disabled"
	registry := mirror.NewRegistry(store)
	controller := upstreamnginx.NewController(cfg, store)
	if err := controller.Start(context.Background()); err != nil {
		t.Fatalf("start disabled controller: %v", err)
	}

	srv, err := New(cfg, cfg, store, registry, nil, nil, nil, controller, nil, buildinfo.Info{})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}

	session, err := srv.sessions.Create(user.ID, user.Username, user.Role)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	handler := srv.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	// Test Manifest Public API with Token
	reqManifest := httptest.NewRequest("GET", "/api/v1/cluster/manifest", nil)
	reqManifest.Header.Set("Authorization", "Bearer secret-token")
	wManifest := httptest.NewRecorder()
	handler.ServeHTTP(wManifest, reqManifest)

	if wManifest.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for cluster manifest, got %d: %s", wManifest.Code, wManifest.Body.String())
	}

	// Test Cluster Broadcast Sync API
	reqSync := httptest.NewRequest("POST", "/admin/api/v1/cluster/sync", nil)
	reqSync.Header.Set("X-CSRF-Token", session.CSRFToken)
	reqSync.AddCookie(&http.Cookie{Name: "mirrorrelay_session", Value: session.ID})
	reqSync.RemoteAddr = "127.0.0.1:12345"

	wSync := httptest.NewRecorder()
	handler.ServeHTTP(wSync, reqSync)
	if wSync.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for cluster sync, got %d: %s", wSync.Code, wSync.Body.String())
	}
	entries, err := store.ListAudit(context.Background(), 20)
	if err != nil {
		t.Fatal(err)
	}
	foundFailedAggregate := false
	for _, entry := range entries {
		if entry.Action == "broadcast_cluster_sync" && !entry.Succeeded && strings.Contains(entry.Detail, "succeeded=0 failed=0") {
			foundFailedAggregate = true
			break
		}
	}
	if !foundFailedAggregate {
		t.Fatalf("zero-target cluster sync was audited as successful or duplicated: %+v", entries)
	}
}
