package api

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/LuisCMerrick/RepoGate/internal/auth"
	"github.com/LuisCMerrick/RepoGate/internal/buildinfo"
	"github.com/LuisCMerrick/RepoGate/internal/cachectl"
	"github.com/LuisCMerrick/RepoGate/internal/cluster"
	"github.com/LuisCMerrick/RepoGate/internal/config"
	"github.com/LuisCMerrick/RepoGate/internal/database"
	"github.com/LuisCMerrick/RepoGate/internal/health"
	"github.com/LuisCMerrick/RepoGate/internal/mirror"
	"github.com/LuisCMerrick/RepoGate/internal/model"
	"github.com/LuisCMerrick/RepoGate/internal/profile"
	"github.com/LuisCMerrick/RepoGate/internal/security"
	"github.com/LuisCMerrick/RepoGate/internal/stats"
	"github.com/LuisCMerrick/RepoGate/internal/upstreamnginx"
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

type auditRecorderAdapter struct {
	server *Server
}

func (a *auditRecorderAdapter) Record(user, action, object, detail string, ok bool) {
	if a.server != nil && a.server.store != nil {
		_ = a.server.store.AddAudit(context.Background(), model.AuditEntry{
			Time:      time.Now(),
			Username:  user,
			ClientIP:  "127.0.0.1",
			Action:    action,
			Object:    object,
			Detail:    detail,
			Succeeded: ok,
		})
	}
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

func (s *Server) verifyClusterToken(r *http.Request) bool {
	if s.cfg.Distributed.Token == "" {
		return true
	}
	hdr := r.Header.Get("X-RepoGate-Cluster-Token")
	if hdr == "" {
		authHdr := r.Header.Get("Authorization")
		if strings.HasPrefix(authHdr, "Bearer ") {
			hdr = strings.TrimPrefix(authHdr, "Bearer ")
		}
	}
	return subtle.ConstantTimeCompare([]byte(hdr), []byte(s.cfg.Distributed.Token)) == 1
}

func (s *Server) clusterHealth() model.ClusterHealth {
	hStatus := "healthy"
	repoHealth := make(map[string]bool)
	for _, m := range s.registry.List() {
		if !m.Enabled {
			continue
		}
		viable := repositoryHealthState(m) != "unhealthy"
		repoHealth[m.Slug] = viable
		if !viable {
			hStatus = "degraded"
		}
	}
	return model.ClusterHealth{
		Status:            hStatus,
		Version:           s.build.Version,
		ConfigFingerprint: cluster.CanonicalFingerprint(s.registry.List()),
		Repositories:      repoHealth,
	}
}

func requestHostname(raw string) string {
	if host, _, err := net.SplitHostPort(raw); err == nil {
		raw = host
	}
	return strings.ToLower(strings.Trim(strings.TrimSuffix(raw, "."), "[]"))
}

func (s *Server) publicHandler(proxy http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.cfg.Distributed.Role == "coordinator" {
			if r.URL.Path == "/" && !s.hostRepository(r.Host) {
				s.repositoryIndex(w, r)
				return
			}
			repo, _, matched := s.registry.Route(r.Host, r.URL.Path)
			if matched {
				if repo.Type == "docker-registry" || repo.Type == "oci-registry" {
					writeJSON(w, http.StatusNotImplemented, map[string]string{
						"error":   "distributed_registry_not_supported",
						"message": "Distributed Registry Not Supported",
					})
					return
				}
				clientIP := security.RequestClientIP(r)
				fp := ""
				if s.clusterChecker != nil {
					fp = s.clusterChecker.ClusterFingerprint()
				}
				if s.clusterRouter != nil {
					node, err := s.clusterRouter.SelectNode(clientIP, repo, fp)
					if err != nil {
						if s.clusterMetrics != nil {
							s.clusterMetrics.IncNoAvailableEdge()
						}
						writeJSON(w, http.StatusServiceUnavailable, map[string]string{
							"error":   "no_available_edge",
							"message": "No healthy RepoGate edge node is available",
						})
						return
					}
					if s.clusterMetrics != nil {
						s.clusterMetrics.IncRedirect(node.Name, node.Region)
					}
					dest := strings.TrimRight(node.URL, "/") + r.URL.Path
					if r.URL.RawQuery != "" {
						dest += "?" + r.URL.RawQuery
					}
					http.Redirect(w, r, dest, http.StatusTemporaryRedirect)
					return
				}
			}
		}

		if r.URL.Path != "/" || s.hostRepository(r.Host) {
			proxy.ServeHTTP(w, r)
			return
		}
		s.repositoryIndex(w, r)
	})
}

func (s *Server) hostRepository(requestHost string) bool {
	if host, _, err := net.SplitHostPort(requestHost); err == nil {
		requestHost = host
	}
	requestHost = strings.ToLower(strings.Trim(requestHost, "[]"))
	for _, repository := range s.registry.List() {
		if repository.Enabled && repository.PublicMode == "host" && strings.EqualFold(repository.PublicHost, requestHost) {
			return true
		}
	}
	return false
}

func (s *Server) repositoryIndex(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	allowAdministrative := s.adminCIDRs.Allows(security.RequestClientIP(r))
	repositories := s.registry.List()
	sort.Slice(repositories, func(i, j int) bool {
		return strings.ToLower(repositories[i].Name) < strings.ToLower(repositories[j].Name)
	})
	var body strings.Builder
	body.WriteString(`<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>RepoGate Repository Index</title><style>body{font:15px/1.5 system-ui,sans-serif;max-width:1100px;margin:3rem auto;padding:0 1.25rem;color:#20242b}h1{margin-bottom:.25rem}p{color:#667085}table{width:100%;border-collapse:collapse;margin-top:2rem}th,td{text-align:left;padding:.7rem;border-bottom:1px solid #dfe3e8}a{color:#0969da;text-decoration:none}a:hover{text-decoration:underline}code{font-family:ui-monospace,monospace}@media(prefers-color-scheme:dark){body{background:#11151b;color:#e6edf3}p{color:#9da7b3}th,td{border-color:#30363d}a{color:#58a6ff}}</style></head><body><h1>RepoGate Repository Index</h1><p>Available repositories / 可用仓库</p><table><thead><tr><th>Repository / 仓库</th><th>Type / 类型</th><th>Description / 说明</th></tr></thead><tbody>`)
	visible := 0
	for _, repository := range repositories {
		if !repository.Enabled || (repository.AccessPolicy == "admin" && !allowAdministrative) {
			continue
		}
		href := repository.PublicPath
		label := repository.PublicPath
		if repository.PublicMode == "host" {
			href = "https://" + repository.PublicHost + "/"
			label = repository.PublicHost + "/"
		} else if href == "" {
			href = "/" + repository.Slug + "/"
			label = href
		}
		fmt.Fprintf(&body, `<tr><td><a href="%s"><strong>%s</strong></a><br><code>%s</code></td><td>%s</td><td>%s</td></tr>`,
			html.EscapeString(href), html.EscapeString(repository.Name), html.EscapeString(label),
			html.EscapeString(repository.Type), html.EscapeString(repository.Description))
		visible++
	}
	if visible == 0 {
		body.WriteString(`<tr><td colspan="3">No repositories are currently available. / 当前没有可用仓库。</td></tr>`)
	}
	body.WriteString(`</tbody></table></body></html>`)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Content-Length", strconv.Itoa(body.Len()))
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodGet {
		_, _ = io.WriteString(w, body.String())
	}
}

func (s *Server) adminAccess(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.adminCIDRs.Allows(security.RequestClientIP(r)) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) webHandler(w http.ResponseWriter, r *http.Request) {
	adminPath := s.cfg.Admin.Path
	if r.URL.Path == strings.TrimSuffix(adminPath, "/") {
		http.Redirect(w, r, adminPath, http.StatusPermanentRedirect)
		return
	}
	if r.URL.Path == adminPath {
		r2 := r.Clone(r.Context())
		// FileServer serves index.html for a directory request. Passing
		// /index.html directly would trigger its canonical redirect to ./,
		// which resolves back to the administration root and can loop.
		r2.URL.Path = "/"
		http.FileServer(http.FS(s.web)).ServeHTTP(w, r2)
		return
	}
	http.StripPrefix(adminPath, http.FileServer(http.FS(s.web))).ServeHTTP(w, r)
}

