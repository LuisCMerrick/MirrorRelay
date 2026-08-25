package api

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/LuisCMerrick/MirrorRelay/internal/model"
)

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
	writeJSON(w, 200, map[string]any{
		"mirrors":           len(mirrors),
		"enabled_mirrors":   enabled,
		"healthy_mirrors":   healthy,
		"unhealthy_mirrors": unhealthy,
		"stats":             s.stats.Snapshot(),
		"cache":             cacheSummary,
		"uptime_seconds":    int64(time.Since(s.started).Seconds()),
		"version":           s.build.Version,
		"build_id":          s.build.BuildID,
		"architecture":      s.build.Architecture,
	})
}

func (s *Server) healthStatus(w http.ResponseWriter, _ *http.Request, role string) {
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
	response := map[string]any{
		"status":                 status,
		"mirrorrelay":            "healthy",
		"frontend_socket":        "healthy",
		"frontend_endpoint":      "healthy",
		"external_shared_nginx":  "external",
		"go_router":              "healthy",
		"managed_upstream_nginx": upstreamNginxStatus.State,
		"upstream_endpoint":      upstreamEndpoint,
		"repositories":           repositories,
	}
	if role == "admin" {
		response["frontend_network"] = frontendNetwork
		response["frontend_address"] = frontendAddress
		response["upstream_network"] = network
		response["upstream_address"] = address
	}
	writeJSON(w, 200, response)
}

func repositoryHealthState(repository model.Mirror) string {
	if !repository.Enabled || !repository.HealthCheckEnabled {
		return "disabled"
	}
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
	fmt.Fprintln(w, "# TYPE mirrorrelay_up gauge")
	fmt.Fprintln(w, "mirrorrelay_up 1")
	fmt.Fprintln(w, "# TYPE mirrorrelay_managed_upstream_nginx_up gauge")
	upstreamNginxUp := 0
	if s.upstreamNginx.Status().State == "running" {
		upstreamNginxUp = 1
	}
	fmt.Fprintf(w, "mirrorrelay_managed_upstream_nginx_up %d\n", upstreamNginxUp)
}

func (s *Server) audit(r *http.Request, user, action, object, detail string, ok bool) error {
	entry := model.AuditEntry{
		Time:      time.Now(),
		Username:  user,
		ClientIP:  s.requestClientIP(r),
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
