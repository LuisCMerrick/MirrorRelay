# Configuration reference

[English](configuration.md) | [简体中文](configuration.zh-CN.md)

MirrorRelay reads YAML from `/etc/mirrorrelay/config.yaml` by default. Start from [`configs/config.example.yaml`](../configs/config.example.yaml). Duration values use Go duration syntax such as `15s`, `5m` and `720h`. Byte values are integers.

Environment variables override only the documented deployment/runtime values:

| Variable | Meaning |
|---|---|
| `MIRRORRELAY_ADMIN_HOST` | Dedicated administration hostname |
| `MIRRORRELAY_DISTRIBUTED_ENABLED` | Boolean distributed-mode override |
| `MIRRORRELAY_DISTRIBUTED_ROLE` | `standalone`, `coordinator`, or `edge`; Coordinator/Edge also enable distributed mode |
| `MIRRORRELAY_DISTRIBUTED_TOKEN` | Shared read-only cluster probe credential |
| `MIRRORRELAY_DISTRIBUTED_MUTATION_TOKEN` | This Edge's unique sync/purge credential |
| `MIRRORRELAY_DISTRIBUTED_MUTATION_TOKEN_KEY_FILES` | Coordinator encryption-key files in OS path-list order; the first key encrypts and later keys are decrypt-only during rotation |
| `MIRRORRELAY_COORDINATOR_ID` | Coordinator identity trusted by this Edge |
| `MIRRORRELAY_NODE_NAME` | Distributed node identifier |
| `MIRRORRELAY_NODE_PUBLIC_BASE_URL` | Edge public redirect base URL |
| `MIRRORRELAY_NODE_REGION` | Distributed node region |
| `GOGC` | Go runtime GC target; takes precedence over YAML |
| `GOMEMLIMIT` | Go runtime soft memory limit; takes precedence over YAML |

Configuration loading is strict: unknown YAML keys and multiple YAML documents are rejected so a misspelled safety or transport switch cannot silently fall back to its default.

## Web UI overrides and configuration management

The **Settings** lifecycle can import and export all 22 top-level sections in `config.yaml`. Its structured form exposes the operational sections, while the dedicated **Appearance** page manages `ui_enhancement`. The bootstrap fields `database.path` and `distributed.mutation_token_key_files` must be loaded before the settings database and credential keyring can be opened, so the UI shows them as file-only and never persists an override. Other changes are strictly validated and stored in SQLite. Settings values and their redacted history entry are committed atomically; history is append-only while retained and pruned to `upstream_nginx.history_limit`. On process start, MirrorRelay loads YAML first and then applies the stored Web UI values over the matching fields, so the precedence is:

```text
documented environment variables -> saved Web UI operational values -> YAML
```

Operational settings saved, imported, reset, or rolled back do not hot-reload the running process; restart MirrorRelay to apply them. The `ui_enhancement` portion of an import or rollback is committed in the same transaction and published immediately through the dedicated appearance state. Repository Desired/Active changes use a separate immediate validation and activation path.

Persisted settings written by releases before v0.0.20 contain only the fields those releases exposed. During upgrade, MirrorRelay overlays that legacy document on the current YAML/default configuration and normalizes the former integer `warmup.timeout` representation plus the earliest renamed lifecycle field before strict validation. Fields that did not yet exist therefore inherit their current file/default values; configuration, database, users, history and cache do not need to be purged or recreated.

Use **Export configuration** to download standard or full-backup YAML. Both forms omit the instance-local `http.public_base_url` and `distributed.node.public_base_url`. Standard export also omits cluster credentials, the Webhook URL/signing secret, and the passkey RP ID/origins; full backup is a CSRF-protected `POST` that includes those credentials and must be stored as a secret. Import accepts at most 1 MiB of decoded YAML, validates the exact previewed content, preserves omitted local URLs, passkey bindings and credentials, and always preserves the file-only database/keyring paths. An omitted Edge mutation token is preserved only when that node URL is unchanged; a different origin requires an explicit new credential. Use **Reset to YAML after restart** to delete the stored operational override; use **Appearance → Restore defaults** for the separate immediate appearance override. Invalid stored data causes startup to fail explicitly instead of silently falling back to YAML. History responses never return their stored settings snapshot, and rollback uses its redacted snapshot while preserving the running credential values represented by redaction sentinels.

