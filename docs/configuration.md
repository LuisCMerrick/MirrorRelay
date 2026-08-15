# Configuration reference

[English](configuration.md) | [简体中文](configuration.zh-CN.md)

RepoGate reads YAML from `/etc/repogate/config.yaml` by default. Start from [`configs/config.example.yaml`](../configs/config.example.yaml). Duration values use Go duration syntax such as `15s`, `5m` and `720h`. Byte values are integers.

Environment variables override only the documented bootstrap/runtime values:

| Variable | Meaning |
|---|---|
| `REPOGATE_ADMIN_USERNAME` | Initial administrator name when the database has no users |
| `REPOGATE_ADMIN_PASSWORD` | Initial administrator password; required on the first production start |
| `GOGC` | Go runtime GC target; takes precedence over YAML |
| `GOMEMLIMIT` | Go runtime soft memory limit; takes precedence over YAML |

Configuration loading is strict: unknown YAML keys and multiple YAML documents are rejected so a misspelled safety or transport switch cannot silently fall back to its default.

## Web UI overrides

The **Settings** page can manage most operational keys documented below. It saves one strictly validated settings document in the existing SQLite database. On process start, RepoGate loads YAML first and then applies the stored Web UI values over the matching fields, so the precedence is:

```text
documented environment variables -> saved Web UI operational values -> YAML
```

Saving or resetting the Web UI override does not hot-reload the running process. Restart RepoGate to apply the change. Repository Desired/Active changes use a separate immediate validation and activation path.

The Web UI covers endpoint enablement and loopback ports, ingress behavior, safe HTTP/TLS values, performance, metadata, redirect policy, cache limits/TTLs, operational security, transport, limits, health, logging, shutdown and Managed Upstream Nginx lifecycle values. YAML remains authoritative for bootstrap and trust-boundary locations:

```text
server.frontend_socket, server.frontend_socket_mode, runtime.*,
ingress.snippet_path, redirect.pin_validated_ip, tls.certificate,
tls.private_key, database.path,
cache.path, logging.path, admin.*, upstream_nginx.binary,
upstream_nginx.prefix, upstream_nginx.pid, upstream_nginx.log_path,
upstream_nginx.upstream_socket, upstream_nginx.upstream_socket_mode,
upstream_nginx.ca_bundle
```

Use **Reset to YAML after restart** to delete the stored override. Invalid stored data causes startup to fail explicitly instead of silently falling back to YAML.

## Local endpoints

| Key | Default | Description |
|---|---:|---|
| `server.unix_socket_enabled` | `true` | Listen for External Shared Nginx on a Unix socket |
| `server.frontend_socket` | `/run/repogate/frontend.sock` | Frontend socket path |
| `server.frontend_socket_mode` | `0660` | Required socket mode |
| `server.local_port` | `9081` | Loopback port used only when the frontend Unix socket is explicitly disabled |
| `upstream_nginx.upstream_unix_socket_enabled` | `true` | Use a Unix socket from Go to Managed Upstream Nginx |
| `upstream_nginx.upstream_socket` | `/run/repogate/upstream.sock` | Managed Upstream Nginx socket path |
| `upstream_nginx.upstream_socket_mode` | `0660` | Required socket mode |
| `upstream_nginx.upstream_local_port` | `9082` | Loopback port used only when the upstream Unix socket is explicitly disabled |

TCP fallback endpoints always bind `127.0.0.1`. If both sockets are disabled, the two local ports must differ. There is no implicit fallback from a failed Unix socket to TCP. Loopback TCP does not provide Unix filesystem ownership/mode isolation, so use it only when local-process trust is acceptable.

## Runtime and ingress

| Key | Description |
|---|---|
| `runtime.root` | Product runtime state root |
| `runtime.run_dir` | Socket and PID directory |
| `ingress.mode` | `external` (default) or `managed-standalone` |
| `ingress.generate_snippet` | Publish a reviewed External Shared Nginx integration aid after successful activation |
| `ingress.snippet_path` | Target `.conf` file or directory; a directory receives `repogate.conf` |
| `http.public_base_url` | HTTPS origin (no path/query/fragment) used in client examples and rewritten metadata |
| `http.listen`, `http.https_listen` | Used only by managed standalone ingress |
| `http.read_timeout`, `http.idle_timeout` | RepoGate HTTP server header/request and keepalive limits |
| `http.write_timeout` | RepoGate HTTP write limit; `0` intentionally permits long streaming responses |
| `tls.certificate`, `tls.private_key`, `tls.min_version` | Used only by managed standalone ingress; TLS minimum is `1.2` or `1.3` |
| `admin.path` | File-only administration prefix; defaults to `/admin/`, contains the UI and its nested `api/v1/`, and requires a restart after changes |

