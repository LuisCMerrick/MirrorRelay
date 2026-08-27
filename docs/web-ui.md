# Web UI guide

[English](web-ui.md) | [简体中文](web-ui.zh-CN.md)

MirrorRelay embeds its administration UI under `admin.path`, which defaults to `/admin/`. Its same-origin API is nested at `<admin.path>api/v1/`. The UI manages repositories, profiles, Managed Upstream Nginx, cache operations, health, logs and administrator accounts.

The public shared-domain root `/` is a lightweight repository index. It lists every enabled repository visible to the requesting client and links path-mode repositories to their published paths, but it does not disclose the administration path. A dedicated host-mode repository still owns `/` on its configured host. The generated External Shared Nginx snippet includes exact index and configured administration locations without claiming unrelated paths.

## Access and sign-in

Use the HTTPS URL published by External Shared Nginx, for example:

```text
https://repo.example.com/admin/
```

Change `admin.path` only in `config.yaml`, then restart MirrorRelay and update the reviewed External Shared Nginx snippet. The value is normalized to a trailing slash and may use multiple safe path segments. A non-default path reduces unsolicited discovery but is not an authentication control; HTTPS, administrator credentials and `security.admin_cidrs` remain required controls.

The session cookie is `Secure`, `HttpOnly` and `SameSite=Strict`. Production sign-in therefore requires HTTPS and the UI and API must remain on the same origin. A plain-HTTP address on a non-loopback host can accept the login request but the browser will not retain the session cookie. Do not expose the trusted frontend TCP listener or optional Unix socket directly to an untrusted network.

When the database contains no users, the sign-in page becomes a one-time initial-administrator registration page. The first successful registration creates the sole initial Admin and signs it in immediately; an atomic database condition prevents two concurrent requests from both succeeding. This endpoint is still protected by `admin.host` and `security.admin_cidrs`. Restrict the administration surface and finish registration before exposing it to untrusted networks. After any user exists, registration is permanently unavailable unless an administrator explicitly purges the persistent database.

APT repository details and built-in help offer both modern DEB822 (`/etc/apt/sources.list.d/*.sources`) and traditional one-line `sources.list` output. Distribution/version and output format are independent selectors, so the same Debian or Ubuntu target can use either representation.

The UI starts in English unless the browser's preferred languages include Chinese. Use `EN` or `中文` in the upper-right corner to choose manually; the selection is saved in browser local storage. Language resources are cleanly decoupled into dedicated locale files (`locales/en.js` and `locales/zh.js`). Sign out from the bottom of the sidebar when the session is no longer needed.

## Operating model

Repository and custom-configuration changes follow this activation path:

```text
Edit desired state -> generate candidate -> validate with nginx -t
                   -> publish atomically -> graceful reload -> active state
```

A failed validation or reload leaves the previous active routing configuration in place. The Repositories page can therefore show a failed Desired state while its last valid Active state continues serving traffic. Review the repository error, generated configuration and Managed Upstream Nginx status before retrying.

## Interface hierarchy

The administration UI follows one hierarchy across pages:

- Keep the first screen to three to five decision-relevant status metrics; avoid duplicating build and runtime facts across overview pages.
- Put technical identity, endpoint internals and low-frequency maintenance controls in native disclosure sections or a secondary details dialog. Sections are closed by default unless they contain the page's immediate task.
- Show one clear primary action for the current workflow. Use neutral secondary controls for inspection and reserve the danger treatment for destructive operations.
- Keep generated configuration hidden by default, load sensitive/effective content only when authorized users request it, and place the copy control beside the expanded content.
- Preserve visible keyboard focus, semantic `details`/`summary` behavior and reduced-motion preferences in both light and dark themes.

## Recommended first-use sequence

This sequence assumes an Admin account; role-restricted pages and configuration previews are intentionally absent for other roles.

1. Open **System** and confirm the MirrorRelay identity and local endpoints.
2. Open **Managed Upstream Nginx → Technical details** and confirm its version, build ID and architecture.
3. Open **Health** and confirm that MirrorRelay, the frontend endpoint, Go Router, Managed Upstream Nginx and the upstream endpoint are healthy or running.
4. Open **Ingress integration**, expand and review the generated snippet, then apply it through the existing ingress administrator's normal process.
5. Add one repository, test its upstreams and inspect its generated configuration.
6. Open the public domain root and confirm that the repository appears in the index.
7. Use the client example shown in repository details to make a small request before enabling production traffic.

