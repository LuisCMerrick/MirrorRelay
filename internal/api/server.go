// Package api provides the management API and web handlers.
package api

import (
	"context"
	"errors"
	"io/fs"
	"net/http"
	"strings"
	"sync"
	"time"

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
)

type Store interface {
	UserByName(context.Context, string) (model.User, error)
	UpdatePassword(context.Context, int64, string) error
	CreateUser(context.Context, string, string) error
	ListUsers(context.Context) ([]model.User, error)
	DeleteUser(context.Context, int64) error
	PutSession(context.Context, string, int64, string, string, time.Time) error
	GetSession(context.Context, string) (int64, string, string, time.Time, error)
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
	UpdateClusterNodeStatus(context.Context, int64, string, string, string, string, int, []string, string, time.Time) error
	DeleteClusterNode(context.Context, int64) error
	SetClusterNodeEnabled(context.Context, int64, bool) error
	ClusterSetting(context.Context, string) (string, bool, error)
	PutClusterSetting(context.Context, string, string) error
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
	sessions       *auth.Sessions
	loginLimiter   *auth.LoginLimiter
	adminCIDRs     security.CIDRList
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
		web:           web,
		build:         build,
		started:       time.Now(),
	}
	if cfg.Distributed.Enabled || cfg.Distributed.Role != "standalone" {
		srv.clusterRouter = cluster.NewRouter(cfg)
		srv.clusterMetrics = cluster.NewMetrics()
		srv.clusterChecker = cluster.NewChecker(cfg, store, srv.clusterRouter, srv.clusterMetrics, &auditRecorderAdapter{server: srv})
		if store != nil {
			if nodes, err := store.ListClusterNodes(context.Background()); err == nil {
				srv.clusterRouter.SetNodes(nodes)
			}
		}
	}
	return srv, nil
}

func (s *Server) SetCluster(router *cluster.Router, checker *cluster.Checker, metrics *cluster.Metrics) {
	s.clusterRouter = router
	s.clusterChecker = checker
	s.clusterMetrics = metrics
}

func (s *Server) StartCluster(ctx context.Context) {
	if s.clusterChecker != nil && s.cfg.Distributed.Role == "coordinator" {
		s.clusterChecker.Start(ctx)
	}
}

func (s *Server) publishActiveRouting() error {
	repositories, available := s.upstreamNginx.ActiveRepositories()
	if !available {
		return errors.New("active routing snapshot is unavailable")
	}
	s.registry.Replace(repositories)
	return nil
}

func (s *Server) Handler(proxy http.Handler) http.Handler {
	adminMux := http.NewServeMux()
	adminMux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, map[string]string{"status": "ok"}) })
	adminMux.Handle("/metrics", s.adminAccess(http.HandlerFunc(s.metrics)))
	adminMux.Handle(s.cfg.Admin.Path, s.adminAccess(securityHeaders(http.HandlerFunc(s.webHandler))))
	adminMux.Handle(s.cfg.AdminAPIPath(), s.adminAccess(securityHeaders(http.HandlerFunc(s.apiHandler))))

	adminHost := strings.ToLower(strings.TrimSuffix(s.cfg.Admin.Host, "."))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
			if !s.verifyClusterToken(r) {
				writeError(w, http.StatusUnauthorized, "invalid cluster token")
				return
			}
			if r.URL.Path == "/api/v1/cluster/manifest" {
				var gen int64 = 1
				if s.cache != nil {
					gen = s.cache.GlobalGeneration()
				}
				manifest := cluster.GenerateManifest(s.cfg, s.registry.List(), s.build, gen)
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
