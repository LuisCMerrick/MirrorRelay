# Changelog

## Unreleased

## v0.0.17

- **Control-plane credential and ingress hardening**:
  - Redacted repository credentials for every non-Admin role, made generated,
    effective, custom and ingress configuration plus operational settings
    Admin-only, and separated Operator cluster checks/sync from Admin-only node
    management.
  - Restricted System and health endpoint details by role and aligned the Web
    UI with the API so unavailable secret/configuration actions are not
    rendered; raw failed validation, lifecycle, repository-check and audit
    diagnostics are now Admin-only.
  - Added a required file-backed AES-256-GCM keyring for Coordinator node
    mutation tokens, including transactional plaintext migration and
    first-key rotation.
  - Added explicit trusted-proxy CIDRs for `X-Real-IP`, ignored
    `X-Forwarded-Proto` during public-origin inference (including public help
    pages), validated fallback request authorities, and changed client-IP
    exposure to default off.
  - Replaced the Compose host `/run` bind with a UID/GID-scoped tmpfs and a
    deterministic bridge gateway used by the Docker trusted-proxy example.
  - Validated and precompiled bounded package allow/block rules before
    activation, with fail-closed routing for invalid persisted policy.
  - Made development image builds verify the checked-in Nginx checksum and
    formal images verify both packaged binaries against `BUILD-INFO`.
  - Reworked the bilingual main README quick start around direct Docker,
    Compose, release packages and the exact `go run ./cmd/mirrorrelay -dev`
    development path; Compose now consumes the official multi-architecture
    image by default.

- **Explicit local endpoint defaults**:
  - External Shared Nginx now reaches the Go frontend over configurable
    IP+port TCP by default (`127.0.0.1:9081`); the frontend Unix socket is
    created only when `server.unix_socket_enabled` is explicitly enabled.
  - Added strict `server.local_address` validation, including explicit wildcard
    binding for containers, while Docker examples publish port 9081 only on the
    host loopback interface.
  - Kept the Go-to-Managed-Upstream-Nginx Unix socket enabled by default; only
    an explicit disable selects its loopback TCP port.

- **Multi-architecture Docker Hub release image**:
  - Added automatic Docker Hub publication for every published release using
    `DOCKERHUB_USERNAME` and `DOCKERHUB_TOKEN` repository settings.
  - Publishes one OCI manifest for `linux/amd64` and `linux/arm64` with version,
    v-prefixed version and stable-release `latest` tags, plus SBOM and provenance
    attestations.
  - Assembles each image from the exact MirrorRelay and Managed Upstream Nginx
    binaries already verified by its architecture package job, byte-verifies
    the published arm64 filesystem, and runs both images on native hosted
    runners. Both target binaries are cross-compiled without QEMU.
  - Made the Nginx configure phase genuinely cross-compilation-safe by using
    explicit Linux/musl runtime-probe results and target-compiler checks for
    sizes, endianness and `sys_nerr`; the release build no longer executes
    target binaries and records the patch-set checksum in `BUILD-INFO`.
  - Added an explicit manual container-publication switch for development
    snapshots. Opt-in runs publish the immutable snapshot version and `edge`
    without replacing the stable-release `latest` tag.

## v0.0.16

- **Release security baseline**:
  - Upgraded the Managed Upstream Nginx build from 1.30.2 to 1.30.4, the stable release that fixes CVE-2026-42533, CVE-2026-60005 and CVE-2026-56434; verified the pinned source checksum and official PGP signature before updating the build fixture.
  - Explicitly disabled the unused OpenSSL QUIC implementation in the static build; Managed Upstream Nginx does not enable HTTP/3, while HTTPS upstream support remains enabled.
  - Raised the toolchain baseline to Go 1.26.6, updated `golang.org/x/crypto`, `golang.org/x/net` and `golang.org/x/sys`, and added a pinned CI `govulncheck` gate; the release tree reports no reachable or imported-package vulnerabilities.
  - Aligned the existing development Compose build with Go 1.26.6 and pinned both of its base images by digest; release output remains DEB, RPM and tar.gz only.
  - Enforced mode `0660` for both Unix sockets across strict configuration validation, examples, documentation and package verification.
  - Removed generated pip, npm and Docker client settings that bypassed TLS certificate validation; private PKI deployments must install their CA in the client trust store.
  - Restricted Managed Upstream Nginx validation and access-log data to Admin and Operator roles, made user enumeration Admin-only, and made the UI omit controls and navigation that the current role cannot use.
  - Bounded cluster-wide configuration sync and cache-purge broadcasts to eight workers, matching the bounded health-scan resource model.
  - Made cluster manifest, health, synchronization and purge responses reject unknown fields, trailing JSON and bodies over 64 KiB instead of treating an artificial read limit as EOF.