## Dashboard

Dashboard keeps the first screen focused on repository health, current request/traffic totals and cache hit rate. The static request-path topology is collapsed by default and can be expanded when diagnosing the deployment path.

Dashboard also provides:

- total and enabled repositories;
- healthy and unhealthy repositories;
- active requests, request counts and traffic for today, 24 hours and 7 days;
- cache hit rate and observed cache use;
- per-repository traffic, status classes, cache hits/misses and upstream errors;
- hourly traffic charts and HTTP/cache breakdowns.

Dashboard counters are operational observations, not billing records. Cache usage is scanned asynchronously, so it can lag behind a purge or filesystem change.

## Repositories

### Create or edit a repository

Choose **Repositories → Add repository**. A profile can populate recommended defaults; all populated fields remain editable. Existing repositories remain pinned to the selected profile version until an upgrade is explicitly previewed and applied.

The form is grouped as follows:

| Group | Purpose |
|---|---|
| Identity and routing | Name, slug, repository type, path/host publication and access policy |
| Upstreams and path mapping | Ordered origin URLs, path transformation, Host rewrite, proxy and redirect behavior |
| Headers and timeouts | Controlled request-header additions/removals and connection/read/send limits |
| Cache and rewrite | Cache class, metadata adapter, browsable-HTML same-origin URL rewrite, allowlisted rewrite hosts and class-specific TTL overrides |
| Health and limits | Health probe, enabled state, concurrency profile and bandwidth limit |
| Registry and upstream security | Registry authentication/blob behavior plus HTTP/private-origin permissions |

Enter one upstream per line as `priority URL`:

```text
10 https://primary.example/repository/
20 https://backup.example/repository/
```

Lower priority numbers are considered first. URLs must include `http://` or `https://`; HTTP and private-address origins require both the global security switch and the repository switch. TLS verification cannot be disabled.

For path mode, supply a public path such as `/debian/`. MirrorRelay rejects paths that equal or contain the administration/system paths, overlap another repository path, or replace the root index. It also rejects duplicate public hosts and, when `http.public_base_url` is set, a host-mode repository that would claim the shared host. For host mode, supply a dedicated public host and complete the TLS/server block in External Shared Nginx. Use **Admin CIDR only** only when `security.admin_cidrs` defines the intended clients.

**Rewrite same-origin URLs in browsable HTML** is a per-repository compatibility switch and is off by default. It resolves HTML `href`, `src`, `srcset`, `action` and related URL attributes against the actual upstream page URL. A target inside the configured upstream repository base maps back into the public repository namespace. A target outside that base but on the same upstream origin receives an opaque signed `/_mirrorrelay/upstream/<repository-id>/<upstream-id>/<signature>/<target>` URL, so upstream icons, stylesheets, scripts and images remain available without changing their content. Cross-origin URLs and non-HTTP(S) schemes are left unchanged; mixed `srcset` candidates are handled independently, including data URLs.

The auxiliary route accepts only `GET` and `HEAD`. Its HMAC binds the repository, exact upstream selected for the source HTML, Host policy, path and query; changing any component is rejected. It remains bound to the repository's public host/access policy and uses the same pinned upstream, TLS verification, headers, cache policy and limits through Managed Upstream Nginx. The signing key is generated once and retained in the MirrorRelay database. The generated External Shared Nginx snippet includes `/_mirrorrelay/upstream/` when an enabled path-mode repository needs it. Apply the refreshed snippet to the shared ingress. HTML bodies use the repository metadata rewrite limit, compression policy and generated representation validators.

Select **Validate, save and activate** to submit the candidate. Success means candidate generation, validation, persistence, atomic publication and graceful reload all completed. An error remains in the form and does not replace the active configuration.

Repository static request-header values and token endpoints are credential-bearing fields. They are visible and editable only to Admin users. Operator responses show a redaction sentinel, and an Operator edit must preserve the existing sentinel-backed values; attempts to add, remove, or rotate them are rejected. When credentials exist, changing their upstream/Host, routing, public exposure, package filters, authenticated caching or pull-only policy is also Admin-only.

### Repository actions

