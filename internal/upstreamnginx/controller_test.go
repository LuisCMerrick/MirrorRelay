package upstreamnginx

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/LuisCMerrick/MirrorRelay/internal/config"
	"github.com/LuisCMerrick/MirrorRelay/internal/database"
	"github.com/LuisCMerrick/MirrorRelay/internal/model"
)

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
