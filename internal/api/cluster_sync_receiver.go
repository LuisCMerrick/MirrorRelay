package api

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"

	"github.com/LuisCMerrick/MirrorRelay/internal/cluster"
	"github.com/LuisCMerrick/MirrorRelay/internal/mirror"
	"github.com/LuisCMerrick/MirrorRelay/internal/model"
)

const (
	maxClusterSyncBody  = 16 << 20
	maxClusterPurgeBody = 64 << 10
)

func (s *Server) clusterSyncReceiver(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !s.cfg.Distributed.Enabled || s.cfg.Distributed.Token == "" {
		http.NotFound(w, r)
		return
	}
	if s.cfg.Distributed.Role != "edge" {
		writeError(w, http.StatusForbidden, "cluster sync receivers are available only on edge nodes")
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !s.verifyClusterToken(r) {
		writeError(w, http.StatusUnauthorized, "invalid cluster token")
		return
	}

	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()
	if r.URL.Path == cluster.SyncApplyPath {
		s.applyClusterSync(w, r)
		return
	}
	s.applyClusterPurge(w, r)
}

func (s *Server) applyClusterSync(w http.ResponseWriter, r *http.Request) {
	var payload model.ClusterSyncRequest
	if err := decodeBoundedClusterJSON(w, r, maxClusterSyncBody, &payload); err != nil {
		return
	}
	if payload.Repositories == nil || payload.CustomConfigs == nil {
		writeError(w, http.StatusBadRequest, "repositories and custom_configs must be JSON arrays")
		return
	}
	if payload.Manifest.ProtocolVersion != cluster.ClusterProtocolVersion {
		writeError(w, http.StatusConflict, "cluster protocol version is incompatible")
		return
	}
	if payload.Manifest.ConfigGeneration <= 0 || payload.Manifest.ConfigFingerprint == "" || strings.TrimSpace(payload.Manifest.NodeID) == "" {
		writeError(w, http.StatusBadRequest, "cluster manifest node, generation and fingerprint are required")
		return
	}
	for index := range payload.Repositories {
		if err := mirror.NormalizeAndValidate(&payload.Repositories[index], s.cfg.Security.AllowHTTPUpstream, s.cfg.Security.AllowPrivateUpstream); err != nil {
			writeError(w, http.StatusUnprocessableEntity, fmt.Sprintf("repository %d is invalid: %v", index, err))
			return
		}
	}
	if err := mirror.ValidateRouteConflicts(payload.Repositories, s.cfg.Admin.Path, s.cfg.HTTP.PublicBaseURL, s.cfg.Admin.Host); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	fingerprint := cluster.CanonicalFingerprint(payload.Repositories)
	if fingerprint == "" || fingerprint != payload.Manifest.ConfigFingerprint {
		writeError(w, http.StatusConflict, "cluster configuration fingerprint does not match the payload")
		return
	}
	capabilities := cluster.ExtractCapabilities(payload.Repositories)
	if !slices.Equal(capabilities, payload.Manifest.Capabilities) {
		writeError(w, http.StatusConflict, "cluster capabilities do not match the payload")
		return
	}
	if s.upstreamNginx == nil {
		writeError(w, http.StatusServiceUnavailable, "Managed Upstream Nginx is not initialized")
		return
	}
	if _, err := s.upstreamNginx.ApplyConfiguration(r.Context(), payload.Repositories, payload.CustomConfigs,
		"cluster", "apply coordinator configuration generation"); err != nil {
		_ = s.audit(r, "cluster", "cluster_sync_apply", "cluster", err.Error(), false)
		writeError(w, http.StatusUnprocessableEntity, "cluster configuration activation failed: "+err.Error())
		return
	}
	if err := s.publishActiveRouting(); err != nil {
		_ = s.audit(r, "cluster", "cluster_sync_apply", "cluster", err.Error(), false)
		writeInternal(w, err)
		return
	}
	actualRepositories := s.registry.List()
	actualFingerprint := cluster.CanonicalFingerprint(actualRepositories)
	actualCapabilities := cluster.ExtractCapabilities(actualRepositories)
	if actualFingerprint != payload.Manifest.ConfigFingerprint || !slices.Equal(actualCapabilities, payload.Manifest.Capabilities) {
		writeError(w, http.StatusInternalServerError, "activated cluster configuration acknowledgement is inconsistent")
		return
	}
	_ = s.audit(r, "cluster", "cluster_sync_apply", "cluster", actualFingerprint, true)
	writeJSON(w, http.StatusOK, model.ClusterSyncResponse{
		Status:             "applied",
		Fingerprint:        actualFingerprint,
		ProtocolVersion:    cluster.ClusterProtocolVersion,
		ConfigGeneration:   payload.Manifest.ConfigGeneration,
		MirrorRelayVersion: s.build.Version,
		Capabilities:       actualCapabilities,
	})
}

func (s *Server) applyClusterPurge(w http.ResponseWriter, r *http.Request) {
	var payload model.ClusterPurgeRequest
	if err := decodeBoundedClusterJSON(w, r, maxClusterPurgeBody, &payload); err != nil {
		return
	}
	payload.Scope = strings.TrimSpace(payload.Scope)
	payload.RepositorySlug = strings.ToLower(strings.TrimSpace(payload.RepositorySlug))
	payload.ObjectID = strings.TrimSpace(payload.ObjectID)
	if s.cache == nil || s.registry == nil {
		writeError(w, http.StatusServiceUnavailable, "cache manager is not initialized")
		return
	}

	repositoryID := int64(0)
	switch payload.Scope {
	case "global":
		if payload.RepositorySlug != "" || payload.ObjectID != "" {
			writeError(w, http.StatusBadRequest, "global purge cannot include a repository or object")
			return
		}
	case "repository":
		if payload.RepositorySlug == "" || payload.ObjectID != "" {
			writeError(w, http.StatusBadRequest, "repository purge requires only repository_slug")
			return
		}
		repository, found := s.registry.Get(payload.RepositorySlug)
		if !found {
			writeError(w, http.StatusNotFound, "repository is not active on this edge")
			return
		}
		repositoryID = repository.ID
	case "object":
		if payload.RepositorySlug == "" || !validObjectID(payload.ObjectID) {
			writeError(w, http.StatusBadRequest, "object purge requires repository_slug and a SHA-256 object_id")
			return
		}
		repository, found := s.registry.Get(payload.RepositorySlug)
		if !found {
			writeError(w, http.StatusNotFound, "repository is not active on this edge")
			return
		}
		repositoryID = repository.ID
	default:
		writeError(w, http.StatusBadRequest, "purge scope must be global, repository or object")
		return
	}

	job, err := s.cache.Purge(r.Context(), payload.Scope, repositoryID, payload.ObjectID, "cluster")
	if err != nil {
		_ = s.audit(r, "cluster", "cluster_cache_purge", payload.Scope, err.Error(), false)
		writeInternal(w, err)
		return
	}
	detail := payload.Scope
	if payload.RepositorySlug != "" {
		detail += ":" + payload.RepositorySlug
	}
	if payload.ObjectPath != "" {
		detail += ":" + payload.ObjectPath
	}
	_ = s.audit(r, "cluster", "cluster_cache_purge", payload.Scope, detail, true)
	writeJSON(w, http.StatusOK, model.ClusterPurgeResponse{
		Status:          "applied",
		Scope:           payload.Scope,
		RepositorySlug:  payload.RepositorySlug,
		ObjectID:        payload.ObjectID,
		Generation:      job.NewGeneration,
		PhysicalReclaim: job.ReclaimState,
	})
}

func decodeBoundedClusterJSON(w http.ResponseWriter, r *http.Request, maximum int64, out any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maximum)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("multiple JSON values are not allowed")
		}
		writeError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return err
	}
	return nil
}

func validObjectID(value string) bool {
	if len(value) != 64 {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == 32 && strings.ToLower(value) == value
}