func (s *Server) apiHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	path := strings.TrimPrefix(r.URL.Path, strings.TrimSuffix(s.cfg.AdminAPIPath(), "/"))
	if path == "/auth/login" && r.Method == http.MethodPost {
		s.login(w, r)
		return
	}
	session, ok := s.sessions.Get(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		provided := r.Header.Get("X-CSRF-Token")
		if len(provided) != len(session.CSRFToken) || subtle.ConstantTimeCompare([]byte(provided), []byte(session.CSRFToken)) != 1 {
			writeError(w, http.StatusForbidden, "invalid CSRF token")
			return
		}
		s.mutationMu.Lock()
		defer s.mutationMu.Unlock()
	}
	switch {
	case path == "/auth/session" && r.Method == http.MethodGet:
		writeJSON(w, 200, map[string]any{"username": session.Username, "csrf_token": session.CSRFToken})
	case path == "/auth/logout" && r.Method == http.MethodPost:
		_ = s.audit(r, session.Username, "logout", "session", "", true)
		s.sessions.Delete(r)
		s.sessions.ClearCookie(w)
		writeJSON(w, 200, map[string]bool{"ok": true})
	case path == "/auth/password" && r.Method == http.MethodPut:
		s.password(w, r, session)
	case path == "/users" && r.Method == http.MethodGet:
		users, err := s.store.ListUsers(r.Context())
		if err != nil {
			writeInternal(w, err)
			return
		}
		writeJSON(w, 200, users)
	case path == "/users" && r.Method == http.MethodPost:
		s.createUser(w, r, session)
	case strings.HasPrefix(path, "/users/") && r.Method == http.MethodDelete:
		s.deleteUser(w, r, session, strings.TrimPrefix(path, "/users/"))
	case path == "/mirrors" && r.Method == http.MethodGet:
		mirrors, err := s.store.ListMirrors(r.Context())
		if err != nil {
			writeInternal(w, err)
			return
		}
		writeJSON(w, 200, mirrors)
	case path == "/mirrors" && r.Method == http.MethodPost:
		s.createMirror(w, r, session)
	case strings.HasPrefix(path, "/mirrors/"):
		s.mirrorAction(w, r, session, strings.TrimPrefix(path, "/mirrors/"))
	case path == "/cache" && r.Method == http.MethodGet:
		summary := s.cache.Summary()
		jobs, err := s.store.ListPurgeJobs(r.Context(), 100)
		if err != nil {
			writeInternal(w, err)
			return
		}
		summary["purge_jobs"] = jobs
		writeJSON(w, 200, summary)
	case path == "/cache" && r.Method == http.MethodDelete:
		s.clearCache(w, r, session, 0)
	case path == "/stats" && r.Method == http.MethodGet:
		s.dashboard(w, r)
	case path == "/health" && r.Method == http.MethodGet:
		s.healthStatus(w, r)
	case path == "/audit" && r.Method == http.MethodGet:
		entries, err := s.store.ListAudit(r.Context(), 200)
		if err != nil {
			writeInternal(w, err)
			return
		}
		writeJSON(w, 200, entries)
	case path == "/access" && r.Method == http.MethodGet:
		lines, err := readLastLines(filepath.Join(s.cfg.UpstreamNginx.LogPath, "access.log"), 200, 2<<20)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			writeInternal(w, err)
			return
		}
		writeJSON(w, 200, lines)
	case path == "/system" && r.Method == http.MethodGet:
		frontendNetwork, frontendAddress := s.cfg.FrontendEndpoint()
		upstreamNetwork, upstreamAddress := s.cfg.UpstreamEndpoint()
		writeJSON(w, 200, map[string]any{
			"version": s.build.Version, "build_id": s.build.BuildID, "git_commit": s.build.GitCommit,
			"build_timestamp": s.build.BuildTimestamp, "go_version": s.build.GoVersion,
			"target_os": s.build.TargetOS, "architecture": s.build.Architecture,
			"uptime_seconds": int64(time.Since(s.started).Seconds()), "ingress_mode": s.cfg.Ingress.Mode,
			"public_base_url": s.cfg.HTTP.PublicBaseURL, "tls_min_version": s.cfg.TLS.MinVersion,
			"https_listen": s.cfg.HTTP.HTTPSListen, "tls_certificate": s.cfg.TLS.Certificate,
			"tls_private_key": s.cfg.TLS.PrivateKey, "frontend_network": frontendNetwork,
			"frontend_address": frontendAddress, "upstream_network": upstreamNetwork,
			"upstream_address": upstreamAddress, "upstream_nginx": s.upstreamNginx.Status(),
		})
	case path == "/settings" && r.Method == http.MethodGet:
		s.webSettings(w, r)
	case path == "/settings" && r.Method == http.MethodPut:
		s.updateWebSettings(w, r, session)
	case path == "/settings" && r.Method == http.MethodDelete:
		s.resetWebSettings(w, r, session)
	case path == "/templates" && r.Method == http.MethodGet:
		writeJSON(w, 200, profile.List())
	case path == "/profiles" && r.Method == http.MethodGet:
		writeJSON(w, 200, profile.List())
	case strings.HasPrefix(path, "/profiles/") && r.Method == http.MethodGet:
		name := strings.TrimPrefix(path, "/profiles/")
		if candidate, found := profile.Find(name, r.URL.Query().Get("version")); found {
			writeJSON(w, 200, candidate)
			return
		}
		writeError(w, 404, "profile not found")
	case path == "/upstream-nginx/status" && r.Method == http.MethodGet:
		writeJSON(w, 200, s.upstreamNginx.Status())
	case path == "/upstream-nginx/test" && r.Method == http.MethodPost:
		mirrors, err := s.store.ListMirrors(r.Context())
		if err != nil {
			writeInternal(w, err)
			return
		}
		generated, result, err := s.upstreamNginx.Validate(r.Context(), mirrors)
		if err != nil {
			writeError(w, 422, validationMessage(result, err))
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "configuration_hash": generated.Hash, "validation_result": result})
	case path == "/ingress/snippet" && r.Method == http.MethodGet:
		mirrors, err := s.store.ListMirrors(r.Context())
		if err != nil {
			writeInternal(w, err)
			return
		}
		generated, err := s.upstreamNginx.Preview(r.Context(), mirrors)
		if err != nil {
			writeInternal(w, err)
			return
		}
		network, address := s.cfg.FrontendEndpoint()
		writeJSON(w, 200, map[string]any{"frontend_network": network, "frontend_address": address, "mode": s.cfg.Ingress.Mode, "configuration": generated.Files["external-nginx-integration.conf"]})
	case path == "/upstream-nginx/config" && r.Method == http.MethodGet:
		value, err := s.upstreamNginx.EffectiveConfig(r.Context())
		if err != nil {
			writeError(w, 404, err.Error())
			return
		}
		writeJSON(w, 200, map[string]string{"configuration": value})
	case path == "/upstream-nginx/history" && r.Method == http.MethodGet:
		values, err := s.upstreamNginx.History(r.Context())
		if err != nil {
			writeInternal(w, err)
			return
		}
		writeJSON(w, 200, values)
	case path == "/upstream-nginx/reload" && r.Method == http.MethodPost:
		v, err := s.upstreamNginx.Reconcile(r.Context(), session.Username, "manual reconcile")
		if err != nil {
			_ = s.audit(r, session.Username, "upstream_nginx_reload", "managed-upstream-nginx", err.Error(), false)
			writeError(w, 422, err.Error())
			return
		}
		if err := s.publishActiveRouting(); err != nil {
			writeInternal(w, err)
			return
		}
		_ = s.audit(r, session.Username, "upstream_nginx_reload", "managed-upstream-nginx", fmt.Sprintf("version %d", v.Version), true)
		writeJSON(w, 200, v)
	case strings.HasPrefix(path, "/upstream-nginx/history/") && strings.HasSuffix(path, "/rollback") && r.Method == http.MethodPost:
		raw := strings.TrimSuffix(strings.TrimPrefix(path, "/upstream-nginx/history/"), "/rollback")
		version, err := strconv.ParseInt(strings.Trim(raw, "/"), 10, 64)
		if err != nil {
			writeError(w, 400, "invalid configuration version")
			return
		}
		v, err := s.upstreamNginx.Rollback(r.Context(), version, session.Username)
		if err != nil {
			_ = s.audit(r, session.Username, "config_rollback", "managed-upstream-nginx", err.Error(), false)
			writeError(w, 422, err.Error())
			return
		}
		if err := s.publishActiveRouting(); err != nil {
			writeInternal(w, err)
			return
		}
		_ = s.audit(r, session.Username, "config_rollback", "managed-upstream-nginx", fmt.Sprintf("version %d", version), true)
		writeJSON(w, 200, v)
	case path == "/custom-configs" && r.Method == http.MethodGet:
		values, err := s.store.ListCustomConfigs(r.Context())
		if err != nil {
			writeInternal(w, err)
			return
		}
		writeJSON(w, 200, values)
	case path == "/custom-configs" && r.Method == http.MethodPost:
		s.createCustomConfig(w, r, session)
	case strings.HasPrefix(path, "/custom-configs/"):
		s.customConfigAction(w, r, session, strings.TrimPrefix(path, "/custom-configs/"))
	case strings.HasPrefix(path, "/upstream-nginx/rollback/") && r.Method == http.MethodPost:
		s.rollbackConfig(w, r, session, strings.TrimPrefix(path, "/upstream-nginx/rollback/"))
	case path == "/cluster/overview" && r.Method == http.MethodGet:
		s.clusterOverview(w, r)
	case path == "/cluster/nodes" && r.Method == http.MethodGet:
		s.listClusterNodes(w, r)
	case path == "/cluster/nodes" && r.Method == http.MethodPost:
		s.createClusterNode(w, r, session)
	case strings.HasPrefix(path, "/cluster/nodes/"):
		s.clusterNodeAction(w, r, session, strings.TrimPrefix(path, "/cluster/nodes/"))
	case path == "/cluster/fingerprint/reset" && r.Method == http.MethodPost:
		s.resetClusterFingerprint(w, r, session)
	default:
		writeError(w, http.StatusNotFound, "not found")
	}
}

