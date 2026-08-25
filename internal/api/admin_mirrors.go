package api

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/LuisCMerrick/MirrorRelay/internal/auth"
	"github.com/LuisCMerrick/MirrorRelay/internal/cachectl"
	"github.com/LuisCMerrick/MirrorRelay/internal/database"
	"github.com/LuisCMerrick/MirrorRelay/internal/mirror"
	"github.com/LuisCMerrick/MirrorRelay/internal/model"
	"github.com/LuisCMerrick/MirrorRelay/internal/profile"
	"github.com/LuisCMerrick/MirrorRelay/internal/security"
)

func (s *Server) createMirror(w http.ResponseWriter, r *http.Request, session auth.Session) {
	var m model.Mirror
	if decodeJSON(w, r, &m) != nil {
		return
	}
	if session.Role != "admin" && hasMirrorSecrets(m) {
		writeError(w, http.StatusForbidden, "repository credentials may only be configured by an administrator")
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
		writeError(w, 422, validationMessageForRole(result, err, session.Role))
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
		writeError(w, 502, activationMessageForRole("repository saved as desired state but Managed Upstream Nginx activation failed", err, session.Role))
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
	writeJSON(w, 201, mirrorForRole(created, session.Role))
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
		writeJSON(w, 200, mirrorForRole(m, session.Role))
		return
	}
	if len(parts) == 2 && parts[1] == "state" && r.Method == http.MethodGet {
		active, activeFound := s.registry.GetByID(id)
		writeJSON(w, 200, map[string]any{
			"desired": mirrorForRole(m, session.Role), "active": mirrorForRole(active, session.Role), "active_found": activeFound,
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
		if session.Role != "admin" {
			if err := restoreRedactedMirrorSecrets(m, &updated); err != nil {
				writeError(w, http.StatusForbidden, err.Error())
				return
			}
		}
		updated.ID = id
		if err := mirror.NormalizeAndValidate(&updated, s.cfg.Security.AllowHTTPUpstream, s.cfg.Security.AllowPrivateUpstream); err != nil {
			writeError(w, 400, err.Error())
			return
		}
		if session.Role != "admin" {
			if err := validateOperatorMirrorSecretBindings(m, updated); err != nil {
				writeError(w, http.StatusForbidden, err.Error())
				return
			}
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
			writeError(w, 422, validationMessageForRole(result, err, session.Role))
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
			writeError(w, 502, activationMessageForRole("repository saved as desired state but Managed Upstream Nginx activation failed", err, session.Role))
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
		writeJSON(w, 200, mirrorForRole(updated, session.Role))
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
			writeError(w, 422, validationMessageForRole(result, err, session.Role))
			return
		}
		if _, err := s.cache.Purge(r.Context(), "repository", id, "", session.Username); err != nil {
			writeInternal(w, err)
			return
		}
		s.broadcastClusterPurge(r, session.Username, model.ClusterPurgeRequest{Scope: "repository", RepositorySlug: m.Slug})
		if err := s.store.DeleteMirror(r.Context(), id); err != nil {
			writeInternal(w, err)
			return
		}
		if _, err := s.upstreamNginx.Reconcile(r.Context(), session.Username, "delete repository "+m.Slug); err != nil {
			_ = s.audit(r, session.Username, "delete", "repository", err.Error(), false)
			writeError(w, 502, activationMessageForRole("repository deleted from desired state but Managed Upstream Nginx activation failed", err, session.Role))
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
			writeError(w, 422, validationMessageForRole(result, err, session.Role))
			return
		}
		if err := s.store.SetMirrorEnabled(r.Context(), id, enabled); err != nil {
			writeInternal(w, err)
			return
		}
		if _, err := s.upstreamNginx.Reconcile(r.Context(), session.Username, parts[1]+" repository "+m.Slug); err != nil {
			writeError(w, 502, activationMessageForRole("desired state saved but Managed Upstream Nginx activation failed", err, session.Role))
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
		writeJSON(w, 200, healthResultsForRole(results, session.Role))
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
		if !s.requireRole(w, session, "admin") {
			return
		}
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
		if session.Role != "admin" {
			preserveMirrorSecrets(m, &candidate)
		}
		if err := mirror.NormalizeAndValidate(&candidate, s.cfg.Security.AllowHTTPUpstream, s.cfg.Security.AllowPrivateUpstream); err != nil {
			writeError(w, 400, err.Error())
			return
		}
		if session.Role != "admin" {
			if err := validateOperatorMirrorSecretBindings(m, candidate); err != nil {
				writeError(w, http.StatusForbidden, err.Error())
				return
			}
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
			writeError(w, 422, validationMessageForRole(result, err, session.Role))
			return
		}
		if parts[2] == "preview" {
			beforeResponse := mirrorForRole(m, session.Role)
			candidateResponse := mirrorForRole(candidate, session.Role)
			response := map[string]any{"repository": candidateResponse, "diff": profileDiff(beforeResponse, candidateResponse), "configuration_hash": generated.Hash}
			if session.Role == "admin" {
				response["configuration"] = generated.Effective
				response["validation_result"] = result
			}
			writeJSON(w, 200, response)
			return
		}
		candidate.ConfigState = "pending"
		updated, err := s.store.UpdateMirror(r.Context(), candidate)
		if err != nil {
			writeInternal(w, err)
			return
		}
		if _, err := s.upstreamNginx.Reconcile(r.Context(), session.Username, "apply profile "+input.Name+" "+input.Version+" to "+m.Slug); err != nil {
			writeError(w, 502, activationMessageForRole("profile desired state saved but Managed Upstream Nginx activation failed", err, session.Role))
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
		writeJSON(w, 200, mirrorForRole(updated, session.Role))
		return
	}
	writeError(w, 404, "not found")
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
	purgeRequest := model.ClusterPurgeRequest{Scope: scope}
	if id > 0 {
		if repository, found := s.registry.GetByID(id); found {
			purgeRequest.RepositorySlug = repository.Slug
		} else if repository, lookupErr := s.store.Mirror(r.Context(), id); lookupErr == nil {
			purgeRequest.RepositorySlug = repository.Slug
		}
	}
	clusterSummary := s.broadcastClusterPurge(r, session.Username, purgeRequest)
	_ = s.audit(r, session.Username, "clear_cache", "cache", object, true)
	writeJSON(w, 200, addClusterPurgeSummary(map[string]any{"logical_purge": "completed", "physical_reclaim": job.ReclaimState, "job": job}, clusterSummary))
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
		clusterSummary := s.broadcastClusterPurge(r, session.Username, model.ClusterPurgeRequest{Scope: "repository", RepositorySlug: repository.Slug})
		_ = s.audit(r, session.Username, "cache_purge", "repository", repository.Slug, true)
		writeJSON(w, 200, addClusterPurgeSummary(map[string]any{"logical_purge": "completed", "physical_reclaim": job.ReclaimState, "job": job}, clusterSummary))
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
	clusterSummary := s.broadcastClusterPurge(r, session.Username, model.ClusterPurgeRequest{
		Scope: "object", RepositorySlug: repository.Slug, ObjectID: objectID, ObjectPath: input.Path,
	})
	_ = s.audit(r, session.Username, "cache_purge", "object", repository.Slug+":"+input.Path, true)
	writeJSON(w, 200, addClusterPurgeSummary(map[string]any{"logical_purge": "completed", "physical_reclaim": job.ReclaimState, "object_id": objectID, "job": job}, clusterSummary))
}

func (s *Server) validateUpstreams(parent context.Context, m model.Mirror) error {
	ctx, cancel := context.WithTimeout(parent, 10*time.Second)
	defer cancel()
	for index, u := range m.Upstreams {
		if !u.Enabled {
			continue
		}
		if err := security.ValidateResolvedURL(ctx, u.URL, s.cfg.Security.AllowHTTPUpstream && m.AllowHTTP,
			s.cfg.Security.AllowPrivateUpstream && m.AllowPrivate, net.DefaultResolver); err != nil {
			return fmt.Errorf("upstream %d: %w", index+1, err)
		}
	}
	if m.TokenUpstream != "" {
		if err := security.ValidateResolvedURL(ctx, m.TokenUpstream, s.cfg.Security.AllowHTTPUpstream && m.AllowHTTP,
			s.cfg.Security.AllowPrivateUpstream && m.AllowPrivate, net.DefaultResolver); err != nil {
			return fmt.Errorf("token upstream: %w", err)
		}
	}
	return nil
}