| Action | Result |
|---|---|
| Details | Shows Desired versus Active state, counters, upstream health and client examples |
| Copy URL | Copies the published repository URL; browser clipboard permission may be required |
| Test | Runs configured health checks against all enabled upstreams |
| Config | Admin-only preview of the generated repository portion of the Nginx candidate |
| Purge | Logically invalidates the whole repository or one optional object path |
| Edit | Opens the full repository form |
| Enable / Disable | Validates and activates a new desired state with the selected enabled flag |
| Delete | Deletes the repository and logically invalidates its cache; this cannot be undone |

The Details dialog provides generated client commands and a profile-upgrade preview when a newer pinned profile exists. Admin users can additionally request the complete generated configuration. Review the field-by-field diff and, when authorized, the generated configuration before selecting **Apply upgrade**.

All row and dialog actions are same-origin API operations. They display a success or error message after completion, and the active button is temporarily disabled while its request is running. **Copy URL** uses the browser Clipboard API when available and falls back to a local text selection for browsers that deny Clipboard API access.

Cache purge is generation-based. New requests stop using the invalidated namespace immediately, while old physical files remain until the asynchronous Nginx cache manager reclaims them according to `inactive` and `max_size`.

## Profiles

Profiles are read-only, versioned starting points for supported repository ecosystems. The page shows the profile name, version, repository type, default upstream, publication/proxy modes and whether caching or metadata rewriting is enabled. Selecting a profile never makes future profile releases apply automatically.

## Managed Upstream Nginx

The first screen shows only process state, uptime, the current configuration version and configuration history. Select **Technical details** to open the secondary view containing PID, start time, binary version, build ID, architecture, SHA-256, configuration hash, reload/exit data and Nginx compile options.

The effective Nginx configuration is Admin-only, hidden, and not requested from the API until **Effective configuration** is expanded. Operator and Viewer users retain the permitted status/history view without being offered a configuration control they cannot access. Non-Admin status/history omits the process PID, generated integration snippet and raw validation/lifecycle diagnostics; repository Test output also redacts credential-bearing URL components and raw connection errors.

- **Regenerate, validate and reload** reconciles the current desired repositories and custom fragments.
- **Rollback** restores both repositories and custom configuration from the selected immutable history version, validates it and performs a graceful reload.

Do not treat rollback as a text-only Nginx rollback: it changes the persisted repository and custom-configuration state to the selected snapshot.

## Custom configuration

The Custom configuration page and API are Admin-only because fragments are code-level changes. Custom fragments target a controlled `http`, `server`, `location`, `upstream` or repository context. Use repository ID `0` for a global fragment. Saving or deleting a fragment generates and validates the complete candidate before activation.

MirrorRelay rejects directives that could escape the selected context, create listeners/routes/upstream targets, weaken TLS validation, alter cache identity or bypass, access the filesystem or environment, or use reserved variables and internal headers. Custom fragments are an advanced escape hatch; prefer repository fields whenever they express the requirement.

## Ingress integration

This Admin-only page reports the ingress mode and frontend endpoint. The generated External Shared Nginx snippet is hidden by default; expand it to review and copy it. MirrorRelay does not install, edit or reload the shared ingress. Complete any certificate placeholders and apply the snippet using that ingress deployment's normal change procedure.

## Cache

Cache shows usage, file count, warm-up status and repository hit/miss traffic first. Warm-up tasks, targeted invalidation, storage limits, global generation and **Global logical purge** are grouped into collapsed maintenance sections. The purge action invalidates every current cache namespace after confirmation.

The purge/reclaim table distinguishes immediate logical invalidation from delayed physical reclamation. A Pending or Running reclaim entry is expected until the observation window has elapsed and disk use has been rescanned.

## Health

Health shows MirrorRelay, Managed Upstream Nginx and the repository-health aggregate first. Expand **Component and endpoint details** for the frontend endpoint, External Shared Nginx, Go Router and upstream endpoint. Exact local network/socket coordinates are shown only to Admin; lower roles receive component state without those filesystem/listener details. An unknown repository usually means no successful check has completed; an unhealthy repository should be investigated with **Repositories → Test**, its upstream details and the logs.

## Access log, audit log and system

- **Access log** displays the latest Managed Upstream Nginx access records for Admin and Operator users and supports manual refresh. Query strings are not written to this log.
- **Audit log** records administrative users, client addresses, actions, objects/details and success or failure.
- **System** is available to Admin and Operator. It shows uptime, RSS and Managed Upstream Nginx state first, then groups build identity, Go runtime counters and Nginx lifecycle data into secondary disclosure sections. Only Admin receives ingress/TLS/listen/socket paths and other sensitive endpoint details. Exact Nginx binary and compile details live in the Nginx page's **Technical details** view.

