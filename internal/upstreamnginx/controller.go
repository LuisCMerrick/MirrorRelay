// Package upstreamnginx manages the generation and lifecycle of Managed Upstream Nginx.
package upstreamnginx

import (
	"context"
	"crypto/sha256"
	"debug/elf"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/LuisCMerrick/MirrorRelay/internal/config"
	"github.com/LuisCMerrick/MirrorRelay/internal/model"
)

type Store interface {
	ListMirrors(context.Context) ([]model.Mirror, error)
	ReplaceMirrors(context.Context, []model.Mirror) error
	ReplaceCustomConfigs(context.Context, []model.CustomConfig) error
	ReplaceConfiguration(context.Context, []model.Mirror, []model.CustomConfig) error
	ListCustomConfigs(context.Context) ([]model.CustomConfig, error)
	AddConfigVersion(context.Context, model.ConfigVersion, int) (model.ConfigVersion, error)
	ListConfigVersions(context.Context, int) ([]model.ConfigVersion, error)
	ConfigVersion(context.Context, int64) (model.ConfigVersion, error)
	SetActiveConfigVersion(context.Context, int64) error
	SetConfigState(context.Context, []int64, string, string) error
}

type Status struct {
	Mode                 string    `json:"mode"`
	State                string    `json:"state"`
	PID                  int       `json:"pid,omitempty"`
	StartedAt            time.Time `json:"started_at,omitempty"`
	UptimeSeconds        int64     `json:"uptime_seconds"`
	Version              string    `json:"version,omitempty"`
	BuildID              string    `json:"build_id,omitempty"`
	Architecture         string    `json:"architecture,omitempty"`
	SHA256               string    `json:"sha256,omitempty"`
	BuildOptions         string    `json:"build_options,omitempty"`
	CurrentConfigVersion int64     `json:"current_config_version,omitempty"`
	CurrentConfigHash    string    `json:"current_config_hash,omitempty"`
	LastReload           time.Time `json:"last_reload,omitempty"`
	LastReloadResult     string    `json:"last_reload_result,omitempty"`
	LastError            string    `json:"last_error,omitempty"`
	LastExitAt           time.Time `json:"last_exit_at,omitempty"`
	LastExitCode         *int      `json:"last_exit_code,omitempty"`
	LastExitReason       string    `json:"last_exit_reason,omitempty"`
	IntegrationSnippet   string    `json:"integration_snippet,omitempty"`
	IntegrationResult    string    `json:"integration_result,omitempty"`
}

type commandRunner interface {
	Run(context.Context, string, ...string) (string, error)
	Start(string, ...string) (processHandle, error)
}

type processHandle interface {
	PID() int
	Wait() error
}

type execRunner struct{}

