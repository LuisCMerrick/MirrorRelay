package database

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/LuisCMerrick/RepoGate/internal/model"
	"github.com/LuisCMerrick/RepoGate/internal/stats"
)

func TestRepositoryRoundTripAndConfigHistory(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "repogate.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	repository := model.Mirror{Name: "Docker Hub", Slug: "docker", Type: "docker-registry", Enabled: true,
		PublicMode: "host", PublicHost: "docker.example.com", PublicPath: "/", ProxyMode: "registry",
		CacheEnabled: true, CacheProfile: "registry", HealthCheckEnabled: true, HealthMethod: "HEAD", HealthExpected: 200,
		ProfileName: "Docker Hub", ProfileVersion: "1.0.0", AuthMode: "full_proxy", BlobRedirectMode: "full_proxy",
		PullOnly: true, ConfigState: "pending", HTMLRewriteEnabled: true, RewriteHosts: []string{"auth.docker.io", "production.cloudfront.docker.com"},
		HeaderAdd: map[string]string{"X-Repository-Client": "RepoGate"}, HeaderRemove: []string{"X-Legacy"},
		ConnectTimeoutSec: 7, ReadTimeoutSec: 7200, SendTimeoutSec: 7100, MetadataLimitBytes: 64 << 20,
		MetadataTTLSec: 60, PackageTTLSec: 7200, ImmutableTTLSec: 86400, BlobTTLSec: 172800, CacheAuthenticated: true,
		Upstreams: []model.Upstream{{URL: "https://registry-1.docker.io/", Host: "registry-1.docker.io", Priority: 10, Weight: 1, Enabled: true}}}
	created, err := store.CreateMirror(ctx, repository)
	if err != nil {
		t.Fatal(err)
	}
	if created.Type != repository.Type || created.PublicHost != repository.PublicHost || len(created.Upstreams) != 1 || created.Upstreams[0].Host != "registry-1.docker.io" ||
		created.HeaderAdd["X-Repository-Client"] != "RepoGate" || len(created.HeaderRemove) != 1 || created.MetadataLimitBytes != 64<<20 ||
		created.ConnectTimeoutSec != 7 || created.BlobTTLSec != 172800 || !created.CacheAuthenticated || !created.HTMLRewriteEnabled || len(created.RewriteHosts) != 2 {
		t.Fatalf("repository round trip mismatch: %+v", created)
	}
	version, err := store.AddConfigVersion(ctx, model.ConfigVersion{Operator: "test", Description: "create", ConfigurationHash: "abc", ValidationOK: true, Active: true, Snapshot: "[]", Configuration: "events {}"}, 20)
	if err != nil {
		t.Fatal(err)
	}
	if version.Version != 1 || !version.Active {
		t.Fatalf("unexpected version: %+v", version)
	}
	if err := store.ReplaceMirrors(ctx, []model.Mirror{created}); err != nil {
		t.Fatal(err)
	}
	values, err := store.ListMirrors(ctx)
	if err != nil || len(values) != 1 || values[0].ID != created.ID || !values[0].HTMLRewriteEnabled {
		t.Fatalf("replace result: values=%+v err=%v", values, err)
	}
}

func TestListMirrorsReturnsEmptySliceForEmptyStore(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "repogate.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	values, err := store.ListMirrors(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if values == nil || len(values) != 0 {
		t.Fatalf("empty repository list = %#v, want non-nil empty slice", values)
	}
}

func TestSettingsRoundTripAndDelete(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "repogate.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	if _, found, err := store.Setting(ctx, "web"); err != nil || found {
		t.Fatalf("unexpected initial setting: found=%v err=%v", found, err)
	}
	if err := store.PutSetting(ctx, "web", `{"version":1}`); err != nil {
		t.Fatal(err)
	}
	value, found, err := store.Setting(ctx, "web")
	if err != nil || !found || value != `{"version":1}` {
		t.Fatalf("setting round trip: value=%q found=%v err=%v", value, found, err)
	}
	if err := store.PutSetting(ctx, "web", `{"version":2}`); err != nil {
		t.Fatal(err)
	}
	value, found, err = store.Setting(ctx, "web")
	if err != nil || !found || value != `{"version":2}` {
		t.Fatalf("setting update: value=%q found=%v err=%v", value, found, err)
	}
	if err := store.DeleteSetting(ctx, "web"); err != nil {
		t.Fatal(err)
	}
	if _, found, err := store.Setting(ctx, "web"); err != nil || found {
		t.Fatalf("setting survived delete: found=%v err=%v", found, err)
	}
}

