# AGENTS.md - MirrorRelay Engineering & Operational Guide

This document is the authoritative engineering guide and operational manual for autonomous agents and engineers working on the **MirrorRelay** codebase.

---

## 1. System Overview & Architectural Model

MirrorRelay is a self-hosted pull-through caching gateway for Linux software package repositories (APT, RPM/DNF, Alpine APK, OpenWrt OPKG, PyPI, npm, Maven/Gradle, NuGet, Rust Cargo, Go Modules, Conda) and Docker / OCI container registries. It eliminates full-mirror storage burdens by caching files on demand with automatic LRU eviction.

### Dual-Plane Architecture

```text
Clients (apt / dnf / pip / npm / docker)
   │ (HTTP / HTTPS 80 / 443)
   ▼
External Shared Nginx (Host System Administrator Ingress)
   │ (TCP 127.0.0.1:9081 by default; optional Unix Socket 0660)
   ▼
MirrorRelay Frontend (Go Core Control Plane & Router)
   ├── Routing engine, RBAC & authentication (Argon2id, WebAuthn Passkeys)
   ├── Metadata & HTML URL rewrite engine (bounded buffer <= 8 MiB)
   ├── OCI Token Broker & presigned S3/CDN redirect broker
   ├── SQLite persistent desired-state store (modernc.org/sqlite)
   └── Managed Upstream Nginx candidate generator & preflight validator
   │ (Unix Domain Socket /run/mirrorrelay/upstream.sock, mode 0660)
   ▼
Managed Upstream Nginx (Dedicated Musl Data Plane)
   ├── Disk cache (/var/cache/mirrorrelay)
   ├── Upstream connection pooling & keepalive
   ├── Strict TLS SNI verification (proxy_ssl_verify on)
   └── Multi-upstream failover & health checking
   │ (HTTPS / HTTP)
   ▼
Origin Servers (deb.debian.org, pypi.org, registry-1.docker.io, etc.)
```

### Component Roles & Terminology
- **External Shared Nginx**: Ingress plane managed by the host system administrator. Terminates public TLS. MirrorRelay **never** installs, modifies, restarts, or reloads this instance. MirrorRelay only writes an integration snippet (`mirrorrelay.conf`) for the administrator to include.
- **Managed Upstream Nginx**: Dedicated data plane process managed exclusively by MirrorRelay. Statically compiled with musl libc, OpenSSL (`no-quic`), PCRE2, and zlib (zero dynamic library dependencies).
- **Mandatory Naming Rule**: Always use **"Managed Upstream Nginx"**. Never use prohibited legacy terms: *"Autonomous Upstream Nginx"*, *"受管上游 Nginx"*, or *"受管 Nginx"*. (Enforced by `scripts/verify-docs.mjs`).

---

## 2. Inviolable Engineering & Security Invariants

When modifying or extending MirrorRelay, you must uphold these invariants without exception:

1. **Upstream Isolation Invariant**: The Go control plane **never** directly establishes HTTP/HTTPS connections to external upstream package repositories. All upstream payload traffic flows through the `Managed Upstream Nginx` data plane.
2. **SSRF & DNS Rebinding Defense**:
   - All upstream URLs, token endpoints, and redirect targets are resolved to numeric IP addresses before connection.
   - IP addresses must be verified against blocked CIDRs (IPv4/IPv6 private networks, loopbacks, link-local, carrier-grade NAT, cloud metadata services). See `internal/security/network.go`.
   - Low-level dialing binds directly to the validated numeric IP while preserving the original hostname for TLS SNI and certificate chain verification.
3. **Desired vs. Active State Separation**:
   - Configuration changes persist in SQLite as *desired state*.
   - A candidate `nginx.conf` is rendered in a temporary directory.
   - Preflight validation is executed via `nginx -t -c <candidate_path>`.
   - Only upon complete validation is the active configuration directory atomically replaced, followed by a graceful reload (`SIGHUP`).
   - If validation fails, the active running configuration remains completely untouched (fail-closed).
4. **Header Sanitization**: Inbound client requests with `X-Mirror-Internal-*` headers must be unconditionally stripped before proxying to prevent route forgery.
5. **Bounded Memory Invariant**:
   - Metadata and HTML rewrites are strictly capped by `metadata.rewrite_buffer_limit_bytes` (default 8 MiB).
   - Large binary artifacts (`.deb`, `.rpm`, `.whl`, Docker blobs) must be streamed via fixed-size buffers (e.g. 64 KiB) or offloaded via zero-copy `X-Accel-Redirect`. Large files must **never** be buffered into Go heap memory.
