package api

import (
	"crypto/subtle"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/LuisCMerrick/MirrorRelay/internal/auth"
	"github.com/LuisCMerrick/MirrorRelay/internal/database"
	"github.com/LuisCMerrick/MirrorRelay/internal/help"
	"github.com/LuisCMerrick/MirrorRelay/internal/profile"
)

func (s *Server) adminAccess(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.adminCIDRs.Allows(s.requestClientIP(r)) {
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
		r2.URL.Path = "/"
		http.FileServer(http.FS(s.web)).ServeHTTP(w, r2)
		return
	}
	http.StripPrefix(adminPath, http.FileServer(http.FS(s.web))).ServeHTTP(w, r)
}

func (s *Server) apiHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	path := strings.TrimPrefix(r.URL.Path, strings.TrimSuffix(s.cfg.AdminAPIPath(), "/"))
	if path == "/auth/bootstrap" {
		switch r.Method {
		case http.MethodGet:
			s.initialAdminStatus(w, r)
		case http.MethodPost:
			s.mutationMu.Lock()
			defer s.mutationMu.Unlock()
			s.registerInitialAdmin(w, r)
		default:
			w.Header().Set("Allow", "GET, POST")
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
		return
	}
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
		writeJSON(w, 200, map[string]any{"username": session.Username, "role": session.Role, "csrf_token": session.CSRFToken})
	case path == "/auth/logout" && r.Method == http.MethodPost:
		_ = s.audit(r, session.Username, "logout", "session", "", true)
		s.sessions.Delete(r)
		s.sessions.ClearCookie(w)
		writeJSON(w, 200, map[string]bool{"ok": true})
	case path == "/auth/password" && r.Method == http.MethodPut:
		s.password(w, r, session)
	case path == "/webhooks/test" && r.Method == http.MethodPost:
		s.testWebhook(w, r, session)
	case path == "/users" && r.Method == http.MethodGet:
		if !s.requireRole(w, session, "admin") {
			return
		}
		users, err := s.store.ListUsers(r.Context())
		if err != nil {
			writeInternal(w, err)
			return
		}
		writeJSON(w, 200, users)
	case path == "/users" && r.Method == http.MethodPost:
		if !s.requireRole(w, session, "admin") {
			return
		}
		s.createUser(w, r, session)
	case strings.HasPrefix(path, "/users/") && r.Method == http.MethodDelete:
		if !s.requireRole(w, session, "admin") {
			return
		}
		s.deleteUser(w, r, session, strings.TrimPrefix(path, "/users/"))
	case path == "/mirrors" && r.Method == http.MethodGet:
		mirrors, err := s.store.ListMirrors(r.Context())
		if err != nil {
			writeInternal(w, err)
			return
		}
		writeJSON(w, 200, mirrorsForRole(mirrors, session.Role))
	case path == "/mirrors" && r.Method == http.MethodPost:
		if !s.requireRole(w, session, "admin", "operator") {
			return
		}
		s.createMirror(w, r, session)
	case strings.HasPrefix(path, "/mirrors/"):
		if r.Method != http.MethodGet && !s.requireRole(w, session, "admin", "operator") {
			return
		}
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
		if !s.requireRole(w, session, "admin", "operator") {
			return
		}
		s.clearCache(w, r, session, 0)
	case path == "/stats" && r.Method == http.MethodGet:
		s.dashboard(w, r)
	case path == "/health" && r.Method == http.MethodGet:
		s.healthStatus(w, r, session.Role)
	case path == "/audit" && r.Method == http.MethodGet:
		entries, err := s.store.ListAudit(r.Context(), 200)
		if err != nil {
			writeInternal(w, err)
			return
		}
		writeJSON(w, 200, auditEntriesForRole(entries, session.Role))
	case path == "/access" && r.Method == http.MethodGet:
		if !s.requireRole(w, session, "admin", "operator") {
			return
		}
		lines, err := readLastLines(filepath.Join(s.cfg.UpstreamNginx.LogPath, "access.log"), 200, 2<<20)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			writeInternal(w, err)
			return
		}
		writeJSON(w, 200, lines)
	case path == "/system" && r.Method == http.MethodGet:
		if !s.requireRole(w, session, "admin", "operator") {
			return
		}
		frontendNetwork, frontendAddress := s.cfg.FrontendEndpoint()
		upstreamNetwork, upstreamAddress := s.cfg.UpstreamEndpoint()
		response := map[string]any{
			"version": s.build.Version, "build_id": s.build.BuildID, "git_commit": s.build.GitCommit,
			"build_timestamp": s.build.BuildTimestamp, "go_version": s.build.GoVersion,
			"target_os": s.build.TargetOS, "architecture": s.build.Architecture,
			"uptime_seconds": int64(time.Since(s.started).Seconds()), "ingress_mode": s.cfg.Ingress.Mode,
			"public_base_url": s.cfg.HTTP.PublicBaseURL, "tls_min_version": s.cfg.TLS.MinVersion,
			"upstream_nginx":   upstreamNginxStatusForRole(s.upstreamNginx.Status(), session.Role),
			"zero_copy_bypass": s.cfg.Performance.ZeroCopyBypass,
		}
		if session.Role == "admin" {
			response["https_listen"] = s.cfg.HTTP.HTTPSListen
			response["tls_certificate"] = s.cfg.TLS.Certificate
			response["tls_private_key"] = s.cfg.TLS.PrivateKey
			response["frontend_network"] = frontendNetwork
			response["frontend_address"] = frontendAddress
			response["upstream_network"] = upstreamNetwork
			response["upstream_address"] = upstreamAddress
		}
		writeJSON(w, 200, response)
	case (path == "/system/restart" || path == "/restart") && r.Method == http.MethodPost:
		if !s.requireRole(w, session, "admin") {
			return
		}
		_ = s.audit(r, session.Username, "system_restart", "system", "service restart requested from Web UI", true)
		s.dispatchAlert("security_alert", "Service Restart", fmt.Sprintf("MirrorRelay restart triggered by %s", session.Username), nil)
		writeJSON(w, 200, map[string]any{"ok": true, "status": "restarting", "message": "restart initiated"})
		go func() {
			time.Sleep(100 * time.Millisecond)
			s.triggerRestart()
		}()
	case path == "/settings" && r.Method == http.MethodGet:
		if !s.requireRole(w, session, "admin") {
			return
		}
		s.webSettings(w, r, session)
	case path == "/settings" && r.Method == http.MethodPut:
		if !s.requireRole(w, session, "admin") {
			return
		}
		s.updateWebSettings(w, r, session)
	case path == "/settings" && r.Method == http.MethodDelete:
		if !s.requireRole(w, session, "admin") {
			return
		}
		s.resetWebSettings(w, r, session)
	case path == "/settings/export" && (r.Method == http.MethodGet || r.Method == http.MethodPost):
		if !s.requireRole(w, session, "admin") {
			return
		}
		s.exportSettings(w, r, session)
	case path == "/settings/import/preview" && r.Method == http.MethodPost:
		if !s.requireRole(w, session, "admin") {
			return
		}
		s.previewImportSettings(w, r, session)
	case path == "/settings/import" && r.Method == http.MethodPost:
		if !s.requireRole(w, session, "admin") {
			return
		}
		s.applyImportSettings(w, r, session)
	case path == "/settings/history" && r.Method == http.MethodGet:
		if !s.requireRole(w, session, "admin") {
			return
		}
		s.listSettingsHistory(w, r, session)
	case strings.HasPrefix(path, "/settings/history/") && strings.HasSuffix(path, "/rollback") && r.Method == http.MethodPost:
		if !s.requireRole(w, session, "admin") {
			return
		}
		raw := strings.TrimSuffix(strings.TrimPrefix(path, "/settings/history/"), "/rollback")
		s.rollbackSettingsHistory(w, r, session, raw)
	case path == "/appearance" && r.Method == http.MethodGet:
		writeJSON(w, 200, s.appearanceConfig())
	case path == "/appearance" && r.Method == http.MethodPut:
		if !s.requireRole(w, session, "admin") {
			return
		}
		s.updateAppearance(w, r, session)
	case (path == "/appearance/reset" || (path == "/appearance" && r.Method == http.MethodDelete)) && (r.Method == http.MethodPost || r.Method == http.MethodDelete):
		if !s.requireRole(w, session, "admin") {
			return
		}
		s.resetAppearance(w, r, session)
	case path == "/help/templates" && r.Method == http.MethodGet:
		writeJSON(w, 200, help.ListTemplates())
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
		writeJSON(w, 200, upstreamNginxStatusForRole(s.upstreamNginx.Status(), session.Role))
	case path == "/upstream-nginx/test" && r.Method == http.MethodPost:
		if !s.requireRole(w, session, "admin", "operator") {
			return
		}
		mirrors, err := s.store.ListMirrors(r.Context())
		if err != nil {
			writeInternal(w, err)
			return
		}
		generated, result, err := s.upstreamNginx.Validate(r.Context(), mirrors)
		if err != nil {
			writeError(w, 422, validationMessageForRole(result, err, session.Role))
			return
		}
		response := map[string]any{"ok": true, "configuration_hash": generated.Hash}
		if session.Role == "admin" {
			response["validation_result"] = result
		}
		writeJSON(w, 200, response)
	case path == "/ingress/snippet" && r.Method == http.MethodGet:
		if !s.requireRole(w, session, "admin") {
			return
		}
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
		if !s.requireRole(w, session, "admin") {
			return
		}
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
		writeJSON(w, 200, configVersionsForRole(values, session.Role))
	case path == "/upstream-nginx/reload" && r.Method == http.MethodPost:
		if !s.requireRole(w, session, "admin", "operator") {
			return
		}
		v, err := s.upstreamNginx.Reconcile(r.Context(), session.Username, "manual reconcile")
		if err != nil {
			_ = s.audit(r, session.Username, "upstream_nginx_reload", "managed-upstream-nginx", err.Error(), false)
			writeError(w, 422, activationMessageForRole("Managed Upstream Nginx reload failed", err, session.Role))
			return
		}
		if err := s.publishActiveRouting(); err != nil {
			writeInternal(w, err)
			return
		}
		_ = s.audit(r, session.Username, "upstream_nginx_reload", "managed-upstream-nginx", fmt.Sprintf("version %d", v.Version), true)
		s.dispatchAlert("config_change", "Nginx Reloaded", fmt.Sprintf("Configuration reloaded to version %d by %s", v.Version, session.Username), nil)
		writeJSON(w, 200, configVersionForRole(v, session.Role))
	case strings.HasPrefix(path, "/upstream-nginx/history/") && strings.HasSuffix(path, "/rollback") && r.Method == http.MethodPost:
		if !s.requireRole(w, session, "admin", "operator") {
			return
		}
		raw := strings.TrimSuffix(strings.TrimPrefix(path, "/upstream-nginx/history/"), "/rollback")
		version, err := strconv.ParseInt(strings.Trim(raw, "/"), 10, 64)
		if err != nil {
			writeError(w, 400, "invalid configuration version")
			return
		}
		v, err := s.upstreamNginx.Rollback(r.Context(), version, session.Username)
		if err != nil {
			_ = s.audit(r, session.Username, "config_rollback", "managed-upstream-nginx", err.Error(), false)
			writeError(w, 422, activationMessageForRole("Managed Upstream Nginx rollback failed", err, session.Role))
			return
		}
		if err := s.publishActiveRouting(); err != nil {
			writeInternal(w, err)
			return
		}
		_ = s.audit(r, session.Username, "config_rollback", "managed-upstream-nginx", fmt.Sprintf("version %d", version), true)
		s.dispatchAlert("config_change", "Config Rollback", fmt.Sprintf("Configuration rolled back to version %d by %s", version, session.Username), nil)
		writeJSON(w, 200, configVersionForRole(v, session.Role))
	case path == "/custom-configs" && r.Method == http.MethodGet:
		if !s.requireRole(w, session, "admin") {
			return
		}
		values, err := s.store.ListCustomConfigs(r.Context())
		if err != nil {
			writeInternal(w, err)
			return
		}
		writeJSON(w, 200, values)
	case path == "/custom-configs" && r.Method == http.MethodPost:
		if !s.requireRole(w, session, "admin") {
			return
		}
		s.createCustomConfig(w, r, session)
	case strings.HasPrefix(path, "/custom-configs/"):
		if !s.requireRole(w, session, "admin") {
			return
		}
		s.customConfigAction(w, r, session, strings.TrimPrefix(path, "/custom-configs/"))
	case strings.HasPrefix(path, "/upstream-nginx/rollback/") && r.Method == http.MethodPost:
		if !s.requireRole(w, session, "admin", "operator") {
			return
		}
		s.rollbackConfig(w, r, session, strings.TrimPrefix(path, "/upstream-nginx/rollback/"))
	case path == "/cluster/overview" && r.Method == http.MethodGet:
		s.clusterOverview(w, r)
	case path == "/cluster/nodes" && r.Method == http.MethodGet:
		s.listClusterNodes(w, r)
	case path == "/cluster/nodes" && r.Method == http.MethodPost:
		if !s.requireRole(w, session, "admin") {
			return
		}
		s.createClusterNode(w, r, session)
	case strings.HasPrefix(path, "/cluster/nodes/"):
		tail := strings.TrimPrefix(path, "/cluster/nodes/")
		operational := r.Method == http.MethodPost && (strings.HasSuffix(tail, "/check") || strings.HasSuffix(tail, "/sync"))
		if r.Method != http.MethodGet {
			if operational {
				if !s.requireRole(w, session, "admin", "operator") {
					return
				}
			} else if !s.requireRole(w, session, "admin") {
				return
			}
		}
		s.clusterNodeAction(w, r, session, tail)
	case path == "/cluster/fingerprint/reset" && r.Method == http.MethodPost:
		if !s.requireRole(w, session, "admin") {
			return
		}
		s.resetClusterFingerprint(w, r, session)
	case path == "/cluster/sync" && r.Method == http.MethodPost:
		if !s.requireRole(w, session, "admin", "operator") {
			return
		}
		s.syncAllClusterNodes(w, r, session)
	case path == "/warmup/jobs" && r.Method == http.MethodGet:
		s.listWarmupJobs(w, r)
	case path == "/warmup/jobs" && r.Method == http.MethodPost:
		if !s.requireRole(w, session, "admin", "operator") {
			return
		}
		s.createWarmupJob(w, r, session)
	case strings.HasPrefix(path, "/warmup/jobs/"):
		s.warmupJobAction(w, r, session, strings.TrimPrefix(path, "/warmup/jobs/"))
	case path == "/warmup/status" && r.Method == http.MethodGet:
		s.warmupStatus(w, r)
	default:
		writeError(w, http.StatusNotFound, "not found")
	}
}

