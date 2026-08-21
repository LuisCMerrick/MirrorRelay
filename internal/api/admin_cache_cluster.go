package api

import (
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/LuisCMerrick/MirrorRelay/internal/model"
)

type clusterPurgeSummary struct {
	Targets   int               `json:"targets"`
	Succeeded int               `json:"succeeded"`
	Failed    int               `json:"failed"`
	Errors    map[string]string `json:"errors,omitempty"`
}

func (s *Server) broadcastClusterPurge(r *http.Request, operator string, payload model.ClusterPurgeRequest) *clusterPurgeSummary {
	if !s.cfg.Distributed.Enabled || s.cfg.Distributed.Role != "coordinator" || s.clusterSync == nil {
		return nil
	}
	results := s.clusterSync.BroadcastPurge(r.Context(), payload)
	summary := &clusterPurgeSummary{Targets: len(results), Errors: make(map[string]string)}
	ids := make([]int64, 0, len(results))
	for id := range results {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	details := make([]string, 0, len(ids))
	for _, id := range ids {
		err := results[id]
		if err == nil {
			summary.Succeeded++
			continue
		}
		summary.Failed++
		key := fmt.Sprintf("node_%d", id)
		summary.Errors[key] = err.Error()
		details = append(details, key+": "+err.Error())
	}
	if summary.Failed == 0 {
		summary.Errors = nil
	}
	detail := fmt.Sprintf("scope=%s targets=%d succeeded=%d failed=%d", payload.Scope, summary.Targets, summary.Succeeded, summary.Failed)
	if len(details) > 0 {
		detail += "; " + strings.Join(details, "; ")
	}
	_ = s.audit(r, operator, "cluster_cache_purge_broadcast", "cluster", detail, summary.Failed == 0)
	return summary
}

func addClusterPurgeSummary(response map[string]any, summary *clusterPurgeSummary) map[string]any {
	if summary != nil {
		response["cluster_broadcast"] = summary
	}
	return response
}