func (execRunner) Run(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

type execProcess struct {
	command *exec.Cmd
}

func (p execProcess) PID() int { return p.command.Process.Pid }
func (p execProcess) Wait() error {
	return p.command.Wait()
}

func (execRunner) Start(name string, args ...string) (processHandle, error) {
	command := exec.Command(name, args...)
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if err := command.Start(); err != nil {
		return nil, err
	}
	return execProcess{command: command}, nil
}

type Controller struct {
	cfg       config.Config
	store     Store
	generator *Generator
	runner    commandRunner

	mu        sync.RWMutex
	applyMu   sync.Mutex
	commandMu sync.Mutex
	versionMu sync.Mutex
	status    Status
	failures  []time.Time
	stop      chan struct{}
	childPID  int
	active    []model.Mirror
	activeSet bool
	publisher func([]model.Mirror)
}

type configurationSnapshot struct {
	Repositories []model.Mirror       `json:"repositories"`
	Custom       []model.CustomConfig `json:"custom_configs"`
}

func decodeConfigurationSnapshot(value string) (configurationSnapshot, error) {
	var snapshot configurationSnapshot
	if err := json.Unmarshal([]byte(value), &snapshot); err != nil {
		return configurationSnapshot{}, fmt.Errorf("decode configuration snapshot: %w", err)
	}
	if snapshot.Repositories == nil || snapshot.Custom == nil {
		return configurationSnapshot{}, errors.New("configuration snapshot is missing current fields")
	}
	return snapshot, nil
}

func NewController(cfg config.Config, store Store) *Controller {
	cfg.UpstreamNginx.Prefix = absolutePath(cfg.UpstreamNginx.Prefix)
	cfg.Cache.Path = absolutePath(cfg.Cache.Path)
	cfg.TLS.Certificate = absolutePath(cfg.TLS.Certificate)
	cfg.TLS.PrivateKey = absolutePath(cfg.TLS.PrivateKey)
	cfg.UpstreamNginx.PID = absolutePath(cfg.UpstreamNginx.PID)
	cfg.UpstreamNginx.UpstreamSocket = absolutePath(cfg.UpstreamNginx.UpstreamSocket)
	return newController(cfg, store, NewGenerator(cfg, nil), execRunner{})
}

func newController(cfg config.Config, store Store, generator *Generator, runner commandRunner) *Controller {
	return &Controller{cfg: cfg, store: store, generator: generator, runner: runner,
		status: Status{Mode: cfg.UpstreamNginx.Mode, State: "stopped"}, stop: make(chan struct{})}
}

func (c *Controller) Enabled() bool { return c.cfg.UpstreamNginx.Mode != "disabled" }

func (c *Controller) Validate(ctx context.Context, repositories []model.Mirror) (Generated, string, error) {
	custom, err := c.store.ListCustomConfigs(ctx)
	if err != nil {
		return Generated{}, "", err
	}
	return c.ValidateWithCustom(ctx, repositories, custom)
}

func (c *Controller) ValidateWithCustom(ctx context.Context, repositories []model.Mirror, custom []model.CustomConfig) (Generated, string, error) {
	generated, err := c.generator.Generate(ctx, repositories, custom)
	if err != nil {
		return Generated{}, "", err
	}
	if !c.Enabled() {
		return generated, "Managed Upstream Nginx disabled; static generation passed", nil
	}
	c.versionMu.Lock()
	defer c.versionMu.Unlock()
	versionDir, err := c.writeVersion(generated)
	if err != nil {
		return Generated{}, "", err
	}
	out, err := c.runUpstreamNginx(ctx, "-t", "-p", withTrailingSlash(c.cfg.UpstreamNginx.Prefix), "-c", filepath.Join(versionDir, "nginx.conf"))
	if err != nil {
		_ = os.RemoveAll(versionDir)
		return generated, out, fmt.Errorf("nginx -t failed: %w", err)
	}
	return generated, strings.TrimSpace(out), nil
}

func (c *Controller) Preview(ctx context.Context, repositories []model.Mirror) (Generated, error) {
	custom, err := c.store.ListCustomConfigs(ctx)
	if err != nil {
		return Generated{}, err
	}
	return c.generator.Generate(ctx, repositories, custom)
}

func (c *Controller) Reconcile(ctx context.Context, operator, description string) (model.ConfigVersion, error) {
	c.applyMu.Lock()
	defer c.applyMu.Unlock()
	return c.reconcileLocked(ctx, operator, description)
}

func (c *Controller) reconcileLocked(ctx context.Context, operator, description string) (model.ConfigVersion, error) {
	repositories, err := c.store.ListMirrors(ctx)
	if err != nil {
		return model.ConfigVersion{}, err
	}
	generated, validation, err := c.Validate(ctx, repositories)
	if err != nil {
		_ = c.store.SetConfigState(context.Background(), nil, "failed", err.Error())
		c.setFailure(err)
		return model.ConfigVersion{}, err
	}
	if c.Enabled() {
		history, historyErr := c.store.ListConfigVersions(ctx, 1)
		if historyErr == nil && len(history) == 1 && history[0].Active && history[0].ConfigurationHash == generated.Hash {
			if pid, running := c.currentPID(); running {
				_ = c.store.SetConfigState(context.Background(), nil, "active", "")
				c.mu.Lock()
				c.status.State, c.status.PID = "running", pid
				c.status.CurrentConfigVersion, c.status.CurrentConfigHash = history[0].Version, history[0].ConfigurationHash
				if c.status.StartedAt.IsZero() {
					c.status.StartedAt = time.Now()
				}
				c.status.LastReloadResult = "attached existing Managed Upstream Nginx; configuration unchanged"
				c.mu.Unlock()
				c.setActiveRepositories(repositories)
				c.publishIntegration(generated)
				return history[0], nil
			}
		}
	}
	custom, err := c.store.ListCustomConfigs(ctx)
	if err != nil {
		return model.ConfigVersion{}, err
	}
	snapshot, err := json.Marshal(configurationSnapshot{Repositories: repositories, Custom: custom})
	if err != nil {
		return model.ConfigVersion{}, err
	}
	v := model.ConfigVersion{CreatedAt: time.Now(), Operator: operator, Description: description,
		ConfigurationHash: generated.Hash, ValidationOK: true, ValidationResult: validation, Snapshot: string(snapshot), Configuration: generated.Effective}
	if !c.Enabled() {
		v.Active = true
		v, err = c.store.AddConfigVersion(ctx, v, c.cfg.UpstreamNginx.HistoryLimit)
		if err == nil {
			_ = c.store.SetConfigState(context.Background(), nil, "active", "")
			c.setActiveRepositories(repositories)
			c.pruneVersions(ctx)
			c.publishIntegration(generated)
		}
		return v, err
	}
	previousTarget, _ := os.Readlink(filepath.Join(c.cfg.UpstreamNginx.Prefix, "current"))
	if err := c.publish(generated.Hash); err != nil {
		_ = c.store.SetConfigState(context.Background(), nil, "failed", err.Error())
		c.setFailure(err)
		return model.ConfigVersion{}, err
	}
	if err := c.activate(ctx); err != nil {
		failure := err
		if recoveryErr := c.restorePublishedConfiguration(previousTarget); recoveryErr != nil {
			failure = errors.Join(err, fmt.Errorf("restore previous configuration: %w", recoveryErr))
		}
		_ = c.store.SetConfigState(context.Background(), nil, "failed", failure.Error())
		c.setFailure(failure)
		return model.ConfigVersion{}, failure
	}
	v.Active = true
	v, err = c.store.AddConfigVersion(ctx, v, c.cfg.UpstreamNginx.HistoryLimit)
	if err != nil {
		failure := err
		if recoveryErr := c.restorePublishedConfiguration(previousTarget); recoveryErr != nil {
			failure = errors.Join(err, fmt.Errorf("restore previous configuration after history write failure: %w", recoveryErr))
		}
		_ = c.store.SetConfigState(context.Background(), nil, "failed", failure.Error())
		c.setFailure(failure)
		return v, failure
	}
	c.pruneVersions(ctx)
	_ = c.store.SetConfigState(context.Background(), nil, "active", "")
	c.mu.Lock()
	c.status.CurrentConfigVersion = v.Version
	c.status.CurrentConfigHash = v.ConfigurationHash
	c.status.LastReload = time.Now()
	c.status.LastReloadResult = "success"
	c.status.LastError = ""
	c.mu.Unlock()
	c.setActiveRepositories(repositories)
	c.publishIntegration(generated)
	return v, nil
}

func (c *Controller) publishIntegration(generated Generated) {
	if !c.cfg.Ingress.GenerateSnippet || strings.TrimSpace(c.cfg.Ingress.SnippetPath) == "" {
		return
	}
	target := c.cfg.Ingress.SnippetPath
	if !strings.EqualFold(filepath.Ext(target), ".conf") {
		target = filepath.Join(target, "mirrorrelay.conf")
	}
	err := os.MkdirAll(filepath.Dir(target), 0o750)
	if err == nil {
		err = writeFileAtomic(target, []byte(generated.Files["external-nginx-integration.conf"]), 0o640)
	}
	c.mu.Lock()
	c.status.IntegrationSnippet = target
	if err != nil {
		c.status.IntegrationResult = err.Error()
	} else {
		c.status.IntegrationResult = "generated"
	}
	c.mu.Unlock()
	if err != nil {
		slog.Warn("write External Shared Nginx integration snippet", "path", target, "error", err)
	}
}

func (c *Controller) pruneVersions(ctx context.Context) {
	c.versionMu.Lock()
	defer c.versionMu.Unlock()
	values, err := c.store.ListConfigVersions(ctx, c.cfg.UpstreamNginx.HistoryLimit)
	if err != nil {
		return
	}
	keep := make(map[string]bool, len(values))
	for _, value := range values {
		keep[value.ConfigurationHash] = true
	}
	entries, err := os.ReadDir(filepath.Join(c.cfg.UpstreamNginx.Prefix, "versions"))
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() && !keep[entry.Name()] {
			_ = os.RemoveAll(filepath.Join(c.cfg.UpstreamNginx.Prefix, "versions", entry.Name()))
		}
	}
}

