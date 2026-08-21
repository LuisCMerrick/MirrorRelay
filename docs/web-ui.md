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

The session cookie is `Secure`, `HttpOnly` and `SameSite=Strict`. Production sign-in therefore requires HTTPS and the UI and API must remain on the same origin. A plain-HTTP address on a non-loopback host can accept the login request but the browser will not retain the session cookie. Do not expose the private frontend socket or loopback fallback port directly to a network.

On the first start, MirrorRelay creates the initial administrator from `MIRRORRELAY_ADMIN_USERNAME` and `MIRRORRELAY_ADMIN_PASSWORD`. These values are used only when the database contains no users. Configure `security.admin_cidrs` when the administration surface must be restricted to specific networks.

The UI starts in English unless the browser's preferred languages include Chinese. Use `EN` or `中文` in the upper-right corner to choose manually; the selection is saved in browser local storage. Language resources are cleanly decoupled into dedicated locale files (`locales/en.js` and `locales/zh.js`). Sign out from the bottom of the sidebar when the session is no longer needed.

## Operating model

Repository and custom-configuration changes follow this activation path:

```text
Edit desired state -> generate candidate -> validate with nginx -t
                   -> publish atomically -> graceful reload -> active state
```

A failed validation or reload leaves the previous active routing configuration in place. The Repositories page can therefore show a failed Desired state while its last valid Active state continues serving traffic. Review the repository error, generated configuration and Managed Upstream Nginx status before retrying.

## Recommended first-use sequence

1. Open **System** and confirm the MirrorRelay and Managed Upstream Nginx versions, architecture and endpoints.
2. Open **Health** and confirm that MirrorRelay, the frontend endpoint, Go Router, Managed Upstream Nginx and the upstream endpoint are healthy or running.
3. Open **Ingress integration**, review the generated snippet and apply it through the existing ingress administrator's normal process.
4. Add one repository, test its upstreams and inspect its generated configuration.
5. Open the public domain root and confirm that the repository appears in the index.
6. Use the client example shown in repository details to make a small request before enabling production traffic.

## Dashboard

Dashboard summarizes:

- total and enabled repositories;
- healthy and unhealthy repositories;
- Managed Upstream Nginx state;
- active requests, request counts and traffic for today, 24 hours and 7 days;
- cache hit rate and observed cache use;
- per-repository traffic, status classes, cache hits/misses and upstream errors;
- MirrorRelay and Managed Upstream Nginx versions, build IDs, architecture and uptime.

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

**Rewrite same-origin URLs in browsable HTML** is a per-repository compatibility switch and is off by default. It resolves HTML `href`, `src`, `srcset`, `action` and related URL attributes against the actual upstream page URL. A target inside the configured upstream repository base maps back into the public repository namespace. A target outside that base but on the same upstream origin maps to `/_mirrorrelay/upstream/<repository-id>/...`, so upstream icons, stylesheets, scripts and images remain available without changing their content. Cross-origin URLs and non-HTTP(S) schemes are left unchanged.

The auxiliary route accepts only `GET` and `HEAD`, remains bound to the repository's public host/access policy, and uses the same pinned upstream, TLS verification, headers, cache policy and limits as that repository. Enabling it intentionally makes same-origin upstream paths outside the configured repository base reachable through the auxiliary scope; enable it only when that upstream surface is appropriate to expose. The generated External Shared Nginx snippet includes `/_mirrorrelay/upstream/` when an enabled path-mode repository needs it. Apply the refreshed snippet to the shared ingress. HTML bodies use the repository metadata rewrite limit, compression policy and generated representation validators.

Select **Validate, save and activate** to submit the candidate. Success means candidate generation, validation, persistence, atomic publication and graceful reload all completed. An error remains in the form and does not replace the active configuration.

### Repository actions

| Action | Result |
|---|---|
| Details | Shows Desired versus Active state, counters, upstream health and client examples |
| Copy URL | Copies the published repository URL; browser clipboard permission may be required |
| Test | Runs configured health checks against all enabled upstreams |
| Config | Previews the generated repository portion of the Nginx candidate |
| Purge | Logically invalidates the whole repository or one optional object path |
| Edit | Opens the full repository form |
| Enable / Disable | Validates and activates a new desired state with the selected enabled flag |
| Delete | Deletes the repository and logically invalidates its cache; this cannot be undone |

The Details dialog also provides the complete effective configuration, generated client commands and a profile-upgrade preview when a newer pinned profile exists. Review the field-by-field diff and generated configuration before selecting **Apply upgrade**.

All row and dialog actions are same-origin API operations. They display a success or error message after completion, and the active button is temporarily disabled while its request is running. **Copy URL** uses the browser Clipboard API when available and falls back to a local text selection for browsers that deny Clipboard API access.

Cache purge is generation-based. New requests stop using the invalidated namespace immediately, while old physical files remain until the asynchronous Nginx cache manager reclaims them according to `inactive` and `max_size`.

## Profiles

Profiles are read-only, versioned starting points for supported repository ecosystems. The page shows the profile name, version, repository type, default upstream, publication/proxy modes and whether caching or metadata rewriting is enabled. Selecting a profile never makes future profile releases apply automatically.

## Managed Upstream Nginx