var webSettingsFileOnly = []string{
	"server.frontend_socket",
	"server.frontend_socket_mode",
	"runtime.*",
	"ingress.snippet_path",
	"redirect.pin_validated_ip",
	"tls.certificate",
	"tls.private_key",
	"database.path",
	"cache.path",
	"logging.path",
	"admin.*",
	"upstream_nginx.binary",
	"upstream_nginx.prefix",
	"upstream_nginx.pid",
	"upstream_nginx.log_path",
	"upstream_nginx.upstream_socket",
	"upstream_nginx.upstream_socket_mode",
	"upstream_nginx.ca_bundle",
}

func webSettingsEqual(left, right config.WebSettings) bool {
	leftJSON, leftErr := json.Marshal(left)
	rightJSON, rightErr := json.Marshal(right)
	return leftErr == nil && rightErr == nil && string(leftJSON) == string(rightJSON)
}

func (s *Server) webSettings(w http.ResponseWriter, r *http.Request) {
	current := config.WebSettingsFrom(s.cfg)
	fromFile := config.WebSettingsFrom(s.fileConfig)
	stored, found, err := s.store.Setting(r.Context(), config.WebSettingsKey)
	if err != nil {
		writeInternal(w, err)
		return
	}
	if !found {
		writeJSON(w, http.StatusOK, map[string]any{
			"settings": fromFile, "source": "configuration_file", "restart_required": !webSettingsEqual(fromFile, current), "file_only": webSettingsFileOnly,
		})
		return
	}
	settings, err := config.DecodeWebSettings([]byte(stored))
	if err != nil {
		writeInternal(w, err)
		return
	}
	if _, err := settings.Apply(s.fileConfig); err != nil {
		writeInternal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"settings": settings, "source": "web_ui", "restart_required": !webSettingsEqual(settings, current), "file_only": webSettingsFileOnly,
	})
}