## Local endpoints

| Key | Default | Description |
|---|---:|---|
| `server.unix_socket_enabled` | `false` | Explicitly replace the default frontend TCP listener with a Unix socket |
| `server.frontend_socket` | `/run/mirrorrelay/frontend.sock` | Frontend socket path |
| `server.frontend_socket_mode` | `0660` | Required socket mode |
| `server.local_address` | `127.0.0.1` | Frontend TCP listen IP used while the frontend Unix socket is disabled |
| `server.local_port` | `9081` | Frontend TCP listen port used while the frontend Unix socket is disabled |
| `upstream_nginx.upstream_unix_socket_enabled` | `true` | Use a Unix socket from Go to Managed Upstream Nginx; explicitly set `false` to use TCP |
| `upstream_nginx.upstream_socket` | `/run/mirrorrelay/upstream.sock` | Managed Upstream Nginx socket path |
| `upstream_nginx.upstream_socket_mode` | `0660` | Required socket mode |
| `upstream_nginx.upstream_local_port` | `9082` | Loopback port used only when the upstream Unix socket is explicitly disabled |

External Shared Nginx reaches the Go frontend over `127.0.0.1:9081` by default. Set `server.unix_socket_enabled: true` only when that ingress should use `server.frontend_socket` instead. `server.local_address` accepts a literal IPv4 or IPv6 listen address; `0.0.0.0` and `::` are valid explicit wildcard binds and are represented as the corresponding loopback address in generated same-host Nginx configuration. A wildcard or non-loopback bind expands the trusted-ingress surface: protect it with a firewall or, for Docker, publish the container port only on host loopback. Never expose it directly to an untrusted network.

Go reaches Managed Upstream Nginx over its Unix socket by default. Only an explicit `upstream_nginx.upstream_unix_socket_enabled: false` selects the fixed `127.0.0.1` upstream TCP endpoint. If the frontend bind overlaps loopback and both links use TCP, the two local ports must differ. Neither endpoint silently falls back after a Unix socket failure, and every enabled Unix socket must use mode `0660`.

## Runtime and ingress

| Key | Description |
|---|---|
| `runtime.root` | Product runtime state root |
| `runtime.run_dir` | Socket and PID directory |
| `ingress.mode` | `external` (default) or `managed-standalone` |
| `ingress.generate_snippet` | Publish a reviewed External Shared Nginx integration aid after successful activation |
| `ingress.snippet_path` | Target `.conf` file or directory; a directory receives `mirrorrelay.conf` |
| `http.public_base_url` | HTTPS origin (no path/query/fragment) used in client examples and rewritten metadata |
| `http.listen`, `http.https_listen` | Used only by managed standalone ingress |
| `http.read_timeout`, `http.idle_timeout` | MirrorRelay HTTP server header/request and keepalive limits |
| `http.write_timeout` | MirrorRelay HTTP write limit; `0` intentionally permits long streaming responses |
| `tls.certificate`, `tls.private_key`, `tls.min_version` | Used only by managed standalone ingress; TLS minimum is `1.2` or `1.3` |
| `admin.path` | Administration prefix; defaults to `/admin/`, contains the UI and its nested `api/v1/`, and requires a restart plus an updated ingress snippet after changes |

External mode neither tests nor binds public ports and never reloads the shared External Shared Nginx process. Generated host-mode blocks contain certificate placeholders that must be completed by the ingress administrator.

`admin.path` must be an absolute path made from safe URL segments. MirrorRelay adds the trailing slash, rejects system-path conflicts and does not publish the value in the public repository index. Treat a custom path only as reduced discoverability, not as a replacement for authentication or CIDR restrictions. Apply the generated administration location to External Shared Nginx after changing it.

## Managed Upstream Nginx