func (s *Server) createUser(w http.ResponseWriter, r *http.Request, session auth.Session) {
	var input struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Role     string `json:"role"`
	}
	if decodeJSON(w, r, &input) != nil {
		return
	}
	input.Username = strings.TrimSpace(input.Username)
	if !validUsername(input.Username) {
		writeError(w, 400, "username must be 3..64 non-space characters")
		return
	}
	input.Role = strings.ToLower(strings.TrimSpace(input.Role))
	if input.Role == "" {
		input.Role = "operator"
	}
	if input.Role != "admin" && input.Role != "operator" && input.Role != "viewer" {
		writeError(w, 400, "role must be admin, operator or viewer")
		return
	}
	hash, err := auth.HashPassword(input.Password)
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	if err := s.store.CreateUser(r.Context(), input.Username, hash, input.Role); err != nil {
		if database.IsConflict(err) {
			writeError(w, 409, "username already exists")
			return
		}
		writeInternal(w, err)
		return
	}
	_ = s.audit(r, session.Username, "user_create", "user", fmt.Sprintf("%s (%s)", input.Username, input.Role), true)
	writeJSON(w, 201, map[string]string{"username": input.Username, "role": input.Role})
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
		writeError(w, 422, activationMessageForRole("Managed Upstream Nginx rollback failed", err, session.Role))
		return
	}
	if err := s.publishActiveRouting(); err != nil {
		writeInternal(w, err)
		return
	}
	_ = s.audit(r, session.Username, "config_rollback", "managed-upstream-nginx", fmt.Sprintf("version %d", version), true)
	writeJSON(w, 200, configVersionForRole(value, session.Role))
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func validUsername(username string) bool {
	return len(username) >= 3 && len(username) <= 64 && !strings.ContainsAny(username, " \t\r\n")
}

