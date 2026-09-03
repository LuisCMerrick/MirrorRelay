package database

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LuisCMerrick/MirrorRelay/internal/model"
	"github.com/LuisCMerrick/MirrorRelay/internal/stats"
)

func TestOpenMigratesPrePasskeyDatabaseWithoutDataLoss(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "mirrorrelay.db")
	legacy, err := sql.Open("sqlite", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	const legacySchema = `
CREATE TABLE users (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 username TEXT NOT NULL UNIQUE COLLATE NOCASE,
 password_hash TEXT NOT NULL,
 role TEXT NOT NULL DEFAULT 'admin',
 created_at TEXT NOT NULL,
 updated_at TEXT NOT NULL
);
CREATE TABLE settings (
 key TEXT PRIMARY KEY,
 value TEXT NOT NULL,
 updated_at TEXT NOT NULL
);`
	if _, err := legacy.Exec(legacySchema); err != nil {
		legacy.Close()
		t.Fatal(err)
	}
	if _, err := legacy.Exec(`INSERT INTO users(username,password_hash,role,created_at,updated_at) VALUES('admin','legacy-hash','admin','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`); err != nil {
		legacy.Close()
		t.Fatal(err)
	}
	if _, err := legacy.Exec(`INSERT INTO settings(key,value,updated_at) VALUES('legacy-setting','preserved','2026-01-01T00:00:00Z')`); err != nil {
		legacy.Close()
		t.Fatal(err)
	}
	if err := legacy.Close(); err != nil {
		t.Fatal(err)
	}

	store, err := Open(databasePath)
	if err != nil {
		t.Fatalf("open and migrate legacy database: %v", err)
	}
	defer store.Close()
	user, err := store.UserByName(t.Context(), "admin")
	if err != nil || user.PasswordHash != "legacy-hash" || user.PasswordLoginDisabled {
		t.Fatalf("legacy user after migration: user=%+v err=%v", user, err)
	}
	if value, found, err := store.Setting(t.Context(), "legacy-setting"); err != nil || !found || value != "preserved" {
		t.Fatalf("legacy setting after migration: value=%q found=%v err=%v", value, found, err)
	}
	if err := store.CreatePasskey(t.Context(), model.PasskeyCredential{UserID: user.ID, CredentialID: "migrated-credential", PublicKey: "key"}); err != nil {
		t.Fatalf("new passkey table is unavailable after migration: %v", err)
	}
}