| Key | Description |
|---|---|
| `upstream_nginx.mode` | `managed`, `external` advanced mode, or `disabled` |
| `upstream_nginx.binary` | Version-bound executable; packages use `/usr/lib/mirrorrelay/nginx/nginx` |
| `upstream_nginx.prefix` | Runtime configuration root; default `/var/lib/mirrorrelay/runtime/upstream-nginx` |
| `upstream_nginx.pid` | PID file; default `/run/mirrorrelay/upstream-nginx.pid` |
| `upstream_nginx.log_path` | Access/error log directory; default `/var/log/mirrorrelay/upstream-nginx` |
| `upstream_nginx.ca_bundle`, `upstream_nginx.tls_verify_depth` | Platform CA bundle and upstream certificate-chain verification depth; DEB uses `/etc/ssl/certs/ca-certificates.crt`, while RPM uses `/etc/pki/tls/certs/ca-bundle.crt` |
| `upstream_nginx.resolver` | Resolver addresses embedded in the generated configuration |
| `upstream_nginx.resolver_refresh` | Safe DNS re-resolution/reconcile interval |
| `upstream_nginx.history_limit` | Maximum retained Managed Upstream Nginx, Active-state and Web UI settings-history entries |
| `upstream_nginx.restart_*` | Supervisor failure window and exponential-backoff limits |
| `upstream_nginx.worker_processes`, `upstream_nginx.worker_user`, `upstream_nginx.worker_connections` | Managed Upstream Nginx worker settings. `worker_user` is emitted only when the Nginx master runs as root; the packaged non-root service omits the redundant directive. |
| `upstream_nginx.stop_on_mirrorrelay_exit` | Stop Managed Upstream Nginx on normal MirrorRelay exit; default `false` preserves the data plane for attach/restart |

Managed Upstream Nginx runs as a supervised foreground child while started by MirrorRelay, so its actual exit code or terminating signal is recorded. After a MirrorRelay restart, it can attach to a still-running matching Nginx binary and PID; if that externally attached process later disappears, the status explicitly reports that its exit code is unavailable.

Every data-plane update is generated as a candidate, tested with the configured binary, published atomically and gracefully reloaded. A failure remains Desired/Failed and does not replace the active Go routing snapshot. That separation survives MirrorRelay restarts: if the failed Desired state still cannot reconcile, MirrorRelay validates and restores the latest persisted Active configuration and publishes its repository snapshot to the Go router.

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
| `performance.zero_copy_bypass` | Let External Shared Nginx consume the private Managed Upstream Nginx socket after Go authorization; default `true` for host packages, explicitly `false` in the Docker example with a container-private runtime tmpfs |
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

## Smart Cache Warm-Up & Predictive Pre-Fetching

| Key | Description |
|---|---|
| `warmup.enabled` | Enable smart warm-up and predictive pre-fetching engine (default `false`) |
| `warmup.max_concurrency` | Maximum concurrent warm-up download workers (default `4`) |
| `warmup.bandwidth_limit_bps` | Bandwidth throttling for warm-up requests in bytes/sec (default `0` = unlimited) |
| `warmup.timeout` | Per-job warm-up timeout (default `30m`) |
| `warmup.retry_count` | Maximum retry attempts per failed item (default `2`) |
| `warmup.metadata_depth` | Metadata package extraction depth (default `1` = extract packages from APT/RPM/PyPI metadata) |

Warm-up jobs accept either a five-field numeric cron expression evaluated in UTC or one of `@hourly`, `@daily`, and `@every <duration>` (minimum interval `30s`). Numeric fields support `*`, lists, ranges and steps. MirrorRelay validates expressions on create/update, persists the calculated `next_run_at`, and never runs an invalid or unknown expression as a fallback. Metadata-discovered package URLs reuse the configured frontend endpoint, including `server.local_address` and `server.local_port` while the frontend Unix socket is disabled.

## Webhook delivery

