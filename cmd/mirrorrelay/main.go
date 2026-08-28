// Package main is the entry point for the MirrorRelay server.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"runtime/debug"
	"strings"
	"syscall"
	"time"

	"github.com/LuisCMerrick/MirrorRelay/internal/accesslog"
	"github.com/LuisCMerrick/MirrorRelay/internal/api"
	"github.com/LuisCMerrick/MirrorRelay/internal/applog"
	"github.com/LuisCMerrick/MirrorRelay/internal/auth"
	"github.com/LuisCMerrick/MirrorRelay/internal/buildinfo"
	"github.com/LuisCMerrick/MirrorRelay/internal/cachectl"
	"github.com/LuisCMerrick/MirrorRelay/internal/config"
	"github.com/LuisCMerrick/MirrorRelay/internal/database"
	"github.com/LuisCMerrick/MirrorRelay/internal/devcert"
	"github.com/LuisCMerrick/MirrorRelay/internal/health"
	"github.com/LuisCMerrick/MirrorRelay/internal/ipc"
	"github.com/LuisCMerrick/MirrorRelay/internal/mirror"
	"github.com/LuisCMerrick/MirrorRelay/internal/model"
	"github.com/LuisCMerrick/MirrorRelay/internal/proxy"
	"github.com/LuisCMerrick/MirrorRelay/internal/stats"
	"github.com/LuisCMerrick/MirrorRelay/internal/upstreamnginx"
	webassets "github.com/LuisCMerrick/MirrorRelay/internal/web"
)

var (
	version        = "0.0.21"
	gitCommit      = "unknown"
	buildTimestamp = "unknown"
	buildID        = ""
)

var errRestartRequested = errors.New("restart requested")

func main() {
	for {
		err := run()
		if errors.Is(err, errRestartRequested) {
			slog.Info("restarting MirrorRelay process")
			if executable, execErr := os.Executable(); execErr == nil {
				_ = syscall.Exec(executable, os.Args, os.Environ())
			}
			continue
		}
		if err != nil {
			slog.Error("MirrorRelay stopped", "error", err)
			os.Exit(1)
		}
		break
	}
}

