package cluster

import (
	"fmt"
	"io"
	"sync"
	"sync/atomic"

	"github.com/LuisCMerrick/MirrorRelay/internal/model"
)

type Metrics struct {
	mu                   sync.RWMutex
	totalNodes           int64
	healthyNodes         int64
	routableNodes        int64
	configMismatchNodes  int64
	noAvailableEdgeTotal uint64

	redirectTotal       uint64
	redirectByRegion    map[string]*uint64
	redirectByNode      map[string]*uint64
	healthCheckFailures map[string]*uint64
}

func NewMetrics() *Metrics {
	return &Metrics{
		redirectByRegion:    make(map[string]*uint64),
		redirectByNode:      make(map[string]*uint64),
		healthCheckFailures: make(map[string]*uint64),
	}
}

func (m *Metrics) IncRedirect(nodeName, region string) {
	atomic.AddUint64(&m.redirectTotal, 1)

	m.mu.Lock()
	defer m.mu.Unlock()

	if region != "" {
		ptr, ok := m.redirectByRegion[region]
		if !ok {
			var v uint64
			ptr = &v
			m.redirectByRegion[region] = ptr
		}
		atomic.AddUint64(ptr, 1)
	}

	if nodeName != "" {
		ptr, ok := m.redirectByNode[nodeName]
		if !ok {
			var v uint64
			ptr = &v
			m.redirectByNode[nodeName] = ptr
		}
		atomic.AddUint64(ptr, 1)
	}
}

func (m *Metrics) IncNoAvailableEdge() {
	atomic.AddUint64(&m.noAvailableEdgeTotal, 1)
}

func (m *Metrics) IncHealthCheckFailure(nodeName string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if nodeName != "" {
		ptr, ok := m.healthCheckFailures[nodeName]
		if !ok {
			var v uint64
			ptr = &v
			m.healthCheckFailures[nodeName] = ptr
		}
		atomic.AddUint64(ptr, 1)
	}
}

func (m *Metrics) UpdateNodeStats(nodes []model.ClusterNode, clusterFingerprint string) {
	var total, healthy, routable, mismatch int64

	for _, n := range nodes {
		total++
		isHealthy := n.HealthStatus == "healthy"
		if isHealthy {
			healthy++
		}
		isMatch := (clusterFingerprint == "" || n.ConfigFingerprint == clusterFingerprint) && n.ConfigStatus != "mismatch" && n.ConfigStatus != "drifted"
		if !isMatch && n.ConfigStatus == "mismatch" {
			mismatch++
		}
		if n.Enabled && isHealthy && isMatch && (n.ProtocolVersion == 0 || n.ProtocolVersion == ClusterProtocolVersion) {
			routable++
		}
	}

	atomic.StoreInt64(&m.totalNodes, total)
	atomic.StoreInt64(&m.healthyNodes, healthy)
	atomic.StoreInt64(&m.routableNodes, routable)
	atomic.StoreInt64(&m.configMismatchNodes, mismatch)
}

func (m *Metrics) WritePrometheus(w io.Writer) {
	fmt.Fprintln(w, "# TYPE mirrorrelay_cluster_nodes gauge")
	fmt.Fprintf(w, "mirrorrelay_cluster_nodes %d\n", atomic.LoadInt64(&m.totalNodes))

	fmt.Fprintln(w, "# TYPE mirrorrelay_cluster_nodes_healthy gauge")
	fmt.Fprintf(w, "mirrorrelay_cluster_nodes_healthy %d\n", atomic.LoadInt64(&m.healthyNodes))

	fmt.Fprintln(w, "# TYPE mirrorrelay_cluster_nodes_routable gauge")
	fmt.Fprintf(w, "mirrorrelay_cluster_nodes_routable %d\n", atomic.LoadInt64(&m.routableNodes))

	fmt.Fprintln(w, "# TYPE mirrorrelay_cluster_config_mismatch gauge")
	fmt.Fprintf(w, "mirrorrelay_cluster_config_mismatch %d\n", atomic.LoadInt64(&m.configMismatchNodes))

	fmt.Fprintln(w, "# TYPE mirrorrelay_cluster_no_available_edge_total counter")
	fmt.Fprintf(w, "mirrorrelay_cluster_no_available_edge_total %d\n", atomic.LoadUint64(&m.noAvailableEdgeTotal))

	fmt.Fprintln(w, "# TYPE mirrorrelay_cluster_redirect_total counter")
	fmt.Fprintf(w, "mirrorrelay_cluster_redirect_total %d\n", atomic.LoadUint64(&m.redirectTotal))

	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.redirectByNode) > 0 {
		fmt.Fprintln(w, "# TYPE mirrorrelay_cluster_redirect_by_node_total counter")
		for node, ptr := range m.redirectByNode {
			fmt.Fprintf(w, "mirrorrelay_cluster_redirect_by_node_total{node=%q} %d\n", node, atomic.LoadUint64(ptr))
		}
	}

	if len(m.healthCheckFailures) > 0 {
		fmt.Fprintln(w, "# TYPE mirrorrelay_cluster_health_check_failures_total counter")
		for node, ptr := range m.healthCheckFailures {
			fmt.Fprintf(w, "mirrorrelay_cluster_health_check_failures_total{node=%q} %d\n", node, atomic.LoadUint64(ptr))
		}
	}
}
