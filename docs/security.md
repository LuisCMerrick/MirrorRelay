# Security Model and Policy

[English](security.md) | [简体中文](security.zh-CN.md)

MirrorRelay is designed from the ground up for zero-trust network boundaries, strict origin isolation, and defense-in-depth against Server-Side Request Forgery (SSRF) and credential disclosure.

---

## 1. Core Security Invariants

1. **Dedicated Data-Plane Isolation**: The Go control plane never issues direct HTTP requests to upstream package mirrors. All upstream traffic is handled by the isolated, statically linked `Managed Upstream Nginx` binary.
2. **Strict SSRF Defense**: Every upstream target URL and every HTTP redirect hop is resolved and validated against a comprehensive blacklist of private, loopback, link-local, and cloud metadata CIDRs.
3. **No TLS Verification Bypass**: Upstream HTTPS connections always enforce strict TLS certificate validation (`proxy_ssl_verify on`). Insecure certificate bypasses are prohibited.
4. **Internal Header Stripping**: Headers with the `X-Mirror-Internal-*` prefix sent by downstream clients are unconditionally stripped before proxy evaluation.
5. **Least Privilege**: MirrorRelay runs under an unprivileged `mirrorrelay:mirrorrelay` system service account with sandboxed systemd restrictions.

---

## 2. SSRF Protection & IP Pinning

When a repository or redirect target is evaluated:

1. **Scheme & Port Check**: Only `http` (ports 80, 8080) and `https` (port 443, 8443) with valid hostnames are accepted.
2. **DNS Resolution & Filtering**: Hostnames are resolved to numeric IP addresses. If any resolved IP belongs to a blacklisted subnet, the connection is rejected unless `allow_private_upstream` is explicitly enabled:
   - IPv4: `0.0.0.0/8`, `10.0.0.0/8`, `100.64.0.0/10`, `127.0.0.0/8`, `169.254.0.0/16`, `172.16.0.0/12`, `192.0.0.0/24`, `192.0.2.0/24`, `192.168.0.0/16`, `198.18.0.0/15`, `198.51.100.0/24`, `203.0.113.0/24`, `224.0.0.0/4`, `240.0.0.0/4`
   - IPv6: `::/128`, `::1/128`, `fc00::/7`, `fe80::/10`, `ff00::/8`, `::ffff:0:0/96`
3. **IP Pinning with SNI Verification**: Connections are dialed directly to the validated IP address to prevent DNS rebinding attacks while preserving the original hostname for TLS SNI and certificate chain validation.

---

## 3. Authentication & Session Management

- **Password Hashing**: Administrative passwords are hashed using **Argon2id** (memory: 64 MB, iterations: 3, parallelism: 2).
- **Concurrency Rate Limiting**: Password verification is protected by a concurrency semaphore to mitigate CPU-exhaustion DoS attacks.
- **Session Tokens**: 256-bit cryptographically secure random tokens (`crypto/rand`). Stored in SQLite as SHA-256 hashes (plaintext tokens are never stored in the database).
- **Cookie Security**: Session cookies enforce `HttpOnly`, `Secure`, and `SameSite=Strict`.
- **CSRF Protection**: All state-modifying requests (`POST`, `PUT`, `DELETE`) require a valid session CSRF token passed via `X-CSRF-Token` verified in constant time (`crypto/subtle.ConstantTimeCompare`).

---

## 4. Input Validation & Injection Defenses

- **SQL Injection**: 100% of SQLite database queries in `internal/database/store.go` use parameterized `?` placeholders with `PRAGMA foreign_keys = ON`.
- **Path Traversal**: Repository paths are sanitized to reject NUL (`\0`), carriage return, newline, backslashes, directory traversal (`.` / `..`), and encoded URL separators (`%2f`, `%5c`). Cache storage keys use SHA-256 hashes of cleaned canonical paths.
- **JSON Deserialization**: All API request parsers enforce `DisallowUnknownFields()` and `http.MaxBytesReader` limits (1 MB maximum).

---

## 5. System Hardening & Sandboxing

The official `mirrorrelay.service` systemd unit enforces strict Linux namespace and process sandboxing:

```ini
[Service]
User=mirrorrelay
Group=mirrorrelay
UMask=0007
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ProtectKernelTunables=true
ProtectKernelModules=true
ProtectControlGroups=true
RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6
RestrictNamespaces=true
LockPersonality=true
CapabilityBoundingSet=
```

File system permissions:
- `/usr/bin/mirrorrelay` & `/usr/lib/mirrorrelay/nginx/nginx`: `0755 root:root`
- `/etc/mirrorrelay/config.yaml`: `0640 root:mirrorrelay`
- `/var/lib/mirrorrelay/`: `0750 mirrorrelay:mirrorrelay`
- `/var/cache/mirrorrelay/`: `0750 mirrorrelay:mirrorrelay`
- `/run/mirrorrelay/*.sock`: `0660 root:mirrorrelay`

---

## 6. Vulnerability Reporting

If you discover a security vulnerability, please refer to [.github/SECURITY.md](../.github/SECURITY.md) for responsible disclosure instructions.
