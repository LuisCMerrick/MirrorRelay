# MirrorRelay

[English](README.md) | [简体中文](README.zh-CN.md)

[![CI](https://github.com/LuisCMerrick/MirrorRelay/actions/workflows/ci.yml/badge.svg)](https://github.com/LuisCMerrick/MirrorRelay/actions/workflows/ci.yml)
[![Release Build](https://github.com/LuisCMerrick/MirrorRelay/actions/workflows/build.yml/badge.svg)](https://github.com/LuisCMerrick/MirrorRelay/actions/workflows/build.yml)
[![Release](https://img.shields.io/github/v/release/LuisCMerrick/MirrorRelay)](https://github.com/LuisCMerrick/MirrorRelay/releases)
[![License: GPL-3.0](https://img.shields.io/badge/License-GPL--3.0-blue.svg)](LICENSE)

A self-hosted pull-through caching gateway for Linux package repositories and OCI container registries with a web management UI and managed Nginx data plane.

```text
APT · RPM · APK · OPKG · PyPI · npm · Maven · NuGet · Cargo · Go Proxy · Conda · Docker / OCI

No full mirror synchronization required.
```

```text
Client (apt / dnf / pip / npm / docker)
   │
   ▼
External Shared Nginx (Ingress: 80 / 443)
   │ (Unix socket 0660 / Loopback TCP)
   ▼
MirrorRelay (Go Control Plane & Router)
   ├── Web UI & Management API (/admin/)
   ├── Token Broker & Redirect Broker (Docker / OCI)
   ├── Bounded Metadata & HTML Rewriter
   ├── SQLite Persistent Desired State
   └── Candidate Generator & Atomic Publication
   │ (Unix socket 0660 / Loopback TCP)
   ▼
Managed Upstream Nginx (Dedicated Musl Data Plane)
   ├── Proxy Cache & Content Store
   ├── SSL Verification & DNS Pinning
   └── Multi-Upstream Failover
   │
   ▼
Original Upstreams (Debian, Ubuntu, Rocky, PyPI, Docker Hub, etc.)
```

---

## Why MirrorRelay?

Traditional repository setups require either **full mirror synchronization** or **manual Nginx `proxy_pass` rules**. MirrorRelay solves the fundamental drawbacks of both approaches:

| Challenge | Full Mirror Sync (e.g. `apt-mirror`, `bandersnatch`) | Manual Nginx `proxy_pass` | MirrorRelay |
|---|---|---|---|
| **Storage Consumption** | Requires hundreds of gigabytes or terabytes upfront | Low (caches on demand) | **Low**: Pull-through on-demand disk cache |
| **Initial Sync Delay** | Hours to days before first use | Zero delay | **Zero delay**: Immediate availability |
| **Repository Management** | Complex sync scripts and cron jobs | Manual config editing and reloads | **Web UI & API**: Real-time CRUD with audit log |
| **Docker / OCI Registry** | Difficult to mirror private/public registries | Breaks on Bearer token & CDN 302 redirects | **Built-in Token & Redirect Broker** |
| **Data Plane Safety** | N/A | Syntax errors break entire web server | **Desired/Active separation** (`nginx -t` before atomic reload) |
| **Upstream Security** | N/A | Vulnerable to SSRF and DNS rebinding | **Strict CIDR filtering, IP pinning & TLS SNI verification** |
| **Multi-Node Routing** | Manual DNS / CDN configuration | Complex geo-DNS setup | **Coordinator / Edge distributed 307 routing** |

---

## Capabilities

| Feature | Status | Details |
|---|---|---|
| **Package Repository Proxy & Cache** | ✅ Supported | APT, RPM/DNF, APK, OPKG, PyPI, npm, Maven, Cargo, Go Proxy, Conda |
| **Docker / OCI Registry Pull Proxy** | ✅ Supported | Full `/v2/` challenge handling, Token Brokerage, multi-upstream fallback, CDN redirect handling |
| **Multi-Upstream Failover** | ✅ Supported | Automatic health checking, weight, priority, and backup upstream fallback |
| **Desired / Active Separation** | ✅ Supported | Atomic configuration generation, `nginx -t` validation, and hitless graceful reload |
| **Distributed Package Routing** | ✅ Supported | Coordinator / Edge topology with client CIDR, Geo, Priority, and Weight 307 routing |
| **Edge Configuration Consistency** | ✅ Supported | Real-time fingerprint and version check between Coordinator and Edge nodes |
| **Docker / OCI Distributed Routing** | 🚧 Planned | Docker registry distributed routing is planned for future control-plane updates (single-node OCI pull proxy is fully supported) |
| **Bilingual Web Management UI** | ✅ Supported | Zero-dependency responsive UI in English and Chinese with persistent switch and live restart |

---

## Target Compatibility

| Ecosystem | Proxy Mode | Dynamic Cache | Metadata / URL Rewrite | Tested Client / OS |
|---|:---:|:---:|:---:|---|
| **APT** | ✅ | ✅ | Optional HTML URL rewrite | Debian 11/12, Ubuntu 22.04/24.04 |
| **RPM / DNF** | ✅ | ✅ | Optional HTML URL rewrite | Rocky Linux 8/9, AlmaLinux 9, Fedora 40/41 |
| **Alpine APK** | ✅ | ✅ | N/A | Alpine 3.19/3.20/3.21 |
| **OpenWrt OPKG** | ✅ | ✅ | Optional HTML URL rewrite | OpenWrt 22.03/23.05 |
| **PyPI** | ✅ | ✅ | ✅ Simple HTML Index Rewrite | `pip` 23.x/24.x |
| **npm** | ✅ | ✅ | ✅ JSON Registry Metadata Rewrite | `npm` 9.x/10.x, `pnpm`, `yarn` |
| **Go Modules** | ✅ | ✅ | N/A | Go 1.22/1.23/1.24 |
| **Rust Cargo** | ✅ | ✅ | N/A | Cargo / `crates.io` index |
| **Maven / Gradle** | ✅ | ✅ | Optional HTML directory rewrite | Maven 3.8/3.9, Gradle 8.x |
| **Docker / OCI** | ✅ Pull | ✅ Layers & Blobs | ✅ Token & S3/CDN Redirect Broker | Docker Engine 24.x/26.x/27.x, Podman 4.x/5.x |

---

## Quick Start (5 Minutes)

### Option 1: Instant Development Evaluation (Zero Setup)

```bash
git clone https://github.com/LuisCMerrick/MirrorRelay.git
cd MirrorRelay
go run ./cmd/mirrorrelay -dev
```

Open `https://127.0.0.1:8443/admin/` in your browser and sign in with `admin` / `adminadmin`.

### Option 2: Production Package Deployment (DEB / RPM)

1. **Install Package**:
   ```bash
   # Debian / Ubuntu:
   sudo apt-get install --yes ./mirrorrelay_0.0.2_amd64.deb

   # RHEL / Rocky Linux / Fedora:
   sudo dnf install --yes ./mirrorrelay-0.0.2.x86_64.rpm
   ```
2. **Configure Initial Admin Password**:
   ```bash
   echo "MIRRORRELAY_ADMIN_PASSWORD=your_secure_password" | sudo tee /etc/mirrorrelay/environment
   sudo chmod 0600 /etc/mirrorrelay/environment
   sudo systemctl restart mirrorrelay
   ```
3. **Connect External Shared Nginx** (see [Installation Guide](docs/installation.md)):
   Add your web server user (e.g. `www-data` or `nginx`) to the `mirrorrelay` group to access the Unix domain socket:
   ```bash
   sudo usermod -aG mirrorrelay www-data
   ```
   Include the generated snippet `/var/lib/mirrorrelay/integration/external-nginx/mirrorrelay.conf` in your Nginx `server` block and reload Nginx.

### Example Repository & Client Setup

1. **Add Debian Repository** in Web UI:
   - Name: `debian`, Slug: `debian`, Type: `apt`, Public Path: `/debian`
   - Upstream URL: `https://deb.debian.org/debian`
2. **Configure Client** (`/etc/apt/sources.list`):
   ```text
   deb https://mirror.example.com/debian bookworm main contrib non-free
   ```
3. **Verify**:
   ```bash
   sudo apt-get update
   ```

For detailed setup instructions, see the [Quick Start Guide](docs/quick-start.md) and [Installation Guide](docs/installation.md).

---

## Architecture Overview

MirrorRelay uses a clean two-plane architecture separating administrative control from high-throughput upstream data transfer:

```text
External Shared Nginx (Administrator-Owned)
         ↓  (Unix Socket 0660)
MirrorRelay Frontend (Go Service)
  - Policy enforcement & Authentication
  - Dynamic routing & URL rewriting
  - Token brokerage for OCI registries
  - Configuration lifecycle (Desired state in SQLite)
         ↓  (Unix Socket 0660)
Managed Upstream Nginx (Isolated Musl Process)
  - Proxy caching on local disk (/var/cache/mirrorrelay)
  - Connection pooling & SSL verification
  - DNS resolution & pinning
         ↓
Original Upstream (HTTPS / HTTP)
```

- **Safety Invariant**: The Go service never makes direct HTTP calls to upstream package servers. All upstream communication is performed exclusively through the isolated, statically linked `Managed Upstream Nginx` data plane.
- **Hitless Reloads**: Configuration updates are compiled into a candidate Nginx configuration, validated with `nginx -t`, atomically written, and reloaded gracefully (`HUP`) without dropping active connections.

Read more in the [Architecture Guide](docs/architecture.md).

---

## Distributed Deployment

MirrorRelay includes native clustering capabilities to scale traffic across geographical regions or edge locations:

- **Coordinator Node**: Central management authority owning repository definitions, client routing rules, and node health monitoring.
- **Edge Nodes**: Autonomous caching nodes that receive traffic directly from clients via HTTP 307 redirects from the Coordinator.
- **Routing Policies**: Client IP CIDR matching, Geo-location, Priority, and Weight balancing.
- **Failover**: If an Edge node fails health checks, the Coordinator automatically routes traffic to the next healthy node or serves it locally.

Read more in the [Distributed Deployment Guide](docs/distributed.md).

---

## Release Packages

Pre-compiled, statically linked packages are produced for `linux/amd64` and `linux/arm64`:

| Architecture | DEB | RPM | Tarball Archive |
|---|---|---|---|
| **amd64** | `mirrorrelay_<version>_amd64.deb` | `mirrorrelay-<version>.x86_64.rpm` | `mirrorrelay-<version>-linux-amd64.tar.gz` |
| **arm64** | `mirrorrelay_<version>_arm64.deb` | `mirrorrelay-<version>.aarch64.rpm` | `mirrorrelay-<version>-linux-arm64.tar.gz` |

Standard file layout:
```text
/usr/bin/mirrorrelay                         # Main Go service binary
/usr/lib/mirrorrelay/nginx/nginx             # Version-bound musl Managed Upstream Nginx
/etc/mirrorrelay/config.yaml                 # Configuration file (mode 0640)
/usr/lib/systemd/system/mirrorrelay.service  # Sandboxed systemd service unit
/var/lib/mirrorrelay/mirrorrelay.db          # Persistent SQLite database
/var/cache/mirrorrelay/                      # Upstream package and blob disk cache
/var/log/mirrorrelay/upstream-nginx/         # Access and error logs
/run/mirrorrelay/                            # Runtime PID and Unix domain sockets
```

---

## Documentation

- **Getting Started**:
  - [Quick Start Guide](docs/quick-start.md) ([中文](docs/quick-start.zh-CN.md)) — 5-minute onboarding walkthrough.
  - [Installation Guide](docs/installation.md) ([中文](docs/installation.zh-CN.md)) — Production DEB, RPM, and tarball deployment.
  - [Web UI Guide](docs/web-ui.md) ([中文](docs/web-ui.zh-CN.md)) — Bilingual dashboard, settings, and repository actions.
- **Architecture & Deep Dives**:
  - [Architecture Guide](docs/architecture.md) ([中文](docs/architecture.zh-CN.md)) — Two-plane design, lifecycle, and data flow.
  - [Docker & OCI Registry](docs/docker-oci.md) ([中文](docs/docker-oci.zh-CN.md)) — Token brokerage, redirect handling, and setup.
  - [Distributed Deployment](docs/distributed.md) ([中文](docs/distributed.zh-CN.md)) — Coordinator, Edge, and 307 routing.
  - [Security Model](docs/security.md) ([中文](docs/security.zh-CN.md)) — SSRF mitigation, token hashing, and isolation.
- **Reference & Operations**:
  - [Configuration Reference](docs/configuration.md) ([中文](docs/configuration.zh-CN.md)) — Complete YAML configuration guide.
  - [Production Verification](docs/verification.md) ([中文](docs/verification.zh-CN.md)) — Large object, throughput, and failover validation.
  - [Example Configuration](configs/config.example.yaml) — Production-ready YAML template.
  - [Roadmap](ROADMAP.md) — Future development milestones.
  - [Contributing Guide](CONTRIBUTING.md) — Development workflow and coding guidelines.

---

## Security

Security vulnerabilities should be reported responsibly according to our [Security Policy](.github/SECURITY.md).

---

## License

MirrorRelay is licensed under the [GNU General Public License v3.0](LICENSE).  
Third-party component licenses for bundled Nginx dependencies (musl, OpenSSL, PCRE2, zlib) are documented in [nginx/NOTICE.md](nginx/NOTICE.md).