func (s *Server) updateWebSettings(w http.ResponseWriter, r *http.Request, session auth.Session) {
	var input config.WebSettings
	if decodeJSON(w, r, &input) != nil {
		return
	}
	candidate, err := input.Apply(s.fileConfig)
	if err != nil {
		_ = s.audit(r, session.Username, "settings_update", "configuration", err.Error(), false)
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	normalized := config.WebSettingsFrom(candidate)
	encoded, err := json.Marshal(normalized)
	if err != nil {
		writeInternal(w, err)
		return
	}
	if err := s.store.PutSetting(r.Context(), config.WebSettingsKey, string(encoded)); err != nil {
		writeInternal(w, err)
		return
	}
	restartRequired := !webSettingsEqual(normalized, config.WebSettingsFrom(s.cfg))
	_ = s.audit(r, session.Username, "settings_update", "configuration", "saved Web UI override", true)
	writeJSON(w, http.StatusOK, map[string]any{
		"settings": normalized, "source": "web_ui", "restart_required": restartRequired, "file_only": webSettingsFileOnly,
	})
}

func (s *Server) resetWebSettings(w http.ResponseWriter, r *http.Request, session auth.Session) {
	if err := s.store.DeleteSetting(r.Context(), config.WebSettingsKey); err != nil {
		writeInternal(w, err)
		return
	}
	fromFile := config.WebSettingsFrom(s.fileConfig)
	_ = s.audit(r, session.Username, "settings_reset", "configuration", "restore configuration file values after restart", true)
	writeJSON(w, http.StatusOK, map[string]any{
		"settings": fromFile, "source": "configuration_file", "restart_required": !webSettingsEqual(fromFile, config.WebSettingsFrom(s.cfg)), "file_only": webSettingsFileOnly,
	})
}

func (s *Server) createUser(w http.ResponseWriter, r *http.Request, session auth.Session) {
	var input struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if decodeJSON(w, r, &input) != nil {
		return
	}
	input.Username = strings.TrimSpace(input.Username)
	if len(input.Username) < 3 || len(input.Username) > 64 || strings.ContainsAny(input.Username, " \t\r\n") {
		writeError(w, 400, "username must be 3..64 non-space characters")
		return
	}
	hash, err := auth.HashPassword(input.Password)
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	if err := s.store.CreateUser(r.Context(), input.Username, hash); err != nil {
		if database.IsConflict(err) {
			writeError(w, 409, "username already exists")
			return
		}
		writeInternal(w, err)
		return
	}
	_ = s.audit(r, session.Username, "user_create", "user", input.Username, true)
	writeJSON(w, 201, map[string]string{"username": input.Username})
}

func (s *Server) deleteUser(w http.ResponseWriter, r *http.Request, session auth.Session, rawID string) {
	id, err := strconv.ParseInt(strings.Trim(rawID, "/"), 10, 64)
	if err != nil {
		writeError(w, 400, "invalid user id")
		return
	}
	current, err := s.store.UserByName(r.Context(), session.Username)
	if err != nil {
		writeInternal(w, err)
		return
	}
	if current.ID == id {
		writeError(w, 409, "cannot delete the current user")
		return
	}
	if err := s.store.DeleteUser(r.Context(), id); err != nil {
		writeError(w, 404, "user not found")
		return
	}
	_ = s.audit(r, session.Username, "user_delete", "user", strconv.FormatInt(id, 10), true)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) rollbackConfig(w http.ResponseWriter, r *http.Request, session auth.Session, rawVersion string) {
	version, err := strconv.ParseInt(strings.Trim(rawVersion, "/"), 10, 64)
	if err != nil {
		writeError(w, 400, "invalid configuration version")
		return
	}
	value, err := s.upstreamNginx.Rollback(r.Context(), version, session.Username)
	if err != nil {
		_ = s.audit(r, session.Username, "config_rollback", "managed-upstream-nginx", err.Error(), false)
		writeError(w, 422, err.Error())
		return
	}
	if err := s.publishActiveRouting(); err != nil {
		writeInternal(w, err)
		return
	}
	_ = s.audit(r, session.Username, "config_rollback", "managed-upstream-nginx", fmt.Sprintf("version %d", version), true)
	writeJSON(w, 200, value)
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	ip := security.RequestClientIP(r)
	var in loginRequest
	if err := decodeJSON(w, r, &in); err != nil {
		return
	}
	key := ip + ":" + strings.TrimSpace(in.Username)
	release, allowed := s.loginLimiter.Acquire(key)
	if !allowed {
		writeError(w, http.StatusTooManyRequests, "too many login attempts")
		return
	}
	user, err := s.store.UserByName(r.Context(), strings.TrimSpace(in.Username))
	if err != nil || !auth.VerifyPassword(user.PasswordHash, in.Password) {
		release(false)
		_ = s.audit(r, in.Username, "login", "session", "invalid credentials", false)
		time.Sleep(250 * time.Millisecond)
		writeError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	release(true)
	session, err := s.sessions.Create(user.ID, user.Username)
	if err != nil {
		writeInternal(w, err)
		return
	}
	s.sessions.SetCookie(w, session)
	_ = s.audit(r, user.Username, "login", "session", "", true)
	writeJSON(w, 200, map[string]any{"username": user.Username, "csrf_token": session.CSRFToken})
}

func (s *Server) password(w http.ResponseWriter, r *http.Request, session auth.Session) {
	var in struct {
		Current  string `json:"current_password"`
		Password string `json:"new_password"`
	}
	if decodeJSON(w, r, &in) != nil {
		return
	}
	user, err := s.store.UserByName(r.Context(), session.Username)
	if err != nil || !auth.VerifyPassword(user.PasswordHash, in.Current) {
		writeError(w, 400, "current password is incorrect")
		return
	}
	hash, err := auth.HashPassword(in.Password)
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	if err = s.store.UpdatePassword(r.Context(), user.ID, hash); err != nil {
		writeInternal(w, err)
		return
	}
	if err := s.sessions.RevokeUser(r.Context(), user.ID, session.ID); err != nil {
		slog.Warn("failed to revoke user sessions on password change", "user", user.Username, "error", err)
	}
	_ = s.audit(r, session.Username, "change_password", "user", session.Username, true)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) createMirror(w http.ResponseWriter, r *http.Request, session auth.Session) {
	var m model.Mirror
	if decodeJSON(w, r, &m) != nil {
		return
	}
	if err := mirror.NormalizeAndValidate(&m, s.cfg.Security.AllowHTTPUpstream, s.cfg.Security.AllowPrivateUpstream); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	desired, err := s.store.ListMirrors(r.Context())
	if err != nil {
		writeInternal(w, err)
		return
	}
	proposed := append(desired, m)
	if err := mirror.ValidateRouteConflicts(proposed, s.cfg.Admin.Path, s.cfg.HTTP.PublicBaseURL, s.cfg.Admin.Host); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	if err := s.validateUpstreams(r.Context(), m); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	m.ConfigState, m.ConfigError = "pending", ""
	if _, result, err := s.upstreamNginx.Validate(r.Context(), proposed); err != nil {
		_ = s.audit(r, session.Username, "validate", "repository", err.Error(), false)
		writeError(w, 422, validationMessage(result, err))
		return
	}
	created, err := s.store.CreateMirror(r.Context(), m)
	if err != nil {
		if database.IsConflict(err) {
			writeError(w, 409, "mirror slug already exists")
			return
		}
		writeInternal(w, err)
		return
	}
	if _, err = s.upstreamNginx.Reconcile(r.Context(), session.Username, "create repository "+created.Slug); err != nil {
		_ = s.audit(r, session.Username, "create", "repository", err.Error(), false)
		writeError(w, 502, "repository saved as desired state but Managed Upstream Nginx activation failed: "+err.Error())
		return
	}
	if err = s.publishActiveRouting(); err != nil {
		writeInternal(w, err)
		return
	}
	_ = s.audit(r, session.Username, "create", "mirror", created.Slug, true)
	if current, ok := findMirror(s.registry.List(), created.ID); ok {
		created = current
	}
	writeJSON(w, 201, created)
}

func (s *Server) mirrorAction(w http.ResponseWriter, r *http.Request, session auth.Session, tail string) {
	parts := strings.Split(strings.Trim(tail, "/"), "/")
	id, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		writeError(w, 400, "invalid mirror id")
		return
	}
	m, err := s.store.Mirror(r.Context(), id)
	if err != nil {
		writeError(w, 404, "mirror not found")
		return
	}
	if len(parts) == 1 && r.Method == http.MethodGet {
		writeJSON(w, 200, m)
		return
	}
	if len(parts) == 2 && parts[1] == "state" && r.Method == http.MethodGet {
		active, activeFound := s.registry.GetByID(id)
		writeJSON(w, 200, map[string]any{
			"desired": m, "active": active, "active_found": activeFound,
			"effective_config_version": s.upstreamNginx.Status().CurrentConfigVersion,
			"statistics":               s.stats.Snapshot().ByMirror[id],
		})
		return
	}
	if len(parts) == 2 && parts[1] == "client-config" && r.Method == http.MethodGet {
		writeJSON(w, 200, clientExamples(s.cfg, m))
		return
	}
	if len(parts) == 1 && r.Method == http.MethodPut {
		var updated model.Mirror
		if decodeJSON(w, r, &updated) != nil {
			return
		}
		updated.ID = id
		if err := mirror.NormalizeAndValidate(&updated, s.cfg.Security.AllowHTTPUpstream, s.cfg.Security.AllowPrivateUpstream); err != nil {
			writeError(w, 400, err.Error())
			return
		}
		desired, loadErr := s.store.ListMirrors(r.Context())
		if loadErr != nil {
			writeInternal(w, loadErr)
			return
		}
		proposed := replaceCandidate(desired, updated)
		if err := mirror.ValidateRouteConflicts(proposed, s.cfg.Admin.Path, s.cfg.HTTP.PublicBaseURL, s.cfg.Admin.Host); err != nil {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		if err := s.validateUpstreams(r.Context(), updated); err != nil {
			writeError(w, 400, err.Error())
			return
		}
		updated.ConfigState, updated.ConfigError = "pending", ""
		if _, result, err := s.upstreamNginx.Validate(r.Context(), proposed); err != nil {
			_ = s.audit(r, session.Username, "validate", "repository", err.Error(), false)
			writeError(w, 422, validationMessage(result, err))
			return
		}
		updated, err = s.store.UpdateMirror(r.Context(), updated)
		if err != nil {
			if database.IsConflict(err) {
				writeError(w, http.StatusConflict, "mirror slug already exists")
				return
			}
			writeInternal(w, err)
			return
		}
		if _, err = s.upstreamNginx.Reconcile(r.Context(), session.Username, "update repository "+updated.Slug); err != nil {
			_ = s.audit(r, session.Username, "update", "repository", err.Error(), false)
			writeError(w, 502, "repository saved as desired state but Managed Upstream Nginx activation failed: "+err.Error())
			return
		}
		if err = s.publishActiveRouting(); err != nil {
			writeInternal(w, err)
			return
		}
		_ = s.audit(r, session.Username, "update", "mirror", updated.Slug, true)
		if current, ok := findMirror(s.registry.List(), updated.ID); ok {
			updated = current
		}
		writeJSON(w, 200, updated)
		return
	}
	if len(parts) == 1 && r.Method == http.MethodDelete {
		desired, loadErr := s.store.ListMirrors(r.Context())
		if loadErr != nil {
			writeInternal(w, loadErr)
			return
		}
		proposed := removeCandidate(desired, id)
		if _, result, err := s.upstreamNginx.Validate(r.Context(), proposed); err != nil {
			writeError(w, 422, validationMessage(result, err))
			return
		}
		if _, err := s.cache.Purge(r.Context(), "repository", id, "", session.Username); err != nil {
			writeInternal(w, err)
			return
		}
		if err := s.store.DeleteMirror(r.Context(), id); err != nil {
			writeInternal(w, err)
			return
		}
		if _, err := s.upstreamNginx.Reconcile(r.Context(), session.Username, "delete repository "+m.Slug); err != nil {
			_ = s.audit(r, session.Username, "delete", "repository", err.Error(), false)
			writeError(w, 502, "repository deleted from desired state but Managed Upstream Nginx activation failed: "+err.Error())
			return
		}
		if err := s.publishActiveRouting(); err != nil {
			writeInternal(w, err)
			return
		}
		_ = s.audit(r, session.Username, "delete", "mirror", m.Slug, true)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if len(parts) == 2 && r.Method == http.MethodPost && (parts[1] == "enable" || parts[1] == "disable") {
		enabled := parts[1] == "enable"
		candidate := m
		candidate.Enabled, candidate.ConfigState, candidate.ConfigError = enabled, "pending", ""
		desired, loadErr := s.store.ListMirrors(r.Context())
		if loadErr != nil {
			writeInternal(w, loadErr)
			return
		}
		if _, result, err := s.upstreamNginx.Validate(r.Context(), replaceCandidate(desired, candidate)); err != nil {
			writeError(w, 422, validationMessage(result, err))
			return
		}
		if err := s.store.SetMirrorEnabled(r.Context(), id, enabled); err != nil {
			writeInternal(w, err)
			return
		}
		if _, err := s.upstreamNginx.Reconcile(r.Context(), session.Username, parts[1]+" repository "+m.Slug); err != nil {
			writeError(w, 502, "desired state saved but Managed Upstream Nginx activation failed: "+err.Error())
			return
		}
		if err := s.publishActiveRouting(); err != nil {
			writeInternal(w, err)
			return
		}
		_ = s.audit(r, session.Username, parts[1], "mirror", m.Slug, true)
		writeJSON(w, 200, map[string]bool{"enabled": enabled})
		return
	}
	if len(parts) == 2 && parts[1] == "check" && r.Method == http.MethodPost {
		results, err := s.checker.CheckMirror(r.Context(), m)
		if err != nil {
			writeInternal(w, err)
			return
		}
		writeJSON(w, 200, results)
		return
	}
	if len(parts) == 2 && parts[1] == "cache" && r.Method == http.MethodDelete {
		s.clearCache(w, r, session, id)
		return
	}
	if len(parts) == 3 && parts[1] == "cache" && parts[2] == "purge" && r.Method == http.MethodPost {
		s.purgeRepositoryCache(w, r, session, m)
		return
	}
	if len(parts) == 2 && parts[1] == "config" && r.Method == http.MethodGet {
		desired, loadErr := s.store.ListMirrors(r.Context())
		if loadErr != nil {
			writeInternal(w, loadErr)
			return
		}
		generated, err := s.upstreamNginx.Preview(r.Context(), desired)
		if err != nil {
			writeInternal(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"repository_id": id, "configuration_hash": generated.Hash, "configuration": generated.Files["repositories.conf"]})
		return
	}
	if len(parts) == 3 && parts[1] == "profile" && r.Method == http.MethodPost && (parts[2] == "preview" || parts[2] == "apply") {
		var input struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		}
		if decodeJSON(w, r, &input) != nil {
			return
		}
		candidate := m
		if err := profile.Apply(&candidate, input.Name, input.Version); err != nil {
			writeError(w, 400, err.Error())
			return
		}
		if err := mirror.NormalizeAndValidate(&candidate, s.cfg.Security.AllowHTTPUpstream, s.cfg.Security.AllowPrivateUpstream); err != nil {
			writeError(w, 400, err.Error())
			return
		}
		desired, loadErr := s.store.ListMirrors(r.Context())
		if loadErr != nil {
			writeInternal(w, loadErr)
			return
		}
		proposed := replaceCandidate(desired, candidate)
		if err := mirror.ValidateRouteConflicts(proposed, s.cfg.Admin.Path, s.cfg.HTTP.PublicBaseURL, s.cfg.Admin.Host); err != nil {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		generated, result, err := s.upstreamNginx.Validate(r.Context(), proposed)
		if err != nil {
			writeError(w, 422, validationMessage(result, err))
			return
		}
		if parts[2] == "preview" {
			writeJSON(w, 200, map[string]any{"repository": candidate, "diff": profileDiff(m, candidate), "configuration": generated.Effective, "configuration_hash": generated.Hash, "validation_result": result})
			return
		}
		candidate.ConfigState = "pending"
		updated, err := s.store.UpdateMirror(r.Context(), candidate)
		if err != nil {
			writeInternal(w, err)
			return
		}
		if _, err := s.upstreamNginx.Reconcile(r.Context(), session.Username, "apply profile "+input.Name+" "+input.Version+" to "+m.Slug); err != nil {
			writeError(w, 502, "profile desired state saved but Managed Upstream Nginx activation failed: "+err.Error())
			return
		}
		if err := s.publishActiveRouting(); err != nil {
			writeInternal(w, err)
			return
		}
		_ = s.audit(r, session.Username, "profile_upgrade", "repository", updated.Slug+" "+input.Name+" "+input.Version, true)
		if current, ok := findMirror(s.registry.List(), updated.ID); ok {
			updated = current
		}
		writeJSON(w, 200, updated)
		return
	}
	writeError(w, 404, "not found")
}

func profileDiff(before, after model.Mirror) map[string]map[string]any {
	result := make(map[string]map[string]any)
	add := func(name string, oldValue, newValue any) {
		if fmt.Sprint(oldValue) != fmt.Sprint(newValue) {
			result[name] = map[string]any{"before": oldValue, "after": newValue}
		}
	}
	add("profile_name", before.ProfileName, after.ProfileName)
	add("profile_version", before.ProfileVersion, after.ProfileVersion)
	add("type", before.Type, after.Type)
	add("proxy_mode", before.ProxyMode, after.ProxyMode)
	add("public_mode", before.PublicMode, after.PublicMode)
	add("cache_enabled", before.CacheEnabled, after.CacheEnabled)
	add("cache_profile", before.CacheProfile, after.CacheProfile)
	add("rewrite_enabled", before.RewriteEnabled, after.RewriteEnabled)
	add("rewrite_profile", before.RewriteProfile, after.RewriteProfile)
	add("auth_mode", before.AuthMode, after.AuthMode)
	add("blob_redirect_mode", before.BlobRedirectMode, after.BlobRedirectMode)
	add("rewrite_hosts", before.RewriteHosts, after.RewriteHosts)
	add("header_add", before.HeaderAdd, after.HeaderAdd)
	add("header_remove", before.HeaderRemove, after.HeaderRemove)
	add("connect_timeout_sec", before.ConnectTimeoutSec, after.ConnectTimeoutSec)
	add("read_timeout_sec", before.ReadTimeoutSec, after.ReadTimeoutSec)
	add("send_timeout_sec", before.SendTimeoutSec, after.SendTimeoutSec)
	add("metadata_rewrite_limit_bytes", before.MetadataLimitBytes, after.MetadataLimitBytes)
	add("cache_authenticated", before.CacheAuthenticated, after.CacheAuthenticated)
	add("health_check_path", before.HealthCheckPath, after.HealthCheckPath)
	return result
}

type clientExample struct {
	Name    string `json:"name"`
	Command string `json:"command"`
}

func clientExamples(cfg config.Config, repository model.Mirror) []clientExample {
	base := strings.TrimRight(cfg.HTTP.PublicBaseURL, "/")
	if repository.PublicMode == "host" {
		base = "https://" + repository.PublicHost
	} else {
		if base == "" {
			base = "https://mirror.example.com"
		}
		base += "/" + strings.Trim(repository.PublicPath, "/")
	}
	switch repository.Type {
	case "apt":
		suite := "bookworm"
		components := "main contrib non-free-firmware"
		pName := strings.ToLower(repository.ProfileName)
		if strings.Contains(pName, "debian security") || strings.Contains(pName, "debian-security") {
			suite = "bookworm-security"
		} else if strings.Contains(pName, "ubuntu") {
			suite, components = "noble", "main restricted universe multiverse"
		}
		return []clientExample{{Name: "APT source", Command: "deb " + base + " " + suite + " " + components}}
	case "rpm":
		return []clientExample{{Name: "DNF/YUM baseurl", Command: "baseurl=" + base + "/$releasever/BaseOS/$basearch/os/"}}
	case "apk":
		return []clientExample{{Name: "Alpine repository", Command: base + "/v3.x/main"}}
	case "opkg":
		return []clientExample{{Name: "OpenWrt feed", Command: "src/gz mirror " + base + "/releases/<version>/packages/<arch>/base"}}
	case "pypi":
		return []clientExample{{Name: "pip", Command: "pip config set global.index-url " + base + "/simple/"}}
	case "npm":
		return []clientExample{{Name: "npm", Command: "npm config set registry " + base + "/"}}
	case "maven":
		return []clientExample{{Name: "Maven URL", Command: base + "/"}}
	case "goproxy":
		return []clientExample{{Name: "Go", Command: "go env -w GOPROXY=" + base + ",direct"}}
	case "nuget":
		return []clientExample{{Name: "NuGet", Command: "dotnet nuget add source " + base + "/v3/index.json -n mirror"}}
	case "cargo":
		return []clientExample{{Name: "Cargo registry index", Command: "registry = \"" + base + "/\""}}
	case "conda":
		return []clientExample{{Name: "Conda channel", Command: "conda config --add channels " + base}}
	case "docker-registry", "oci-registry":
		host := strings.TrimPrefix(base, "https://")
		return []clientExample{
			{Name: "Docker", Command: "docker pull " + host + "/library/nginx:latest"},
			{Name: "Podman", Command: "podman pull " + host + "/library/alpine:latest"},
		}
	default:
		return []clientExample{{Name: "HTTP", Command: "curl -fLO " + base + "/path/to/object"}}
	}
}

func (s *Server) clearCache(w http.ResponseWriter, r *http.Request, session auth.Session, id int64) {
	scope := "global"
	if id > 0 {
		scope = "repository"
	}
	job, err := s.cache.Purge(r.Context(), scope, id, "", session.Username)
	if err != nil {
		writeInternal(w, err)
		return
	}
	object := "all"
	if id > 0 {
		object = strconv.FormatInt(id, 10)
	}
	_ = s.audit(r, session.Username, "clear_cache", "cache", object, true)
	writeJSON(w, 200, map[string]any{"logical_purge": "completed", "physical_reclaim": job.ReclaimState, "job": job})
}

func (s *Server) purgeRepositoryCache(w http.ResponseWriter, r *http.Request, session auth.Session, repository model.Mirror) {
	var input struct {
		Path  string `json:"path"`
		Query string `json:"query"`
	}
	if decodeJSON(w, r, &input) != nil {
		return
	}
	if input.Path == "" {
		job, err := s.cache.Purge(r.Context(), "repository", repository.ID, "", session.Username)
		if err != nil {
			writeInternal(w, err)
			return
		}
		_ = s.audit(r, session.Username, "cache_purge", "repository", repository.Slug, true)
		writeJSON(w, 200, map[string]any{"logical_purge": "completed", "physical_reclaim": job.ReclaimState, "job": job})
		return
	}
	activeRepository, found := s.registry.GetByID(repository.ID)
	if !found {
		writeError(w, 409, "repository has no active routing state; use repository-level purge")
		return
	}
	active, err := activeUpstream(activeRepository.Upstreams)
	if err != nil {
		writeError(w, 409, err.Error())
		return
	}
	objectID := cachectl.CanonicalObjectID(repository.ID, active.URL, input.Path, input.Query)
	job, err := s.cache.Purge(r.Context(), "object", repository.ID, objectID, session.Username)
	if err != nil {
		writeInternal(w, err)
		return
	}
	_ = s.audit(r, session.Username, "cache_purge", "object", repository.Slug+":"+input.Path, true)
	writeJSON(w, 200, map[string]any{"logical_purge": "completed", "physical_reclaim": job.ReclaimState, "object_id": objectID, "job": job})
}
func (s *Server) dashboard(w http.ResponseWriter, r *http.Request) {
	mirrors := s.registry.List()
	enabled, healthy, unhealthy := 0, 0, 0
	for _, m := range mirrors {
		if m.Enabled {
			enabled++
			switch repositoryHealthState(m) {
			case "healthy":
				healthy++
			case "unhealthy":
				unhealthy++
			}
		}
	}
	cacheSummary := s.cache.Summary()
	writeJSON(w, 200, map[string]any{"mirrors": len(mirrors), "enabled_mirrors": enabled, "healthy_mirrors": healthy, "unhealthy_mirrors": unhealthy, "stats": s.stats.Snapshot(), "cache": cacheSummary, "uptime_seconds": int64(time.Since(s.started).Seconds()), "version": s.build.Version, "build_id": s.build.BuildID, "architecture": s.build.Architecture})
}

func (s *Server) healthStatus(w http.ResponseWriter, _ *http.Request) {
	upstreamNginxStatus := s.upstreamNginx.Status()
	upstreamEndpoint := "error"
	network, address := s.cfg.UpstreamEndpoint()
	frontendNetwork, frontendAddress := s.cfg.FrontendEndpoint()
	connection, err := net.DialTimeout(network, address, 250*time.Millisecond)
	if err == nil {
		upstreamEndpoint = "healthy"
		_ = connection.Close()
	}
	repositories := make([]map[string]any, 0)
	for _, repository := range s.registry.List() {
		healthState := repositoryHealthState(repository)
		repositories = append(repositories, map[string]any{"id": repository.ID, "name": repository.Name, "healthy": healthState != "unhealthy", "health_state": healthState})
	}
	status := "healthy"
	if upstreamNginxStatus.State != "running" || upstreamEndpoint != "healthy" {
		status = "degraded"
	}
	writeJSON(w, 200, map[string]any{
		"status":                 status,
		"repogate":               "healthy",
		"frontend_socket":        "healthy",
		"frontend_endpoint":      "healthy",
		"frontend_network":       frontendNetwork,
		"frontend_address":       frontendAddress,
		"external_shared_nginx":  "external",
		"go_router":              "healthy",
		"managed_upstream_nginx": upstreamNginxStatus.State,
		"upstream_endpoint":      upstreamEndpoint,
		"upstream_network":       network,
		"upstream_address":       address,
		"repositories":           repositories,
	})
}

func repositoryHealthState(repository model.Mirror) string {
	hasUnknown, hasEnabled := false, false
	for _, upstream := range repository.Upstreams {
		if !upstream.Enabled {
			continue
		}
		hasEnabled = true
		switch upstream.HealthStatus {
		case "healthy":
			return "healthy"
		case "", "unknown":
			hasUnknown = true
		}
	}
	if !hasEnabled || hasUnknown {
		return "unknown"
	}
	return "unhealthy"
}

func activeUpstream(values []model.Upstream) (model.Upstream, error) {
	candidates := make([]model.Upstream, 0, len(values))
	for _, value := range values {
		if value.Enabled {
			candidates = append(candidates, value)
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].Priority < candidates[j].Priority })
	for _, status := range []string{"healthy", "unknown", "unhealthy"} {
		for _, value := range candidates {
			actual := value.HealthStatus
			if actual == "" {
				actual = "unknown"
			}
			if actual == status {
				return value, nil
			}
		}
	}
	return model.Upstream{}, errors.New("repository has no enabled upstream")
}
func (s *Server) metrics(w http.ResponseWriter, r *http.Request) {
	names := map[int64]string{}
	for _, m := range s.registry.List() {
		names[m.ID] = m.Name
	}
	s.stats.Metrics(w, names)
	if s.clusterMetrics != nil {
		s.clusterMetrics.WritePrometheus(w)
	}
	fmt.Fprintln(w, "# TYPE repogate_up gauge")
	fmt.Fprintln(w, "repogate_up 1")
	fmt.Fprintln(w, "# TYPE repogate_managed_upstream_nginx_up gauge")
	upstreamNginxUp := 0
	if s.upstreamNginx.Status().State == "running" {
		upstreamNginxUp = 1
	}
	fmt.Fprintf(w, "repogate_managed_upstream_nginx_up %d\n", upstreamNginxUp)
}
func (s *Server) audit(r *http.Request, user, action, object, detail string, ok bool) error {
	entry := model.AuditEntry{
		Time:      time.Now(),
		Username:  user,
		ClientIP:  security.RequestClientIP(r),
		Action:    action,
		Object:    object,
		Detail:    detail,
		Succeeded: ok,
	}
	if err := s.store.AddAudit(r.Context(), entry); err != nil {
		slog.Error("failed to record audit entry", "user", user, "action", action, "object", object, "error", err)
		return err
	}
	return nil
}
func (s *Server) validateUpstreams(parent context.Context, m model.Mirror) error {
	ctx, cancel := context.WithTimeout(parent, 10*time.Second)
	defer cancel()
	for _, u := range m.Upstreams {
		if !u.Enabled {
			continue
		}
		if err := security.ValidateResolvedURL(ctx, u.URL, s.cfg.Security.AllowHTTPUpstream && m.AllowHTTP,
			s.cfg.Security.AllowPrivateUpstream && m.AllowPrivate, net.DefaultResolver); err != nil {
			return fmt.Errorf("upstream %s: %w", u.URL, err)
		}
	}
	if m.TokenUpstream != "" {
		if err := security.ValidateResolvedURL(ctx, m.TokenUpstream, s.cfg.Security.AllowHTTPUpstream && m.AllowHTTP,
			s.cfg.Security.AllowPrivateUpstream && m.AllowPrivate, net.DefaultResolver); err != nil {
			return fmt.Errorf("token upstream %s: %w", m.TokenUpstream, err)
		}
	}
	return nil
}
func findMirror(values []model.Mirror, id int64) (model.Mirror, bool) {
	for _, m := range values {
		if m.ID == id {
			return m, true
		}
	}
	return model.Mirror{}, false
}

func replaceCandidate(values []model.Mirror, replacement model.Mirror) []model.Mirror {
	out := append([]model.Mirror(nil), values...)
	for i := range out {
		if out[i].ID == replacement.ID {
			out[i] = replacement
			return out
		}
	}
	return append(out, replacement)
}

func removeCandidate(values []model.Mirror, id int64) []model.Mirror {
	out := make([]model.Mirror, 0, len(values))
	for _, value := range values {
		if value.ID != id {
			out = append(out, value)
		}
	}
	return out
}

func validationMessage(result string, err error) string {
	if strings.TrimSpace(result) == "" {
		return err.Error()
	}
	return err.Error() + ": " + strings.TrimSpace(result)
}

func readLastLines(path string, limit int, maxBytes int64) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return []string{}, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	start := info.Size() - maxBytes
	if start < 0 {
		start = 0
	}
	if _, err := file.Seek(start, io.SeekStart); err != nil {
		return nil, err
	}
	content, err := io.ReadAll(io.LimitReader(file, maxBytes))
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(string(content)), "\n")
	if start > 0 && len(lines) > 0 {
		lines = lines[1:]
	}
	if len(lines) > limit {
		lines = lines[len(lines)-limit:]
	}
	if len(lines) == 1 && lines[0] == "" {
		return []string{}, nil
	}
	return lines, nil
}