External mode neither tests nor binds public ports and never reloads the shared External Shared Nginx process. Generated host-mode blocks contain certificate placeholders that must be completed by the ingress administrator.

`admin.path` must be an absolute path made from safe URL segments. RepoGate adds the trailing slash, rejects system-path conflicts and does not publish the value in the public repository index. Treat a custom path only as reduced discoverability, not as a replacement for authentication or CIDR restrictions. Apply the generated administration location to External Shared Nginx after changing it.

## Managed Upstream Nginx

| Key | Description |
|---|---|
| `upstream_nginx.mode` | `managed`, `external` advanced mode, or `disabled` |
| `upstream_nginx.binary` | Version-bound executable; packages use `/usr/lib/repogate/nginx/nginx` |
| `upstream_nginx.prefix` | Runtime configuration root; default `/var/lib/repogate/runtime/upstream-nginx` |
| `upstream_nginx.pid` | PID file; default `/run/repogate/upstream-nginx.pid` |
| `upstream_nginx.log_path` | Access/error log directory; default `/var/log/repogate/upstream-nginx` |
| `upstream_nginx.ca_bundle`, `upstream_nginx.tls_verify_depth` | Platform CA bundle and upstream certificate-chain verification depth; DEB uses `/etc/ssl/certs/ca-certificates.crt`, while RPM uses `/etc/pki/tls/certs/ca-bundle.crt` |
| `upstream_nginx.resolver` | Resolver addresses embedded in the generated configuration |
| `upstream_nginx.resolver_refresh` | Safe DNS re-resolution/reconcile interval |
| `upstream_nginx.history_limit` | Immutable configuration versions retained |
| `upstream_nginx.restart_*` | Supervisor failure window and exponential-backoff limits |
| `upstream_nginx.worker_processes`, `upstream_nginx.worker_user`, `upstream_nginx.worker_connections` | Managed Upstream Nginx worker settings. `worker_user` is emitted only when the Nginx master runs as root; the packaged non-root service omits the redundant directive. |
| `upstream_nginx.stop_on_repogate_exit` | Stop Managed Upstream Nginx on normal RepoGate exit; default `false` preserves the data plane for attach/restart |

Managed Upstream Nginx runs as a supervised foreground child while started by RepoGate, so its actual exit code or terminating signal is recorded. After a RepoGate restart, it can attach to a still-running matching Nginx binary and PID; if that externally attached process later disappears, the status explicitly reports that its exit code is unavailable.

Every data-plane update is generated as a candidate, tested with the configured binary, published atomically and gracefully reloaded. A failure remains Desired/Failed and does not replace the active Go routing snapshot. That separation survives RepoGate restarts: if the failed Desired state still cannot reconcile, RepoGate validates and restores the latest persisted Active configuration and publishes its repository snapshot to the Go router.

## Storage and cache

| Key | Description |
|---|---|
| `database.path` | SQLite database path |
| `cache.path` | Managed Upstream Nginx cache directory |
| `cache.max_size_bytes`, `cache.max_files` | Capacity and observation limits |
| `cache.inactive` | Nginx inactive-object removal window |
| `cache.metadata_ttl`, `cache.package_ttl` | Global class defaults; repository values can override them |
| `cache.cleanup_interval` | Reclaim observation interval |
| `cache.wait_for_fill` | Cache lock/fill operational window |
| `cache.minimum_free_bytes` | Reserved-free-space setting exposed to operations |

Purge changes cache generations immediately. Old physical files remain unreachable and are reclaimed by Nginx `inactive`/`max_size`; job states remain Pending/Running until the observation window has elapsed and actual disk use is rescanned.

## Proxy behavior and performance

