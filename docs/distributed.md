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
  - Serves as an automatic fallback origin if all edge nodes are unreachable.

### Edge Node
- **Role**: Regional high-speed caching proxy.
- **Responsibilities**:
  - Receives redirected client traffic and serves requested packages directly.
  - Maintains an independent, high-throughput Managed Upstream Nginx cache storage.
  - Periodically exposes health metrics and configuration fingerprints to the Coordinator.
  - Operates autonomously even during transient Coordinator network partitions.

---

## 3. Traffic Routing Policies

The Coordinator evaluates routing rules in the following sequence:

1. **Client IP / CIDR Matching**: Routes clients from designated subnet ranges (e.g. `10.0.0.0/8` or `192.168.1.0/24`) to specific edge nodes.
2. **Geo / Region Matching**: Uses GeoIP metadata or configured region tags (e.g. `us-east`, `eu-west`, `ap-southeast`) to direct clients to the geographically closest node.
3. **Priority & Weight**: When multiple nodes match, selects the highest-priority node or distributes requests proportionally according to node weights.
4. **Health & Consistency Filtering**: Automatically filters out nodes that fail health checks or have mismatched configuration fingerprints.

---

## 4. Configuration Examples

### Coordinator Node (`/etc/mirrorrelay/config.yaml`)

```yaml
distributed:
  enabled: true
  role: coordinator
  token: "secret-shared-cluster-token-32bytes"
  node:
    id: "coord-01"
    name: "Primary Coordinator"
    public_url: "https://repo-hub.example.com"
  routing:
    strategy: "hybrid"            # cidr, geo, priority, weight, hybrid
    fallback_to_local: true
  health_check:
    interval: "15s"
    timeout: "3s"
  nodes:
    - id: "edge-us"
      name: "Edge US East"
      url: "https://edge-us.example.com"
      region: "us-east"
      priority: 100
      weight: 10
      cidrs:
        - "10.10.0.0/16"
    - id: "edge-eu"
      name: "Edge EU West"
      url: "https://edge-eu.example.com"
      region: "eu-west"
      priority: 100
      weight: 10
      cidrs:
        - "10.20.0.0/16"
```

### Edge Node (`/etc/mirrorrelay/config.yaml`)

```yaml
distributed:
  enabled: true
  role: edge
  token: "secret-shared-cluster-token-32bytes"
  node:
    id: "edge-us"
    name: "Edge US East"
    public_url: "https://edge-us.example.com"
```

---

## 5. Security & Isolation

- **Mutual Cluster Authentication**: Communications between Coordinator and Edge nodes (`/api/v1/cluster/manifest`, `/api/v1/cluster/health`) require a shared cryptographically secure cluster token verified in constant time.
- **SSRF Safety**: Node health probes are strictly validated against private IP restrictions when `allow_private_upstream` is disabled.
- **Cache Independence**: Each Edge node maintains isolated, content-addressed disk storage, eliminating cross-node cache contamination.