func TestHourlyStatisticsRoundTrip(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "repogate.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	hour := time.Now().Format("2006-01-02T15")
	want := stats.PersistentRecord{Hour: hour, MirrorID: 9, Counters: stats.MirrorCounters{
		Requests: 8, Bytes: 1024, UpstreamBytes: 768, CacheBytes: 256, CacheHits: 2, CacheMisses: 4,
		UpstreamErrors: 1, Status2xx: 5, Status3xx: 1, Status4xx: 1, Status5xx: 1,
	}}
	if err := store.SaveStatsHourly(context.Background(), []stats.PersistentRecord{want}, "1970-01-01T00"); err != nil {
		t.Fatal(err)
	}
	got, err := store.LoadStatsHourly(context.Background(), hour)
	if err != nil || len(got) != 1 || got[0] != want {
		t.Fatalf("statistics round trip: got=%+v want=%+v err=%v", got, want, err)
	}
}

func TestSessionPersistenceAndUserCascade(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "repogate.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	if err := store.CreateUser(ctx, "operator", "hash"); err != nil {
		t.Fatal(err)
	}
	user, err := store.UserByName(ctx, "operator")
	if err != nil {
		t.Fatal(err)
	}
	expires := time.Now().Add(time.Hour).Truncate(time.Second)
	if err := store.PutSession(ctx, "session-hash", user.ID, user.Username, "csrf", expires); err != nil {
		t.Fatal(err)
	}
	userID, username, csrf, actualExpiry, err := store.GetSession(ctx, "session-hash")
	if err != nil || userID != user.ID || username != user.Username || csrf != "csrf" || !actualExpiry.Equal(expires) {
		t.Fatalf("session round trip mismatch: id=%d username=%q csrf=%q expires=%s err=%v", userID, username, csrf, actualExpiry, err)
	}
	if err := store.DeleteUser(ctx, user.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, _, _, err := store.GetSession(ctx, "session-hash"); err == nil {
		t.Fatal("session survived user deletion")
	}
}

func TestReplaceConfigurationAndSessionRevoke(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "repogate.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()

	// Test user sessions and revocation
	if err := store.CreateUser(ctx, "admin2", "hash2"); err != nil {
		t.Fatal(err)
	}
	u, err := store.UserByName(ctx, "admin2")
	if err != nil {
		t.Fatal(err)
	}
	exp := time.Now().Add(time.Hour)
	_ = store.PutSession(ctx, "s1", u.ID, u.Username, "c1", exp)
	_ = store.PutSession(ctx, "s2", u.ID, u.Username, "c2", exp)
	_ = store.PutSession(ctx, "s3", u.ID, u.Username, "c3", exp)

	// Delete all sessions for u.ID except s2
	if err := store.DeleteUserSessions(ctx, u.ID, "s2"); err != nil {
		t.Fatal(err)
	}
	if _, _, _, _, err := store.GetSession(ctx, "s1"); err == nil {
		t.Fatal("s1 should be deleted")
	}
	if _, _, _, _, err := store.GetSession(ctx, "s2"); err != nil {
		t.Fatal("s2 should survive")
	}
	if _, _, _, _, err := store.GetSession(ctx, "s3"); err == nil {
		t.Fatal("s3 should be deleted")
	}

	// Test ReplaceConfiguration in single transaction
	mirrors := []model.Mirror{
		{ID: 10, Name: "M1", Slug: "m1", Type: "generic", Enabled: true, Upstreams: []model.Upstream{{URL: "https://upstream.test"}}},
	}
	customs := []model.CustomConfig{
		{ID: 20, Name: "c1", Context: "http", Enabled: true, Content: "# custom"},
	}
	if err := store.ReplaceConfiguration(ctx, mirrors, customs); err != nil {
		t.Fatal(err)
	}
	listM, err := store.ListMirrors(ctx)
	if err != nil || len(listM) != 1 || listM[0].Slug != "m1" {
		t.Fatalf("mirrors not restored properly: %+v", listM)
	}
	listC, err := store.ListCustomConfigs(ctx)
	if err != nil || len(listC) != 1 || listC[0].Name != "c1" {
		t.Fatalf("custom configs not restored properly: %+v", listC)
	}
}