func (c *Controller) Rollback(ctx context.Context, version int64, operator string) (model.ConfigVersion, error) {
	c.applyMu.Lock()
	defer c.applyMu.Unlock()
	previous, err := c.store.ConfigVersion(ctx, version)
	if err != nil {
		return model.ConfigVersion{}, err
	}
	snapshot, err := decodeConfigurationSnapshot(previous.Snapshot)
	if err != nil {
		return model.ConfigVersion{}, err
	}
	generated, err := c.generator.Generate(ctx, snapshot.Repositories, snapshot.Custom)
	if err != nil {
		return model.ConfigVersion{}, err
	}
	c.versionMu.Lock()
	versionDir, err := c.writeVersion(generated)
	if err != nil {
		c.versionMu.Unlock()
		return model.ConfigVersion{}, err
	}
	if c.Enabled() {
		if _, err := c.runUpstreamNginx(ctx, "-t", "-p", withTrailingSlash(c.cfg.UpstreamNginx.Prefix), "-c", filepath.Join(versionDir, "nginx.conf")); err != nil {
			c.versionMu.Unlock()
			return model.ConfigVersion{}, fmt.Errorf("rollback nginx -t failed: %w", err)
		}
	}
	c.versionMu.Unlock()
	if err := c.store.ReplaceConfiguration(ctx, snapshot.Repositories, snapshot.Custom); err != nil {
		return model.ConfigVersion{}, err
	}
	return c.reconcileLocked(ctx, operator, fmt.Sprintf("rollback to version %d", version))
}

func (c *Controller) History(ctx context.Context) ([]model.ConfigVersion, error) {
	values, err := c.store.ListConfigVersions(ctx, c.cfg.UpstreamNginx.HistoryLimit)
	for i := range values {
		values[i].Configuration = ""
	}
	return values, err
}

