# Changelog

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
