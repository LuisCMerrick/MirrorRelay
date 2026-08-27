# MirrorRelay Roadmap

This roadmap outlines current accomplishments and future development milestones for MirrorRelay.

---

## Current Release: v0.0.x (Foundation & Core Stability)

- [x] **Core Architecture**: Go control plane + version-bound musl Managed Upstream Nginx data plane.
- [x] **Package Ecosystems**: Pull-through proxy and disk caching for APT, RPM/DNF, APK, OPKG, PyPI, npm, Maven, NuGet, Cargo, Go Modules, Conda, and ISO/image repositories.
- [x] **Docker / OCI Registry**: Bearer Token Brokerage, multi-upstream fallback, and presigned S3/CDN 302 redirect handling.
- [x] **Data-Plane Safety**: Desired/Active state separation, `nginx -t` validation, atomic configuration swapping, and graceful reloads.
- [x] **Security Engine**: SSRF defense with private IP / CIDR blacklists, IP pinning with preserved TLS SNI, and internal header stripping.
- [x] **Web Management UI**: Embedded, zero-dependency bilingual (English / Chinese) single-page application with strict settings import/export/history and in-place service restarts.
- [x] **Authentication & Recovery**: Argon2id passwords, WebAuthn passkeys, single-use emergency recovery codes, RBAC, and local recovery commands.
- [x] **Observability**: Prometheus `/metrics`, internal/system dashboards, JSON statistics API, access logs, and SQLite audit trail.
- [x] **Notifications**: SSRF-filtered Webhook delivery for DingTalk, Feishu/Lark, WeCom, Slack, and generic JSON with optional HMAC-SHA256 signatures.
- [x] **Distributed Routing (Package Repos)**: Coordinator / Edge multi-node topology with CIDR, Geo, Priority, and Weight 307 redirects.
- [x] **Release Packaging**: Multi-architecture (`amd64`, `arm64`) DEB, RPM, tarball, vendored-source archive, and one Docker Hub OCI manifest.

---

## Planned Milestones

### v0.1.0 — Distributed OCI & Enhanced Observability
- [ ] **Distributed OCI Registry Routing**: Implement Coordinator control plane for Docker/OCI layer blobs to enable edge-redirected container image pulling.
- [ ] **Expanded Prometheus Metrics**: Add upstream-latency histograms, cache-capacity alerts, and richer per-edge series to the existing `/metrics` endpoint.
- [ ] **Structured OpenTelemetry Tracing**: Distributed tracing across Coordinator, Edge, and Upstream Nginx layers.

### v0.2.0 — Storage & Ingress Flexibility
- [ ] **S3 / Object Storage Cache Backend**: Optional cloud object store backend (AWS S3, MinIO, Ceph, Cloudflare R2) alongside local disk cache.
- [ ] **Automated ACME / Let's Encrypt**: Built-in automated certificate provisioning for `managed-standalone` ingress mode.
- [ ] **Granular Repository Permissions**: Role-Based Access Control (RBAC) for managing specific repository scopes.

### v0.3.0 — Advanced Cache & Upstream Intelligence
- [ ] **Upstream Health Analytics & Predictive Routing**: Machine-learning / EWMA based latency scoring for multi-upstream failover.
- [ ] **Selective Upstream Mirroring Rules**: Regex and glob filters to cache specific package versions or architectures selectively.
- [ ] **Advanced Event Policies**: Per-event destinations, retry queues, certificate-expiry events, and configurable escalation policies.