func run() error {
	build := buildinfo.New(version, gitCommit, buildTimestamp, buildID)
	if len(os.Args) > 1 && (os.Args[1] == "version" || os.Args[1] == "--version") {
		return printVersion(build, os.Args[2:])
	}
	if len(os.Args) > 1 && os.Args[1] == "admin" {
		return handleAdminCLI(os.Args[2:])
	}
	var configPath string
	var dev bool
	flag.StringVar(&configPath, "config", "/etc/mirrorrelay/config.yaml", "configuration file")
	flag.BoolVar(&dev, "dev", false, "development mode: managed localhost ingress, self-signed TLS and local data directory")
	flag.Parse()

	cfg, err := config.Load(configPath, dev)
	if err != nil {
		return err
	}
	fileConfig := cfg
	if err := config.EnsureDirectories(cfg); err != nil {
		return fmt.Errorf("create directories: %w", err)
	}
	clusterMutationTokenKeys, err := database.LoadClusterMutationTokenKeyFiles(cfg.Distributed.MutationTokenKeyFiles)
	if err != nil {
		return err
	}
	store, err := database.Open(cfg.Database.Path, database.WithClusterMutationTokenKeys(clusterMutationTokenKeys...))
	if err != nil {
		return err
	}
	defer store.Close()
	if cfg, err = applyStoredWebSettings(context.Background(), store, cfg); err != nil {
		return err
	}
	if _, setByEnvironment := os.LookupEnv("GOGC"); !setByEnvironment && cfg.Performance.GOGC != 0 {
		debug.SetGCPercent(cfg.Performance.GOGC)
	}
	if _, setByEnvironment := os.LookupEnv("GOMEMLIMIT"); !setByEnvironment && cfg.Performance.GoMemoryLimit > 0 {
		debug.SetMemoryLimit(cfg.Performance.GoMemoryLimit)
	}
	if err := config.EnsureDirectories(cfg); err != nil {
		return fmt.Errorf("create configured directories: %w", err)
	}
	applicationLogger := applog.New(cfg.Logging.Path, cfg.Logging.QueueSize, cfg.Logging.MaxSizeMB, cfg.Logging.KeepDays)
	defer applicationLogger.Close()
	slog.SetDefault(slog.New(slog.NewJSONHandler(io.MultiWriter(os.Stderr, applicationLogger), nil)))
	if dev {
		ca, generated, certErr := devcert.Ensure(cfg.TLS.Certificate, cfg.TLS.PrivateKey)
		if certErr != nil {
			return certErr
		}
		slog.Info("development TLS enabled", "certificate", cfg.TLS.Certificate, "CA", ca, "generated", generated)
	}

	if len(cfg.Distributed.Nodes) > 0 {
		for _, seed := range cfg.Distributed.Nodes {
			existing, lookupErr := store.GetClusterNodeByURL(context.Background(), seed.URL)
			if lookupErr != nil && !database.IsNotFound(lookupErr) {
				return fmt.Errorf("load distributed node seed %q: %w", seed.Name, lookupErr)
			}
			if database.IsNotFound(lookupErr) {
				if _, err := store.CreateClusterNode(context.Background(), model.ClusterNode{
					Name:          seed.Name,
					URL:           seed.URL,
					MutationToken: seed.MutationToken,
					Region:        seed.Region,
					Country:       seed.Country,
					Priority:      seed.Priority,
					Weight:        seed.Weight,
					Enabled:       seed.Enabled,
				}); err != nil {
					return fmt.Errorf("create distributed node seed %q: %w", seed.Name, err)
				}
			} else if seed.MutationToken != "" && existing.MutationToken != seed.MutationToken {
				existing.MutationToken = seed.MutationToken
				if _, err := store.UpdateClusterNode(context.Background(), existing); err != nil {
					return fmt.Errorf("update distributed node seed %q mutation credential: %w", seed.Name, err)
				}
			}
		}
	}

	registry := mirror.NewRegistry(store)
	cacheManager := cachectl.New(cfg, store)
	if err := cacheManager.Load(context.Background()); err != nil {
		return fmt.Errorf("load cache generations: %w", err)
	}
	metric := stats.New()
	if err := metric.Load(context.Background(), store); err != nil {
		return fmt.Errorf("load statistics: %w", err)
	}
	accessLogger := accesslog.New(cfg.Logging.Path, cfg.Logging.QueueSize, cfg.Logging.MaxSizeMB, cfg.Logging.KeepDays)
	defer accessLogger.Close()
	checker := health.New(cfg, store, registry)
	upstreamNginxController := upstreamnginx.NewController(cfg, store)
	auxiliarySigningKey, err := store.AuxiliaryURLSigningKey(context.Background())
	if err != nil {
		return err
	}
	engine := proxy.New(cfg, registry, cacheManager, metric, accessLogger, auxiliarySigningKey)
	defer engine.CloseIdleConnections()
	control, err := api.New(cfg, fileConfig, store, registry, cacheManager, metric, checker, upstreamNginxController, webassets.FS(), build)
	if err != nil {
		return err
	}
	control.SetAppearanceStore(engine.AppearanceStore())
	upstreamNginxController.SetActivePublisher(control.PublishActiveRepositories)
	handler := control.Handler(engine)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	metric.StartPersistence(ctx)
	cacheManager.StartReclaimer(ctx)
	control.StartWarmup(ctx)
	restartChannel := make(chan struct{}, 1)
	control.SetRestartTrigger(func() {
		select {
		case restartChannel <- struct{}{}:
		default:
		}
	})
	return runProduct(ctx, cancel, cfg, handler, control, engine, checker, upstreamNginxController, registry, metric, restartChannel)
}

func applyStoredWebSettings(ctx context.Context, store *database.Store, base config.Config) (config.Config, error) {
	applied := base
	raw, found, err := store.Setting(ctx, config.WebSettingsKey)
	if err != nil {
		return base, fmt.Errorf("read Web UI configuration: %w", err)
	}
	if found {
		settings, err := config.DecodeWebSettingsWithBase([]byte(raw), base)
		if err != nil {
			return base, fmt.Errorf("decode Web UI configuration: %w", err)
		}
		candidate, err := settings.Apply(base)
		if err != nil {
			return base, fmt.Errorf("validate Web UI configuration: %w", err)
		}
		applied = candidate
	}
	if applied, err = config.ApplyEnvironment(applied); err != nil {
		return base, fmt.Errorf("apply environment overrides: %w", err)
	}
	rawApp, foundApp, err := store.Setting(ctx, database.AppearanceSettingsKey)
	if err != nil {
		return base, fmt.Errorf("read appearance configuration: %w", err)
	}
	if foundApp {
		var appConfig model.UIEnhancementConfig
		decoder := json.NewDecoder(strings.NewReader(rawApp))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&appConfig); err != nil {
			return base, fmt.Errorf("decode appearance configuration: %w", err)
		}
		if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
			return base, errors.New("decode appearance configuration: multiple JSON values are not allowed")
		}
		if err := config.ValidateUIEnhancement(&appConfig); err != nil {
			return base, fmt.Errorf("validate appearance configuration: %w", err)
		}
		applied.UIEnhancement = appConfig
	}
	return applied, nil
}

