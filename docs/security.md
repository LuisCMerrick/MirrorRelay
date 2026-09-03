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

Client `Authorization` and non-administration `Cookie` credentials are forwarded only while a proxy redirect or Metadata adapter target remains on the repository's effective origin. A cross-origin hop removes them permanently for the remainder of that redirect chain. The configured Registry full-proxy token route is the explicit exception because clients intentionally authenticate to that endpoint.

---

## 3. Authentication & Session Management

- **First-Use Enrollment**: No default or environment-provisioned administrator password exists. While the user table is empty, one registration may create the initial Admin through the configured administration host/path and CIDR boundary. The database condition is atomic, so only one concurrent request can succeed.
- **Password Hashing**: Administrative passwords are limited to 1024 bytes and hashed using **Argon2id** (memory: 64 MB, iterations: 3, up to 4 threads).
- **Concurrency Rate Limiting**: Password verification is protected by a concurrency semaphore to mitigate CPU-exhaustion DoS attacks.
- **Session Tokens**: 256-bit cryptographically secure random tokens (`crypto/rand`). Stored in SQLite as SHA-256 hashes (plaintext tokens are never stored in the database).
- **Cookie Security**: Session cookies enforce `HttpOnly`, `Secure`, and `SameSite=Strict`.
- **CSRF Protection**: Authenticated state-modifying requests (`POST`, `PUT`, `DELETE`) require a valid session CSRF token passed via `X-CSRF-Token` and verified in constant time (`crypto/subtle.ConstantTimeCompare`). The unauthenticated one-time enrollment request is instead constrained by the empty-database condition, atomic insertion and administration network boundary.
- **Passkeys (WebAuthn/FIDO2)**: Registration and authentication use bounded, five-minute, single-use challenges tied to the exact relying-party ID and origin. HTTPS is required outside loopback development. The verifier requires user presence and user verification, validates credential type/algorithm/key structure, and tracks authenticator signature counters.
- **Emergency Recovery**: Recovery codes are generated from unbiased cryptographic randomness, returned in plaintext only when created, stored as SHA-256 hashes, and consumed once. Password login cannot be disabled until the account has both a passkey and unused recovery codes; an atomic deletion condition protects the final passkey even under concurrent requests. The sign-in page keeps recovery-code access visible even if Passkey authentication is disabled or its status probe fails. `mirrorrelay admin reset-password --password-stdin` and `mirrorrelay admin reset-passkeys` provide local recovery using the normal strict configuration and Coordinator keyring path. Both revoke the account's existing sessions; password access is restored atomically with the recovery mutation.
- **Trusted Ingress Identity**: A TCP request may use `X-Real-IP` only when its immediate peer belongs to `security.trusted_proxy_cidrs`; otherwise the socket peer is the client identity. The explicitly enabled frontend Unix socket is trusted through its `0660` permission boundary. Generated ingress configuration overwrites `X-Real-IP` with `$remote_addr`; repository rewrites and public help URL generation ignore `X-Forwarded-Proto`, validate any fallback request authority, and remain HTTPS.
- **Encrypted Cluster Mutation Credentials**: Coordinator node mutation tokens are stored as AES-256-GCM authenticated ciphertext. An ordered file-backed keyring supports startup migration and rotation; a missing key or undecryptable credential stops startup instead of falling back to plaintext.

---

## 4. Input Validation & Injection Defenses

- **SQL Injection**: SQLite queries in `internal/database/` use parameterized `?` placeholders with `PRAGMA foreign_keys = ON`.
- **Path Traversal**: Repository paths are sanitized to reject NUL (`\0`), carriage return, newline, backslashes, directory traversal (`.` / `..`), and encoded URL separators (`%2f`, `%5c`). Cache storage keys use SHA-256 hashes of cleaned canonical paths.
- **Signed Auxiliary HTML Routes**: Same-origin targets outside a repository base are exposed only through HMAC-scoped URLs bound to the repository, exact selected upstream/Host policy, escaped path, and query. A client-modified upstream, path, or query is rejected before the request reaches Managed Upstream Nginx.
- **JSON Deserialization**: API request parsers reject unknown fields and trailing documents and enforce endpoint-specific body limits (normally at most 1 MiB; cluster protocol bodies are limited to 64 KiB).

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
- `/etc/mirrorrelay/cluster-mutation-token.key` when a Coordinator is enabled: `0640 root:mirrorrelay`
- `/var/lib/mirrorrelay/`: `0750 mirrorrelay:mirrorrelay`
- `/var/cache/mirrorrelay/`: `0750 mirrorrelay:mirrorrelay`
- `/run/mirrorrelay/*.sock`: `0660 mirrorrelay:mirrorrelay` for the packaged service; ingress access is granted explicitly by the administrator

