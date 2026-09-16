package upstreamnginx

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/LuisCMerrick/MirrorRelay/internal/config"
)

type validationFailureRunner struct{}

func (validationFailureRunner) Run(ctx context.Context, _ string, _ ...string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "canceled validation", err
	}
	return "simulated validation failure", errors.New("simulated validation failure")
}
func (validationFailureRunner) Start(string, ...string) (processHandle, error) {
	return nil, errors.New("unexpected process start")
}

func TestValidationFailurePreservesExistingVersions(t *testing.T) {
	for _, state := range []string{"new", "existing", "published"} {
		for _, canceled := range []bool{false, true} {
			t.Run(state+map[bool]string{false: "/failure", true: "/canceled"}[canceled], func(t *testing.T) {
				root := t.TempDir()
				cfg := config.Default()
				cfg.UpstreamNginx.Prefix = filepath.Join(root, "upstream")
				cfg.UpstreamNginx.LogPath = filepath.Join(root, "logs")
				cfg.UpstreamNginx.PID = filepath.Join(root, "nginx.pid")
				cfg.UpstreamNginx.UpstreamSocket = filepath.Join(root, "upstream.sock")
				cfg.Cache.Path = filepath.Join(root, "cache")
				controller := newController(cfg, nil, NewGenerator(cfg, nil), validationFailureRunner{})
				generated, err := controller.generator.Generate(context.Background(), nil, nil)
				if err != nil {
					t.Fatal(err)
				}
				var before []byte
				directory := filepath.Join(cfg.UpstreamNginx.Prefix, "versions", generated.Hash)
				if state != "new" {
					if _, err := controller.writeVersion(generated); err != nil {
						t.Fatal(err)
					}
					before, err = os.ReadFile(filepath.Join(directory, "nginx.conf"))
					if err != nil {
						t.Fatal(err)
					}
				}
				if state == "published" {
					if err := controller.publish(generated.Hash); err != nil {
						t.Fatal(err)
					}
				}
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				if canceled {
					cancel()
				}
				if _, _, err := controller.ValidateWithCustom(ctx, nil, nil); err == nil {
					t.Fatal("expected validation failure")
				}
				entries, err := os.ReadDir(filepath.Join(cfg.UpstreamNginx.Prefix, "versions"))
				if err != nil {
					t.Fatal(err)
				}
				wantCount := 1
				if state == "new" {
					wantCount = 0
				}
				if len(entries) != wantCount {
					t.Fatalf("unexpected retained candidates: %v", entries)
				}
				if state != "new" {
					after, err := os.ReadFile(filepath.Join(directory, "nginx.conf"))
					if err != nil || string(after) != string(before) {
						t.Fatalf("existing version changed: %v", err)
					}
				}
				if state == "published" {
					after, err := os.ReadFile(filepath.Join(cfg.UpstreamNginx.Prefix, "current", "nginx.conf"))
					if err != nil || string(after) != string(before) {
						t.Fatalf("active configuration damaged: %v", err)
					}
				}
			})
		}
	}
}