func (s *Server) createCustomConfig(w http.ResponseWriter, r *http.Request, session auth.Session) {
	var value model.CustomConfig
	if decodeJSON(w, r, &value) != nil {
		return
	}
	value.Name = strings.TrimSpace(value.Name)
	value.Context = strings.ToLower(strings.TrimSpace(value.Context))
	if err := upstreamnginx.ValidateCustomName(value.Name); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	if err := upstreamnginx.ValidateCustom(value.Context, value.Content); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	value.LastResult = "syntax policy passed"
	custom, err := s.store.ListCustomConfigs(r.Context())
	if err != nil {
		writeInternal(w, err)
		return
	}
	desired, err := s.store.ListMirrors(r.Context())
	if err != nil {
		writeInternal(w, err)
		return
	}
	if _, result, err := s.upstreamNginx.ValidateWithCustom(r.Context(), desired, append(custom, value)); err != nil {
		writeError(w, 422, validationMessage(result, err))
		return
	}
	created, err := s.store.CreateCustomConfig(r.Context(), value)
	if err != nil {
		if database.IsConflict(err) {
			writeError(w, 409, "custom configuration name already exists")
			return
		}
		writeInternal(w, err)
		return
	}
	if _, err := s.upstreamNginx.Reconcile(r.Context(), session.Username, "create custom config "+created.Name); err != nil {
		_ = s.audit(r, session.Username, "custom_config_create", "managed-upstream-nginx", err.Error(), false)
		writeError(w, 422, "custom configuration saved but activation failed: "+err.Error())
		return
	}
	_ = s.audit(r, session.Username, "custom_config_create", "managed-upstream-nginx", created.Name, true)
	writeJSON(w, 201, created)
}