| Key | Description |
|---|---|
| `webhook.enabled` | Enable asynchronous event delivery |
| `webhook.url` | Single active destination URL; HTTPS is required by default and the provider payload format is auto-detected from the host |
| `webhook.secret` | Optional HMAC-SHA256 signing secret; settings responses and history always redact both the secret and target URL |
| `webhook.events` | Event names to deliver; an empty list enables every event |
| `webhook.timeout` | Request, TLS handshake and response-header timeout |
| `webhook.allow_http` | Independent explicit opt-in for plaintext HTTP; default `false` |
| `webhook.allow_private` | Independent explicit opt-in for private, loopback and link-local destinations; default `false` |

MirrorRelay delivers each event to one configured Webhook destination; DingTalk, Feishu/Lark, WeCom and Slack hosts receive their platform-specific payload, while other hosts receive standard MirrorRelay JSON. Webhook targets and every redirect hop are syntax-checked, DNS-resolved and filtered immediately before connection. The connection uses only policy-approved addresses while retaining TLS hostname verification. Tests can use the running destination or one validated temporary URL/secret. A temporary URL never inherits the running destination's secret, and malformed test input has no delivery side effect.

## Security and limits

| Key | Description |
|---|---|
| `security.allow_http_upstream` | Global half of the two-level HTTP upstream permission |
| `security.allow_private_upstream` | Global half of the two-level private-address permission |
| `security.expose_client_ip` | Forward the validated client address through the internal request path; default `false` |
| `security.trusted_proxy_cidrs` | TCP peer CIDRs allowed to supply `X-Real-IP`; defaults to IPv4/IPv6 loopback, and an empty list trusts no TCP peers |
| `security.admin_cidrs` | CIDRs allowed to access the UI and nested API under `admin.path`; empty means unrestricted at this layer |
| `security.session_timeout` | Server-side session lifetime |
| `security.login_window`, `security.login_max_failures` | Per-client login throttle |
| `admin.host` | Dedicated hostname for administration console and metrics isolation |
| `admin.path` | Base path for administration web console and REST API |
| `admin.passkey.enabled` | Enable WebAuthn passkey registration and authentication |
| `admin.passkey.rp_name` | Display name shown by the authenticator; at most 128 bytes |
| `admin.passkey.rp_id` | Exact lowercase hostname/IP relying-party ID, without scheme, port or path |
| `admin.passkey.origins` | Exact allowed `https://` origins; loopback development alone may use HTTP, and every host must match the RP ID |
| `limits.max_total_concurrency` | Global request concurrency; `0` is unlimited |
| `limits.max_ip_concurrency` | Per-client concurrency; `0` is unlimited |
| `limits.bandwidth_limit_bps` | Global Managed Upstream Nginx upstream bandwidth ceiling; `0` is unlimited |

MirrorRelay uses the TCP peer address unless it belongs to `security.trusted_proxy_cidrs`; only then may a valid `X-Real-IP` replace it. The explicitly enabled, permission-controlled frontend Unix socket is trusted as an ingress peer. External Shared Nginx must overwrite, never append or pass through, the client-supplied `X-Real-IP` value. Generated snippets do this with `$remote_addr`. MirrorRelay does not trust `X-Forwarded-Proto` when producing public links: `http.public_base_url` takes precedence and every inferred public origin uses HTTPS. Keep the default loopback bind, use a private Unix socket, or enforce an equivalent container/firewall boundary when configuring another listen IP. Private and HTTP upstream use also requires the matching per-repository switch. Upstream TLS verification cannot be disabled.

## Health, logs and shutdown

| Key | Description |
|---|---|
| `health.worker_interval` | Scheduler interval for repository checks through Managed Upstream Nginx |
| `logging.path` | MirrorRelay JSON access-log directory |
| `logging.queue_size` | Nonblocking log queue capacity |
| `logging.max_size_mb` | Per-file rotation threshold |
| `logging.keep_days` | Rotated JSON access-log retention |
| `shutdown.grace_period` | Maximum frontend graceful-drain window |

Managed Upstream Nginx access/error logs live under `upstream_nginx.log_path` and are rotated by MirrorRelay by date/size, followed by an Nginx log reopen. MirrorRelay access and application JSON logs are asynchronously written and use the same rotation and retention settings; audit events are stored in SQLite.

## UI Enhancement and Appearance

MirrorRelay provides optional appearance customization, color themes, directory browser rewriting, and repository client help guides.