func TestClusterNodeAndSettingStore(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "repogate.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()

	// 1. Test Cluster Setting
	if err := store.PutClusterSetting(ctx, "cluster_fingerprint", "sha256:abcd"); err != nil {
		t.Fatal(err)
	}
	val, ok, err := store.ClusterSetting(ctx, "cluster_fingerprint")
	if err != nil || !ok || val != "sha256:abcd" {
		t.Fatalf("cluster setting mismatch: val=%s, ok=%v, err=%v", val, ok, err)
	}

	// 2. Create Cluster Nodes
	node1 := model.ClusterNode{
		Name:         "tokyo-01",
		URL:          "https://jp.repo.example.com",
		Region:       "jp-tokyo",
		Country:      "JP",
		Priority:     100,
		Weight:       80,
		Enabled:      true,
		Capabilities: []string{"apt", "rpm", "pypi"},
	}
	created1, err := store.CreateClusterNode(ctx, node1)
	if err != nil {
		t.Fatal(err)
	}
	if created1.ID == 0 || created1.Name != "tokyo-01" || created1.Priority != 100 {
		t.Fatalf("unexpected created node: %+v", created1)
	}

	node2 := model.ClusterNode{
		Name:         "sg-01",
		URL:          "https://sg.repo.example.com",
		Region:       "sg",
		Country:      "SG",
		Priority:     200,
		Weight:       100,
		Enabled:      true,
		Capabilities: []string{"apt", "rpm"},
	}
	created2, err := store.CreateClusterNode(ctx, node2)
	if err != nil {
		t.Fatal(err)
	}

	// 3. List
	nodes, err := store.ListClusterNodes(ctx)
	if err != nil || len(nodes) != 2 {
		t.Fatalf("list nodes failed: nodes=%+v, err=%v", nodes, err)
	}

	// 4. Update status
	now := time.Now()
	err = store.UpdateClusterNodeStatus(ctx, created1.ID, "healthy", "match", "sha256:abcd", "0.0.1", 1, []string{"apt", "rpm", "pypi"}, "", now)
	if err != nil {
		t.Fatal(err)
	}

	got1, err := store.GetClusterNode(ctx, created1.ID)
	if err != nil || got1.HealthStatus != "healthy" || got1.ConfigStatus != "match" || got1.ConfigFingerprint != "sha256:abcd" {
		t.Fatalf("unexpected node after status update: %+v", got1)
	}

	// 5. Get by URL
	gotByUrl, err := store.GetClusterNodeByURL(ctx, "https://jp.repo.example.com")
	if err != nil || gotByUrl.ID != created1.ID {
		t.Fatalf("get by url failed: %+v", gotByUrl)
	}

	// 6. Disable & Delete
	if err := store.SetClusterNodeEnabled(ctx, created2.ID, false); err != nil {
		t.Fatal(err)
	}
	got2, err := store.GetClusterNode(ctx, created2.ID)
	if err != nil || got2.Enabled {
		t.Fatalf("node2 should be disabled: %+v", got2)
	}

	if err := store.DeleteClusterNode(ctx, created2.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetClusterNode(ctx, created2.ID); err == nil {
		t.Fatal("node2 should be deleted")
	}
}