func (s *Server) customConfigAction(w http.ResponseWriter, r *http.Request, session auth.Session, tail string) {
	id, err := strconv.ParseInt(strings.Trim(tail, "/"), 10, 64)
	if err != nil {
		writeError(w, 400, "invalid custom configuration id")
		return
	}
	current, err := s.store.CustomConfig(r.Context(), id)
	if err != nil {
		writeError(w, 404, "custom configuration not found")
		return
	}
	if r.Method == http.MethodGet {
		writeJSON(w, 200, current)
		return
	}
	if r.Method == http.MethodDelete {
		custom, err := s.store.ListCustomConfigs(r.Context())
		if err != nil {
			writeInternal(w, err)
			return
		}
		candidate := make([]model.CustomConfig, 0, len(custom)-1)
		for _, value := range custom {
			if value.ID != id {
				candidate = append(candidate, value)
			}
		}
		desired, loadErr := s.store.ListMirrors(r.Context())
		if loadErr != nil {
			writeInternal(w, loadErr)
			return
		}
		if _, result, err := s.upstreamNginx.ValidateWithCustom(r.Context(), desired, candidate); err != nil {
			writeError(w, 422, validationMessage(result, err))
			return
		}
		if err := s.store.DeleteCustomConfig(r.Context(), id); err != nil {
			writeInternal(w, err)
			return
		}
		if _, err := s.upstreamNginx.Reconcile(r.Context(), session.Username, "delete custom config "+current.Name); err != nil {
			writeError(w, 422, "custom configuration deleted but activation failed: "+err.Error())
			return
		}
		_ = s.audit(r, session.Username, "custom_config_delete", "managed-upstream-nginx", current.Name, true)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method == http.MethodPut {
		var value model.CustomConfig
		if decodeJSON(w, r, &value) != nil {
			return
		}
		value.ID = id
		value.Name = strings.TrimSpace(value.Name)
		value.Context = strings.ToLower(strings.TrimSpace(value.Context))
		if err := upstreamnginx.ValidateCustomName(value.Name); err != nil {
			writeError(w, 400, err.Error())
			return
		}
		if err := upstreamnginx.ValidateCustom(value.Context, value.Content); err != nil {
			writeError(w, 400, err.Error())
			return
		}
		value.LastResult = "syntax policy passed"
		custom, err := s.store.ListCustomConfigs(r.Context())
		if err != nil {
			writeInternal(w, err)
			return
		}
		for i := range custom {
			if custom[i].ID == id {
				custom[i] = value
			}
		}
		desired, loadErr := s.store.ListMirrors(r.Context())
		if loadErr != nil {
			writeInternal(w, loadErr)
			return
		}
		if _, result, err := s.upstreamNginx.ValidateWithCustom(r.Context(), desired, custom); err != nil {
			writeError(w, 422, validationMessage(result, err))
			return
		}
		updated, err := s.store.UpdateCustomConfig(r.Context(), value)
		if err != nil {
			writeInternal(w, err)
			return
		}
		if _, err := s.upstreamNginx.Reconcile(r.Context(), session.Username, "update custom config "+updated.Name); err != nil {
			writeError(w, 422, "custom configuration saved but activation failed: "+err.Error())
			return
		}
		_ = s.audit(r, session.Username, "custom_config_update", "managed-upstream-nginx", updated.Name, true)
		writeJSON(w, 200, updated)
		return
	}
	writeError(w, 405, "method not allowed")
}
func decodeJSON(w http.ResponseWriter, r *http.Request, out any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		writeError(w, 400, "invalid JSON: "+err.Error())
		return err
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("multiple JSON values are not allowed")
		}
		writeError(w, 400, "invalid JSON: "+err.Error())
		return err
	}
	return nil
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
func writeInternal(w http.ResponseWriter, _ error) { writeError(w, 500, "internal server error") }
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; script-src 'self'; connect-src 'self'; img-src 'self' data:")
		next.ServeHTTP(w, r)
	})
}

