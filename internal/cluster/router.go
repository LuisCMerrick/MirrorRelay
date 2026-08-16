package cluster

import (
	"errors"
	"hash/fnv"
	"net"
	"sort"
	"strings"
	"sync"

	"github.com/LuisCMerrick/MirrorRelay/internal/config"
	"github.com/LuisCMerrick/MirrorRelay/internal/model"
)

var (
	ErrNoAvailableEdge               = errors.New("no available edge")
	ErrDistributedRegistryNotAllowed = errors.New("distributed registry routing is not supported")
)

type Router struct {
	mu           sync.RWMutex
	cfg          config.Config
	nodes        []model.ClusterNode
	parsedCIDRs  []parsedNetwork
	regionLookup map[string][]string // country -> regions
}

type parsedNetwork struct {
	cidr   *net.IPNet
	region string
}

func NewRouter(cfg config.Config) *Router {
	r := &Router{
		cfg:          cfg,
		nodes:        nil,
		regionLookup: make(map[string][]string),
	}
	r.rebuildNetworkIndex()
	return r
}

func (r *Router) UpdateConfig(cfg config.Config) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cfg = cfg
	r.rebuildNetworkIndex()
}

func (r *Router) rebuildNetworkIndex() {
	var parsed []parsedNetwork
	for _, m := range r.cfg.Distributed.Routing.ClientNetworks {
		if _, ipNet, err := net.ParseCIDR(m.CIDR); err == nil {
			parsed = append(parsed, parsedNetwork{cidr: ipNet, region: m.Region})
		}
	}
	r.parsedCIDRs = parsed

	lookup := make(map[string][]string)
	for _, reg := range r.cfg.Distributed.Routing.Regions {
		for _, country := range reg.Countries {
			c := strings.ToUpper(strings.TrimSpace(country))
			if c != "" {
				lookup[c] = append(lookup[c], reg.Code)
			}
		}
	}
	r.regionLookup = lookup
}

func (r *Router) SetNodes(nodes []model.ClusterNode) {
	r.mu.Lock()
	defer r.mu.Unlock()
	copied := make([]model.ClusterNode, len(nodes))
	copy(copied, nodes)
	r.nodes = copied
}

func (r *Router) Nodes() []model.ClusterNode {
	r.mu.RLock()
	defer r.mu.RUnlock()
	copied := make([]model.ClusterNode, len(r.nodes))
	copy(copied, r.nodes)
	return copied
}

func (r *Router) SelectNode(clientIP string, repository model.Mirror, clusterFingerprint string) (*model.ClusterNode, error) {
	if repository.Type == "docker-registry" || repository.Type == "oci-registry" {
		return nil, ErrDistributedRegistryNotAllowed
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	var candidates []model.ClusterNode
	repoType := strings.ToLower(strings.TrimSpace(repository.Type))

	for _, n := range r.nodes {
		if !n.Enabled {
			continue
		}
		if n.HealthStatus != "healthy" {
			continue
		}
		if n.ProtocolVersion > 0 && n.ProtocolVersion != ClusterProtocolVersion {
			continue
		}
		if clusterFingerprint != "" && n.ConfigFingerprint != "" && n.ConfigFingerprint != clusterFingerprint {
			continue
		}
		if n.ConfigStatus == "mismatch" || n.ConfigStatus == "drifted" || n.ConfigStatus == "version_incompatible" {
			continue
		}
		if len(n.Capabilities) > 0 && repoType != "" {
			supported := false
			for _, cap := range n.Capabilities {
				if strings.EqualFold(cap, repoType) {
					supported = true
					break
				}
			}
			if !supported {
				continue
			}
		}
		candidates = append(candidates, n)
	}

	if len(candidates) == 0 {
		return nil, ErrNoAvailableEdge
	}

	// 1. CIDR / Client network mapping
	ip := net.ParseIP(strings.TrimSpace(clientIP))
	var targetRegion string
	if ip != nil {
		for _, netMap := range r.parsedCIDRs {
			if netMap.cidr.Contains(ip) {
				targetRegion = netMap.region
				break
			}
		}
	}

	if targetRegion != "" {
		var regionCandidates []model.ClusterNode
		for _, c := range candidates {
			if strings.EqualFold(c.Region, targetRegion) {
				regionCandidates = append(regionCandidates, c)
			}
		}
		if len(regionCandidates) > 0 {
			candidates = regionCandidates
		}
	}

	// 2. Priority filtering (lowest priority number wins)
	minPriority := candidates[0].Priority
	for _, c := range candidates {
		if c.Priority < minPriority {
			minPriority = c.Priority
		}
	}
	var priorityCandidates []model.ClusterNode
	for _, c := range candidates {
		if c.Priority == minPriority {
			priorityCandidates = append(priorityCandidates, c)
		}
	}
	candidates = priorityCandidates

	// 3. Stable Weighted selection / Consistent hashing
	if len(candidates) == 1 {
		selected := candidates[0]
		return &selected, nil
	}

	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Priority != candidates[j].Priority {
			return candidates[i].Priority < candidates[j].Priority
		}
		return candidates[i].ID < candidates[j].ID
	})

	totalWeight := 0
	for _, c := range candidates {
		w := c.Weight
		if w <= 0 {
			w = 100
		}
		totalWeight += w
	}
	if totalWeight <= 0 {
		totalWeight = len(candidates)
	}

	h := fnv.New32a()
	_, _ = h.Write([]byte(clientIP))
	_, _ = h.Write([]byte(":"))
	_, _ = h.Write([]byte(repository.Slug))
	_, _ = h.Write([]byte(":"))
	_, _ = h.Write([]byte(clusterFingerprint))
	val := int(h.Sum32() % uint32(totalWeight))

	cur := 0
	for _, c := range candidates {
		w := c.Weight
		if w <= 0 {
			w = 100
		}
		cur += w
		if val < cur {
			selected := c
			return &selected, nil
		}
	}

	selected := candidates[0]
	return &selected, nil
}
