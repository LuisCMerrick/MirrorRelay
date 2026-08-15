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
