package upstreamnginx

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/LuisCMerrick/MirrorRelay/internal/config"
	"github.com/LuisCMerrick/MirrorRelay/internal/database"
	"github.com/LuisCMerrick/MirrorRelay/internal/mirror"
	"github.com/LuisCMerrick/MirrorRelay/internal/model"
)

type reloadFailureRunner struct {
	reloadCalls  int
	failReloadAt int
}

func (r *reloadFailureRunner) Run(_ context.Context, _ string, args ...string) (string, error) {
	if strings.Contains(strings.Join(args, " "), "-s reload") {
		r.reloadCalls++
		if r.reloadCalls == r.failReloadAt {
			return "forced reload failure", errors.New("forced reload failure")
		}
	}
	return "ok", nil
}

func (*reloadFailureRunner) Start(string, ...string) (processHandle, error) {
	return nil, errors.New("unexpected process start")
}

func TestApplyConfigurationRestoresDesiredAndActiveAfterReloadFailure(t *testing.T) {
	ctx := context.Background()
	store, err := database.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	runtimeRoot := t.TempDir()
	cfg := config.Default()
	cfg.UpstreamNginx.Mode = "external"
	cfg.UpstreamNginx.Binary = executable
	cfg.UpstreamNginx.Prefix = filepath.Join(runtimeRoot, "upstream-nginx")
	cfg.UpstreamNginx.PID = filepath.Join(runtimeRoot, "upstream-nginx.pid")
	cfg.UpstreamNginx.LogPath = filepath.Join(runtimeRoot, "logs")
	cfg.UpstreamNginx.UpstreamSocketEnabled = false
	cfg.UpstreamNginx.UpstreamLocalPort = listener.Addr().(*net.TCPAddr).Port
	cfg.Cache.Path = filepath.Join(runtimeRoot, "cache")
	if err := os.WriteFile(cfg.UpstreamNginx.PID, []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
		t.Fatal(err)
	}

	activeRepository := model.Mirror{
		ID: 1, Name: "Packages", Slug: "packages", Type: "generic", Enabled: true,
		PublicMode: "path", PublicPath: "/packages/", CacheEnabled: true,
		Upstreams: []model.Upstream{{URL: "https://8.8.8.8/", Enabled: true, Priority: 100, Weight: 100}},
	}
	if err := mirror.NormalizeAndValidate(&activeRepository, false, false); err != nil {
		t.Fatal(err)
	}
	if err := store.ReplaceConfiguration(ctx, []model.Mirror{activeRepository}, []model.CustomConfig{}); err != nil {
		t.Fatal(err)
	}

	runner := &reloadFailureRunner{}
	controller := newController(cfg, store, NewGenerator(cfg, nil), runner)
	activeVersion, err := controller.Reconcile(ctx, "test", "initial active configuration")
	if err != nil {
		t.Fatal(err)
	}
	previousTarget, err := os.Readlink(filepath.Join(cfg.UpstreamNginx.Prefix, "current"))
	if err != nil {
		t.Fatal(err)
	}

	candidate := activeRepository
	candidate.Upstreams = append([]model.Upstream(nil), activeRepository.Upstreams...)
	candidate.Upstreams[0].URL = "https://1.1.1.1/"
	runner.failReloadAt = runner.reloadCalls + 1
	if _, err := controller.ApplyConfiguration(ctx, []model.Mirror{candidate}, []model.CustomConfig{}, "cluster", "failing edge sync"); err == nil || !strings.Contains(err.Error(), "forced reload failure") {
		t.Fatalf("candidate reload unexpectedly succeeded: %v", err)
	}

	desired, err := store.ListMirrors(ctx)
	if err != nil {
		t.Fatal(err)
	}
	active, custom, available := controller.ActiveConfiguration()
	if !available || len(desired) != 1 || len(active) != 1 || custom == nil ||
		desired[0].Upstreams[0].URL != activeRepository.Upstreams[0].URL || active[0].Upstreams[0].URL != activeRepository.Upstreams[0].URL {
		t.Fatalf("previous desired/active configuration was not preserved: desired=%+v active=%+v custom=%+v", desired, active, custom)
	}
	currentTarget, err := os.Readlink(filepath.Join(cfg.UpstreamNginx.Prefix, "current"))
	if err != nil || currentTarget != previousTarget {
		t.Fatalf("published configuration was not restored: got=%q want=%q err=%v", currentTarget, previousTarget, err)
	}
	versions, err := store.ListConfigVersions(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) != 2 || versions[0].Active || !versions[1].Active || versions[1].Version != activeVersion.Version {
		t.Fatalf("failed candidate was recorded as active: %+v", versions)
	}
	effective, err := controller.EffectiveConfig(ctx)
	if err != nil || effective != activeVersion.Configuration {
		t.Fatalf("effective configuration did not remain on the active version: err=%v", err)
	}
}