func (s *Server) clusterOverview(w http.ResponseWriter, r *http.Request) {
	fp := ""
	if s.clusterChecker != nil {
		fp = s.clusterChecker.ClusterFingerprint()
	}
	var total, healthy, routable int
	if s.store != nil {
		nodes, _ := s.store.ListClusterNodes(r.Context())
		total = len(nodes)
		for _, n := range nodes {
			if n.HealthStatus == "healthy" {
				healthy++
			}
			isMatch := (fp == "" || n.ConfigFingerprint == fp) && n.ConfigStatus != "mismatch" && n.ConfigStatus != "drifted"
			if n.Enabled && n.HealthStatus == "healthy" && isMatch && (n.ProtocolVersion == 0 || n.ProtocolVersion == cluster.ClusterProtocolVersion) {
				routable++
			}
		}
	}
	overview := model.ClusterOverview{
		Role:               s.cfg.Distributed.Role,
		Enabled:            s.cfg.Distributed.Enabled,
		ClusterFingerprint: fp,
		TotalNodes:         total,
		HealthyNodes:       healthy,
		RoutableNodes:      routable,
		RoutingMode:        s.cfg.Distributed.Routing.Mode,
	}
	writeJSON(w, http.StatusOK, overview)
}

func (s *Server) listClusterNodes(w http.ResponseWriter, r *http.Request) {
	nodes, err := s.store.ListClusterNodes(r.Context())
	if err != nil {
		writeInternal(w, err)
		return
	}
	if nodes == nil {
		nodes = []model.ClusterNode{}
	}
	writeJSON(w, http.StatusOK, nodes)
}