Viewers can read the audit log but cannot access the Managed Upstream Nginx access log. Failed-entry diagnostic details are Admin-only because they can contain internal runtime context; lower roles still receive the actor, action, object, result and time. Use the audit log to establish who changed configuration; Admin/Operator users can use the query-free access log for data-plane requests; use the application and Nginx error logs on disk for startup, validation and upstream failures.

## Settings

The Admin-only **Settings** lifecycle can import and export all 22 top-level sections in `config.yaml`. Its structured form covers server and runtime endpoints, ingress mode, standalone HTTP/TLS, performance, metadata adapters, redirects, database, cache policies, security, admin CIDRs, transport connection pools, limits, logging, health scheduling, graceful shutdown, Managed Upstream Nginx lifecycle, distributed cluster routing, webhook notifications, and cache warm-up; the dedicated **Appearance** page manages UI enhancement. The bootstrap fields `database.path` and `distributed.mutation_token_key_files` are visible but disabled because they must remain in YAML/environment configuration.

### Configuration effective indicators

The Web UI indicates the effective mechanism for configuration changes:
- **Restart required** (`[Restart required]`): Process-level listeners, memory limits, timeouts, and daemon parameters take effect when MirrorRelay is restarted.
- **Reload required** (`[Reload required]`): Managed Upstream Nginx routing changes take effect upon graceful reload.
- **Immediate** (`[Immediate]`): Repositories, appearance settings, and instant cache purges take effect immediately.

When saving settings that require a restart, MirrorRelay displays:
> "Configuration saved. Restart MirrorRelay to take effect."

Selecting **Validate and save** validates the candidate configuration with startup-level checks before storing it in SQLite. You can select **Restart now** / **Restart MirrorRelay** directly in the Web UI or execute:

```sh
sudo systemctl restart mirrorrelay
```

### Configuration export

Administrators can export running configuration as valid YAML:
- **Standard export** (default): Omits sensitive credentials (`distributed.token`, `distributed.mutation_token`, the Webhook URL and signing secret, and edge node mutation tokens), passkey RP ID/origins, and local instance public base URLs (`http.public_base_url`, `distributed.node.public_base_url`).
- **Full backup export**: An explicit CSRF-protected action that includes credentials and passkey bindings for disaster recovery. Instance-local public base URLs remain omitted; protect the downloaded file as secret material.

### Configuration import

The **Import configuration** action supports uploading `.yaml` files or pasting up to 1 MiB of decoded YAML text:
1. **Startup-level validation**: Verifies YAML syntax, structure, constraints, and dependencies.
2. **Diff preview**: Displays a detailed diff table (`Path`, `Current value`, `Imported value`) and indicates whether a restart is required.
3. **Local instance and credential preservation**: If local host URLs or tokens are omitted in the imported YAML, running values are preserved. An Edge node mutation token is preserved only while that node's URL is unchanged; moving a node to another origin requires an explicit new token.
4. **Apply**: Atomically saves the validated candidate and its redacted history entry. Database/keyring bootstrap paths always remain file-only.
5. **Appearance publication**: The imported `ui_enhancement` state is part of that transaction and is published immediately; other imported operational fields still require the indicated restart.

### Configuration versioning and rollback

Every Settings save, import, operational reset, or rollback is recorded in the bounded configuration history:
- Records version ID, timestamp, operator, source (`web_ui`, `configuration_import`, `settings_rollback`, `settings_reset`), diff summary, and safe snapshot.
- Sensitive credentials and webhook targets are never stored in plaintext history snapshots, and the history API never returns the stored snapshot itself.
- Administrators can review retained changes and select **Rollback**. Redacted fields preserve the currently configured credentials instead of erasing or restoring an old secret.
- Rollback restores the appearance snapshot immediately and leaves restart-required operational values pending until restart.

### Distributed cluster and routing settings

The Settings page provides full management for distributed cluster topologies:
- **Cluster identity and security**: Role (`standalone`, `coordinator`, `edge`), join token, mutation token, and coordinator ID.
- **Node attributes**: Local node name, public base URL, region, and country.
- **Routing policies**: Mode (`hybrid`, `cidr`, `geo`, `priority`), with interactive table management for **Client network routing mappings** (CIDR to Region) and **Region country mappings** (Region code to Country codes).
- **Health checks**: Interval, timeout, and healthy/unhealthy failure thresholds.