func TestCreateInitialAdminIsAtomic(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "mirrorrelay.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	var created atomic.Int32
	errCh := make(chan error, 8)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			user, ok, createErr := store.CreateInitialAdmin(context.Background(), "first-admin", "hash")
			if createErr != nil {
				errCh <- createErr
				return
			}
			if ok {
				if user.Username != "first-admin" || user.Role != "admin" {
					errCh <- fmt.Errorf("unexpected initial administrator: %+v", user)
					return
				}
				created.Add(1)
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for createErr := range errCh {
		t.Fatal(createErr)
	}
	count, err := store.CountUsers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if created.Load() != 1 || count != 1 {
		t.Fatalf("initial administrators created=%d users=%d, want 1/1", created.Load(), count)
	}
}

func TestDisablePasswordLoginRequiresPasskeyAndUnusedRecoveryCode(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "mirrorrelay.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ctx := t.Context()
	user, created, err := store.CreateInitialAdmin(ctx, "admin", "hash")
	if err != nil || !created {
		t.Fatalf("create administrator: created=%v err=%v", created, err)
	}
	if updated, err := store.DisablePasswordLogin(ctx, user.ID); err != nil || updated {
		t.Fatalf("disable without recovery methods: updated=%v err=%v", updated, err)
	}
	if err := store.CreatePasskey(ctx, model.PasskeyCredential{
		UserID: user.ID, CredentialID: "credential", PublicKey: "key", DisplayName: "test",
	}); err != nil {
		t.Fatal(err)
	}
	if updated, err := store.DisablePasswordLogin(ctx, user.ID); err != nil || updated {
		t.Fatalf("disable without recovery code: updated=%v err=%v", updated, err)
	}
	if err := store.SaveRecoveryCodes(ctx, user.ID, []string{"unused"}); err != nil {
		t.Fatal(err)
	}
	if updated, err := store.DisablePasswordLogin(ctx, user.ID); err != nil || !updated {
		t.Fatalf("disable with passkey and recovery code: updated=%v err=%v", updated, err)
	}
	loaded, err := store.UserByName(ctx, user.Username)
	if err != nil || !loaded.PasswordLoginDisabled {
		t.Fatalf("password-login state: disabled=%v err=%v", loaded.PasswordLoginDisabled, err)
	}
	passkeys, err := store.ListPasskeysByUserID(ctx, user.ID)
	if err != nil || len(passkeys) != 1 {
		t.Fatalf("list passkeys: count=%d err=%v", len(passkeys), err)
	}
	if err := store.DeletePasskey(ctx, passkeys[0].ID, user.ID); err == nil {
		t.Fatal("deleted the final passkey while password login was disabled")
	}
	if err := store.CreatePasskey(ctx, model.PasskeyCredential{
		UserID: user.ID, CredentialID: "credential-2", PublicKey: "key", DisplayName: "second",
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.DeletePasskey(ctx, passkeys[0].ID, user.ID); err != nil {
		t.Fatalf("delete one of two passkeys: %v", err)
	}
	passkeys, err = store.ListPasskeysByUserID(ctx, user.ID)
	if err != nil || len(passkeys) != 1 {
		t.Fatalf("passkey count after safe deletion: count=%d err=%v", len(passkeys), err)
	}
	if err := store.DeletePasskey(ctx, passkeys[0].ID, user.ID); err == nil {
		t.Fatal("deleted the remaining passkey after a safe deletion")
	}

	if err := store.SetPasswordLoginDisabled(ctx, user.ID, false); err != nil {
		t.Fatal(err)
	}
	if _, err := store.VerifyAndUseRecoveryCode(ctx, user.ID, "unused"); err != nil {
		t.Fatal(err)
	}
	if updated, err := store.DisablePasswordLogin(ctx, user.ID); err != nil || updated {
		t.Fatalf("disable with only a used recovery code: updated=%v err=%v", updated, err)
	}
}

func TestEmergencyRecoveryMutationsRevokeSessions(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "mirrorrelay.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := t.Context()
	user, created, err := store.CreateInitialAdmin(ctx, "admin", "old-hash")
	if err != nil || !created {
		t.Fatalf("create administrator: created=%v err=%v", created, err)
	}
	if err := store.CreatePasskey(ctx, model.PasskeyCredential{UserID: user.ID, CredentialID: "credential", PublicKey: "key"}); err != nil {
		t.Fatal(err)
	}
	if err := store.PutSession(ctx, "passkey-reset-session", user.ID, user.Username, user.Role, "csrf", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := store.ResetPasskeysAndEnablePassword(ctx, user.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, _, _, _, err := store.GetSession(ctx, "passkey-reset-session"); err == nil {
		t.Fatal("passkey recovery left an existing session active")
	}
	if count, err := store.CountPasskeysByUserID(ctx, user.ID); err != nil || count != 0 {
		t.Fatalf("passkeys after recovery: count=%d err=%v", count, err)
	}

	if err := store.PutSession(ctx, "password-reset-session", user.ID, user.Username, user.Role, "csrf", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := store.ResetPasswordAndSessions(ctx, user.ID, "new-hash"); err != nil {
		t.Fatal(err)
	}
	if _, _, _, _, _, err := store.GetSession(ctx, "password-reset-session"); err == nil {
		t.Fatal("password recovery left an existing session active")
	}
	updated, err := store.UserByName(ctx, user.Username)
	if err != nil || updated.PasswordHash != "new-hash" || updated.PasswordLoginDisabled {
		t.Fatalf("password recovery state: user=%+v err=%v", updated, err)
	}
}

func TestAuxiliaryURLSigningKeyPersists(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "mirrorrelay.db")
	store, err := Open(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	first, err := store.AuxiliaryURLSigningKey(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	second, err := reopened.AuxiliaryURLSigningKey(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 32 || !bytes.Equal(first, second) {
		t.Fatal("auxiliary URL signing key changed across database reopen")
	}
}

func TestRepositoryRoundTripAndConfigHistory(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "mirrorrelay.db"))
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
		HeaderAdd: map[string]string{"X-Repository-Client": "MirrorRelay"}, HeaderRemove: []string{"X-Legacy"},
		ConnectTimeoutSec: 7, ReadTimeoutSec: 7200, SendTimeoutSec: 7100, MetadataLimitBytes: 64 << 20,
		MetadataTTLSec: 60, PackageTTLSec: 7200, ImmutableTTLSec: 86400, BlobTTLSec: 172800, CacheAuthenticated: true,
		Upstreams: []model.Upstream{{URL: "https://registry-1.docker.io/", Host: "registry-1.docker.io", Priority: 10, Weight: 1, Enabled: true}}}
	created, err := store.CreateMirror(ctx, repository)
	if err != nil {
		t.Fatal(err)
	}
	if created.Type != repository.Type || created.PublicHost != repository.PublicHost || len(created.Upstreams) != 1 || created.Upstreams[0].Host != "registry-1.docker.io" ||
		created.HeaderAdd["X-Repository-Client"] != "MirrorRelay" || len(created.HeaderRemove) != 1 || created.MetadataLimitBytes != 64<<20 ||
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

func TestCorruptRepositoryPolicyJSONFailsClosed(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "mirrorrelay.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	repository, err := store.CreateMirror(ctx, model.Mirror{
		Name: "Policy", Slug: "policy", Type: "generic",
		BlockedPackages: []string{"blocked-*"},
		Upstreams:       []model.Upstream{{URL: "https://packages.example/", Enabled: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, `UPDATE mirrors SET blocked_packages='{' WHERE id=?`, repository.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Mirror(ctx, repository.ID); err == nil || !strings.Contains(err.Error(), "blocked_packages") {
		t.Fatalf("corrupt package policy did not fail closed: %v", err)
	}
}

func TestListMirrorsReturnsEmptySliceForEmptyStore(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "mirrorrelay.db"))
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
	store, err := Open(filepath.Join(t.TempDir(), "mirrorrelay.db"))
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

func TestSettingAndHistoryMutationIsAtomic(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "mirrorrelay.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()

	version := model.SettingVersion{Version: 1, Operator: "admin", Source: "test", Settings: `{}`}
	if _, err := store.PutSettingWithVersion(ctx, "web", `{"version":1}`, version, 10); err != nil {
		t.Fatal(err)
	}
	if _, err := store.PutSettingWithVersion(ctx, "web", `{"version":2}`, version, 10); err == nil {
		t.Fatal("duplicate history version unexpectedly succeeded")
	}
	value, found, err := store.Setting(ctx, "web")
	if err != nil || !found || value != `{"version":1}` {
		t.Fatalf("failed history insert changed setting: value=%q found=%v err=%v", value, found, err)
	}

	if _, err := store.DeleteSettingWithVersion(ctx, "web", version, 10); err == nil {
		t.Fatal("delete with duplicate history version unexpectedly succeeded")
	}
	value, found, err = store.Setting(ctx, "web")
	if err != nil || !found || value != `{"version":1}` {
		t.Fatalf("failed history insert deleted setting: value=%q found=%v err=%v", value, found, err)
	}

	pairVersion := model.SettingVersion{Version: 2, Operator: "admin", Source: "test", Settings: `{}`}
	if _, err := store.PutSettingsWithVersion(ctx, map[string]string{
		"web": `{"version":2}`, "appearance": `{"theme":"dark"}`,
	}, pairVersion, 10); err != nil {
		t.Fatal(err)
	}
	if _, err := store.PutSettingsWithVersion(ctx, map[string]string{
		"web": `{"version":3}`, "appearance": `{"theme":"light"}`,
	}, pairVersion, 10); err == nil {
		t.Fatal("duplicate pair history version unexpectedly succeeded")
	}
	for key, want := range map[string]string{"web": `{"version":2}`, "appearance": `{"theme":"dark"}`} {
		value, found, err := store.Setting(ctx, key)
		if err != nil || !found || value != want {
			t.Fatalf("failed pair history insert changed %s: value=%q found=%v err=%v", key, value, found, err)
		}
	}
}

func TestHourlyStatisticsRoundTrip(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "mirrorrelay.db"))
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
	store, err := Open(filepath.Join(t.TempDir(), "mirrorrelay.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	if err := store.CreateUser(ctx, "operator", "hash", "operator"); err != nil {
		t.Fatal(err)
	}
	user, err := store.UserByName(ctx, "operator")
	if err != nil || user.Role != "operator" {
		t.Fatalf("user mismatch: %v, role=%q", err, user.Role)
	}
	expires := time.Now().Add(time.Hour).Truncate(time.Second)
	if err := store.PutSession(ctx, "session-hash", user.ID, user.Username, user.Role, "csrf", expires); err != nil {
		t.Fatal(err)
	}
	userID, username, role, csrf, actualExpiry, err := store.GetSession(ctx, "session-hash")
	if err != nil || userID != user.ID || username != user.Username || role != "operator" || csrf != "csrf" || !actualExpiry.Equal(expires) {
		t.Fatalf("session round trip mismatch: id=%d username=%q role=%q csrf=%q expires=%s err=%v", userID, username, role, csrf, actualExpiry, err)
	}
	if err := store.DeleteUser(ctx, user.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, _, _, _, err := store.GetSession(ctx, "session-hash"); err == nil {
		t.Fatal("session survived user deletion")
	}
}

func TestReplaceConfigurationAndSessionRevoke(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "mirrorrelay.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()

	// Test user sessions and revocation
	if err := store.CreateUser(ctx, "admin2", "hash2", "admin"); err != nil {
		t.Fatal(err)
	}
	u, err := store.UserByName(ctx, "admin2")
	if err != nil {
		t.Fatal(err)
	}
	exp := time.Now().Add(time.Hour)
	_ = store.PutSession(ctx, "s1", u.ID, u.Username, u.Role, "c1", exp)
	_ = store.PutSession(ctx, "s2", u.ID, u.Username, u.Role, "c2", exp)
	_ = store.PutSession(ctx, "s3", u.ID, u.Username, u.Role, "c3", exp)

	// Delete all sessions for u.ID except s2
	if err := store.DeleteUserSessions(ctx, u.ID, "s2"); err != nil {
		t.Fatal(err)
	}
	if _, _, _, _, _, err := store.GetSession(ctx, "s1"); err == nil {
		t.Fatal("s1 should be deleted")
	}
	if _, _, _, _, _, err := store.GetSession(ctx, "s2"); err != nil {
		t.Fatal("s2 should survive")
	}
	if _, _, _, _, _, err := store.GetSession(ctx, "s3"); err == nil {
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

func TestRepositorySnapshotReplacementPreservesWarmupJobsForSurvivingMirrors(t *testing.T) {
	tests := []struct {
		name    string
		replace func(context.Context, *Store, []model.Mirror) error
	}{
		{
			name: "mirrors",
			replace: func(ctx context.Context, store *Store, mirrors []model.Mirror) error {
				return store.ReplaceMirrors(ctx, mirrors)
			},
		},
		{
			name: "complete configuration",
			replace: func(ctx context.Context, store *Store, mirrors []model.Mirror) error {
				return store.ReplaceConfiguration(ctx, mirrors, []model.CustomConfig{})
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store, err := Open(filepath.Join(t.TempDir(), "mirrorrelay.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			ctx := t.Context()
			first, err := store.CreateMirror(ctx, model.Mirror{
				Name: "First", Slug: "first", Type: "generic", Enabled: true,
				Upstreams: []model.Upstream{{URL: "https://first.example", Enabled: true}},
			})
			if err != nil {
				t.Fatal(err)
			}
			second, err := store.CreateMirror(ctx, model.Mirror{
				Name: "Second", Slug: "second", Type: "generic", Enabled: true,
				Upstreams: []model.Upstream{{URL: "https://second.example", Enabled: true}},
			})
			if err != nil {
				t.Fatal(err)
			}
			kept, err := store.CreateWarmupJob(ctx, model.WarmupJob{
				MirrorID: first.ID, Name: "keep", URLPatterns: []string{"/one", "/two"}, Enabled: true,
				Status: "completed", TotalItems: 2, CompletedItems: 2, BytesDownloaded: 1234,
				LastRunAt: "2026-08-29T01:02:03Z", NextRunAt: "2026-08-30T01:02:03Z",
			})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := store.CreateWarmupJob(ctx, model.WarmupJob{
				MirrorID: second.ID, Name: "remove", URLPatterns: []string{"/gone"}, Enabled: true,
			}); err != nil {
				t.Fatal(err)
			}

			first.Name = "First updated"
			if err := test.replace(ctx, store, []model.Mirror{first}); err != nil {
				t.Fatal(err)
			}
			jobs, err := store.ListWarmupJobs(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if len(jobs) != 1 {
				t.Fatalf("warm-up jobs after replacement = %+v, want one surviving job", jobs)
			}
			actual := jobs[0]
			if actual.ID != kept.ID || actual.MirrorID != first.ID || actual.MirrorName != "First updated" ||
				actual.Name != kept.Name || !actual.Enabled || actual.Status != "completed" || actual.TotalItems != 2 ||
				actual.CompletedItems != 2 || actual.BytesDownloaded != 1234 || actual.LastRunAt != kept.LastRunAt ||
				actual.NextRunAt != kept.NextRunAt || !slices.Equal(actual.URLPatterns, kept.URLPatterns) {
				t.Fatalf("surviving warm-up job changed: got %+v want %+v", actual, kept)
			}
		})
	}
}

func TestClusterNodeAndSettingStore(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "mirrorrelay.db"), WithClusterMutationTokenKeys(bytes.Repeat([]byte{0x42}, 32)))
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
		Name:          "tokyo-01",
		URL:           "https://jp.repo.example.com",
		MutationToken: "tokyo-mutation-secret",
		Region:        "jp-tokyo",
		Country:       "JP",
		Priority:      100,
		Weight:        80,
		Enabled:       true,
		Capabilities:  []string{"apt", "rpm", "pypi"},
	}
	created1, err := store.CreateClusterNode(ctx, node1)
	if err != nil {
		t.Fatal(err)
	}
	if created1.ID == 0 || created1.Name != "tokyo-01" || created1.Priority != 100 {
		t.Fatalf("unexpected created node: %+v", created1)
	}
	var plaintextToken, encryptedToken string
	if err := store.db.QueryRowContext(ctx, `SELECT mutation_token,mutation_token_ciphertext FROM cluster_nodes WHERE id=?`, created1.ID).Scan(&plaintextToken, &encryptedToken); err != nil {
		t.Fatal(err)
	}
	if plaintextToken != "" || encryptedToken == "" || strings.Contains(encryptedToken, node1.MutationToken) {
		t.Fatalf("cluster mutation token was not encrypted at rest: plaintext=%q ciphertext=%q", plaintextToken, encryptedToken)
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
	created1.HealthStatus = "degraded"
	created1.ConfigStatus = "match"
	created1.ConfigFingerprint = "sha256:abcd"
	created1.ConfigGeneration = 9
	created1.NodeID = "tokyo-edge"
	created1.CoordinatorID = "coordinator-1"
	created1.CoordinatorEpoch = "epoch-1"
	created1.Version = "0.0.1"
	created1.ProtocolVersion = 2
	created1.RepositoryHealth = map[string]bool{"debian": true, "pypi": false}
	created1.LatencyMS = 37
	created1.LastCheck = now
	err = store.UpdateClusterNodeStatus(ctx, created1)
	if err != nil {
		t.Fatal(err)
	}

	got1, err := store.GetClusterNode(ctx, created1.ID)
	if err != nil || got1.HealthStatus != "degraded" || got1.ConfigStatus != "match" || got1.ConfigFingerprint != "sha256:abcd" ||
		got1.ConfigGeneration != 9 || got1.CoordinatorEpoch != "epoch-1" || !got1.RepositoryHealth["debian"] ||
		got1.RepositoryHealth["pypi"] || got1.LatencyMS != 37 || got1.MutationToken != "tokyo-mutation-secret" {
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

func TestClusterMutationTokenMigrationAndKeyRotation(t *testing.T) {
	ctx := context.Background()
	databasePath := filepath.Join(t.TempDir(), "mirrorrelay.db")
	legacyStore, err := Open(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	const token = "legacy-edge-mutation-secret"
	if _, err := legacyStore.db.ExecContext(ctx, `INSERT INTO cluster_nodes(name,url,region,mutation_token,created_at,updated_at) VALUES(?,?,?,?,?,?)`,
		"legacy-edge", "https://legacy-edge.example", "test", token, nowText(), nowText()); err != nil {
		t.Fatal(err)
	}
	if err := legacyStore.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(databasePath); err == nil {
		t.Fatal("database with plaintext cluster credentials opened without an encryption key")
	}

	primaryKey := bytes.Repeat([]byte{0x31}, 32)
	migrated, err := Open(databasePath, WithClusterMutationTokenKeys(primaryKey))
	if err != nil {
		t.Fatal(err)
	}
	node, err := migrated.GetClusterNodeByURL(ctx, "https://legacy-edge.example")
	if err != nil || node.MutationToken != token {
		t.Fatalf("legacy token was not migrated transparently: node=%+v err=%v", node, err)
	}
	var plaintext, firstCiphertext string
	if err := migrated.db.QueryRowContext(ctx, `SELECT mutation_token,mutation_token_ciphertext FROM cluster_nodes WHERE id=?`, node.ID).Scan(&plaintext, &firstCiphertext); err != nil {
		t.Fatal(err)
	}
	if plaintext != "" || !strings.HasPrefix(firstCiphertext, clusterMutationTokenEnvelopePrefix) || strings.Contains(firstCiphertext, token) {
		t.Fatalf("legacy token remained readable at rest: plaintext=%q ciphertext=%q", plaintext, firstCiphertext)
	}
	if err := migrated.Close(); err != nil {
		t.Fatal(err)
	}

	rotatedKey := bytes.Repeat([]byte{0x52}, 32)
	rotated, err := Open(databasePath, WithClusterMutationTokenKeys(rotatedKey, primaryKey))
	if err != nil {
		t.Fatal(err)
	}
	var rotatedCiphertext string
	if err := rotated.db.QueryRowContext(ctx, `SELECT mutation_token_ciphertext FROM cluster_nodes WHERE id=?`, node.ID).Scan(&rotatedCiphertext); err != nil {
		t.Fatal(err)
	}
	if rotatedCiphertext == firstCiphertext {
		t.Fatal("legacy-key ciphertext was not rotated to the primary key")
	}
	if err := rotated.Close(); err != nil {
		t.Fatal(err)
	}

	currentOnly, err := Open(databasePath, WithClusterMutationTokenKeys(rotatedKey))
	if err != nil {
		t.Fatalf("rotated database did not open with only the current key: %v", err)
	}
	if _, err := currentOnly.GetClusterNode(ctx, node.ID); err != nil {
		t.Fatal(err)
	}
	if err := currentOnly.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(databasePath, WithClusterMutationTokenKeys(primaryKey)); err == nil {
		t.Fatal("rotated database unexpectedly opened with only the retired key")
	}
}

func TestLoadClusterMutationTokenKeyFiles(t *testing.T) {
	directory := t.TempDir()
	key := bytes.Repeat([]byte{0x73}, 32)
	keyPath := filepath.Join(directory, "cluster.key")
	if err := os.WriteFile(keyPath, []byte(base64.StdEncoding.EncodeToString(key)+"\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(keyPath, 0o640); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadClusterMutationTokenKeyFiles([]string{keyPath})
	if err != nil || len(loaded) != 1 || !bytes.Equal(loaded[0], key) {
		t.Fatalf("valid cluster key file did not load: keys=%x err=%v", loaded, err)
	}

	if err := os.Chmod(keyPath, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadClusterMutationTokenKeyFiles([]string{keyPath}); err == nil {
		t.Fatal("world-readable cluster key file was accepted")
	}

	malformedPath := filepath.Join(directory, "malformed.key")
	if err := os.WriteFile(malformedPath, []byte("too-short\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadClusterMutationTokenKeyFiles([]string{malformedPath}); err == nil {
		t.Fatal("malformed cluster key file was accepted")
	}
}

func TestPasskeysAndRecoveryCodes(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "mirrorrelay.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ctx := context.Background()
	user, ok, err := store.CreateInitialAdmin(ctx, "admin", "hash")
	if err != nil || !ok {
		t.Fatalf("failed to create admin: %v", err)
	}

	// Create Passkey
	pk := model.PasskeyCredential{
		UserID:         user.ID,
		CredentialID:   "cred-id-123",
		PublicKey:      "pk-data-456",
		SignCount:      0,
		AAGUID:         "aaguid-789",
		Transports:     []string{"internal", "usb"},
		BackupEligible: true,
		BackupState:    false,
		DisplayName:    "My Security Key",
	}
	if err := store.CreatePasskey(ctx, pk); err != nil {
		t.Fatalf("failed to create passkey: %v", err)
	}

	// Retrieve Passkey
	loaded, err := store.GetPasskeyByCredentialID(ctx, "cred-id-123")
	if err != nil {
		t.Fatalf("failed to get passkey: %v", err)
	}
	if loaded.DisplayName != "My Security Key" || loaded.UserID != user.ID || len(loaded.Transports) != 2 {
		t.Fatalf("loaded passkey mismatch: %+v", loaded)
	}

	// Advance sign count and reject repeats or rollbacks, including a reset to
	// zero after the authenticator has started using a positive counter.
	if advanced, err := store.AdvancePasskeySignCount(ctx, "cred-id-123", 0); err != nil || !advanced {
		t.Fatalf("zero-counter authenticator use: advanced=%v err=%v", advanced, err)
	}
	if advanced, err := store.AdvancePasskeySignCount(ctx, "cred-id-123", 5); err != nil || !advanced {
		t.Fatalf("failed to advance sign count: advanced=%v err=%v", advanced, err)
	}
	loaded, _ = store.GetPasskeyByCredentialID(ctx, "cred-id-123")
	if loaded.SignCount != 5 || loaded.LastUsedAt == nil {
		t.Fatalf("expected sign count 5 and non-nil last_used_at, got: %+v", loaded)
	}
	for _, next := range []uint32{5, 4, 0} {
		if advanced, err := store.AdvancePasskeySignCount(ctx, "cred-id-123", next); err != nil || advanced {
			t.Fatalf("non-advancing sign count %d: advanced=%v err=%v", next, advanced, err)
		}
	}
	if advanced, err := store.AdvancePasskeySignCount(ctx, "cred-id-123", 6); err != nil || !advanced {
		t.Fatalf("failed to advance sign count to 6: advanced=%v err=%v", advanced, err)
	}

	// Rename passkey
	if err := store.UpdatePasskeyDisplayName(ctx, loaded.ID, user.ID, "Renamed Key"); err != nil {
		t.Fatalf("failed to rename passkey: %v", err)
	}
	loaded, _ = store.GetPasskeyByCredentialID(ctx, "cred-id-123")
	if loaded.DisplayName != "Renamed Key" {
		t.Fatalf("rename failed: %s", loaded.DisplayName)
	}

	// Recovery codes
	hashes := []string{"hash1", "hash2", "hash3"}
	if err := store.SaveRecoveryCodes(ctx, user.ID, hashes); err != nil {
		t.Fatalf("failed to save recovery codes: %v", err)
	}
	count, err := store.CountValidRecoveryCodes(ctx, user.ID)
	if err != nil || count != 3 {
		t.Fatalf("expected 3 valid codes, got %d (err: %v)", count, err)
	}

	// Use recovery code
	used, err := store.VerifyAndUseRecoveryCode(ctx, user.ID, "hash1")
	if err != nil || !used {
		t.Fatalf("failed to use recovery code: %v", err)
	}
	// Using again should fail
	if _, err := store.VerifyAndUseRecoveryCode(ctx, user.ID, "hash1"); err == nil {
		t.Fatal("reusing recovery code should fail")
	}
	count, _ = store.CountValidRecoveryCodes(ctx, user.ID)
	if count != 2 {
		t.Fatalf("expected 2 remaining codes, got %d", count)
	}

	// Delete passkey
	if err := store.DeletePasskey(ctx, loaded.ID, user.ID); err != nil {
		t.Fatalf("failed to delete passkey: %v", err)
	}
	total, err := store.CountPasskeysByUserID(ctx, user.ID)
	if err != nil || total != 0 {
		t.Fatalf("expected 0 passkeys after delete, got %d", total)
	}
}