func (s *Server) createClusterNode(w http.ResponseWriter, r *http.Request, session auth.Session) {
	var in model.ClusterNode
	if decodeJSON(w, r, &in) != nil {
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	in.URL = strings.TrimSpace(in.URL)
	in.Region = strings.TrimSpace(in.Region)
	if in.Name == "" || in.URL == "" || in.Region == "" {
		writeError(w, http.StatusBadRequest, "node name, url, and region are required")
		return
	}
	u, err := url.Parse(in.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		writeError(w, http.StatusBadRequest, "invalid node url")
		return
	}
	if in.Priority <= 0 {
		in.Priority = 100
	}
	if in.Weight <= 0 {
		in.Weight = 100
	}
	in.Enabled = true

	created, err := s.store.CreateClusterNode(r.Context(), in)
	if err != nil {
		if database.IsConflict(err) {
			writeError(w, http.StatusConflict, "node url already exists")
			return
		}
		writeInternal(w, err)
		return
	}

	if s.clusterChecker != nil {
		probed, _ := s.clusterChecker.CheckNode(r.Context(), created)
		created = probed
		if all, err := s.store.ListClusterNodes(r.Context()); err == nil && s.clusterRouter != nil {
			s.clusterRouter.SetNodes(all)
		}
	}

	_ = s.audit(r, session.Username, "create_cluster_node", "cluster_node", created.Name, true)
	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) clusterNodeAction(w http.ResponseWriter, r *http.Request, session auth.Session, subpath string) {
	parts := strings.Split(strings.Trim(subpath, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		writeError(w, http.StatusBadRequest, "invalid node id")
		return
	}
	id, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid node id")
		return
	}
	node, err := s.store.GetClusterNode(r.Context(), id)
	if err != nil {
		if database.IsNotFound(err) {
			writeError(w, http.StatusNotFound, "node not found")
			return
		}
		writeInternal(w, err)
		return
	}

	if len(parts) == 1 && (r.Method == http.MethodPatch || r.Method == http.MethodPut || r.Method == http.MethodPost) {
		var in model.ClusterNode
		if decodeJSON(w, r, &in) != nil {
			return
		}
		in.ID = id
		if strings.TrimSpace(in.Name) == "" {
			in.Name = node.Name
		}
		if strings.TrimSpace(in.URL) == "" {
			in.URL = node.URL
		}
		if strings.TrimSpace(in.Region) == "" {
			in.Region = node.Region
		}
		if in.Priority <= 0 {
			in.Priority = node.Priority
		}
		if in.Weight <= 0 {
			in.Weight = node.Weight
		}
		updated, err := s.store.UpdateClusterNode(r.Context(), in)
		if err != nil {
			writeInternal(w, err)
			return
		}
		if s.clusterChecker != nil {
			probed, _ := s.clusterChecker.CheckNode(r.Context(), updated)
			updated = probed
			if all, err := s.store.ListClusterNodes(r.Context()); err == nil && s.clusterRouter != nil {
				s.clusterRouter.SetNodes(all)
			}
		}
		_ = s.audit(r, session.Username, "update_cluster_node", "cluster_node", updated.Name, true)
		writeJSON(w, http.StatusOK, updated)
		return
	}

	if len(parts) == 1 && r.Method == http.MethodDelete {
		if err := s.store.DeleteClusterNode(r.Context(), id); err != nil {
			writeInternal(w, err)
			return
		}
		if all, err := s.store.ListClusterNodes(r.Context()); err == nil && s.clusterRouter != nil {
			s.clusterRouter.SetNodes(all)
		}
		_ = s.audit(r, session.Username, "delete_cluster_node", "cluster_node", node.Name, true)
		w.WriteHeader(http.StatusNoContent)
		return
	}

	if len(parts) == 2 && parts[1] == "check" && r.Method == http.MethodPost {
		if s.clusterChecker != nil {
			probed, _ := s.clusterChecker.CheckNode(r.Context(), node)
			node = probed
			if all, err := s.store.ListClusterNodes(r.Context()); err == nil && s.clusterRouter != nil {
				s.clusterRouter.SetNodes(all)
			}
		}
		writeJSON(w, http.StatusOK, node)
		return
	}

	if len(parts) == 2 && (parts[1] == "enable" || parts[1] == "disable") && r.Method == http.MethodPost {
		enabled := parts[1] == "enable"
		if err := s.store.SetClusterNodeEnabled(r.Context(), id, enabled); err != nil {
			writeInternal(w, err)
			return
		}
		if all, err := s.store.ListClusterNodes(r.Context()); err == nil && s.clusterRouter != nil {
			s.clusterRouter.SetNodes(all)
		}
		_ = s.audit(r, session.Username, parts[1]+"_cluster_node", "cluster_node", node.Name, true)
		writeJSON(w, http.StatusOK, map[string]bool{"enabled": enabled})
		return
	}

	writeError(w, http.StatusNotFound, "not found")
}

func (s *Server) resetClusterFingerprint(w http.ResponseWriter, r *http.Request, session auth.Session) {
	if s.clusterChecker != nil {
		_ = s.clusterChecker.SetClusterFingerprint(r.Context(), "")
		_ = s.clusterChecker.CheckAll(r.Context())
	}
	_ = s.audit(r, session.Username, "reset_cluster_fingerprint", "cluster", "", true)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