6. **Filesystem & Socket Permissions**:
   - Service runs under unprivileged account `mirrorrelay:mirrorrelay` (UID/GID 65532).
   - Unix domain sockets are strictly mode `0660`.
   - Configuration files are mode `0640` (`root:mirrorrelay`).
   - SQLite DB and cache directories are mode `0750` (`mirrorrelay:mirrorrelay`).
7. **Credentials & Cryptography**:
   - Passwords hashed with **Argon2id** (64 MiB memory, 3 iterations, bounded input <= 1024 bytes).
   - WebAuthn/FIDO2 Passkeys use single-use, 5-minute bounded challenges, strict RP ID/origin binding, and authenticator signature counter validation.
   - Emergency account recovery codes are 8 single-use codes stored as SHA-256 hashes only.
   - Cluster mutation tokens are stored encrypted using **AES-256-GCM** with a file-based keyring.
   - Non-admin API responses and logs must redact all secrets, tokens, and sensitive headers.
8. **Package Guard (Supply-Chain Protection)**:
   - Rules starting with `^` or ending with `$` are compiled as RE2 regular expressions.
   - All other rules are evaluated as Go path globs.
   - Maximum 128 rules per list, maximum 512 bytes per rule. Evaluation is fail-closed.

---

## 3. Codebase Structure & Package Catalog

```text
/opt/MirrorRelay/
├── cmd/
│   └── mirrorrelay/              # Application entry point, CLI flag parsing, admin recovery commands
├── internal/
│   ├── accesslog/                # Asynchronous structured access logging with buffered channels
│   ├── api/                      # REST API endpoints (Admin, Cluster, Mirrors, Settings, Passkey, Metrics)
│   ├── appearance/               # Web UI branding, title, theme, and custom CSS persistence
│   ├── applog/                   # Daemon application logging with size/day-based rotation
│   ├── auth/                     # Password authentication (Argon2id), session management, WebAuthn
│   ├── browser/                  # Directory index HTML parsing and secure rendering
│   ├── buildinfo/                # Build metadata, Git commit, timestamp, and unique Build ID
│   ├── cachectl/                 # Cache capacity monitoring, LRU accounting, and manual purge operations
│   ├── cluster/                  # Distributed Coordinator / Edge synchronization, health, and 307 routing
│   ├── config/                   # YAML configuration parsing, defaults, directory creation, validation
│   ├── database/                 # SQLite storage engine, schema migrations, AES-256-GCM token keyring
│   ├── devcert/                  # Self-signed TLS certificate generation for local development (-dev mode)
│   ├── health/                   # Periodic upstream repository health checking worker
│   ├── help/                     # Client configuration snippet generator (apt sources, pip.conf, etc.)
│   ├── ipc/                      # Inter-process signal handling and reload triggers
│   ├── limit/                    # Concurrency control and bandwidth rate limiting
│   ├── mirror/                   # In-memory repository definitions and upstream state tracking
│   ├── model/                    # Domain data models, mirror configurations, validation logic
│   ├── profile/                  # Built-in repository profile templates (APT, RPM, PyPI, Docker, etc.)
│   ├── proxy/                    # HTTP proxy engine, OCI token broker, URL rewriters, zero-copy X-Accel
│   ├── security/                 # SSRF defense, IP prefix validation, secure DNS resolution, target dialer
│   ├── stats/                    # Prometheus metrics collectors and request throughput stats
│   ├── upstreamnginx/            # Managed Upstream Nginx configuration generator, syntax tester, supervisor
│   ├── upstreamnginxlog/         # Real-time tailing of Nginx access and error logs
│   ├── warmup/                   # Intelligent cache pre-warming engine and scheduler
│   ├── web/                      # Embedded zero-dependency Web UI assets (embed.FS)
│   └── webhook/                  # Alert notification dispatcher (DingTalk, Feishu, WeCom, Slack, JSON)
├── build/                        # Musl Nginx Dockerfiles and cross-compilation patchsets
├── configs/                      # Sample configuration files (config.example.yaml, config.docker.yaml)
├── deploy/                       # Systemd unit file (mirrorrelay.service) with strict security sandboxing
├── docs/                         # Paired bilingual documentation (*.md and *.zh-CN.md)
├── nginx/                        # Local Managed Upstream Nginx fixture and SHA-256 checksums
├── packaging/                    # Debian (deb) and RedHat (rpm) packaging specifications
└── scripts/                      # Build, verification, and packaging helper scripts
```