| Key | Description |
|---|---|
| `ui_enhancement.enabled` | Public repository UI enhancement switch (default `false`). When `false`, upstream directory responses are not restyled or rewritten. The administration theme selector remains available. |
| `ui_enhancement.theme` | Theme mode: `system` (default), `light`, or `dark` |
| `ui_enhancement.accent_color` | Three-, six- or backward-compatible eight-digit accent color hex code (for example `#2563eb` or `#2563eb80`) |
| `ui_enhancement.branding.title` | Custom instance title / site name (default `MirrorRelay`) |
| `ui_enhancement.branding.logo` | Optional same-origin absolute logo path beginning with `/`; the ingress must serve the asset |
| `ui_enhancement.branding.favicon` | Optional same-origin absolute favicon path beginning with `/`; the ingress must serve the asset |
| `ui_enhancement.login.title` | Login page heading title |
| `ui_enhancement.login.subtitle` | Login page subtitle |
| `ui_enhancement.custom_css.enabled` | Enable custom CSS stylesheet injection |
| `ui_enhancement.custom_css.file` | Clean absolute path to a regular `.css` file (served via `GET /ui/custom.css`; symbolic links and files larger than 1 MiB are rejected) |
| `ui_enhancement.repository_browser.enabled` | Enable modern responsive directory listing browser (default `true` when UI enhancement is active) |

The administration UI also exposes a browser-local Light / Dark / Auto selector on the login page and in the header. Auto follows `prefers-color-scheme`; a saved browser preference overrides the instance default until changed locally.

> **Safe UI behavior**: On an upstream directory URL, `?safe-ui=1` skips MirrorRelay's generated directory browser and leaves the ordinary upstream-response path in place (a repository's separately enabled compatibility URL rewrite can still apply). On a generated `/help/` page, it suppresses optional custom CSS only; the built-in help layout and selector/copy script remain active.

## Client Configuration Help

MirrorRelay supports built-in and customized interactive client configuration documentation for repositories (e.g. Debian, Ubuntu, Rocky Linux, Alpine, PyPI, npm, Docker CE, OpenWrt).

- Public overview route: `GET /help/`
- Interactive repository help route: `GET /help/<slug>/`
- Credential scrubbing: Upstream URLs referenced in help templates automatically scrub embedded credentials and query strings.

## Repository overrides

The Web UI and repository API expose profile/version, routing mode, multiple upstreams, strip/add prefixes, Host and request-header changes, connect/read/send timeouts, cache class TTLs, authenticated caching, metadata rewrite hosts/buffer limits, the per-repository `html_rewrite_enabled` switch, health policy, concurrency/bandwidth limits, access policy, client help documentation settings (`help.enabled`, `help.template`, `help.title`, `help.summary`), Registry auth/token/blob policy and the HTTP/private permission switches. Repository validation rejects root/system/administration/`/_mirrorrelay/` path conflicts, duplicate or overlapping repository paths, duplicate public hosts and a host-mode repository that claims the configured shared host.

Each of `blocked_packages` and `allowed_packages` accepts at most 128 rules, and each rule is limited to 512 bytes. A rule beginning with `^` or ending with `$` is parsed as a whole-string RE2 regular expression; every other rule is parsed as a Go glob. Rules are compiled when a repository candidate is validated and again when an Active routing snapshot is built; an invalid candidate is rejected, while unexpectedly invalid persisted state fails closed instead of silently skipping the policy.

`html_rewrite_enabled` defaults to `false`. When enabled for a browsable repository response, MirrorRelay resolves same-origin HTML URLs against the selected upstream page. URLs below the effective upstream base (including `add_prefix`) return to the public repository namespace (including `strip_prefix`). Other same-origin paths receive an opaque `/_mirrorrelay/upstream/<repository-id>/<upstream-id>/<signature>/<target>` URL. The HMAC covers the repository, exact selected upstream/Host policy, escaped target path, and query, so clients cannot substitute another upstream, root path, or query. Only targets emitted by MirrorRelay are accepted; cross-origin URLs remain unchanged. The request still uses the repository's access policy, pinned address, TLS verification, cache policy, and limits through Managed Upstream Nginx. The generated shared-ingress snippet contains the required auxiliary location for path-mode repositories.