- **First-use administrator registration**:
  - Replaced startup-time preset administrator credentials with a one-time registration form shown only while the user database is empty; successful registration creates an Admin and an authenticated session.
  - Made initial creation an atomic conditional database operation, kept it behind the configured administration host/path and CIDR boundary, and changed shipped YAML examples to loopback-only administrator CIDRs.
  - Removed `admin.initial_username`, `admin.initial_password`, `MIRRORRELAY_ADMIN_USERNAME`, `MIRRORRELAY_ADMIN_PASSWORD` and `MIRRORRELAY_ADMIN_PASSWORD_FILE`; existing configuration must remove those YAML keys before upgrading.
- **APT client configuration formats**:
  - Added independent DEB822 (`.sources`) and traditional `sources.list` selectors and downloads for Debian, Debian Security and Ubuntu.
  - Corrected distribution-specific suites, components and archive keyrings, including preventing doubled Debian Security suite suffixes.
- **Calmer administration UI hierarchy**:
  - Flattened the shared visual system, reduced card and panel density, removed decorative glow/hover motion, and standardized keyboard-accessible native disclosure sections across Dashboard, System, Health, Cluster, Cache, Settings, Appearance and repository details.
  - Reduced the Managed Upstream Nginx first screen to state, uptime and configuration version; moved PID, binary/build identity, architecture, checksum, lifecycle data and compile options into an on-demand technical-details view.
  - Made effective Managed Upstream Nginx and External Shared Nginx snippets hidden by default, with explicit expand-and-copy controls; effective configuration is now fetched only after an authorized user expands it.
  - Completed English/Chinese coverage for every statically referenced UI label and added an automated locale-parity/reference test to prevent silent English fallbacks.
  - Removed the unreferenced legacy monolithic Web UI bundle so the embedded administration surface has one auditable implementation path.
- **Distributed protocol v2 correctness and trust boundaries**:
  - Made the complete Active repository plus custom Managed Upstream Nginx snapshot authoritative, unified configuration-generation semantics, and required manifest/health generation and fingerprint agreement before routing.
  - Persisted Coordinator identity/epoch and accepted Edge generation/fingerprint state, rejecting stale generations, same-generation conflicts and retired-epoch replays while preserving idempotent retries.
  - Split the shared read-only probe credential from unique per-Edge sync/purge credentials, bound mutations to the configured Coordinator identity/epoch, and stopped returning mutation credentials through the API.
  - Routed degraded nodes only for explicitly healthy repositories, kept unknown repositories unroutable, made health scans bounded-concurrent, and surfaced status-persistence failures without replacing the last durable routing snapshot.
- **Browsable HTML auxiliary-route hardening**:
  - Replaced caller-selectable same-origin root paths with HMAC-scoped URLs bound to the repository, exact selected upstream/Host policy, escaped path and query; signing keys are generated once and persisted in SQLite.
  - Reworked `srcset` candidate parsing so data URLs retain internal commas while eligible normal candidates are independently rewritten.
- **Logging and Viewer credential privacy**:
  - Removed query strings from Managed Upstream Nginx access records and limited the access-log API/UI to Admin and Operator roles.
  - Omitted registry token endpoints entirely from Viewer responses, including secrets carried in paths, encoded paths, queries or malformed values.

## v0.0.15
- **Distributed control-plane correctness and security**:
  - Added authenticated Edge receivers for complete Active configuration snapshots and global/repository/object cache invalidation, with bounded strict JSON, atomic Managed Upstream Nginx activation, verified acknowledgements and accurate partial-failure audit records.
  - Made the Coordinator's local Active fingerprint authoritative, preserved protocol/capability metadata after sync, required HTTPS for cluster origins by default, and applied uniform URL/SSRF policy before any cluster token is sent.
  - Preserved percent-encoded request paths across Coordinator-to-Edge redirects.
- **Outbound and credential hardening**:
  - Added DNS/IP/redirect SSRF enforcement for webhooks with independent HTTP/private-address opt-ins, and fixed temporary webhook test overrides and malformed-input side effects.
  - Clarified that Webhook delivery has one active destination, split test targets by platform, and prevented temporary URLs from inheriting the running destination's signing secret.
  - Redacted repository static credentials from Viewer responses and restricted generated/effective Managed Upstream Nginx configuration to Admin and Operator roles.
- **Web UI themes**:
  - Added persistent Light, Dark and Auto controls to the login page and administration header; Auto follows the operating-system preference and the light palette uses theme-aware semantic colors throughout controls, charts and dialogs.
- **Runtime and scheduler correctness**:
  - Published appearance updates through a shared atomic snapshot, preserved TCP frontend ports in metadata-derived warm-up URLs, and replaced permissive pseudo-cron matching with validated schedules and persisted next-run times.
- **Release source archive**:
  - Added a reproducible `mirrorrelay-<version>-source-with-vendor.tar.gz` GitHub Release asset and included it in `SHA256SUMS`.

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