---

## 6. Supply Chain Security & Package Name Guard

MirrorRelay includes built-in supply chain poisoning and dependency confusion defenses:
- **Package Blacklisting (`blocked_packages`)**: Regex and glob wildcard patterns (e.g. `^malicious-.*`, `bad-pkg-*.tar.gz`) preventing pulling poisoned or compromised packages.
- **Package Whitelisting (`allowed_packages`)**: Restricts packages to an enterprise-approved subset (e.g. `^internal-.*`).
- Each list is limited to 128 patterns and each pattern to 512 bytes. Rules beginning with `^` or ending with `$` are treated as whole-string RE2 expressions; every other rule is a Go glob. This explicit split prevents one ambiguous rule from gaining the union of two different match semantics. Rules are compiled before activation; invalid candidates are rejected and unexpected invalid Active state fails closed.
- When a client requests a blocked package, MirrorRelay immediately terminates the request with HTTP `403 Forbidden` and details the violated security policy rule.
- The development image verifies its checked-in Managed Upstream Nginx fixture against `nginx.sha256` during the build. Formal images verify both architecture-matched binaries against the package job's `BUILD-INFO`; release packages also carry `BUILD-INFO` and internal checksums, while the published OCI manifest has SBOM and provenance attestations.

---

## 7. Role-Based Access Control (RBAC)

MirrorRelay supports a three-tier permission model:
- **`admin` (Administrator)**: Full operational control, repository credential reveal/rotation, custom Managed Upstream Nginx fragments, cluster-node record management, user management, system settings override, service restart, and webhook test execution.
- **`operator` (Operator)**: Repository CRUD for non-secret fields, cache purge, health checks, cluster check/sync, and Nginx reload/rollback. Repository static-header values and token endpoints remain redacted and unchanged during edits. Operators cannot read generated/effective/custom Nginx configuration, manage node credentials, manage users, or alter system-level settings.
- **`viewer` (Viewer / Auditor)**: Read-only access to metrics, the audit log, redacted repository details, and health status. Viewers cannot read the System endpoint, Managed Upstream Nginx access records, or generated/effective/custom configuration. All mutating API calls are denied with HTTP `403 Forbidden`.

All non-Admin repository responses replace static-header values and token endpoints with a sentinel, and fail closed when redacting malformed or legacy upstream URLs containing query/userinfo credentials. Manual repository-check responses use the same URL redaction and replace raw connection errors with a generic failure. Only an Admin can use the ordinary repository editor to add, remove, or rotate those credential-bearing fields. While credentials are configured, only Admin may change their upstream/Host and routing bindings or the public access, package-filter, authenticated-cache and pull-only policies that control the credential's reach. System information is limited to Admin and Operator; the Operator representation omits certificate/private-key paths, listen/socket endpoints, process PID, generated integration snippets and raw Nginx diagnostics. The Health API likewise omits local network/socket endpoint coordinates for every non-Admin role. Configuration-history validation output, repository activation errors and failed audit-entry details are likewise Admin-only. Operational settings and the generated ingress configuration are Admin-only.

Managed Upstream Nginx records `$uri` rather than `$request_uri`, so access logs never retain query values such as tokens or signatures. The management API limits those records to Admin and Operator.

---

## 8. Webhook Security & Alerting

Enterprise notifications are delivered with HMAC-SHA256 signatures:
- One Webhook destination is active at a time. MirrorRelay auto-detects DingTalk, Feishu/Lark, WeCom and Slack hosts and uses their platform payloads; other hosts receive generic JSON.
- Headers include `X-MirrorRelay-Signature: sha256=<hex_digest>` and `X-MirrorRelay-Event: <event_name>` for payload authenticity verification.
- HTTPS is required by default. Plaintext HTTP and private/loopback/link-local targets require separate explicit `webhook.allow_http` and `webhook.allow_private` settings.
- The configured target and every redirect hop are resolved and filtered before connection. The safe dialer rejects DNS rebinding to a blocked address, TLS hostname verification stays enabled, and at most five redirect hops are accepted.
- Webhook tests can use the running destination or one temporary destination validated under the same policy. A temporary destination does not inherit the running signing secret. Invalid JSON stops immediately without sending a notification. Ordinary settings and history responses are Admin-only and redact the configured target and secret; only an explicit CSRF-protected full-backup export includes them.

---

## 9. Vulnerability Reporting

If you discover a security vulnerability, please refer to [.github/SECURITY.md](../.github/SECURITY.md) for responsible disclosure instructions.
