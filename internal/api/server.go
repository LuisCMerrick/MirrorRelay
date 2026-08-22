// Package api provides the management API and web handlers.
package api

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/LuisCMerrick/MirrorRelay/internal/appearance"
	"github.com/LuisCMerrick/MirrorRelay/internal/auth"
	"github.com/LuisCMerrick/MirrorRelay/internal/buildinfo"
	"github.com/LuisCMerrick/MirrorRelay/internal/cachectl"
	"github.com/LuisCMerrick/MirrorRelay/internal/cluster"
	"github.com/LuisCMerrick/MirrorRelay/internal/config"
	"github.com/LuisCMerrick/MirrorRelay/internal/health"
	"github.com/LuisCMerrick/MirrorRelay/internal/mirror"
	"github.com/LuisCMerrick/MirrorRelay/internal/model"
	"github.com/LuisCMerrick/MirrorRelay/internal/security"
	"github.com/LuisCMerrick/MirrorRelay/internal/stats"
	"github.com/LuisCMerrick/MirrorRelay/internal/upstreamnginx"
	"github.com/LuisCMerrick/MirrorRelay/internal/warmup"
	"github.com/LuisCMerrick/MirrorRelay/internal/webhook"
)

type Store interface {
	CountUsers(context.Context) (int, error)
	CreateInitialAdmin(context.Context, string, string) (model.User, bool, error)
	UserByName(context.Context, string) (model.User, error)
	UpdatePassword(context.Context, int64, string) error
	CreateUser(context.Context, string, string, string) error
	ListUsers(context.Context) ([]model.User, error)
	DeleteUser(context.Context, int64) error
	PutSession(context.Context, string, int64, string, string, string, time.Time) error
	GetSession(context.Context, string) (int64, string, string, string, time.Time, error)
	DeleteSession(context.Context, string) error
	DeleteUserSessions(context.Context, int64, ...string) error
	CreateMirror(context.Context, model.Mirror) (model.Mirror, error)
	UpdateMirror(context.Context, model.Mirror) (model.Mirror, error)
	ListMirrors(context.Context) ([]model.Mirror, error)
	Mirror(context.Context, int64) (model.Mirror, error)
	DeleteMirror(context.Context, int64) error
	SetMirrorEnabled(context.Context, int64, bool) error
	AddAudit(context.Context, model.AuditEntry) error
	ListAudit(context.Context, int) ([]model.AuditEntry, error)
	ListCustomConfigs(context.Context) ([]model.CustomConfig, error)
	CustomConfig(context.Context, int64) (model.CustomConfig, error)
	CreateCustomConfig(context.Context, model.CustomConfig) (model.CustomConfig, error)
	UpdateCustomConfig(context.Context, model.CustomConfig) (model.CustomConfig, error)
	DeleteCustomConfig(context.Context, int64) error
	ListPurgeJobs(context.Context, int) ([]model.PurgeJob, error)
	Setting(context.Context, string) (string, bool, error)
	PutSetting(context.Context, string, string) error
	DeleteSetting(context.Context, string) error
	ListClusterNodes(context.Context) ([]model.ClusterNode, error)
	GetClusterNode(context.Context, int64) (model.ClusterNode, error)
	GetClusterNodeByURL(context.Context, string) (model.ClusterNode, error)
	CreateClusterNode(context.Context, model.ClusterNode) (model.ClusterNode, error)
	UpdateClusterNode(context.Context, model.ClusterNode) (model.ClusterNode, error)
	UpdateClusterNodeStatus(context.Context, model.ClusterNode) error
	DeleteClusterNode(context.Context, int64) error
	SetClusterNodeEnabled(context.Context, int64, bool) error
	ClusterSetting(context.Context, string) (string, bool, error)
	PutClusterSetting(context.Context, string, string) error
	ListWarmupJobs(context.Context) ([]model.WarmupJob, error)
	GetWarmupJob(context.Context, int64) (model.WarmupJob, error)
	CreateWarmupJob(context.Context, model.WarmupJob) (model.WarmupJob, error)
	UpdateWarmupJob(context.Context, model.WarmupJob) (model.WarmupJob, error)
	DeleteWarmupJob(context.Context, int64) error
	UpdateWarmupJobProgress(ctx context.Context, id int64, status string, total, completed, failed int, downloadedBytes int64, errMsg, lastRun, nextRun string) error
	UpdateWarmupJobSchedule(context.Context, int64, string) error
}

type Server struct {
	cfg            config.Config
	fileConfig     config.Config
	store          Store
	registry       *mirror.Registry
	cache          *cachectl.Manager
	stats          *stats.Stats
	checker        *health.Checker
	upstreamNginx  *upstreamnginx.Controller
	clusterRouter  *cluster.Router
	clusterChecker *cluster.Checker
	clusterMetrics *cluster.Metrics
	clusterSync    *cluster.SyncManager
	clusterEpoch   string
	warmupEngine   *warmup.Engine
	sessions       *auth.Sessions
	loginLimiter   *auth.LoginLimiter
	adminCIDRs     security.CIDRList
	webhook        *webhook.Dispatcher
	appearance     *appearance.Store
	web            fs.FS
	build          buildinfo.Info
	started        time.Time
	mutationMu     sync.Mutex
	restartMu      sync.RWMutex
	restartTrigger func()
}

