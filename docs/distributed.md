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
1. **Health & Consistency Filtering**: A healthy node may serve its healthy repositories. A degraded node may serve only repositories that it explicitly reports healthy; missing, unknown, or unhealthy repository state is never routable. Protocol, Coordinator identity/epoch, Active configuration generation, and fingerprint must all match.
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
  token: "read-only-probe-token-at-least-32bytes"
  allow_http: false           # HTTPS is required unless explicitly enabled
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
      mutation_token: "unique-edge-us-mutation-token-32bytes"
      region: "us-east"
      country: "US"
      priority: 100
      weight: 10
      enabled: true
    - name: "Edge EU West"
      url: "https://edge-eu.example.com"
      mutation_token: "unique-edge-eu-mutation-token-32bytes"
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
  token: "read-only-probe-token-at-least-32bytes"
  mutation_token: "unique-edge-us-mutation-token-32bytes"
  coordinator_id: "coord-01"
  allow_http: false
  node:
    name: "Edge US East"
    public_base_url: "https://edge-us.example.com"
    region: "us-east"
    country: "US"
```

---

## 5. Security & Isolation

- **Split Cluster Credentials**: `distributed.token` is a shared, read-only probe credential for manifest and health. Every Edge has a different `mutation_token` for sync and purge; it must differ from the probe credential and every other Edge token. Coordinator seed records store the matching per-Edge credential, API responses never return it, and the Web UI leaves it blank while editing. Credentials are verified in constant time.
- **Coordinator Binding & Replay Defense**: Every Edge configures `coordinator_id`. Protocol v2 sync and purge envelopes bind that identity to a persisted random Coordinator epoch. Edge persists the highest accepted generation and fingerprint before activation: older generations, conflicting same-generation payloads, and retired epochs are rejected; an identical applied payload is idempotent.
- **SSRF Safety**: A node URL is an origin only: credentials, path prefixes, query strings and fragments are rejected. HTTPS is required by default. Plaintext HTTP needs `distributed.allow_http: true`, while private/loopback/link-local targets separately require `security.allow_private_upstream: true`. Health checks, configuration sync and cache purge all enforce the same policy before sending the token, use policy-filtered addresses, preserve TLS hostname verification and do not follow redirects.
- **Cache Independence**: Each Edge node maintains isolated, content-addressed disk storage, eliminating cross-node cache contamination.

Protocol v2 is intentionally incompatible with protocol v1. Before enabling routing after an upgrade, configure a unique `distributed.node.name` on the Coordinator and every Edge, add a unique `mutation_token` to every Coordinator node seed/record, and configure the matching `mutation_token` plus `coordinator_id` on each Edge. Nodes remain unroutable until a v2 sync succeeds and a subsequent health probe confirms the requested repository.

---

## 6. Cluster Edge Sync & Distributed Cache Broadcast

MirrorRelay provides automated one-click and scheduled synchronization between the Coordinator and Edge nodes:

1. **Complete Active Configuration Synchronization**:
   - The Coordinator calculates the canonical fingerprint from its complete Active repository and custom Managed Upstream Nginx configuration snapshot and never adopts an Edge fingerprint as authority. Database-local IDs/timestamps are excluded, repository associations use stable slugs, and custom content line endings are normalized.
   - Using the Web UI ("Sync all nodes") or REST API (`POST /admin/api/v1/cluster/sync`), the Coordinator concurrently sends the complete Active repositories and custom configuration snapshot to every enabled Edge at `POST /api/v1/cluster/sync/apply`.
   - Each Edge verifies the protocol, Coordinator identity/epoch, monotonic generation, payload fingerprint, capabilities, repositories and route conflicts before applying the snapshot through the normal Managed Upstream Nginx candidate validation, atomic publication and graceful reload path. A validation or reload failure keeps the previous Active configuration.
   - HTTP 200 alone is not success: the Coordinator requires a strict `applied` JSON acknowledgement with the exact fingerprint, protocol, generation and capabilities before persisting the Edge as synchronized.
2. **Distributed Cache Invalidation Broadcast**:
   - Global, repository and object invalidation on the Coordinator first advances the local generation, then broadcasts the same scope to every enabled Edge at `POST /api/v1/cluster/sync/purge`.
   - Every purge is bound to the Edge's accepted Coordinator identity and epoch, and every acknowledgement is checked. API responses and one aggregate audit event report targets, successes and per-node failures, including partial failure, while local invalidation remains effective.
3. **Drift Detection & Automatic Alerting**:
   - A bounded worker pool probes Edge nodes and verifies that manifest and health describe the same generation/fingerprint. Persistence errors are observable and do not replace the last durable routing snapshot. Any drift triggers a `config_change` webhook notification and highlights the affected node in the Web UI.
