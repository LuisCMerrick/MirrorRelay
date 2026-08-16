package api

import (
	"crypto/subtle"
	"net/http"
	"net/url"
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