---

## 4. Development & Coding Standards

### Go Standards
- **Pure Go & CGO-Free**: Build with `CGO_ENABLED=0`. Pure Go SQLite driver (`modernc.org/sqlite`) is mandatory.
- **Language Level**: Go 1.26.6+.
- **Formatting**: Strictly format with `gofmt`. No pull request or commit should produce diffs with `gofmt -l .`.
- **Language of Code**: All Go identifiers, comments, log messages, and error strings **must be written in English**. (No Chinese comments in `.go` source files).
- **Error Handling**: Use standard Go error wrapping with `fmt.Errorf("...: %w", err)`.
- **Style**: Idiomatic, compact, minimal vertical spacing, no unnecessary comments that merely repeat code.

### Web Management UI Standards
- **Zero Runtime Dependencies**: The Web UI in `internal/web/dist/` is pure vanilla JavaScript (ES6+), HTML5, and CSS. Do not introduce npm packages, bundlers, or external frontend frameworks.
- **Complete UI Redesign & Rewrite**:
  - The management interface is undergoing a complete rewrite.
  - **Do Not Reference Previous UI Design**: Ignore the existing visual layout, styling, and design patterns. The new UI must be built from the ground up with a clean, modern, intuitive, and responsive aesthetic.
  - **Zero Feature Omission (Mandatory Functional Parity)**: While visual styling and design must not reference the past, **no feature or functional capability from the previous version may be omitted**. Every administrative workflow and management feature must be fully preserved in the new implementation:
    - **Dashboard & Overview**: Real-time throughput, active connections, cache hit ratio, memory/disk gauges, system uptime, and quick diagnostic metrics.
    - **Mirror Management**: Repository listing, filtering, search, add/edit/delete repositories, upstream endpoint configuration, failover backends, path rewrites, Package Guard rules, and custom request/response headers.
    - **Mirror Details & Ops**: Per-repository telemetry, upstream health status, manual cache purge by path/glob, and repository package browsing.
    - **Managed Upstream Nginx Control**: Live worker status, candidate configuration preview, validation output, graceful reload trigger (`SIGHUP`), and real-time Nginx access/error log streaming.
    - **Cache & Warmup Management**: Storage capacity monitoring, high/low watermark threshold gauges, LRU eviction stats, manual purge operations, and scheduled cache pre-warming rules.
    - **Cluster Orchestration**: Coordinator vs. Edge node roles, node discovery, peer health monitoring, HTTP 307 redirect status, and AES-256-GCM token synchronization.
    - **Health & Probing**: Upstream probe scheduling, synthetic health checks, failure detection thresholds, and latency graphs.
    - **Ingress Configuration**: Dynamic `mirrorrelay.conf` generation for host External Shared Nginx, port/socket binding, and SNI pass-through snippets.
    - **Built-in Profiles**: Profile browser and one-click application for APT, RPM/DNF, PyPI, Docker/OCI, Alpine APK, OpenWrt OPKG, Go, Cargo, npm, NuGet, and Maven.
    - **Appearance & Customization**: Instance title, branding, theme selection (light/dark/auto), custom CSS overrides, and public repository portal appearance.
    - **System & Settings Management**: Dynamic settings schema editor covering all daemon subsystems, live config validation, and service reload/restart controls.
    - **User Access & RBAC**: Administrator user management, role-based permissions, Argon2id credentials, and active session inspection/revocation.
    - **Security & Passkeys (WebAuthn)**: FIDO2/WebAuthn passkey registration and management, credential counters, and single-use emergency recovery code generation.
    - **Logs & Audit Trails**: Real-time and historical access log search, daemon application logs with size/day rotation, and security audit event streams.
    - **Client Configuration Snippets**: Interactive client setup guide with copy-ready configuration snippets (APT `sources.list`, DNF/YUM `.repo`, pip `pip.conf`, npm `.npmrc`, Docker `daemon.json`, etc.).
- **Strict Bilingual Localization Parity**:
  - Every UI string must be localized in both `internal/web/dist/locales/en.js` and `locales/zh.js`.
  - Keys in both dictionaries must match exactly.
  - Verify before commit using `./scripts/verify-web-assets.sh`.