func (c *Controller) Start(ctx context.Context) error {
	if !c.Enabled() {
		return nil
	}
	if err := c.ensureRuntime(); err != nil {
		return err
	}
	if _, err := c.Reconcile(ctx, "system", "startup reconcile"); err != nil {
		if recoveryErr := c.recoverLastActive(ctx, err); recoveryErr != nil {
			return errors.Join(err, fmt.Errorf("recover last active configuration: %w", recoveryErr))
		}
		slog.Warn("startup desired configuration failed; restored last active configuration", "error", err)
	}
	c.discoverVersion(ctx)
	go c.supervise(ctx)
	go c.refreshResolvedUpstreams(ctx)
	go c.rotateUpstreamNginxLogs(ctx)
	return nil
}

func (c *Controller) ActiveRepositories() ([]model.Mirror, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return cloneRepositories(c.active), c.activeSet
}

func (c *Controller) SetActivePublisher(publisher func([]model.Mirror)) {
	c.mu.Lock()
	c.publisher = publisher
	repositories := cloneRepositories(c.active)
	available := c.activeSet
	c.mu.Unlock()
	if publisher != nil && available {
		publisher(repositories)
	}
}

func (c *Controller) setActiveRepositories(repositories []model.Mirror) {
	active := cloneRepositories(repositories)
	c.mu.Lock()
	c.active = active
	c.activeSet = true
	publisher := c.publisher
	c.mu.Unlock()
	if publisher != nil {
		publisher(cloneRepositories(active))
	}
}

func (c *Controller) recoverLastActive(ctx context.Context, desiredError error) error {
	versions, err := c.store.ListConfigVersions(ctx, c.cfg.UpstreamNginx.HistoryLimit)
	if err != nil {
		return err
	}
	var active model.ConfigVersion
	for _, version := range versions {
		if version.Active {
			active = version
			break
		}
	}
	if active.Version == 0 {
		return errors.New("no active configuration snapshot is available")
	}
	snapshot, err := decodeConfigurationSnapshot(active.Snapshot)
	if err != nil {
		return err
	}
	if c.Enabled() {
		versionDir := filepath.Join(c.cfg.UpstreamNginx.Prefix, "versions", active.ConfigurationHash)
		configuration := filepath.Join(versionDir, "nginx.conf")
		if _, err := os.Stat(configuration); err != nil {
			return fmt.Errorf("active configuration files are unavailable: %w", err)
		}
		if output, err := c.runUpstreamNginx(ctx, "-t", "-p", withTrailingSlash(c.cfg.UpstreamNginx.Prefix), "-c", configuration); err != nil {
			return fmt.Errorf("active configuration validation failed: %w (%s)", err, strings.TrimSpace(output))
		}
		if err := c.publishTarget(versionDir); err != nil {
			return err
		}
		if err := c.activate(ctx); err != nil {
			return err
		}
	}
	c.mu.Lock()
	c.status.CurrentConfigVersion = active.Version
	c.status.CurrentConfigHash = active.ConfigurationHash
	c.status.LastReload = time.Now()
	c.status.LastReloadResult = "desired reconcile failed; restored last active configuration"
	c.status.LastError = desiredError.Error()
	c.mu.Unlock()
	c.setActiveRepositories(snapshot.Repositories)
	return nil
}

func (c *Controller) rotateUpstreamNginxLogs(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	pendingReopen := false
	run := func() {
		rotated, err := rotateUpstreamNginxLogFiles(c.cfg.UpstreamNginx.LogPath, time.Now(), c.cfg.Logging.MaxSizeMB<<20, time.Duration(c.cfg.Logging.KeepDays)*24*time.Hour)
		if err != nil {
			slog.Warn("rotate Managed Upstream Nginx logs", "error", err)
			return
		}
		pendingReopen = pendingReopen || rotated
		if !pendingReopen {
			return
		}
		if _, running := c.currentPID(); !running {
			return
		}
		reopenCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		out, reopenErr := c.runUpstreamNginx(reopenCtx, "-s", "reopen", "-p", withTrailingSlash(c.cfg.UpstreamNginx.Prefix), "-c", filepath.Join(c.cfg.UpstreamNginx.Prefix, "current", "nginx.conf"))
		cancel()
		if reopenErr != nil {
			slog.Warn("reopen Managed Upstream Nginx logs", "error", reopenErr, "output", strings.TrimSpace(out))
			return
		}
		pendingReopen = false
	}
	run()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			run()
		}
	}
}