func TestRecoverLastActivePublishesPersistedRoutingSnapshot(t *testing.T) {
	store, err := database.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	repositories := []model.Mirror{{ID: 7, Name: "Active", Slug: "active", Enabled: true, Upstreams: []model.Upstream{{ID: 70, URL: "https://active.example/", Enabled: true}}}}
	snapshot, err := json.Marshal(configurationSnapshot{Repositories: repositories, Custom: []model.CustomConfig{}})
	if err != nil {
		t.Fatal(err)
	}
	version, err := store.AddConfigVersion(context.Background(), model.ConfigVersion{
		ConfigurationHash: "active-hash", ValidationOK: true, Active: true, Snapshot: string(snapshot), Configuration: "events {}",
	}, 20)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.UpstreamNginx.Mode = "disabled"
	controller := newController(cfg, store, nil, nil)
	var published []model.Mirror
	controller.SetActivePublisher(func(repositories []model.Mirror) {
		published = repositories
		if len(repositories) > 0 {
			repositories[0].Name = "callback mutation"
		}
	})
	if err := controller.recoverLastActive(context.Background(), errors.New("desired validation failed")); err != nil {
		t.Fatal(err)
	}
	active, available := controller.ActiveRepositories()
	status := controller.Status()
	if !available || len(published) != 1 || len(active) != 1 || active[0].Name != "Active" || active[0].Upstreams[0].URL != "https://active.example/" ||
		status.CurrentConfigVersion != version.Version || status.CurrentConfigHash != "active-hash" || status.LastError != "desired validation failed" {
		t.Fatalf("persisted active state was not restored: repositories=%#v status=%#v", active, status)
	}
	active[0].Upstreams[0].URL = "https://mutated.example/"
	again, _ := controller.ActiveRepositories()
	if again[0].Upstreams[0].URL != "https://active.example/" {
		t.Fatal("active repository snapshot was returned by reference")
	}
}

func TestDecodeConfigurationSnapshotRequiresCurrentObjectShape(t *testing.T) {
	if _, err := decodeConfigurationSnapshot(`[]`); err == nil {
		t.Fatal("array-only configuration snapshot was accepted")
	}
}

func TestWaitForManagedProcessRecordsExitCode(t *testing.T) {
	command := exec.Command("sh", "-c", "exit 23")
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	process := execProcess{command: command}
	controller := newController(config.Default(), nil, nil, nil)
	controller.status.State = "running"
	controller.status.PID = process.PID()
	controller.childPID = process.PID()
	controller.waitForManagedProcess(process)
	status := controller.Status()
	if status.State != "restarting" || status.PID != 0 || status.LastExitCode == nil || *status.LastExitCode != 23 {
		t.Fatalf("managed process exit was reported incorrectly: %#v", status)
	}
}

func TestExecutablePathResolvesSymlinks(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	want, err := filepath.EvalSymlinks(executable)
	if err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "nginx-test-link")
	if err := os.Symlink(executable, link); err != nil {
		t.Fatal(err)
	}
	got, err := executablePath(link)
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Clean(want) {
		t.Fatalf("resolved executable = %q, want %q", got, want)
	}
}

func TestUpstreamNginxBinaryMetadataIncludesArchitectureAndBuildID(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	architecture, checksum, buildID := upstreamNginxBinaryMetadata(executable, "nginx version: nginx/1.30.2")
	if architecture == "" || len(checksum) != 64 || !strings.HasPrefix(buildID, "nginx-1.30.2-linux-") {
		t.Fatalf("unexpected binary metadata: architecture=%q checksum=%q build_id=%q", architecture, checksum, buildID)
	}
}

func TestSetFailureKeepsRunningDataPlaneState(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.UpstreamNginx.Binary = executable
	cfg.UpstreamNginx.PID = filepath.Join(t.TempDir(), "nginx.pid")
	if err := os.WriteFile(cfg.UpstreamNginx.PID, []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
		t.Fatal(err)
	}
	controller := newController(cfg, nil, nil, nil)
	controller.setFailure(errors.New("reload failed"))
	status := controller.Status()
	if status.State != "running" || status.PID != os.Getpid() || status.LastReloadResult != "failed" {
		t.Fatalf("running data plane was reported incorrectly after control-plane failure: %#v", status)
	}
}

func TestCurrentPIDRejectsDifferentExecutableAtConfiguredPath(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	replacement := filepath.Join(t.TempDir(), "replacement-nginx")
	if err := os.WriteFile(replacement, content, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.UpstreamNginx.Binary = replacement
	cfg.UpstreamNginx.PID = filepath.Join(t.TempDir(), "nginx.pid")
	if err := os.WriteFile(cfg.UpstreamNginx.PID, []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
		t.Fatal(err)
	}
	controller := newController(cfg, nil, nil, nil)
	if pid, running := controller.currentPID(); running || pid != 0 {
		t.Fatalf("different executable inode was accepted as the configured data plane: pid=%d", pid)
	}
}

func TestRotateUpstreamNginxLogFilesByDateAndSize(t *testing.T) {
	directory := t.TempDir()
	now := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	old := now.Add(-24 * time.Hour)
	access := filepath.Join(directory, "access.log")
	errorLog := filepath.Join(directory, "error.log")
	if err := os.WriteFile(access, []byte("access"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(errorLog, []byte("oversized"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(access, old, old); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(errorLog, now, now); err != nil {
		t.Fatal(err)
	}
	rotated, err := rotateUpstreamNginxLogFiles(directory, now, 4, 30*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if !rotated {
		t.Fatal("logs were not rotated")
	}
	for _, path := range []string{
		filepath.Join(directory, "access-2026-08-11.log"),
		filepath.Join(directory, "error-2026-08-12.log"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("rotated log %s: %v", path, err)
		}
	}
}