The Webhook section configures one active notification destination with automatic provider format detection (DingTalk, Feishu/Lark, WeCom, Slack, or Generic JSON) and an integrated testing panel.

## Appearance and Branding

Light, Dark and Auto controls are always available on the login page and in the administration header. The browser saves this preference locally; Auto follows the operating-system `prefers-color-scheme` value and updates when it changes.

The **Appearance** page manages the instance default theme, branding identity, custom CSS, and public directory browser settings:

- **Theme Mode**: Set the instance default for browsers without a local preference: `System` (auto-detect OS preference), `Light`, or `Dark`.
- **Public UI Enhancement**: Control public repository directory restyling independently from the administration theme selector.
- **Accent Color**: Customize the primary theme accent color (default `#2563eb`).
- **Branding**: Set the instance title/name plus same-origin logo and favicon paths. Paths must begin with `/`, and the administrator-owned ingress must serve those assets.
- **Login Page**: Customize login page heading title and subtitle.
- **Directory Browser**: Enable modern responsive directory listing with breadcrumbs, instant filter, and SVG icons.
- **Custom CSS**: Enable a regular `.css` stylesheet file served from `/ui/custom.css`; symbolic links and files larger than 1 MiB are rejected.
- **Reset to Defaults**: Restore default styling and branding settings at any time.

## Users and My account

Administrators can create Admin, Operator and Viewer accounts with a 3–64 character non-space username and a password from 10 characters through 1024 bytes. The currently signed-in user cannot delete their own account. Use **My account** to change the current password, register/rename/remove WebAuthn passkeys, and generate eight single-use emergency recovery codes. Recovery codes are shown only when generated; save them offline before leaving the dialog. Password login can be disabled only after at least one passkey and one unused recovery code exist, and an atomic database condition prevents even concurrent requests from removing the final passkey. The recovery-code control remains available on the sign-in page when Passkey authentication is disabled or temporarily unavailable. Local recovery commands are `mirrorrelay admin reset-password --username <username> --password-stdin` and `mirrorrelay admin reset-passkeys --username <username>`; they load the configured Coordinator keyring when needed and revoke the account's existing sessions.

The interface follows the same role policy as the API and omits controls the current account cannot use. Admin manages users, repository credentials, custom Nginx code, cluster-node records and process-level settings. Operator manages non-secret repository fields, cache, validation, Nginx reload/rollback and cluster check/sync operations. Viewer receives a minimal read-only operational view and cannot open System. Use separate accounts so audit records identify the operator, and remove accounts that are no longer required.

Generated client configuration never disables TLS certificate verification. Add an organization CA to the client trust store when a private PKI is required instead of using insecure client flags.

## Troubleshooting

| Symptom | Check |
|---|---|
| Login briefly succeeds, then returns to the login form | Use the HTTPS ingress URL; confirm the browser accepts same-origin cookies and that UI/API are not split across origins |
| `401 authentication required` | Session cookie is absent, expired or was cleared; sign in again over HTTPS |
| `403 forbidden` before login | The effective client address is outside `security.admin_cidrs` |
| Repository remains Desired/Failed | Open Details for the validation error, then inspect generated config and Managed Upstream Nginx status |
| Upstream is unhealthy | Run Test; verify URL, DNS, CA bundle, expected status, private/HTTP authorization and redirect hosts |
| Directory page loads without icons or styles | Enable **Rewrite same-origin URLs in browsable HTML**, activate the repository, and apply the refreshed `/_mirrorrelay/upstream/` External Shared Nginx location |
| Purge completed but disk use did not immediately fall | Logical invalidation is immediate; wait for asynchronous physical reclaim and the next usage scan |
| A repository action does nothing after an upgrade | Hard-refresh the configured `admin.path` once so the browser loads the current embedded script; then inspect the visible error and Audit log |
| Copy buttons fail | The UI automatically uses its local copy fallback; if the browser still blocks it, copy from the displayed value manually |
| Saved Settings do not affect the process | Restart MirrorRelay and confirm that the page no longer reports pending saved values |

For host-level diagnosis, continue with [Configuration](configuration.md), [Installation](installation.md) and [Verification](verification.md).