func rotateUpstreamNginxLogFiles(directory string, now time.Time, maximumSize int64, keep time.Duration) (bool, error) {
	if maximumSize <= 0 {
		maximumSize = 1024 << 20
	}
	if keep <= 0 {
		keep = 30 * 24 * time.Hour
	}
	rotated := false
	for _, name := range []string{"access", "error"} {
		path := filepath.Join(directory, name+".log")
		info, err := os.Stat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return rotated, err
		}
		if info.Size() < maximumSize && info.ModTime().Format("2006-01-02") == now.Format("2006-01-02") {
			continue
		}
		target, err := nextRotatedLogPath(directory, name, info.ModTime())
		if err != nil {
			return rotated, err
		}
		if err := os.Rename(path, target); err != nil {
			return rotated, err
		}
		rotated = true
	}
	entries, err := os.ReadDir(directory)
	if errors.Is(err, os.ErrNotExist) {
		return rotated, nil
	}
	if err != nil {
		return rotated, err
	}
	cutoff := now.Add(-keep)
	for _, entry := range entries {
		if entry.IsDir() || (!strings.HasPrefix(entry.Name(), "access-") && !strings.HasPrefix(entry.Name(), "error-")) || !strings.HasSuffix(entry.Name(), ".log") {
			continue
		}
		info, infoErr := entry.Info()
		if infoErr == nil && info.ModTime().Before(cutoff) {
			if removeErr := os.Remove(filepath.Join(directory, entry.Name())); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
				return rotated, removeErr
			}
		}
	}
	return rotated, nil
}

func nextRotatedLogPath(directory, name string, modified time.Time) (string, error) {
	base := name + "-" + modified.Format("2006-01-02")
	for sequence := 0; sequence < 1_000_000; sequence++ {
		fileName := base + ".log"
		if sequence > 0 {
			fileName = base + "." + strconv.Itoa(sequence) + ".log"
		}
		path := filepath.Join(directory, fileName)
		if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
			return path, nil
		} else if err != nil {
			return "", err
		}
	}
	return "", errors.New("Managed Upstream Nginx log rotation sequence exhausted")
}

func (c *Controller) refreshResolvedUpstreams(ctx context.Context) {
	ticker := time.NewTicker(c.cfg.UpstreamNginx.ResolverRefresh)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := c.Reconcile(ctx, "system", "scheduled safe DNS refresh"); err != nil {
				c.setFailure(fmt.Errorf("safe DNS refresh: %w", err))
			}
		}
	}
}

func (c *Controller) Status() Status {
	c.mu.RLock()
	defer c.mu.RUnlock()
	status := c.status
	if !status.StartedAt.IsZero() && status.State == "running" {
		status.UptimeSeconds = int64(time.Since(status.StartedAt).Seconds())
	}
	return status
}

func (c *Controller) EffectiveConfig(ctx context.Context) (string, error) {
	values, err := c.store.ListConfigVersions(ctx, 1)
	if err != nil {
		return "", err
	}
	if len(values) == 0 {
		return "", errors.New("no active configuration")
	}
	return values[0].Configuration, nil
}

func (c *Controller) activate(ctx context.Context) error {
	pid, running := c.currentPID()
	c.mu.Lock()
	if running {
		c.status.State = "reloading"
	} else {
		c.status.State = "starting"
	}
	c.mu.Unlock()
	if running {
		out, err := c.runUpstreamNginx(ctx, "-s", "reload", "-p", withTrailingSlash(c.cfg.UpstreamNginx.Prefix), "-c", filepath.Join(c.cfg.UpstreamNginx.Prefix, "current", "nginx.conf"))
		if err != nil {
			return fmt.Errorf("graceful reload: %w (%s)", err, strings.TrimSpace(out))
		}
		c.mu.Lock()
		c.status.State, c.status.PID = "running", pid
		c.mu.Unlock()
		return c.waitForUpstreamEndpoint(ctx)
	}
	if c.cfg.UpstreamNginx.Mode == "external" {
		return errors.New("externally managed Managed Upstream Nginx is not running; start it with the configured prefix before applying")
	}
	if err := c.checkPorts(); err != nil {
		return err
	}
	if err := c.prepareUpstreamEndpoint(); err != nil {
		return err
	}
	process, err := c.startUpstreamNginx("-e", filepath.Join(c.cfg.UpstreamNginx.LogPath, "bootstrap-error.log"), "-p", withTrailingSlash(c.cfg.UpstreamNginx.Prefix), "-c", filepath.Join(c.cfg.UpstreamNginx.Prefix, "current", "nginx.conf"), "-g", "daemon off;")
	if err != nil {
		return fmt.Errorf("start Managed Upstream Nginx: %w", err)
	}
	c.mu.Lock()
	c.childPID = process.PID()
	c.mu.Unlock()
	go c.waitForManagedProcess(process)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if pid, ok := c.currentPID(); ok {
			if endpointErr := c.waitForUpstreamEndpoint(ctx); endpointErr != nil {
				return endpointErr
			}
			c.mu.Lock()
			c.status.State, c.status.PID, c.status.StartedAt = "running", pid, time.Now()
			c.mu.Unlock()
			return nil
		}
		time.Sleep(25 * time.Millisecond)
	}
	return errors.New("nginx did not create a live pid within 5 seconds")
}

