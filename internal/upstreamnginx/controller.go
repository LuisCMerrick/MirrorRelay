// Package upstreamnginx manages the generation and lifecycle of Managed Upstream Nginx.
package upstreamnginx

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
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
	ActiveConfigVersion(context.Context) (model.ConfigVersion, error)
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

type Controller struct {
	cfg       config.Config
	store     Store
	generator *Generator
	runner    commandRunner

	mu           sync.RWMutex
	applyMu      sync.Mutex
	commandMu    sync.Mutex
	versionMu    sync.Mutex
	status       Status
	failures     []time.Time
	stop         chan struct{}
	childPID     int
	active       []model.Mirror
	activeCustom []model.CustomConfig
	activeSet    bool
	publisher    func([]model.Mirror)
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
	custom, err := c.store.ListCustomConfigs(ctx)
	if err != nil {
		return model.ConfigVersion{}, err
	}
	generated, validationOutput, err := c.ValidateWithCustom(ctx, repositories, custom)
	desiredIDs := make([]int64, 0, len(repositories))
	for _, repository := range repositories {
		desiredIDs = append(desiredIDs, repository.ID)
	}
	if err != nil {
		if stateErr := c.store.SetConfigState(ctx, desiredIDs, "failed", err.Error()); stateErr != nil {
			slog.Warn("record failed repository state", "error", stateErr)
		}
		c.setFailure(err)
		return model.ConfigVersion{}, err
	}
	snapshotBytes, err := json.Marshal(configurationSnapshot{Repositories: repositories, Custom: custom})
	if err != nil {
		return model.ConfigVersion{}, err
	}
	versionRecord := model.ConfigVersion{
		Active:            false,
		ConfigurationHash: generated.Hash,
		Configuration:     generated.Effective,
		Snapshot:          string(snapshotBytes),
		ValidationOK:      true,
		ValidationResult:  validationOutput,
		Operator:          operator,
		Description:       description,
	}
	activeVersion, err := c.store.AddConfigVersion(ctx, versionRecord, c.cfg.UpstreamNginx.HistoryLimit)
	if err != nil {
		return model.ConfigVersion{}, err
	}
	previousTarget := ""
	if c.Enabled() {
		if currentTarget, readErr := os.Readlink(filepath.Join(c.cfg.UpstreamNginx.Prefix, "current")); readErr == nil {
			previousTarget = currentTarget
		}
		if publishErr := c.publish(generated.Hash); publishErr != nil {
			_ = c.restorePublishedConfiguration(previousTarget)
			return model.ConfigVersion{}, publishErr
		}
		if activateErr := c.activate(ctx); activateErr != nil {
			_ = c.restorePublishedConfiguration(previousTarget)
			c.setFailure(activateErr)
			return model.ConfigVersion{}, activateErr
		}
	}
	if err := c.store.SetActiveConfigVersion(ctx, activeVersion.Version); err != nil {
		if c.Enabled() {
			_ = c.restorePublishedConfiguration(previousTarget)
		}
		return model.ConfigVersion{}, fmt.Errorf("record active configuration version: %w", err)
	}
	activeVersion.Active = true
	if stateErr := c.store.SetConfigState(ctx, desiredIDs, "active", ""); stateErr != nil {
		slog.Warn("record active repository state", "error", stateErr)
	}
	c.mu.Lock()
	c.status.CurrentConfigVersion = activeVersion.Version
	c.status.CurrentConfigHash = activeVersion.ConfigurationHash
	c.status.LastReload = time.Now()
	c.status.LastReloadResult = "success"
	c.status.LastError = ""
	c.mu.Unlock()
	c.setActiveConfiguration(repositories, custom)
	return activeVersion, nil
}

// ApplyConfiguration validates a complete candidate before changing desired
// state, then reconciles it under the same controller lock. If publication or
// reload fails, the previous desired configuration is restored and the prior
// active configuration remains published.
func (c *Controller) ApplyConfiguration(ctx context.Context, repositories []model.Mirror, custom []model.CustomConfig, operator, description string) (model.ConfigVersion, error) {
	c.applyMu.Lock()
	defer c.applyMu.Unlock()
	if _, _, err := c.ValidateWithCustom(ctx, repositories, custom); err != nil {
		return model.ConfigVersion{}, err
	}
	previousRepositories, err := c.store.ListMirrors(ctx)
	if err != nil {
		return model.ConfigVersion{}, err
	}
	previousCustom, err := c.store.ListCustomConfigs(ctx)
	if err != nil {
		return model.ConfigVersion{}, err
	}
	if err := c.store.ReplaceConfiguration(ctx, repositories, custom); err != nil {
		return model.ConfigVersion{}, err
	}
	version, err := c.reconcileLocked(ctx, operator, description)
	if err == nil {
		return version, nil
	}
	if restoreErr := c.store.ReplaceConfiguration(ctx, previousRepositories, previousCustom); restoreErr != nil {
		return model.ConfigVersion{}, errors.Join(err, fmt.Errorf("restore previous desired configuration: %w", restoreErr))
	}
	return model.ConfigVersion{}, err
}

func (c *Controller) Rollback(ctx context.Context, version int64, operator string) (model.ConfigVersion, error) {
	c.applyMu.Lock()
	defer c.applyMu.Unlock()
	versionRecord, err := c.store.ConfigVersion(ctx, version)
	if err != nil {
		return model.ConfigVersion{}, err
	}
	snapshot, err := decodeConfigurationSnapshot(versionRecord.Snapshot)
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
		repositories, err := c.store.ListMirrors(ctx)
		if err != nil {
			return err
		}
		custom, err := c.store.ListCustomConfigs(ctx)
		if err != nil {
			return err
		}
		c.setActiveConfiguration(repositories, custom)
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

func (c *Controller) ActiveConfiguration() ([]model.Mirror, []model.CustomConfig, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return cloneRepositories(c.active), cloneCustomConfigs(c.activeCustom), c.activeSet
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

func (c *Controller) setActiveConfiguration(repositories []model.Mirror, custom []model.CustomConfig) {
	active := cloneRepositories(repositories)
	activeCustom := cloneCustomConfigs(custom)
	c.mu.Lock()
	c.active = active
	c.activeCustom = activeCustom
	c.activeSet = true
	publisher := c.publisher
	c.mu.Unlock()
	if publisher != nil {
		publisher(cloneRepositories(active))
	}
}

func (c *Controller) recoverLastActive(ctx context.Context, desiredError error) error {
	active, err := c.store.ActiveConfigVersion(ctx)
	if err != nil {
		return fmt.Errorf("load active configuration snapshot: %w", err)
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
	c.setActiveConfiguration(snapshot.Repositories, snapshot.Custom)
	return nil
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
	value, err := c.store.ActiveConfigVersion(ctx)
	if err != nil {
		return "", err
	}
	return value.Configuration, nil
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

func cloneCustomConfigs(values []model.CustomConfig) []model.CustomConfig {
	if values == nil {
		return nil
	}
	cloned := make([]model.CustomConfig, len(values))
	copy(cloned, values)
	return cloned
}
