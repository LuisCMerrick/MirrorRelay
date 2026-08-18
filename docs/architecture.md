# Architecture Guide

[English](architecture.md) | [简体中文](architecture.zh-CN.md)

This document provides a comprehensive technical overview of MirrorRelay's architecture, data plane design, and configuration lifecycle.

---

## 1. High-Level Request Pipeline

MirrorRelay uses a clean two-plane separation between ingress/control logic and high-throughput upstream data transfer:

```text
Client (apt / dnf / pip / npm / docker)
   │ (HTTP / HTTPS on port 80 / 443)
   ▼
External Shared Nginx (Administrator-Owned)
   │ (Unix Domain Socket /run/mirrorrelay/frontend.sock, mode 0660)
   ▼
MirrorRelay Frontend (Go Service)
   ├── Routing Engine & Authentication
   ├── Metadata & HTML URL Rewriter (Bounded Memory Buffer)
   ├── Registry Token Broker & Redirect Broker
   ├── SQLite Persistent Desired State Store
   └── Upstream Nginx Candidate Generator & Validator
   │ (Unix Domain Socket /run/mirrorrelay/upstream.sock, mode 0660)
   ▼
Managed Upstream Nginx (Dedicated Musl Data Plane)
   ├── High-Throughput Proxy Caching (/var/cache/mirrorrelay)
   ├── Connection Pooling & Keep-Alive
   ├── TLS Hostname & Certificate Chain Verification
   └── Multi-Upstream Health Failover
   │ (HTTPS / HTTP)
   ▼
Original Upstream (deb.debian.org, pypi.org, registry-1.docker.io, etc.)
```

---

## 2. Why Two Nginx Instances?

A unique design choice in MirrorRelay is the separation between **External Shared Nginx** and **Managed Upstream Nginx**:

### External Shared Nginx (Ingress Plane)
- **Ownership**: Owned by the system administrator.
- **Role**: Terminates public TLS, handles domain certificates (e.g. Let's Encrypt / Certbot), and proxies requests to MirrorRelay's frontend Unix socket.
- **Isolation**: MirrorRelay packages **never** install, modify, restart, or reload External Shared Nginx. MirrorRelay generates a scoped integration snippet (e.g. `mirrorrelay.conf`) that the administrator can include.

### Managed Upstream Nginx (Data Plane)
- **Ownership**: Fully managed and version-bound by MirrorRelay.
- **Role**: Executes all high-throughput HTTP/HTTPS communication to Original Upstreams, manages the local disk cache, and handles connection pooling.
- **Binary Properties**: Built statically against musl libc, OpenSSL, PCRE2, and zlib with zero external runtime dependencies (`ldd` returns no shared libraries).
- **Security Invariant**: The Go service **never** makes direct HTTP connections to upstream package servers. All upstream traffic flows through Managed Upstream Nginx.

---

## 3. Go Control Plane & Router

The Go process (`/usr/bin/mirrorrelay`) owns all business logic, policy enforcement, and metadata handling:

### A. Desired vs. Active State Separation
1. **Desired State**: Stored in SQLite (`/var/lib/mirrorrelay/mirrorrelay.db`). Created or updated via Web UI or REST API.
2. **Candidate Generation**: The Go engine renders a candidate Nginx configuration (`nginx.conf`) in temporary storage.
3. **Pre-Flight Validation**: Executes `/usr/lib/mirrorrelay/nginx/nginx -t -c <candidate_path>` to ensure 100% syntactical correctness.
4. **Atomic Publication**: If pre-flight passes, the candidate is atomically copied over the active configuration.
5. **Graceful Reload**: Sends `SIGHUP` to the Managed Upstream Nginx master process. Existing worker processes finish serving active connections while new workers adopt the updated configuration without dropping traffic.
6. **Safety Fallback**: If validation fails, the desired change is recorded with an error state, but the currently running active configuration remains untouched.

### B. Bounded Metadata & HTML URL Rewriting
For repository types that embed upstream URLs inside response payloads (such as PyPI Simple HTML index pages or browsable package trees):
- Go buffers only bounded metadata (enforcing strict byte limits, e.g. 10 MB maximum).
- Rewrites internal upstream links to point back to MirrorRelay's public namespace or scoped upstream endpoints (`/_mirrorrelay/upstream/<id>/`).
- Large binary artifacts (e.g. `.deb`, `.rpm`, `.whl`, `.tar.gz`, Docker layer blobs) are streamed directly with fixed-size buffers, never buffered into Go memory.

### C. Token Broker & Redirect Broker
- **Registry Token Broker**: Intercepts `/v2/` `401 Unauthorized` Bearer challenges from upstream OCI registries, obtains authorized Bearer tokens using configured upstream credentials, and injects them without disclosing credentials to downstream clients.
- **Redirect Broker**: Inspects HTTP `301/302/307` redirect locations (e.g. S3/CloudFront presigned URLs), applies strict SSRF and IP pinning checks, and either rewrites the redirect or proxies the payload safely.

### D. Zero-Copy X-Accel Acceleration (Pure Binary Bypass)
For large immutable package artifacts (`.deb`, `.rpm`, `.whl`, `.tar.gz`, `.iso`, OCI layer blobs):
- When enabled (`performance.zero_copy_bypass: true`) and running with Ingress Nginx (`X-Accel-Supported: 1`), Go performs all RBAC, SSRF, and Package Guard checks.
- Once validated, Go returns an immediate HTTP 200 with `X-Accel-Redirect: /_repo/<repo_id>/<upstream_id>/package/<path>` and internal routing metadata.
- Ingress Nginx intercepts the header and proxies directly to the Managed Upstream Nginx Unix socket via Linux kernel buffers, completely bypassing Go user-space memory and achieving line-rate zero-copy throughput.

---

## 4. Security & Network Invariants

- **SSRF Defense**: Upstream URLs and redirect hops are resolved and validated against a comprehensive blacklist of private, loopback, link-local, and reserved CIDRs before connection.
- **IP Pinning**: Upstream DNS lookups are pinned to resolved numeric IPs during dialing while preserving TLS SNI (`server_name`) verification.
- **Header Sanitization**: Inbound client headers with `X-Mirror-Internal-*` prefixes are unconditionally stripped to prevent spoofing of internal routing state.
- **Socket Permissions**: Unix domain sockets default to mode `0660` owned by `root:mirrorrelay`.

---

## 5. Storage & File System Layout

```text
/etc/mirrorrelay/config.yaml                 # Read-only configuration file (mode 0640)
/var/lib/mirrorrelay/mirrorrelay.db          # SQLite database (desired state, users, audit logs)
/var/lib/mirrorrelay/runtime/upstream-nginx/ # Active nginx.conf and configuration history
/var/cache/mirrorrelay/                      # Nginx proxy_cache data directory
/var/log/mirrorrelay/upstream-nginx/         # Access and error log files
/run/mirrorrelay/frontend.sock               # Ingress socket from External Shared Nginx
/run/mirrorrelay/upstream.sock               # Internal socket to Managed Upstream Nginx
/run/mirrorrelay/upstream-nginx.pid          # Nginx master process PID
```