func (s *Server) SetRestartTrigger(trigger func()) {
	s.restartMu.Lock()
	defer s.restartMu.Unlock()
	s.restartTrigger = trigger
}

func (s *Server) triggerRestart() {
	s.restartMu.RLock()
	trigger := s.restartTrigger
	s.restartMu.RUnlock()
	if trigger != nil {
		trigger()
	}
}

func New(cfg, fileConfig config.Config, store Store, registry *mirror.Registry, cacheManager *cachectl.Manager, metric *stats.Stats, checker *health.Checker, upstreamNginx *upstreamnginx.Controller, web fs.FS, build buildinfo.Info) (*Server, error) {
	cidrs, err := security.ParseCIDRs(cfg.Security.AdminCIDRs)
	if err != nil {
		return nil, err
	}
	srv := &Server{
		cfg:           cfg,
		fileConfig:    fileConfig,
		store:         store,
		registry:      registry,
		cache:         cacheManager,
		stats:         metric,
		checker:       checker,
		sessions:      auth.NewSessionsWithPath(store, cfg.Security.SessionTimeout, cfg.Admin.Path),
		loginLimiter:  auth.NewLoginLimiter(cfg.Security.LoginWindow, cfg.Security.LoginMaxFailures),
		upstreamNginx: upstreamNginx,
		adminCIDRs:    cidrs,
		webhook:       webhook.New(cfg.Webhook),
		appearance:    appearance.New(cfg.UIEnhancement),
		web:           web,
		build:         build,
		started:       time.Now(),
	}
	if store != nil {
		srv.warmupEngine = warmup.NewEngine(cfg, store, &auditRecorderAdapter{server: srv})
	}
	if cfg.Distributed.Enabled {
		srv.clusterRouter = cluster.NewRouter(cfg)
		srv.clusterMetrics = cluster.NewMetrics()
		srv.clusterChecker = cluster.NewChecker(cfg, store, srv.clusterRouter, srv.clusterMetrics, &auditRecorderAdapter{server: srv})
		if cfg.Distributed.Role == "coordinator" {
			epoch, epochErr := cluster.EnsureCoordinatorEpoch(context.Background(), store)
			if epochErr != nil {
				return nil, epochErr
			}
			srv.clusterEpoch = epoch
			srv.clusterSync = cluster.NewSyncManager(cfg, store, epoch)
			if repositories, custom, available := srv.activeClusterConfiguration(); available {
				manifest := srv.clusterManifestForConfiguration(repositories, custom)
				if err := srv.clusterChecker.SetExpectedConfiguration(context.Background(), manifest); err != nil {
					return nil, fmt.Errorf("persist Coordinator cluster state: %w", err)
				}
			}
		}
		if store != nil {
			if nodes, err := store.ListClusterNodes(context.Background()); err == nil {
				srv.clusterRouter.SetNodes(nodes)
			}
		}
	}
	return srv, nil
}

func (s *Server) SetAppearanceStore(store *appearance.Store) {
	if store != nil {
		s.appearance = store
	}
}

func (s *Server) appearanceConfig() model.UIEnhancementConfig {
	if s.appearance != nil {
		return s.appearance.Load()
	}
	return s.cfg.UIEnhancement
}

func (s *Server) StartWarmup(ctx context.Context) {
	if s.warmupEngine != nil {
		s.warmupEngine.Start(ctx)
	}
}

func (s *Server) StopWarmup() {
	if s.warmupEngine != nil {
		s.warmupEngine.Stop()
	}
}

func (s *Server) SetCluster(router *cluster.Router, checker *cluster.Checker, metrics *cluster.Metrics) {
	s.clusterRouter = router
	s.clusterChecker = checker
	s.clusterMetrics = metrics
}

func (s *Server) StartCluster(ctx context.Context) {
	if s.cfg.Distributed.Enabled && s.clusterChecker != nil && s.cfg.Distributed.Role == "coordinator" {
		s.clusterChecker.Start(ctx)
	}
}

func (s *Server) publishActiveRouting() error {
	repositories, available := s.upstreamNginx.ActiveRepositories()
	if !available {
		return errors.New("active routing snapshot is unavailable")
	}
	s.PublishActiveRepositories(repositories)
	return nil
}

