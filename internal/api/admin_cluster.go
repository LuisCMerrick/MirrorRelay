package api

import (
	"crypto/subtle"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/LuisCMerrick/MirrorRelay/internal/auth"
	"github.com/LuisCMerrick/MirrorRelay/internal/cluster"
	"github.com/LuisCMerrick/MirrorRelay/internal/database"
	"github.com/LuisCMerrick/MirrorRelay/internal/model"
)

func (s *Server) verifyClusterToken(r *http.Request) bool {
	if s.cfg.Distributed.Token == "" {
		return false
	}
	hdr := r.Header.Get("X-MirrorRelay-Cluster-Token")
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
	canonicalURL, err := cluster.ValidateNodeURL(r.Context(), s.cfg, in.URL)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid node url: "+err.Error())
		return
	}
	in.URL = canonicalURL
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
		canonicalURL, err := cluster.ValidateNodeURL(r.Context(), s.cfg, in.URL)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid node url: "+err.Error())
			return
		}
		in.URL = canonicalURL
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

	if len(parts) == 2 && parts[1] == "sync" && r.Method == http.MethodPost {
		if s.clusterSync == nil {
			writeError(w, http.StatusBadRequest, "cluster sync is not initialized")
			return
		}
		payload, err := s.clusterSyncPayload()
		if err != nil {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		res := s.clusterSync.SyncNode(r.Context(), node, payload)
		detail := node.Name
		if res.Error != "" {
			detail += ": " + res.Error
		}
		_ = s.audit(r, session.Username, "sync_cluster_node", "cluster_node", detail, res.Success)
		writeJSON(w, http.StatusOK, res)
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

func (s *Server) syncAllClusterNodes(w http.ResponseWriter, r *http.Request, session auth.Session) {
	if s.clusterSync == nil {
		writeError(w, http.StatusBadRequest, "cluster sync is not initialized")
		return
	}
	payload, err := s.clusterSyncPayload()
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	results := s.clusterSync.BroadcastSync(r.Context(), payload)
	succeeded := 0
	failures := make([]string, 0)
	for _, result := range results {
		if result.Success {
			succeeded++
		} else {
			failures = append(failures, fmt.Sprintf("node %d (%s): %s", result.NodeID, result.NodeURL, result.Error))
		}
	}
	ok := len(results) > 0 && len(failures) == 0
	detail := fmt.Sprintf("succeeded=%d failed=%d", succeeded, len(failures))
	if len(failures) > 0 {
		detail += "; " + strings.Join(failures, "; ")
	}
	_ = s.audit(r, session.Username, "broadcast_cluster_sync", "cluster", detail, ok)
	writeJSON(w, http.StatusOK, results)
}

func (s *Server) resetClusterFingerprint(w http.ResponseWriter, r *http.Request, session auth.Session) {
	if s.clusterChecker != nil {
		fingerprint := cluster.CanonicalFingerprint(s.registry.List())
		if err := s.clusterChecker.SetClusterFingerprint(r.Context(), fingerprint); err != nil {
			_ = s.audit(r, session.Username, "reset_cluster_fingerprint", "cluster", err.Error(), false)
			writeInternal(w, err)
			return
		}
		if err := s.clusterChecker.CheckAll(r.Context()); err != nil {
			_ = s.audit(r, session.Username, "reset_cluster_fingerprint", "cluster", err.Error(), false)
			writeInternal(w, err)
			return
		}
	}
	_ = s.audit(r, session.Username, "reset_cluster_fingerprint", "cluster", "", true)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) clusterSyncPayload() (model.ClusterSyncRequest, error) {
	if s.upstreamNginx == nil {
		return model.ClusterSyncRequest{}, fmt.Errorf("Managed Upstream Nginx is not initialized")
	}
	repositories, custom, available := s.upstreamNginx.ActiveConfiguration()
	if !available {
		return model.ClusterSyncRequest{}, fmt.Errorf("active configuration snapshot is unavailable")
	}
	generation := s.upstreamNginx.Status().CurrentConfigVersion
	if generation <= 0 {
		generation = 1
	}
	manifest := cluster.GenerateManifest(s.cfg, repositories, s.build, generation)
	return model.ClusterSyncRequest{Manifest: manifest, Repositories: repositories, CustomConfigs: custom}, nil
}