Authenticated caching must be enabled only for content whose cache identity is public and does not vary by authorization. By default, requests carrying `Authorization` or `Cookie` bypass cache, and a configured static credential header disables cache. Nginx also does not cache responses with `Set-Cookie` unless that built-in protection is deliberately overridden; MirrorRelay's normal custom configuration cannot override it.

Normal custom Managed Upstream Nginx fragments are context-scoped and cannot create listeners, routes or upstream targets; alter TLS verification, cache identity or cache bypass; access the filesystem or process environment; or reference MirrorRelay's reserved variables, zones and internal headers. Every candidate is parsed by MirrorRelay and must also pass the bundled binary's `nginx -t` before activation. Unsafe/hop-by-hop/internal headers, invalid hosts, configuration-control characters and `insecure_skip_verify` are rejected.

## Distributed deployment

MirrorRelay supports distributed multi-node clusters consisting of a Coordinator and multiple Edge nodes.

```text
Client -> Coordinator -> (HTTP 307 Temporary Redirect) -> Edge Node -> Managed Upstream Nginx -> Origin
```

| Key | Description |
|---|---|
| `distributed.enabled` | Global distributed mode switch |
| `distributed.role` | Node role: `standalone` (default), `coordinator`, or `edge` |
| `distributed.token` | Required shared read-only credential for manifest and health probes |
| `distributed.mutation_token` | Edge-only sync/purge credential; must differ from the probe token and every other Edge token |
| `distributed.mutation_token_key_files` | Coordinator-only ordered AES-256 keyring used to encrypt stored per-Edge mutation tokens; at least one absolute path is required |
| `distributed.coordinator_id` | Coordinator identity accepted by an Edge for protocol v2 mutation envelopes |
| `distributed.allow_http` | Independent explicit opt-in for plaintext cluster-node origins; default `false` |
| `distributed.node.name` | Unique node identifier; required for Coordinator/Edge roles and included in manifests |
| `distributed.node.public_base_url` | Base URL used by coordinator when redirecting clients to this edge |
| `distributed.node.region` | Node region identifier for geolocation and CIDR routing |
| `distributed.node.country` | ISO 3166-1 alpha-2 country code |
| `distributed.routing.mode` | Routing algorithm mode: `hybrid` (default), `cidr`, `geo`, or `priority` |
| `distributed.routing.client_networks` | CIDR-to-region routing rules mapping client IP ranges to regions |
| `distributed.routing.regions` | Country-to-region mapping definitions |
| `distributed.health_check.interval` | Coordinator node health and consistency check interval |
| `distributed.health_check.timeout` | Coordinator probe request timeout |
| `distributed.health_check.healthy_threshold` | Consecutive successes required to mark a node healthy |
| `distributed.health_check.unhealthy_threshold` | Consecutive failures required to mark a node unhealthy |
| `distributed.nodes` | Initial seed Edge nodes for Coordinator bootstrap; every item requires a unique `mutation_token` |

### Distributed Invariants

- **Data plane isolation**: The Coordinator never fetches upstream or edge packages; it returns `HTTP 307 Temporary Redirect` preserving the original request path and query string.
- **Config consistency**: The Coordinator derives the authoritative fingerprint from its complete local Active repositories plus custom Managed Upstream Nginx configuration. Edge reports can never establish it. Protocol, Coordinator identity/epoch, configuration generation and fingerprint must match, and the requested repository must be explicitly healthy.
- **Replay defense**: Edge persists accepted Coordinator identity, epoch, generation and fingerprint. Older generations, conflicts and retired epochs are rejected; sync/purge uses a different per-Edge mutation credential from the shared probe credential.
- **Safe control plane**: Cluster origins must be absolute origins without credentials, path, query or fragment. HTTPS is the default; `distributed.allow_http` and the global private-address policy are separate explicit decisions.
- **Container registries**: Distributed routing is explicitly disabled for Docker and OCI registries (returns HTTP 501).