| Key | Description |
|---|---|
| `performance.stream_buffer_size_bytes` | Fixed streaming buffer: `32768`, `65536`, or `131072` |
| `performance.go_memory_limit_bytes` | Go soft memory limit; `0` leaves the runtime/environment unchanged |
| `performance.gogc` | Applied only when `GOGC` is absent |
| `metadata.rewrite_buffer_limit_bytes` | Default maximum buffered metadata entity |
| `metadata.output_compression` | `auto`, `identity`, or `gzip` |
| `metadata.gzip_min_length_bytes` | Minimum rewritten response size for gzip |
| `metadata.validator_entries` | In-memory metadata validator capacity |
| `redirect.max_hops` | Redirect limit, 1–20 |
| `redirect.pin_validated_ip` | Must remain `true` |
| `redirect.reject_mixed_dns_result` | Reject a hostname if its answer set mixes permitted and forbidden addresses |
| `transport.*` | Go-to-Managed Upstream Nginx connection pool and response-header timeouts; no fixed total body timeout is applied |

## Security and limits

| Key | Description |
|---|---|
| `security.allow_http_upstream` | Global half of the two-level HTTP upstream permission |
| `security.allow_private_upstream` | Global half of the two-level private-address permission |
| `security.expose_client_ip` | Permit the configured client context to be forwarded internally |
| `security.admin_cidrs` | CIDRs allowed to access the UI and nested API under `admin.path`; empty means unrestricted at this layer |
| `security.session_timeout` | Server-side session lifetime |
| `security.login_window`, `security.login_max_failures` | Per-client login throttle |
| `limits.max_total_concurrency` | Global request concurrency; `0` is unlimited |
| `limits.max_ip_concurrency` | Per-client concurrency; `0` is unlimited |
| `limits.bandwidth_limit_bps` | Global Managed Upstream Nginx upstream bandwidth ceiling; `0` is unlimited |

RepoGate trusts forwarded client IP headers only when the immediate peer is the local Unix socket or loopback listener. Private and HTTP upstream use also requires the matching per-repository switch. Upstream TLS verification cannot be disabled.

## Health, logs and shutdown

| Key | Description |
|---|---|
| `health.worker_interval` | Scheduler interval for repository checks through Managed Upstream Nginx |
| `logging.path` | RepoGate JSON access-log directory |
| `logging.queue_size` | Nonblocking log queue capacity |
| `logging.max_size_mb` | Per-file rotation threshold |
| `logging.keep_days` | Rotated JSON access-log retention |
| `shutdown.grace_period` | Maximum frontend graceful-drain window |

Managed Upstream Nginx access/error logs live under `upstream_nginx.log_path` and are rotated by RepoGate by date/size, followed by an Nginx log reopen. RepoGate access and application JSON logs are asynchronously written and use the same rotation and retention settings; audit events are stored in SQLite.

## Repository overrides

The Web UI and repository API expose profile/version, routing mode, multiple upstreams, strip/add prefixes, Host and request-header changes, connect/read/send timeouts, cache class TTLs, authenticated caching, metadata rewrite hosts/buffer limits, the per-repository `html_rewrite_enabled` switch, health policy, concurrency/bandwidth limits, access policy, Registry auth/token/blob policy and the HTTP/private permission switches. Repository validation rejects root/system/administration/`/_repogate/` path conflicts, duplicate or overlapping repository paths, duplicate public hosts and a host-mode repository that claims the configured shared host.

`html_rewrite_enabled` defaults to `false`. When enabled for a browsable repository response, RepoGate resolves same-origin HTML URLs against the selected upstream page. URLs below the effective upstream base (including `add_prefix`) return to the public repository namespace (including `strip_prefix`); other paths on the same origin use `/_repogate/upstream/<repository-id>/`. The auxiliary scope never authorizes another origin and uses the repository's normal upstream group and policy. It does expand the reachable path surface on that origin, so treat the switch as an explicit publication decision. The generated shared-ingress snippet contains the required auxiliary location for path-mode repositories.

Authenticated caching must be enabled only for content whose cache identity is public and does not vary by authorization. By default, requests carrying `Authorization` or `Cookie` bypass cache, and a configured static credential header disables cache. Nginx also does not cache responses with `Set-Cookie` unless that built-in protection is deliberately overridden; RepoGate's normal custom configuration cannot override it.

Normal custom Managed Upstream Nginx fragments are context-scoped and cannot create listeners, routes or upstream targets; alter TLS verification, cache identity or cache bypass; access the filesystem or process environment; or reference RepoGate's reserved variables, zones and internal headers. Every candidate is parsed by RepoGate and must also pass the bundled binary's `nginx -t` before activation. Unsafe/hop-by-hop/internal headers, invalid hosts, configuration-control characters and `insecure_skip_verify` are rejected.
