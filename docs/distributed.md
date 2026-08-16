# Distributed Deployment Guide

[English](distributed.md) | [简体中文](distributed.zh-CN.md)

MirrorRelay includes built-in distributed clustering capabilities, allowing you to deploy a central Coordinator node with multiple geographically distributed Edge nodes.

---

## 1. Cluster Architecture

```text
                                  ┌───────────────────────────────┐
                                  │      Client Requests          │
                                  │ (apt / dnf / pip / npm)       │
                                  └───────────────┬───────────────┘
                                                  │
                                                  │ 1. GET /debian/...
                                                  ▼
                                  ┌───────────────────────────────┐
                                  │       Coordinator Node        │
                                  │   - Repository Definitions    │
                                  │   - Health Probes & Checks    │
                                  │   - Fingerprint Verifications │
                                  │   - Routing Engine            │
                                  └───────┬───────────────┬───────┘
                                          │               │
                 2. HTTP 307 Redirect     │               │ 2. HTTP 307 Redirect
              (to Edge Node 1: US East)   │               │ (to Edge Node 2: EU West)
                                          ▼               ▼
          ┌──────────────────────────────────┐ ┌──────────────────────────────────┐
          │        Edge Node 1 (US)          │ │        Edge Node 2 (EU)          │
          │  - Autonomous Upstream Nginx     │ │  - Autonomous Upstream Nginx     │
          │  - Local Disk Cache Store        │ │  - Local Disk Cache Store        │
          │  - Direct Client Serving         │ │  - Direct Client Serving         │
          └────────────────┬─────────────────┘ └────────────────┬─────────────────┘
                           │                                    │
                           ▼                                    ▼
                Original Upstream Server             Original Upstream Server
```

---

## 2. Cluster Roles

### Coordinator Node
- **Role**: Central configuration authority and traffic dispatcher.
- **Responsibilities**:
  - Manages global repository configurations and profile definitions via Web UI/API.
  - Actively probes Edge nodes for availability and configuration consistency.
  - Matches client requests against routing rules and issues HTTP `307 Temporary Redirect` responses.
  - Returns `HTTP 503 Service Unavailable` with `error: no_available_edge` if no healthy edge node is available (the Coordinator never acts as a data-plane fallback or origin proxy).

### Edge Node
- **Role**: Regional high-speed caching proxy.
- **Responsibilities**:
  - Receives redirected client traffic and serves requested packages directly.
  - Maintains an independent, high-throughput Managed Upstream Nginx cache storage.
  - Periodically exposes health metrics and configuration fingerprints to the Coordinator.
  - Operates autonomously even during transient Coordinator network partitions.

---

## 3. Traffic Routing Policies

The `distributed.routing.mode` setting controls how the Coordinator selects candidate edge nodes. Valid modes are:
- `hybrid` (default): Evaluates client CIDR and Geo region mappings first, selects nodes with the lowest priority value (highest precedence), and balances traffic proportionally using node weights.
- `cidr`: Strictly routes based on `client_networks` CIDR subnet matching.
- `geo`: Strictly routes based on country and region mapping.
- `priority`: Strictly selects the node with the lowest numeric priority value (e.g. priority 1 takes precedence over priority 100).

Candidates are filtered before selection:
1. **Health & Consistency Filtering**: Nodes that fail health probes (`health_status != "healthy"`) or have mismatched configuration fingerprints are automatically excluded.
2. **Capability Check**: Ensures the edge node supports the requested repository type.
3. **Weight Distribution**: When multiple candidates share the same priority, requests are distributed proportionally according to each node's `weight`.

If all candidate nodes are unhealthy or offline, the Coordinator returns `HTTP 503 Service Unavailable`.

---

## 4. Configuration Examples

### Coordinator Node (`/etc/mirrorrelay/config.yaml`)

```yaml
distributed:
  enabled: true
  role: coordinator           # standalone, coordinator, edge
  token: "secret-shared-cluster-token-32bytes"
  node:
    name: "coord-01"
    public_base_url: "https://repo-hub.example.com"
    region: "us-east"
    country: "US"
  routing:
    mode: hybrid              # hybrid, cidr, geo, priority
    client_networks:
      - cidr: "10.10.0.0/16"
        region: "us-east"
      - cidr: "10.20.0.0/16"
        region: "eu-west"
    regions:
      - code: "us-east"
        countries: ["US", "CA"]
      - code: "eu-west"
        countries: ["GB", "DE", "FR"]
  health_check:
    interval: 10s
    timeout: 3s
    healthy_threshold: 2
    unhealthy_threshold: 3
  nodes:
    - name: "Edge US East"
      url: "https://edge-us.example.com"
      region: "us-east"
      country: "US"
      priority: 100
      weight: 10
      enabled: true
    - name: "Edge EU West"
      url: "https://edge-eu.example.com"
      region: "eu-west"
      country: "DE"
      priority: 100
      weight: 10
      enabled: true
```

### Edge Node (`/etc/mirrorrelay/config.yaml`)

```yaml
distributed:
  enabled: true
  role: edge                  # standalone, coordinator, edge
  token: "secret-shared-cluster-token-32bytes"
  node:
    name: "Edge US East"
    public_base_url: "https://edge-us.example.com"
    region: "us-east"
    country: "US"
```

---

## 5. Security & Isolation

- **Mutual Cluster Authentication**: Communications between Coordinator and Edge nodes (`/api/v1/cluster/manifest`, `/api/v1/cluster/health`) require a shared cryptographically secure cluster token verified in constant time.
- **SSRF Safety**: Node health probes are strictly validated against private IP restrictions when `allow_private_upstream` is disabled.
- **Cache Independence**: Each Edge node maintains isolated, content-addressed disk storage, eliminating cross-node cache contamination.