func printVersion(build buildinfo.Info, arguments []string) error {
	flags := flag.NewFlagSet("version", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	verbose := flags.Bool("verbose", false, "show complete build metadata")
	if err := flags.Parse(arguments); err != nil {
		return fmt.Errorf("version arguments: %w", err)
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("version arguments: unexpected value %q", flags.Arg(0))
	}
	if *verbose {
		fmt.Fprintln(os.Stdout, build.Verbose())
	} else {
		fmt.Fprintln(os.Stdout, build.Short())
	}
	return nil
}

func runProduct(
	ctx context.Context,
	cancel context.CancelFunc,
	cfg config.Config,
	handler http.Handler,
	control *api.Server,
	engine *proxy.Engine,
	checker *health.Checker,
	upstreamNginxController *upstreamnginx.Controller,
	registry *mirror.Registry,
	metric *stats.Stats,
	restartChannel <-chan struct{},
) error {
	listener, err := ipc.ListenLocal(cfg.Server.UnixSocketEnabled, cfg.Server.FrontendSocket, cfg.Server.FrontendSocketMode, cfg.Server.LocalAddress, cfg.Server.LocalPort)
	if err != nil {
		return fmt.Errorf("create frontend endpoint: %w", err)
	}
	defer listener.Close()
	server := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: cfg.HTTP.ReadTimeout,
		IdleTimeout:       cfg.HTTP.IdleTimeout,
		WriteTimeout:      cfg.HTTP.WriteTimeout,
	}
	if err := upstreamNginxController.Start(ctx); err != nil {
		return fmt.Errorf("start Managed Upstream Nginx: %w", err)
	}
	if activeRepositories, available := upstreamNginxController.ActiveRepositories(); available {
		registry.Replace(activeRepositories)
	} else if err := registry.Reload(ctx); err != nil {
		return fmt.Errorf("publish active routing configuration: %w", err)
	}
	control.StartCluster(ctx)
	checker.Start(ctx)

	errorChannel := make(chan error, 1)
	go func() {
		network, address := cfg.FrontendEndpoint()
		slog.Info("frontend endpoint listening", "network", network, "address", address)
		errorChannel <- server.Serve(listener)
	}()
	signalChannel := make(chan os.Signal, 1)
	signal.Notify(signalChannel, syscall.SIGTERM, syscall.SIGINT)
	defer signal.Stop(signalChannel)
	var isRestart bool
	select {
	case signal := <-signalChannel:
		slog.Info("MirrorRelay shutdown requested", "signal", signal.String())
	case <-restartChannel:
		slog.Info("MirrorRelay restart requested from Web UI")
		isRestart = true
	case serveErr := <-errorChannel:
		if !errors.Is(serveErr, http.ErrServerClosed) {
			return serveErr
		}
	}

	cancel()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), cfg.Shutdown.GracePeriod)
	shutdownErr := server.Shutdown(shutdownCtx)
	shutdownCancel()
	engine.CloseIdleConnections()
	if shutdownErr != nil {
		return fmt.Errorf("graceful frontend shutdown: %w", shutdownErr)
	}
	flushCtx, flushCancel := context.WithTimeout(context.Background(), 5*time.Second)
	flushErr := metric.Flush(flushCtx)
	flushCancel()
	if flushErr != nil {
		return fmt.Errorf("persist statistics: %w", flushErr)
	}
	if !isRestart && cfg.UpstreamNginx.StopOnMirrorRelayExit {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 30*time.Second)
		stopErr := upstreamNginxController.Stop(stopCtx)
		stopCancel()
		if stopErr != nil {
			return stopErr
		}
	}
	if isRestart {
		return errRestartRequested
	}
	return nil
}

