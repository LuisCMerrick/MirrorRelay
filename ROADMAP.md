# MirrorRelay Roadmap

This roadmap outlines current accomplishments and future development milestones for MirrorRelay.

---

## Current Release: v0.0.x (Foundation & Core Stability)

- [x] **Core Architecture**: Go control plane + version-bound musl Managed Upstream Nginx data plane.
- [x] **Package Ecosystems**: Pull-through proxy and disk caching for APT, RPM/DNF, APK, OPKG, PyPI, npm, Maven, Cargo, Go Modules, Conda.
- [x] **Docker / OCI Registry**: Bearer Token Brokerage, multi-upstream fallback, and presigned S3/CDN 302 redirect handling.
- [x] **Data-Plane Safety**: Desired/Active state separation, `nginx -t` validation, atomic configuration swapping, and graceful reloads.
- [x] **Security Engine**: SSRF defense with private IP / CIDR blacklists, IP pinning with preserved TLS SNI, and internal header stripping.
- [x] **Web Management UI**: Embedded, zero-dependency bilingual (English / Chinese) single-page application with in-place service restarts.
- [x] **Distributed Routing (Package Repos)**: Coordinator / Edge multi-node topology with CIDR, Geo, Priority, and Weight 307 redirects.
- [x] **Release Packaging**: Multi-architecture (`amd64`, `arm64`) DEB, RPM, and tarball releases.

---

## Planned Milestones

### v0.1.0 — Distributed OCI & Enhanced Observability
- [ ] **Distributed OCI Registry Routing**: Implement Coordinator control plane for Docker/OCI layer blobs to enable edge-redirected container image pulling.
- [ ] **Prometheus Metrics Exporter**: Native `/metrics` endpoint exporting cache HIT/MISS rates, upstream latency, bandwidth, and edge health.
- [ ] **Structured OpenTelemetry Tracing**: Distributed tracing across Coordinator, Edge, and Upstream Nginx layers.

### v0.2.0 — Storage & Ingress Flexibility
- [ ] **S3 / Object Storage Cache Backend**: Optional cloud object store backend (AWS S3, MinIO, Ceph, Cloudflare R2) alongside local disk cache.
- [ ] **Automated ACME / Let's Encrypt**: Built-in automated certificate provisioning for `managed-standalone` ingress mode.
- [ ] **Granular Repository Permissions**: Role-Based Access Control (RBAC) for managing specific repository scopes.

### v0.3.0 — Advanced Cache & Upstream Intelligence
- [ ] **Upstream Health Analytics & Predictive Routing**: Machine-learning / EWMA based latency scoring for multi-upstream failover.
- [ ] **Selective Upstream Mirroring Rules**: Regex and glob filters to cache specific package versions or architectures selectively.
- [ ] **Webhook & Event Notifications**: Outbound webhooks on upstream errors, certificate expiries, and node failovers.