func (c *Controller) writeVersion(g Generated) (string, error) {
	if err := c.ensureRuntime(); err != nil {
		return "", err
	}
	versionDir := filepath.Join(c.cfg.UpstreamNginx.Prefix, "versions", g.Hash)
	if _, err := os.Stat(filepath.Join(versionDir, "nginx.conf")); err == nil {
		return versionDir, nil
	}
	if err := os.MkdirAll(filepath.Join(versionDir, "generated"), 0o750); err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Join(versionDir, "custom"), 0o750); err != nil {
		return "", err
	}
	main := strings.ReplaceAll(g.Main, filepath.Join(c.cfg.UpstreamNginx.Prefix, "current"), versionDir)
	if err := writeFileAtomic(filepath.Join(versionDir, "nginx.conf"), []byte(main), 0o640); err != nil {
		return "", err
	}
	for name, content := range g.Files {
		dir := "generated"
		if strings.HasPrefix(name, "custom-") {
			dir = "custom"
		}
		if err := writeFileAtomic(filepath.Join(versionDir, dir, name), []byte(content), 0o640); err != nil {
			return "", err
		}
	}
	return versionDir, nil
}

func (c *Controller) publish(hash string) error {
	target := filepath.Join(c.cfg.UpstreamNginx.Prefix, "versions", hash)
	if _, err := os.Stat(filepath.Join(target, "nginx.conf")); err != nil {
		return fmt.Errorf("validated configuration not found: %w", err)
	}
	return c.publishTarget(target)
}

func (c *Controller) publishTarget(target string) error {
	temporary := filepath.Join(c.cfg.UpstreamNginx.Prefix, ".current-"+strconv.Itoa(os.Getpid()))
	_ = os.Remove(temporary)
	if err := os.Symlink(target, temporary); err != nil {
		return err
	}
	if err := os.Rename(temporary, filepath.Join(c.cfg.UpstreamNginx.Prefix, "current")); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	return nil
}

func (c *Controller) ensureRuntime() error {
	for _, path := range []string{"run", "temp/client", "temp/proxy", "versions"} {
		if err := os.MkdirAll(filepath.Join(c.cfg.UpstreamNginx.Prefix, path), 0o750); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(c.cfg.UpstreamNginx.LogPath, 0o750); err != nil {
		return err
	}
	if err := os.MkdirAll(c.cfg.Cache.Path, 0o750); err != nil {
		return err
	}
	return nil
}

func (c *Controller) currentPID() (int, bool) {
	b, err := os.ReadFile(c.cfg.UpstreamNginx.PID)
	if err != nil {
		return 0, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil || pid <= 0 {
		return 0, false
	}
	if err := syscall.Kill(pid, 0); err != nil {
		return 0, false
	}
	actualInfo, err := os.Stat(filepath.Join("/proc", strconv.Itoa(pid), "exe"))
	if err != nil {
		return 0, false
	}
	expected, err := executablePath(c.cfg.UpstreamNginx.Binary)
	if err != nil {
		return 0, false
	}
	expectedInfo, err := os.Stat(expected)
	if err != nil || !os.SameFile(actualInfo, expectedInfo) {
		return 0, false
	}
	return pid, true
}

func executablePath(binary string) (string, error) {
	resolved := binary
	if !strings.ContainsRune(binary, os.PathSeparator) {
		value, err := exec.LookPath(binary)
		if err != nil {
			return "", err
		}
		resolved = value
	}
	absolute, err := filepath.Abs(resolved)
	if err != nil {
		return "", err
	}
	if evaluated, evaluateErr := filepath.EvalSymlinks(absolute); evaluateErr == nil {
		absolute = evaluated
	}
	return filepath.Clean(absolute), nil
}

func (c *Controller) supervise(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	backoff := c.cfg.UpstreamNginx.RestartInitialBackoff
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, ok := c.currentPID(); ok {
				backoff = c.cfg.UpstreamNginx.RestartInitialBackoff
				continue
			}
			c.mu.Lock()
			if c.status.State == "stopped" || c.status.State == "stopping" {
				c.mu.Unlock()
				continue
			}
			if c.childPID != 0 {
				c.mu.Unlock()
				continue
			}
			if c.status.State == "running" {
				unknownExitCode := -1
				c.status.LastExitAt = time.Now()
				c.status.LastExitCode = &unknownExitCode
				c.status.LastExitReason = "attached Managed Upstream Nginx process disappeared; exit status unavailable"
				c.status.State = "restarting"
				c.status.PID = 0
			}
			c.mu.Unlock()
			if !c.mayRestart() {
				c.setFailure(errors.New("nginx restart limit exceeded"))
				continue
			}
			timer := time.NewTimer(backoff)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
			c.applyMu.Lock()
			_, running := c.currentPID()
			var err error
			if !running {
				err = c.activate(ctx)
			}
			c.applyMu.Unlock()
			if err != nil {
				c.setFailure(err)
				backoff *= 2
				if backoff > c.cfg.UpstreamNginx.RestartMaxBackoff {
					backoff = c.cfg.UpstreamNginx.RestartMaxBackoff
				}
			}
		}
	}
}

