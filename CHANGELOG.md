# Changelog

## v0.0.12
- **Settings State Discrepancy & False Restart Prompt Resolution**:
  - Eliminated false "Saved values differ from the running process; restart MirrorRelay" warnings on the settings page by normalizing dynamic appearance configs (`UIEnhancement`), `nil` vs empty slice representations (`AdminCIDRs`), and duration string formatting across comparison routines.
- **Wildcard Rewrite Host & Upstream Redirect Mirror Matching**:
  - Enhanced `isAllowedRewriteOrigin` with wildcard subdomain matching (`*.acc.umu.se`, `*.debian.org`, `*.ubuntu.com`, `*.rockylinux.org`, `*.pkgbuild.com`, `*.archlinux.org`), enabling seamless proxying and caching for Debian CD/Live ISO and other mirrors that redirect via geo-dispatchers.
- **CI Automation on Push**:
  - Configured GitHub Actions CI workflow to trigger automatically on direct pushes to `main`.

## v0.0.11
- **Docker / OCI Registry Health Check Handshake Compliance**:
  - Aligned health checker probe validation with the OCI Distribution Specification and Docker Registry v2 API: unauthenticated `/v2/` probes receiving `HTTP 401 Unauthorized` with `Www-Authenticate` challenge headers are recognized as healthy endpoints alongside `HTTP 200 OK`.
- **Disabled Repository State Semantics & UI Normalization**:
  - Fixed health state resolution for disabled repositories (`Enabled = false` or `HealthCheckEnabled = false`) to explicitly return `"disabled"` instead of `"unknown"`.
  - Updated Web UI dashboard, repository table, and health view to render neutral `"Disabled"` (`已禁用`) badges and suppress alarm indicators for disabled mirrors.
- **Operating System ISO / Image Repository Support & Templates**:
  - Added native `iso` repository type support across core registry, routing, and proxy pipelines.
  - Added built-in profiles and interactive help guides for major OS distributions: Ubuntu Releases (ISO), Debian CD/Live ISO, Rocky Linux ISO, Arch Linux ISO, and Generic ISO / OS Images.
  - Added interactive Client Setup Generator downloads and multi-threaded CLI commands (`aria2c -x 16`, `wget -c`, `curl -C -`, `sha256sum -c`) for ISO repositories.

## v0.0.10
- **Smart Cache Warm-Up & Predictive Pre-Fetching Engine (`warmup`)**:
  - Implemented intelligent upstream package & index warm-up with recursive metadata extraction (APT `Packages.gz`/`InRelease`, RPM `repodata.xml`, PyPI `simple/`).
  - Feature is strictly disabled by default (`warmup.enabled: false`) and can be enabled per-deployment.
  - Added configurable concurrency throttling, bandwidth limiting, retry counts, cron/interval scheduling, and live execution telemetry.
  - Built dedicated Cache Warm-Up management panel in Web UI and REST API (`/admin/api/v1/warmup/*`).
- **Distributed Multi-Node Cluster Edge Synchronization & Purge Broadcast**:
  - Implemented canonical configuration fingerprint calculation and active manifest generation across all repositories.
  - Added multi-edge node push/pull synchronization (`POST /cluster/sync` and `/cluster/nodes/:id/sync`) with sub-millisecond drift detection.
  - Added distributed cache invalidation broadcast (`POST /cluster/sync/purge`) ensuring real-time multi-node cache consistency.
- **Enterprise Security, Compliance Audit & Multi-Channel Alerting**:
  - Multi-role RBAC authorization (`admin`, `operator`, `viewer`) with per-session token validation.
  - Package Guard supply chain protection supporting regex and wildcard blacklists and whitelists.
  - Persistent structured JSON Audit Trail (`audit_logs`) tracking administrative actions with client IP and attribution.
  - Multi-channel Webhook notifications (DingTalk, Feishu, WeCom, Slack, Generic JSON) with HMAC-SHA256 signatures.
  - Network boundary isolation via dedicated administration hostname (`admin.host`).
- **Zero-Copy / X-Accel-Redirect Binary Bypass Acceleration**:
  - Adaptive dual-mode delivery pipeline: non-rewritten cache HITs dynamically delegate directly to Managed Upstream Nginx via internal `X-Accel-Redirect`, leveraging Linux kernel zero-copy `sendfile(2)` and avoiding userspace memory copying.
- **Visual Analytics, Interactive Setup Generator & Real-Time Log Streaming**:
  - High-performance vector SVG chart engines for 24h hourly request rates, throughput, HTTP status, and cache hit distributions.
  - Live log streaming for access and audit logs with multi-field search and poll-stream modes.
  - Interactive Client Setup Generator supporting one-liner CLI commands and configuration downloads for 10+ package ecosystems.
  - Precision targeted cache object invalidation explorer (`/mirrors/:id/cache/purge`).
