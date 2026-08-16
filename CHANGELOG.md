# Changelog

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