func (c *Controller) mayRestart() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	cutoff := time.Now().Add(-c.cfg.UpstreamNginx.RestartWindow)
	kept := c.failures[:0]
	for _, at := range c.failures {
		if at.After(cutoff) {
			kept = append(kept, at)
		}
	}
	c.failures = kept
	if len(c.failures) >= c.cfg.UpstreamNginx.RestartMaxFailures {
		return false
	}
	c.failures = append(c.failures, time.Now())
	return true
}

func (c *Controller) setFailure(err error) {
	pid, running := c.currentPID()
	c.mu.Lock()
	defer c.mu.Unlock()
	if running {
		c.status.State, c.status.PID = "running", pid
	} else {
		c.status.State, c.status.PID = "failed", 0
	}
	c.status.LastError = err.Error()
	c.status.LastReload = time.Now()
	c.status.LastReloadResult = "failed"
}

func (c *Controller) restorePublishedConfiguration(previousTarget string) error {
	recoveryCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if previousTarget != "" {
		if err := c.publishTarget(previousTarget); err != nil {
			return err
		}
		return c.activate(recoveryCtx)
	}
	if c.cfg.UpstreamNginx.Mode != "managed" {
		return errors.New("no previous managed configuration is available")
	}
	if err := c.Stop(recoveryCtx); err != nil {
		return err
	}
	if err := os.Remove(filepath.Join(c.cfg.UpstreamNginx.Prefix, "current")); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func (c *Controller) discoverVersion(ctx context.Context) {
	out, err := c.runUpstreamNginx(ctx, "-V")
	lines := strings.Split(strings.TrimSpace(out), "\n")
	versionLine := ""
	if len(lines) > 0 && lines[0] != "" {
		versionLine = lines[0]
	}
	architecture, checksum, binaryBuildID := upstreamNginxBinaryMetadata(c.cfg.UpstreamNginx.Binary, versionLine)
	if err != nil && out == "" && checksum == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if versionLine != "" {
		c.status.Version = versionLine
	}
	if len(lines) > 1 {
		c.status.BuildOptions = strings.Join(lines[1:], "\n")
	}
	c.status.Architecture = architecture
	c.status.SHA256 = checksum
	c.status.BuildID = binaryBuildID
}

func upstreamNginxBinaryMetadata(binary, versionLine string) (architecture, checksum, buildID string) {
	path, err := executablePath(binary)
	if err != nil {
		return "", "", ""
	}
	file, err := os.Open(path)
	if err != nil {
		return "", "", ""
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err == nil {
		checksum = fmt.Sprintf("%x", hash.Sum(nil))
	}
	_ = file.Close()

	if binaryELF, err := elf.Open(path); err == nil {
		switch binaryELF.Machine {
		case elf.EM_X86_64:
			architecture = "linux/amd64"
		case elf.EM_AARCH64:
			architecture = "linux/arm64"
		default:
			architecture = "linux/" + strings.ToLower(binaryELF.Machine.String())
		}
		_ = binaryELF.Close()
	}
	if checksum == "" {
		return architecture, "", ""
	}
	version := strings.TrimSpace(versionLine)
	if separator := strings.LastIndexByte(version, '/'); separator >= 0 {
		version = version[separator+1:]
	}
	if version == "" {
		version = "unknown"
	}
	archID := strings.ReplaceAll(architecture, "/", "-")
	if archID == "" {
		archID = "unknown"
	}
	buildID = fmt.Sprintf("nginx-%s-%s-%s", version, archID, checksum[:12])
	return architecture, checksum, buildID
}

func (c *Controller) checkPorts() error {
	addresses := make([]string, 0, 3)
	if c.cfg.Ingress.Mode == "managed-standalone" {
		addresses = append(addresses, c.cfg.HTTP.Listen, c.cfg.HTTP.HTTPSListen)
	}
	if !c.cfg.UpstreamNginx.UpstreamSocketEnabled {
		_, address := c.cfg.UpstreamEndpoint()
		addresses = append(addresses, address)
	}
	for _, address := range addresses {
		listener, err := net.Listen("tcp", address)
		if err != nil {
			return fmt.Errorf("listen port conflict on %s: %w", address, err)
		}
		_ = listener.Close()
	}
	return nil
}

func (c *Controller) prepareUpstreamEndpoint() error {
	if !c.cfg.UpstreamNginx.UpstreamSocketEnabled {
		return nil
	}
	info, err := os.Lstat(c.cfg.UpstreamNginx.UpstreamSocket)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect upstream socket: %w", err)
	}
	if info.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("refusing to replace non-socket upstream path %s", c.cfg.UpstreamNginx.UpstreamSocket)
	}
	connection, dialErr := net.DialTimeout("unix", c.cfg.UpstreamNginx.UpstreamSocket, 250*time.Millisecond)
	if dialErr == nil {
		_ = connection.Close()
		return errors.New("upstream socket already has a live listener but Managed Upstream Nginx PID is unavailable")
	}
	if err := os.Remove(c.cfg.UpstreamNginx.UpstreamSocket); err != nil {
		return fmt.Errorf("remove stale upstream socket: %w", err)
	}
	return nil
}