### Documentation Parity & Quality Standards
- **Bilingual Documentation Pairing**: Every Markdown document in `docs/<name>.md` must have an exact paired counterpart `docs/<name>.zh-CN.md`.
- **Structural Identity**: Paired documents must have identical heading-level sequences and identical code fence language sequences.
- **Local Link Integrity**: Every relative link in Markdown files must resolve to an existing file in the repository.
- **Enforcement**: Run `node scripts/verify-docs.mjs` to validate all documentation rules.

---

## 5. Verification & Testing Workflow

Always verify before committing or claiming work is complete. The repository provides a comprehensive verification toolchain.

### Step 1: Run Full Repository Verification Suite
```bash
make check
```
`make check` executes:
1. `test -z "$(gofmt -l .)"` — Code formatting check.
2. `go mod verify` — Dependency checksum verification.
3. `go vet ./...` — Go static analysis.
4. `go test ./...` — Full unit test suite.
5. `./scripts/verify-web-assets.sh` — JavaScript syntax and bilingual locale parity checks.
6. `node scripts/verify-docs.mjs` — Markdown link integrity, bilingual structural parity, and prohibited term scan.

### Step 2: Concurrency & Race Detector Check
```bash
go test -race -p 1 -count=1 ./...
```

### Step 3: Multi-Architecture Cross-Compilation Verification
Ensure that changes compile cleanly for both supported production architectures without CGO:
```bash
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o /dev/null ./cmd/mirrorrelay
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -o /dev/null ./cmd/mirrorrelay
```

### Step 4: Integration Testing (with Local Managed Upstream Nginx)
When modifying Nginx generation, configuration rendering, or proxy mechanics:
```bash
MIRRORRELAY_TEST_UPSTREAM_NGINX="$PWD/nginx/sbin/nginx" \
  go test ./internal/upstreamnginx -run '^TestRealManagedUpstreamNginx' -count=1
```

### Step 5: Running Local Development Instance
```bash
go run ./cmd/mirrorrelay -dev
```
- Access the Web UI at `https://127.0.0.1:8443/admin/`.
- Local self-signed certificates and temporary database are initialized automatically under `./runtime-dev`.

---

## 6. Common Operations & CLI Subcommands

### Administrative CLI Commands
- Print version and build details:
  ```bash
  mirrorrelay version --verbose
  ```
- Reset administrator password from standard input (bypasses UI lockout):
  ```bash
  mirrorrelay admin reset-password --config /etc/mirrorrelay/config.yaml --username admin --password-stdin
  ```
- Clear configured passkeys for an account (re-enables password authentication):
  ```bash
  mirrorrelay admin reset-passkeys --config /etc/mirrorrelay/config.yaml --username admin
  ```

### Release Packaging
To build release packages (Debian deb, RPM, tarballs, vendored source archive):
```bash
make release VERSION=0.0.21
```

---

## 7. Agent Operational Protocol

As an autonomous agent working in this repository:
1. **Act, Do Not Ask for Permitted Actions**: When given a task, explore the codebase, implement the necessary changes, run tests, and present the final verified result.
2. **Follow John Carmack `.plan` / BurntSushi PR Style**:
   - Provide a complete, self-consistent, reviewable unit of work.
   - Explicitly detail what was changed, the architectural rationale, trade-offs made, and exact verification proof.
3. **Verification Before Assertion**: Never state that a feature is working, a bug is fixed, or tests pass without having executed the command and confirmed its output.
4. **Minimal Diff Principle**: Focus on the specific task. Avoid sweeping refactors, stylistic drift, or modifying files unrelated to the objective.
5. **Zero Legacy Baggage & No Backward Compatibility Burden**:
   - MirrorRelay has **no historical baggage** and **no existing production users**.
   - There is **no requirement to maintain backward compatibility** with earlier versions (v0.0.x).
   - Do not introduce compatibility shims, legacy migration wrappers, or compromise clean architecture for the sake of deprecated behavior. Redesign and refactor decisively when needed.
6. **Complete Frontend UI Freedom with Functional Rigor**:
   - When developing or modifying the frontend, ignore previous visual designs and layouts completely. Build a fresh, modern user experience from first principles.
   - However, uphold strict functional completeness: never drop, overlook, or degrade any capability present in the previous version.
7. **Remote Push Policy**:
   - Pushing to the remote Git repository is explicitly permitted and expected.
   - Once changes are complete, fully verified (`make check`), and clean, commit them and push directly to `origin main` (or the relevant tracking branch) to ensure end-to-end delivery.
