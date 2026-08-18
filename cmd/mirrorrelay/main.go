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
	version        = "0.0.1"
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
	store, err := database.Open(cfg.Database.Path)
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
			if _, err := store.GetClusterNodeByURL(context.Background(), seed.URL); err != nil {
				_, _ = store.CreateClusterNode(context.Background(), model.ClusterNode{
					Name:     seed.Name,
					URL:      seed.URL,
					Region:   seed.Region,
					Country:  seed.Country,
					Priority: seed.Priority,
					Weight:   seed.Weight,
					Enabled:  seed.Enabled,
				})
			}
		}
	}

	if err := bootstrapAdmin(context.Background(), store, cfg, dev); err != nil {
		return err
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
	upstreamNginxController.SetActivePublisher(registry.Replace)
	engine := proxy.New(cfg, registry, cacheManager, metric, accessLogger)
	defer engine.CloseIdleConnections()
	control, err := api.New(cfg, fileConfig, store, registry, cacheManager, metric, checker, upstreamNginxController, webassets.FS(), build)
	if err != nil {
		return err
	}
	handler := control.Handler(engine)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	metric.StartPersistence(ctx)
	cacheManager.StartReclaimer(ctx)
	control.StartCluster(ctx)
	restartChannel := make(chan struct{}, 1)
	control.SetRestartTrigger(func() {
		select {
		case restartChannel <- struct{}{}:
		default:
		}
	})
	return runProduct(ctx, cancel, cfg, handler, engine, checker, upstreamNginxController, registry, metric, restartChannel)
}

func applyStoredWebSettings(ctx context.Context, store *database.Store, base config.Config) (config.Config, error) {
	applied := base
	raw, found, err := store.Setting(ctx, config.WebSettingsKey)
	if err != nil {
		return base, fmt.Errorf("read Web UI configuration: %w", err)
	}
	if found {
		settings, err := config.DecodeWebSettings([]byte(raw))
		if err != nil {
			return base, fmt.Errorf("decode Web UI configuration: %w", err)
		}
		candidate, err := settings.Apply(base)
		if err != nil {
			return base, fmt.Errorf("validate Web UI configuration: %w", err)
		}
		applied = candidate
	}
	if rawApp, foundApp, err := store.Setting(ctx, database.AppearanceSettingsKey); err == nil && foundApp {
		var appConfig model.UIEnhancementConfig
		if err := json.Unmarshal([]byte(rawApp), &appConfig); err == nil {
			if err := config.ValidateUIEnhancement(&appConfig); err == nil {
				applied.UIEnhancement = appConfig
			}
		}
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
	engine *proxy.Engine,
	checker *health.Checker,
	upstreamNginxController *upstreamnginx.Controller,
	registry *mirror.Registry,
	metric *stats.Stats,
	restartChannel <-chan struct{},
) error {
	listener, err := ipc.ListenLocal(cfg.Server.UnixSocketEnabled, cfg.Server.FrontendSocket, cfg.Server.FrontendSocketMode, cfg.Server.LocalPort)
	if err != nil {
		return fmt.Errorf("create frontend socket: %w", err)
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

func bootstrapAdmin(ctx context.Context, store *database.Store, cfg config.Config, dev bool) error {
	count, err := store.CountUsers(ctx)
	if err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	if cfg.Admin.InitialPassword == "" {
		return errors.New("no administrator exists: set MIRRORRELAY_ADMIN_PASSWORD for the first startup")
	}
	hash, err := auth.HashPassword(cfg.Admin.InitialPassword)
	if err != nil {
		return fmt.Errorf("initial administrator password: %w", err)
	}
	if err := store.CreateUser(ctx, cfg.Admin.InitialUsername, hash, "admin"); err != nil {
		return err
	}
	if dev {
		slog.Warn("development administrator created", "username", cfg.Admin.InitialUsername, "password", cfg.Admin.InitialPassword)
	} else {
		slog.Info("initial administrator created", "username", cfg.Admin.InitialUsername)
	}
	return nil
}
