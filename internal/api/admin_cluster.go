package api

import (
	"context"
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

func clusterRequestToken(r *http.Request, headerName string) string {
	value := r.Header.Get(headerName)
	if value == "" {
		authorization := r.Header.Get("Authorization")
		if strings.HasPrefix(authorization, "Bearer ") {
			value = strings.TrimPrefix(authorization, "Bearer ")
		}
	}
	return value
}

func verifyClusterCredential(r *http.Request, headerName, expected string) bool {
	if expected == "" {
		return false
	}
	provided := clusterRequestToken(r, headerName)
	return len(provided) == len(expected) && subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) == 1
}

func (s *Server) verifyClusterProbeToken(r *http.Request) bool {
	return verifyClusterCredential(r, "X-MirrorRelay-Cluster-Token", s.cfg.Distributed.Token)
}

func (s *Server) verifyClusterMutationToken(r *http.Request) bool {
	return verifyClusterCredential(r, "X-MirrorRelay-Cluster-Mutation-Token", s.cfg.Distributed.MutationToken)
}

func (s *Server) clusterHealth() model.ClusterHealth {
	hStatus := "healthy"
	repoHealth := make(map[string]bool)
	repositories, custom, available := s.activeClusterConfiguration()
	if !available {
		hStatus = "degraded"
		repositories = s.registry.List()
		custom = []model.CustomConfig{}
	}
	healthRepositories := repositories
	if s.registry != nil {
		healthRepositories = s.registry.List()
	}
	for _, m := range healthRepositories {
		if !m.Enabled {
			continue
		}
		healthState := repositoryHealthState(m)
		// A repository with checks enabled must complete a successful probe
		// before it is advertised as routable. Explicitly disabled health
		// checks remain an administrator-approved viable state.
		viable := healthState == "healthy" || healthState == "disabled"
		repoHealth[m.Slug] = viable
		if !viable {
			hStatus = "degraded"
		}
	}
	manifest := s.clusterManifestForConfiguration(repositories, custom)
	return model.ClusterHealth{
		Status:            hStatus,
		Version:           s.build.Version,
		ConfigGeneration:  manifest.ConfigGeneration,
		ConfigFingerprint: manifest.ConfigFingerprint,
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
		nodes, err := s.store.ListClusterNodes(r.Context())
		if err != nil {
			writeInternal(w, err)
			return
		}
		total = len(nodes)
		for _, n := range nodes {
			if n.HealthStatus == "healthy" {
				healthy++
			}
			isMatch := fp != "" && n.ConfigFingerprint == fp && n.ConfigStatus == "match"
			hasHealthyRepository := false
			for _, healthy := range n.RepositoryHealth {
				hasHealthyRepository = hasHealthyRepository || healthy
			}
			if n.Enabled && (n.HealthStatus == "healthy" || n.HealthStatus == "degraded") && isMatch &&
				n.ProtocolVersion == cluster.ClusterProtocolVersion && hasHealthyRepository {
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
	for index := range nodes {
		nodes[index] = clusterNodeForResponse(nodes[index])
	}
	writeJSON(w, http.StatusOK, nodes)
}

func clusterNodeForResponse(node model.ClusterNode) model.ClusterNode {
	node.MutationTokenConfigured = node.MutationToken != ""
	node.MutationToken = ""
	return node
}

func (s *Server) refreshClusterRouter(ctx context.Context) error {
	if s.clusterRouter == nil || s.store == nil {
		return nil
	}
	nodes, err := s.store.ListClusterNodes(ctx)
	if err != nil {
		return err
	}
	s.clusterRouter.SetNodes(nodes)
	return nil
}

func (s *Server) validateClusterMutationToken(ctx context.Context, excludedNodeID int64, token string) (string, error) {
	if token == s.cfg.Distributed.Token {
		return "edge mutation token must differ from the cluster probe credential", nil
	}
	nodes, err := s.store.ListClusterNodes(ctx)
	if err != nil {
		return "", err
	}
	for _, node := range nodes {
		if node.ID != excludedNodeID && node.MutationToken == token {
			return "edge mutation token is already assigned to another node", nil
		}
	}
	return "", nil
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
	if strings.TrimSpace(in.MutationToken) == "" {
		writeError(w, http.StatusBadRequest, "a unique edge mutation token is required")
		return
	}
	in.MutationToken = strings.TrimSpace(in.MutationToken)
	if message, err := s.validateClusterMutationToken(r.Context(), 0, in.MutationToken); err != nil {
		writeInternal(w, err)
		return
	} else if message != "" {
		writeError(w, http.StatusBadRequest, message)
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
		probed, checkErr := s.clusterChecker.CheckNode(r.Context(), created)
		created = probed
		if checkErr != nil {
			writeInternal(w, checkErr)
			return
		}
	}
	if err := s.refreshClusterRouter(r.Context()); err != nil {
		writeInternal(w, err)
		return
	}

	_ = s.audit(r, session.Username, "create_cluster_node", "cluster_node", created.Name, true)
	writeJSON(w, http.StatusCreated, clusterNodeForResponse(created))
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
		if strings.TrimSpace(in.MutationToken) == "" {
			in.MutationToken = node.MutationToken
		} else {
			in.MutationToken = strings.TrimSpace(in.MutationToken)
		}
		if message, err := s.validateClusterMutationToken(r.Context(), id, in.MutationToken); err != nil {
			writeInternal(w, err)
			return
		} else if message != "" {
			writeError(w, http.StatusBadRequest, message)
			return
		}
		updated, err := s.store.UpdateClusterNode(r.Context(), in)
		if err != nil {
			writeInternal(w, err)
			return
		}
		if s.clusterChecker != nil {
			probed, checkErr := s.clusterChecker.CheckNode(r.Context(), updated)
			updated = probed
			if checkErr != nil {
				writeInternal(w, checkErr)
				return
			}
		}
		if err := s.refreshClusterRouter(r.Context()); err != nil {
			writeInternal(w, err)
			return
		}
		_ = s.audit(r, session.Username, "update_cluster_node", "cluster_node", updated.Name, true)
		writeJSON(w, http.StatusOK, clusterNodeForResponse(updated))
		return
	}

	if len(parts) == 1 && r.Method == http.MethodDelete {
		if err := s.store.DeleteClusterNode(r.Context(), id); err != nil {
			writeInternal(w, err)
			return
		}
		if err := s.refreshClusterRouter(r.Context()); err != nil {
			writeInternal(w, err)
			return
		}
		_ = s.audit(r, session.Username, "delete_cluster_node", "cluster_node", node.Name, true)
		w.WriteHeader(http.StatusNoContent)
		return
	}

	if len(parts) == 2 && parts[1] == "check" && r.Method == http.MethodPost {
		if s.clusterChecker != nil {
			probed, checkErr := s.clusterChecker.CheckNode(r.Context(), node)
			node = probed
			if checkErr != nil {
				writeInternal(w, checkErr)
				return
			}
		}
		if err := s.refreshClusterRouter(r.Context()); err != nil {
			writeInternal(w, err)
			return
		}
		writeJSON(w, http.StatusOK, clusterNodeForResponse(node))
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
		if res.Success {
			if err := s.refreshClusterRouter(r.Context()); err != nil {
				writeInternal(w, err)
				return
			}
		}
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
		if err := s.refreshClusterRouter(r.Context()); err != nil {
			writeInternal(w, err)
			return
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
	if err := s.refreshClusterRouter(r.Context()); err != nil {
		_ = s.audit(r, session.Username, "broadcast_cluster_sync", "cluster", err.Error(), false)
		writeInternal(w, err)
		return
	}
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
		repositories, custom, available := s.activeClusterConfiguration()
		if !available {
			writeError(w, http.StatusConflict, "active configuration snapshot is unavailable")
			return
		}
		manifest := s.clusterManifestForConfiguration(repositories, custom)
		if err := s.clusterChecker.SetExpectedConfiguration(r.Context(), manifest); err != nil {
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
	manifest := cluster.GenerateManifest(s.cfg, repositories, custom, s.build, generation,
		strings.TrimSpace(s.cfg.Distributed.Node.Name), s.clusterEpoch)
	return model.ClusterSyncRequest{Manifest: manifest, Repositories: repositories, CustomConfigs: custom}, nil
}