- **Frontend Quality & Layout Hardening**:
  - Comprehensive audit and alignment fixes across all 16 page controllers, 5 modal dialogs, and responsive breakpoints.

## v0.0.5
- **Modular Web UI Architecture (ES Modules)**:
  - Transitioned the entire frontend codebase to a clean ES Module architecture (`internal/web/dist/js/`) loaded via `<script type="module" src="js/main.js"></script>`.
  - Decoupled router, state management, API client, DOM utilities, formatters, and page controllers into dedicated modules (`js/router.js`, `js/state.js`, `js/api.js`, `js/dom.js`, `js/pages/*.js`).
  - Streamlined i18n dynamic resource loading via ES module default exports.
- **Modern Control Plane Design System**:
  - Upgraded Web UI styling (`app.css`) with polished surface hierarchy, CSS design tokens, glowing indicator badges, and responsive layout improvements.
  - Organized sidebar navigation into clear functional sections: `Core`, `Gateway`, `Cluster & Ops`, `Logs & System`, `Admin`.
- **UI Resilience & Safety Hardening**:
  - Added full dialog coverage in HTML markup (`preview-dialog`, `custom-dialog`, `node-dialog`).
  - Added optional chaining across all event bindings and DOM queries to eliminate null pointer exceptions.
  - Added automated syntax checking (`find internal/web/dist -name "*.js" -exec node --check {} +`) into the CI and Makefile pipelines.

## v0.0.4
- **UI Enhancement, Themes & Appearance Customization**:
  - Added full theme customization support: Light, Dark, and System modes with configurable accent colors.
  - Added customizable site title, header branding, and login page branding.
  - Added Appearance management page in Web UI, database persistence (`appearance_settings`), config validation, and administrative REST API endpoints (`/appearance`, `/appearance/reset`).
- **Repository Browser & Interactive Help System**:
  - Implemented Repository Browser file exploration mode with local file caching and directory tree navigation.
  - Built built-in interactive Help guide generator (`/help/`, `/help/<slug>/`, `/help/templates`) covering 10 major package ecosystems (APT, RPM, PyPI, Docker/OCI, npm, Maven, Go, NuGet, Cargo, Conda).
  - Embedded zero-dependency SVG icon set in `internal/browser/` with safe UI fallback mode.
- **Go Codebase Modularization (<20KB Refactoring)**:
  - Completely refactored and modularized oversized Go files across packages `internal/help/`, `internal/config/`, `internal/upstreamnginx/`, `internal/database/`, `internal/proxy/`, and `internal/api/`.
  - Guaranteed 100% of all Go source files across the entire codebase are strictly under 20KB (largest is 18KB), ensuring high maintainability and clean single-responsibility boundaries.
  - Preserved 100% test coverage with complete `-race` compliance and multi-architecture build verification.
- **Documentation & Routing Updates**:
  - Synchronized paired English and Chinese documentation for Appearance, Themes, and Help features in `docs/configuration.md`, `docs/configuration.zh-CN.md`, `docs/web-ui.md`, and `docs/web-ui.zh-CN.md`.
  - Added Debian Security upstream profile and client example handling.

## v0.0.3
- **Documentation & Open Source Overhaul**:
  - Restructured README with clear one-line positioning, architecture topology, Why MirrorRelay comparison, Target Compatibility, and 5-minute Quick Start.
  - Added dedicated deep-dive guides: `docs/quick-start.md`, `docs/architecture.md`, `docs/docker-oci.md`, `docs/distributed.md`, `docs/security.md`, and paired Chinese versions.
  - Added `ROADMAP.md` and `CONTRIBUTING.md`.
  - Harmonized Quick Start and Installation guides with `sudo systemctl enable --now mirrorrelay.service`.
  - Fixed Distributed Routing documentation to accurately reflect allowed modes (`hybrid`, `cidr`, `geo`, `priority`) and Coordinator 503 behavior when no edge is available.
- **Security & Quality Hardening**:
  - Added SSRF validation to Cluster Node Health Checker (`internal/cluster/checker.go`).
  - Strengthened Content-Security-Policy headers and public index security.
  - Added package-level documentation across all Go modules.
  - Hardened systemd service unit with strict Linux namespace protections.
  - Added unit test coverage for `internal/accesslog` and `internal/limit`.
- **Web UI & i18n Improvements**:
  - Fixed dynamic placeholder formatting for localized strings (`L('text %s', val)`).
  - Improved mobile drawer responsiveness, ARIA accessibility, and visual feedback for save/restart actions.

## v0.0.2
- Renamed project from RepoGate to MirrorRelay
- Added Web UI in-place service restart functionality
- Decoupled i18n language dictionaries into standalone resource files (`locales/en.js`, `locales/zh.js`)

## v0.0.1
- Initial release with pull-through repository proxying and Managed Upstream Nginx data plane
- Embedded bilingual Web UI and REST API
- Multi-architecture DEB, RPM, and tar.gz release packaging