This page shows process state, PID, uptime, binary version, build ID, architecture, current configuration version, last reload/exit details, build options and the effective configuration.

- **Regenerate, validate and reload** reconciles the current desired repositories and custom fragments.
- **Rollback** restores both repositories and custom configuration from the selected immutable history version, validates it and performs a graceful reload.

Do not treat rollback as a text-only Nginx rollback: it changes the persisted repository and custom-configuration state to the selected snapshot.

## Custom configuration

Custom fragments target a controlled `http`, `server`, `location`, `upstream` or repository context. Use repository ID `0` for a global fragment. Saving or deleting a fragment generates and validates the complete candidate before activation.

MirrorRelay rejects directives that could escape the selected context, create listeners/routes/upstream targets, weaken TLS validation, alter cache identity or bypass, access the filesystem or environment, or use reserved variables and internal headers. Custom fragments are an advanced escape hatch; prefer repository fields whenever they express the requirement.

## Ingress integration

This page reports the ingress mode and frontend network/address, then displays the generated External Shared Nginx snippet. MirrorRelay does not install, edit or reload the shared ingress. Review the file, complete any certificate placeholders and apply it using that ingress deployment's normal change procedure.

## Cache

Cache shows file count, scanned bytes, maximum size, global generation, configured path/limits and repository hit/miss traffic. **Global logical purge** invalidates every current cache namespace after confirmation.

The purge/reclaim table distinguishes immediate logical invalidation from delayed physical reclamation. A Pending or Running reclaim entry is expected until the observation window has elapsed and disk use has been rescanned.

## Health

Health separates MirrorRelay, the frontend endpoint, External Shared Nginx, Go Router, Managed Upstream Nginx, the upstream endpoint and each repository. An unknown repository usually means no successful check has completed; an unhealthy repository should be investigated with **Repositories → Test**, its upstream details and the logs.

## Access log, audit log and system

- **Access log** displays the latest Managed Upstream Nginx access records and supports manual refresh.
- **Audit log** records administrative users, client addresses, actions, objects/details and success or failure.
- **System** reports MirrorRelay build/runtime information, memory and file-descriptor counters, ingress/TLS endpoints and the exact Managed Upstream Nginx checksum and lifecycle status.

Use the audit log to establish who changed configuration; use the access log for data-plane requests; use the application and Nginx error logs on disk for startup, validation and upstream failures.

## Settings

The **Settings** page manages most operational values that also exist in `config.yaml`: local Unix/TCP endpoints, ingress mode, HTTP/TLS behavior, performance, metadata, redirects, cache defaults, security and administrator CIDRs, transport pools and timeouts, concurrency and bandwidth limits, log rotation, health scheduling, shutdown and Managed Upstream Nginx lifecycle settings.

Selecting **Validate and save** validates the complete merged configuration before storing the override in SQLite. The saved Web UI values take precedence over matching YAML values on the next MirrorRelay start. They do not hot-reload the running process. You can select **Restart now** / **Restart MirrorRelay** directly in the Web UI or execute the displayed CLI command:

```sh
sudo systemctl restart mirrorrelay
```

After restart, the page automatically reconnects and reports that the running process matches the saved values. A **Restart service** button is also available directly on the **System** page. **Reset to YAML after restart** removes the Web UI override; restart again to make the YAML values active.

Bootstrap, credential, filesystem and executable locations remain file-only so the running service cannot relocate its own trust boundary or database. The page displays the exact protected list, including socket paths/modes, runtime paths, ingress snippet path, TLS key/certificate paths, database/cache/log paths, initial administrator settings and Managed Upstream Nginx binary, prefix, PID, log, socket and CA-bundle paths.

Repository Desired/Active changes are separate from this page and continue to validate and activate immediately.

The Webhook section configures one active notification destination. MirrorRelay detects DingTalk, Feishu/Lark, WeCom and Slack formats from the URL and uses generic JSON for other hosts. The test panel separates the running destination from one-time platform targets; selecting a one-time target does not save or add another channel.

## Appearance and Branding

Light, Dark and Auto controls are always available on the login page and in the administration header. The browser saves this preference locally; Auto follows the operating-system `prefers-color-scheme` value and updates when it changes.

The **Appearance** page manages the instance default theme, branding identity, custom CSS, and public directory browser settings:

- **Theme Mode**: Set the instance default for browsers without a local preference: `System` (auto-detect OS preference), `Light`, or `Dark`.
- **Public UI Enhancement**: Control public repository directory restyling independently from the administration theme selector.
- **Accent Color**: Customize the primary theme accent color (default `#2563eb`).
- **Branding**: Set the instance title/name, custom logo URL, and favicon URL.
- **Login Page**: Customize login page heading title and subtitle.
- **Directory Browser**: Enable modern responsive directory listing with breadcrumbs, instant filter, and SVG icons.
- **Custom CSS**: Enable custom stylesheet file injection (served securely from `/ui/custom.css`).
- **Reset to Defaults**: Restore default styling and branding settings at any time.

## Users and My account

Users can create additional administrators with a 3–64 character non-space username and a password of at least 10 characters. The currently signed-in administrator cannot delete their own account. Use **My account** to change the current password by supplying the existing password and a new password.

Administrator accounts have the same UI privileges in this release. Use separate accounts so audit records identify the operator, and remove accounts that are no longer required.

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