func (c *Controller) waitForUpstreamEndpoint(ctx context.Context) error {
	network, address := c.cfg.UpstreamEndpoint()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		connection, err := net.DialTimeout(network, address, 100*time.Millisecond)
		if err == nil {
			_ = connection.Close()
			if network == "unix" {
				if chmodErr := os.Chmod(address, c.cfg.UpstreamNginx.UpstreamSocketMode); chmodErr != nil {
					return fmt.Errorf("set upstream socket permissions: %w", chmodErr)
				}
			}
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(25 * time.Millisecond):
		}
	}
	return fmt.Errorf("Managed Upstream Nginx did not create a reachable upstream %s endpoint %s within 5 seconds", network, address)
}

func (c *Controller) Stop(ctx context.Context) error {
	if !c.Enabled() || c.cfg.UpstreamNginx.Mode != "managed" {
		return nil
	}
	if _, running := c.currentPID(); !running {
		return nil
	}
	c.mu.Lock()
	c.status.State = "stopping"
	c.mu.Unlock()
	out, err := c.runUpstreamNginx(ctx, "-s", "quit", "-p", withTrailingSlash(c.cfg.UpstreamNginx.Prefix), "-c", filepath.Join(c.cfg.UpstreamNginx.Prefix, "current", "nginx.conf"))
	if err != nil {
		return fmt.Errorf("graceful stop Managed Upstream Nginx: %w (%s)", err, strings.TrimSpace(out))
	}
	for {
		if _, running := c.currentPID(); !running {
			c.mu.Lock()
			c.status.State, c.status.PID = "stopped", 0
			c.mu.Unlock()
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func (c *Controller) runUpstreamNginx(ctx context.Context, args ...string) (string, error) {
	c.commandMu.Lock()
	defer c.commandMu.Unlock()
	return c.runner.Run(ctx, c.cfg.UpstreamNginx.Binary, args...)
}

func (c *Controller) startUpstreamNginx(args ...string) (processHandle, error) {
	c.commandMu.Lock()
	defer c.commandMu.Unlock()
	return c.runner.Start(c.cfg.UpstreamNginx.Binary, args...)
}

func (c *Controller) waitForManagedProcess(process processHandle) {
	err := process.Wait()
	exitCode := 0
	reason := "Managed Upstream Nginx exited"
	if err != nil {
		exitCode = -1
		reason = err.Error()
		var exitError *exec.ExitError
		if errors.As(err, &exitError) {
			exitCode = exitError.ExitCode()
			if status, ok := exitError.Sys().(syscall.WaitStatus); ok && status.Signaled() {
				reason = "Managed Upstream Nginx terminated by signal " + status.Signal().String()
			} else {
				reason = fmt.Sprintf("Managed Upstream Nginx exited with code %d", exitCode)
			}
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.status.LastExitAt = time.Now()
	c.status.LastExitCode = &exitCode
	c.status.LastExitReason = reason
	if c.childPID != process.PID() {
		return
	}
	c.childPID = 0
	c.status.PID = 0
	if c.status.State == "stopping" {
		c.status.State = "stopped"
	} else if c.status.State != "stopped" {
		c.status.State = "restarting"
	}
}

func withTrailingSlash(path string) string {
	return strings.TrimRight(path, string(os.PathSeparator)) + string(os.PathSeparator)
}

func writeFileAtomic(path string, content []byte, mode os.FileMode) error {
	temporary := path + ".tmp-" + strconv.Itoa(os.Getpid())
	if err := os.WriteFile(temporary, content, mode); err != nil {
		return err
	}
	if err := os.Rename(temporary, path); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	return nil
}

func cloneRepositories(values []model.Mirror) []model.Mirror {
	if values == nil {
		return nil
	}
	cloned := make([]model.Mirror, len(values))
	for index, repository := range values {
		repository.Upstreams = append([]model.Upstream(nil), repository.Upstreams...)
		repository.RewriteHosts = append([]string(nil), repository.RewriteHosts...)
		repository.HeaderRemove = append([]string(nil), repository.HeaderRemove...)
		if repository.HeaderAdd != nil {
			headers := make(map[string]string, len(repository.HeaderAdd))
			for name, value := range repository.HeaderAdd {
				headers[name] = value
			}
			repository.HeaderAdd = headers
		}
		cloned[index] = repository
	}
	return cloned
}