func handleAdminCLI(args []string) error {
	return handleAdminCLIWithIO(args, os.Stdin, os.Stdout)
}

func handleAdminCLIWithIO(args []string, stdin io.Reader, stdout io.Writer) error {
	if len(args) == 0 {
		return errors.New("admin subcommand required: reset-password or reset-passkeys")
	}
	subcmd := args[0]
	if subcmd != "reset-password" && subcmd != "reset-passkeys" {
		return fmt.Errorf("unknown admin subcommand: %s (valid: reset-password, reset-passkeys)", subcmd)
	}
	fs := flag.NewFlagSet("admin "+subcmd, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	configPath := fs.String("config", "/etc/mirrorrelay/config.yaml", "path to configuration file")
	username := fs.String("username", "", "admin username")
	passwordStdin := fs.Bool("password-stdin", false, "read the new password from standard input")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}
	if *username == "" {
		return errors.New("-username is required")
	}
	if subcmd == "reset-password" && !*passwordStdin {
		return errors.New("-password-stdin is required for reset-password")
	}
	if subcmd == "reset-passkeys" && *passwordStdin {
		return errors.New("-password-stdin is only valid for reset-password")
	}
	cfg, err := config.Load(*configPath, false)
	if err != nil {
		return fmt.Errorf("load configuration %s: %w", *configPath, err)
	}
	clusterMutationTokenKeys, err := database.LoadClusterMutationTokenKeyFiles(cfg.Distributed.MutationTokenKeyFiles)
	if err != nil {
		return fmt.Errorf("load cluster mutation-token keyring: %w", err)
	}
	store, err := database.Open(cfg.Database.Path, database.WithClusterMutationTokenKeys(clusterMutationTokenKeys...))
	if err != nil {
		return fmt.Errorf("open database %s: %w", cfg.Database.Path, err)
	}
	defer store.Close()

	ctx := context.Background()
	user, err := store.UserByName(ctx, *username)
	if err != nil {
		return fmt.Errorf("user %q not found: %w", *username, err)
	}

	switch subcmd {
	case "reset-password":
		password, err := readPasswordLine(stdin)
		if err != nil {
			return err
		}
		hash, err := auth.HashPassword(password)
		if err != nil {
			return fmt.Errorf("hash password: %w", err)
		}
		if err := store.ResetPasswordAndSessions(ctx, user.ID, hash); err != nil {
			return fmt.Errorf("update password: %w", err)
		}
		_ = store.AddAudit(ctx, model.AuditEntry{
			Username:  "CLI",
			Action:    "password_reset_by_cli",
			Object:    "user",
			Detail:    "password reset for user " + user.Username,
			Succeeded: true,
		})
		fmt.Fprintf(stdout, "Successfully reset password and enabled password login for user %q\n", user.Username)
		return nil

	case "reset-passkeys":
		if err := store.ResetPasskeysAndEnablePassword(ctx, user.ID); err != nil {
			return fmt.Errorf("reset passkeys and enable password login: %w", err)
		}
		_ = store.AddAudit(ctx, model.AuditEntry{
			Username:  "CLI",
			Action:    "passkey_reset_by_cli",
			Object:    "user",
			Detail:    "all passkeys cleared and password login enabled for user " + user.Username,
			Succeeded: true,
		})
		fmt.Fprintf(stdout, "Successfully cleared all passkeys and re-enabled password login for user %q\n", user.Username)
		return nil
	}
	return nil
}

func readPasswordLine(reader io.Reader) (string, error) {
	data, err := io.ReadAll(io.LimitReader(reader, 1026))
	if err != nil {
		return "", fmt.Errorf("read password from standard input: %w", err)
	}
	if len(data) > 1025 {
		return "", errors.New("password from standard input exceeds 1024 bytes")
	}
	password := strings.TrimSuffix(string(data), "\n")
	password = strings.TrimSuffix(password, "\r")
	if len(password) > auth.MaxPasswordBytes {
		return "", fmt.Errorf("password from standard input exceeds %d bytes", auth.MaxPasswordBytes)
	}
	if strings.ContainsAny(password, "\r\n") {
		return "", errors.New("password input must contain exactly one line")
	}
	if password == "" {
		return "", errors.New("password from standard input is empty")
	}
	return password, nil
}