func (s *Server) initialAdminStatus(w http.ResponseWriter, r *http.Request) {
	count, err := s.store.CountUsers(r.Context())
	if err != nil {
		writeInternal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"required": count == 0})
}

func (s *Server) registerInitialAdmin(w http.ResponseWriter, r *http.Request) {
	count, err := s.store.CountUsers(r.Context())
	if err != nil {
		writeInternal(w, err)
		return
	}
	if count != 0 {
		writeError(w, http.StatusConflict, "initial administrator already exists")
		return
	}
	var in struct {
		Username             string `json:"username"`
		Password             string `json:"password"`
		PasswordConfirmation string `json:"password_confirmation"`
	}
	if decodeJSON(w, r, &in) != nil {
		return
	}
	in.Username = strings.TrimSpace(in.Username)
	if !validUsername(in.Username) {
		writeError(w, http.StatusBadRequest, "username must be 3..64 non-space characters")
		return
	}
	if in.Password != in.PasswordConfirmation {
		writeError(w, http.StatusBadRequest, "password confirmation does not match")
		return
	}
	hash, err := auth.HashPassword(in.Password)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	user, created, err := s.store.CreateInitialAdmin(r.Context(), in.Username, hash)
	if err != nil {
		writeInternal(w, err)
		return
	}
	if !created {
		writeError(w, http.StatusConflict, "initial administrator already exists")
		return
	}
	session, err := s.sessions.Create(user.ID, user.Username, user.Role)
	if err != nil {
		writeInternal(w, err)
		return
	}
	s.sessions.SetCookie(w, session)
	_ = s.audit(r, user.Username, "initial_admin_register", "user", user.Username, true)
	writeJSON(w, http.StatusCreated, map[string]any{"username": user.Username, "role": user.Role, "csrf_token": session.CSRFToken})
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	ip := s.requestClientIP(r)
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
	session, err := s.sessions.Create(user.ID, user.Username, user.Role)
	if err != nil {
		writeInternal(w, err)
		return
	}
	s.sessions.SetCookie(w, session)
	_ = s.audit(r, user.Username, "login", "session", "", true)
	writeJSON(w, 200, map[string]any{"username": user.Username, "role": user.Role, "csrf_token": session.CSRFToken})
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

func (s *Server) requireRole(w http.ResponseWriter, session auth.Session, allowedRoles ...string) bool {
	role := session.Role
	if role == "" {
		role = "admin"
	}
	for _, r := range allowedRoles {
		if strings.EqualFold(r, role) {
			return true
		}
	}
	writeError(w, http.StatusForbidden, "insufficient permissions: role "+role+" is not allowed to perform this operation")
	return false
}