func (s *Server) PublishActiveRepositories(repositories []model.Mirror) {
	s.registry.Replace(repositories)
	if s.cfg.Distributed.Enabled && s.clusterChecker != nil && s.cfg.Distributed.Role == "coordinator" {
		_, custom, available := s.activeClusterConfiguration()
		if !available {
			slog.Error("persist Coordinator cluster state", "error", "active configuration snapshot is unavailable")
			return
		}
		manifest := s.clusterManifestForConfiguration(repositories, custom)
		if err := s.clusterChecker.SetExpectedConfiguration(context.Background(), manifest); err != nil {
			slog.Error("persist Coordinator cluster state", "error", err)
		}
	}
}

func (s *Server) activeClusterConfiguration() ([]model.Mirror, []model.CustomConfig, bool) {
	if s.upstreamNginx == nil {
		return nil, nil, false
	}
	return s.upstreamNginx.ActiveConfiguration()
}

func (s *Server) clusterManifestForConfiguration(repositories []model.Mirror, custom []model.CustomConfig) model.ClusterManifest {
	generation := int64(0)
	if s.upstreamNginx != nil {
		generation = s.upstreamNginx.Status().CurrentConfigVersion
	}
	coordinatorID, coordinatorEpoch := "", ""
	if s.cfg.Distributed.Role == "coordinator" {
		coordinatorID = strings.TrimSpace(s.cfg.Distributed.Node.Name)
		coordinatorEpoch = s.clusterEpoch
	} else if state, found, err := cluster.LoadEdgeSyncState(context.Background(), s.store); err == nil && found {
		coordinatorID = state.CoordinatorID
		coordinatorEpoch = state.CoordinatorEpoch
		generation = state.ConfigGeneration
	}
	return cluster.GenerateManifest(s.cfg, repositories, custom, s.build, generation, coordinatorID, coordinatorEpoch)
}

func (s *Server) Handler(proxy http.Handler) http.Handler {
	if s.appearance == nil {
		s.appearance = appearance.New(s.cfg.UIEnhancement)
	}
	adminMux := http.NewServeMux()
	adminMux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, map[string]string{"status": "ok"}) })
	adminMux.Handle("/metrics", s.adminAccess(http.HandlerFunc(s.metrics)))
	adminMux.Handle(s.cfg.Admin.Path, s.adminAccess(securityHeaders(http.HandlerFunc(s.webHandler))))
	adminMux.Handle(s.cfg.AdminAPIPath(), s.adminAccess(securityHeaders(http.HandlerFunc(s.apiHandler))))

	adminHost := strings.ToLower(strings.TrimSuffix(s.cfg.Admin.Host, "."))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == cluster.SyncApplyPath || r.URL.Path == cluster.SyncPurgePath {
			s.clusterSyncReceiver(w, r)
			return
		}
		reqHost := requestHostname(r.Host)
		if adminHost != "" {
			if reqHost == adminHost {
				if r.URL.Path == "/healthz" || r.URL.Path == "/metrics" ||
					strings.HasPrefix(r.URL.Path, s.cfg.Admin.Path) ||
					strings.HasPrefix(r.URL.Path, s.cfg.AdminAPIPath()) ||
					r.URL.Path == strings.TrimSuffix(s.cfg.Admin.Path, "/") {
					adminMux.ServeHTTP(w, r)
					return
				}
				http.NotFound(w, r)
				return
			}
			if strings.HasPrefix(r.URL.Path, s.cfg.Admin.Path) ||
				strings.HasPrefix(r.URL.Path, s.cfg.AdminAPIPath()) ||
				r.URL.Path == strings.TrimSuffix(s.cfg.Admin.Path, "/") {
				http.NotFound(w, r)
				return
			}
		}

		if r.URL.Path == "/api/v1/cluster/manifest" || r.URL.Path == "/api/v1/cluster/health" {
			if !s.cfg.Distributed.Enabled || s.cfg.Distributed.Token == "" {
				http.NotFound(w, r)
				return
			}
			if !s.verifyClusterProbeToken(r) {
				writeError(w, http.StatusUnauthorized, "invalid cluster token")
				return
			}
			if r.URL.Path == "/api/v1/cluster/manifest" {
				repositories, custom, available := s.activeClusterConfiguration()
				if !available {
					repositories = s.registry.List()
					custom = []model.CustomConfig{}
				}
				manifest := s.clusterManifestForConfiguration(repositories, custom)
				writeJSON(w, http.StatusOK, manifest)
				return
			}
			if r.URL.Path == "/api/v1/cluster/health" {
				writeJSON(w, http.StatusOK, s.clusterHealth())
				return
			}
		}

		if r.URL.Path == "/healthz" || r.URL.Path == "/metrics" ||
			strings.HasPrefix(r.URL.Path, s.cfg.Admin.Path) ||
			strings.HasPrefix(r.URL.Path, s.cfg.AdminAPIPath()) ||
			r.URL.Path == strings.TrimSuffix(s.cfg.Admin.Path, "/") {
			adminMux.ServeHTTP(w, r)
			return
		}

		s.publicHandler(proxy).ServeHTTP(w, r)
	})
}
