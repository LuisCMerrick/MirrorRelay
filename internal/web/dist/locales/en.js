'use strict';

window.MIRRORRELAY_LOCALES = window.MIRRORRELAY_LOCALES || {};
window.MIRRORRELAY_LOCALES.en = {
  lang: 'en',
  locale: 'en-US',
  dictionary: {
    "tagline": "Linux repository reverse-proxy gateway",
    "username": "Username",
    "password": "Password",
    "signIn": "Sign in",
    "signOut": "Sign out",
    "restart": "Restart",
    "restartService": "Restart service",
    "navDashboard": "Dashboard",
    "navRepositories": "Repositories",
    "navProfiles": "Profiles",
    "navUpstreamNginx": "Managed Upstream Nginx",
    "navCustom": "Custom configuration",
    "navIngress": "Ingress integration",
    "navCluster": "Cluster",
    "navCache": "Cache",
    "navHealth": "Health",
    "navAccess": "Access log",
    "navAudit": "Audit log",
    "navSystem": "System",
    "navSettings": "Settings",
    "navUsers": "Users",
    "navAccount": "My account",
    "addRepository": "Add repository",
    "addCustom": "Add custom configuration",
    "addNode": "Add node",
    "resetFingerprint": "Reset fingerprint",
    "identityRouting": "Identity and routing",
    "profileVersion": "Profile / version",
    "name": "Name",
    "repositoryType": "Repository type",
    "publicMode": "Public mode",
    "publicHost": "Public host",
    "publicPath": "Public path",
    "accessPolicy": "Access policy",
    "description": "Description",
    "nodeURL": "Public base URL",
    "region": "Region",
    "country": "Country",
    "priority": "Priority",
    "weight": "Weight",
    "save": "Save",
    "upstreamsPaths": "Upstreams and path mapping",
    "upstreamList": "Upstreams (one “priority URL” per line)",
    "stripPrefix": "Strip prefix",
    "addPrefix": "Add prefix",
    "hostRewrite": "Host rewrite",
    "proxyMode": "Proxy mode",
    "redirectPolicy": "Redirect policy",
    "headersTimeouts": "Headers and timeouts",
    "headerAdd": "Add request headers (one “Name: Value” per line)",
    "headerRemove": "Remove request headers (comma or newline separated)",
    "connectTimeout": "Connect timeout (seconds)",
    "readTimeout": "Read timeout (seconds)",
    "sendTimeout": "Send timeout (seconds)",
    "cacheRewrite": "Cache and rewrite",
    "cacheProfile": "Cache profile",
    "rewriteProfile": "Rewrite profile",
    "rewriteHosts": "Allowed rewrite/redirect hosts (comma or newline separated)",
    "metadataLimit": "Metadata rewrite limit (bytes, 0 = global)",
    "metadataTTL": "Metadata TTL (seconds, 0 = global)",
    "packageTTL": "Package TTL (seconds, 0 = global)",
    "immutableTTL": "Immutable TTL (seconds, 0 = default)",
    "blobTTL": "Blob TTL (seconds, 0 = default)",
    "cacheEnabled": "Enable disk cache",
    "rewriteEnabled": "Enable metadata rewrite",
    "htmlRewriteEnabled": "Rewrite same-origin URLs in browsable HTML",
    "cacheAuthenticated": "Cache authenticated responses (public content only)",
    "healthLimits": "Health and limits",
    "healthPath": "Health-check path",
    "expectedStatus": "Expected status",
    "healthInterval": "Check interval (seconds)",
    "healthTimeout": "Check timeout (seconds)",
    "healthMethod": "Check method",
    "rateProfile": "Rate-limit profile",
    "maxConcurrency": "Repository max concurrency (0 = profile)",
    "bandwidthLimit": "Per-connection limit B/s (0 = unlimited)",
    "healthEnabled": "Enable health checks",
    "repositoryEnabled": "Enable repository",
    "registrySecurity": "Registry and upstream security",
    "registryAuth": "Registry auth",
    "blobRedirect": "Blob redirect",
    "tokenUpstream": "Token upstream (optional)",
    "pullOnly": "Pull only",
    "allowHTTP": "Allow HTTP upstream (requires system policy)",
    "allowPrivate": "Allow private upstream (requires system policy)",
    "cancel": "Cancel",
    "saveActivate": "Validate, save and activate",
    "enabled": "Enabled",
    "configuration": "Configuration",
    "validateApply": "Validate and apply",
    "slug": "Slug",
    "customOption": "Custom",
    "pathMode": "Path mode",
    "hostMode": "Host mode",
    "publicAccess": "Public",
    "adminAccess": "Admin CIDR only",
    "transparentMode": "Transparent",
    "rewriteMode": "Rewrite adapter",
    "passClient": "Pass to client",
    "followBroker": "Follow through broker",
    "rewriteLocal": "Rewrite to local URL",
    "fullProxy": "Full proxy",
    "standard": "Standard",
    "packages": "Packages",
    "unlimitedDefault": "Unlimited/default",
    "conservative": "Conservative (16)",
    "balanced": "Balanced (64)",
    "bulk": "Bulk (256)",
    "directAuth": "Direct auth",
    "fullProxyAuth": "Full-proxy auth",
    "passRedirect": "Pass redirect",
    "context": "Context",
    "repositoryID": "Repository ID (0 = global)"
},
  pageMeta: {
    "dashboard": [
        "Dashboard",
        "Live service status"
    ],
    "mirrors": [
        "Repositories",
        "Profiles, upstreams and active data-plane configuration"
    ],
    "profiles": [
        "Profiles",
        "Versioned, overridable repository defaults"
    ],
    "upstream-nginx": [
        "Managed Upstream Nginx",
        "Status, effective configuration, history and rollback"
    ],
    "custom": [
        "Custom configuration",
        "Controlled advanced Managed Upstream Nginx directives"
    ],
    "ingress": [
        "Ingress integration",
        "External Shared Nginx connection details"
    ],
    "cluster": [
        "Cluster",
        "Distributed edge nodes, routing and configuration consistency"
    ],
    "cache": [
        "Cache",
        "Generation invalidation and asynchronous physical reclaim"
    ],
    "health": [
        "Health",
        "MirrorRelay, local transports, Managed Upstream Nginx and repositories"
    ],
    "access": [
        "Access log",
        "Latest 200 Managed Upstream Nginx access records"
    ],
    "audit": [
        "Audit log",
        "Administrative actions and client addresses"
    ],
    "system": [
        "System",
        "Runtime, memory and build information"
    ],
    "settings": [
        "Settings",
        "Validated operational configuration saved through the Web UI"
    ],
    "users": [
        "Users",
        "Administrator account management"
    ],
    "account": [
        "My account",
        "Change the current password"
    ]
},
  stateLabels: {
    "active": "Active",
    "pending": "Pending",
    "failed": "Failed",
    "healthy": "Healthy",
    "unhealthy": "Unhealthy",
    "unknown": "Unknown",
    "running": "Running",
    "completed": "Completed",
    "disabled": "Disabled",
    "restarting": "Restarting"
},
  settingsGroups: [
    {
        "title": "Local endpoints and ingress",
        "fields": [
            {
                "path": "server.unix_socket_enabled",
                "label": "Frontend Unix socket",
                "type": "boolean"
            },
            {
                "path": "server.local_port",
                "label": "Frontend loopback port",
                "type": "number",
                "min": 1,
                "max": 65535
            },
            {
                "path": "ingress.mode",
                "label": "Ingress mode",
                "type": "select",
                "options": [
                    [
                        "external",
                        "External Shared Nginx"
                    ],
                    [
                        "managed-standalone",
                        "Managed standalone"
                    ]
                ]
            },
            {
                "path": "ingress.generate_snippet",
                "label": "Generate ingress snippet",
                "type": "boolean"
            },
            {
                "path": "http.listen",
                "label": "Standalone HTTP listen",
                "type": "text"
            },
            {
                "path": "http.https_listen",
                "label": "Standalone HTTPS listen",
                "type": "text"
            },
            {
                "path": "http.public_base_url",
                "label": "Public base URL",
                "type": "text",
                "placeholder": "https://mirror.example.com"
            },
            {
                "path": "tls.min_version",
                "label": "Minimum TLS version",
                "type": "select",
                "options": [
                    [
                        "1.2",
                        "TLS 1.2"
                    ],
                    [
                        "1.3",
                        "TLS 1.3"
                    ]
                ]
            },
            {
                "path": "http.read_timeout",
                "label": "HTTP read timeout",
                "type": "text"
            },
            {
                "path": "http.write_timeout",
                "label": "HTTP write timeout",
                "type": "text"
            },
            {
                "path": "http.idle_timeout",
                "label": "HTTP idle timeout",
                "type": "text"
            }
        ]
    },
    {
        "title": "Performance, metadata and redirects",
        "fields": [
            {
                "path": "performance.stream_buffer_size_bytes",
                "label": "Streaming buffer bytes",
                "type": "select",
                "valueType": "number",
                "options": [
                    [
                        32768,
                        "32 KiB"
                    ],
                    [
                        65536,
                        "64 KiB"
                    ],
                    [
                        131072,
                        "128 KiB"
                    ]
                ]
            },
            {
                "path": "performance.go_memory_limit_bytes",
                "label": "Go memory limit bytes (0 = environment/default)",
                "type": "number",
                "min": 0
            },
            {
                "path": "performance.gogc",
                "label": "GOGC (-1..10000)",
                "type": "number",
                "min": -1,
                "max": 10000
            },
            {
                "path": "metadata.rewrite_buffer_limit_bytes",
                "label": "Metadata rewrite limit bytes",
                "type": "number",
                "min": 1
            },
            {
                "path": "metadata.output_compression",
                "label": "Metadata output compression",
                "type": "select",
                "options": [
                    [
                        "auto",
                        "Automatic"
                    ],
                    [
                        "identity",
                        "Identity"
                    ],
                    [
                        "gzip",
                        "Gzip"
                    ]
                ]
            },
            {
                "path": "metadata.gzip_min_length_bytes",
                "label": "Gzip minimum bytes",
                "type": "number",
                "min": 0
            },
            {
                "path": "metadata.validator_entries",
                "label": "Validator entries",
                "type": "number",
                "min": 1
            },
            {
                "path": "redirect.max_hops",
                "label": "Maximum redirect hops",
                "type": "number",
                "min": 1,
                "max": 20
            },
            {
                "path": "redirect.reject_mixed_dns_result",
                "label": "Reject mixed permitted/forbidden DNS results",
                "type": "boolean"
            }
        ]
    },
    {
        "title": "Cache defaults",
        "fields": [
            {
                "path": "cache.max_size_bytes",
                "label": "Maximum cache bytes",
                "type": "number",
                "min": 1
            },
            {
                "path": "cache.max_files",
                "label": "Maximum observed files",
                "type": "number",
                "min": 1
            },
            {
                "path": "cache.minimum_free_bytes",
                "label": "Minimum free bytes",
                "type": "number",
                "min": 0
            },
            {
                "path": "cache.inactive",
                "label": "Inactive window",
                "type": "text"
            },
            {
                "path": "cache.metadata_ttl",
                "label": "Metadata TTL",
                "type": "text"
            },
            {
                "path": "cache.package_ttl",
                "label": "Package TTL",
                "type": "text"
            },
            {
                "path": "cache.cleanup_interval",
                "label": "Cleanup observation interval",
                "type": "text"
            },
            {
                "path": "cache.wait_for_fill",
                "label": "Cache fill wait window",
                "type": "text"
            }
        ]
    },
    {
        "title": "Security and administration",
        "fields": [
            {
                "path": "security.allow_http_upstream",
                "label": "Allow HTTP upstream globally",
                "type": "boolean"
            },
            {
                "path": "security.allow_private_upstream",
                "label": "Allow private upstream globally",
                "type": "boolean"
            },
            {
                "path": "security.expose_client_ip",
                "label": "Expose validated client IP internally",
                "type": "boolean"
            },
            {
                "path": "security.session_timeout",
                "label": "Session timeout",
                "type": "text"
            },
            {
                "path": "security.login_window",
                "label": "Login throttle window",
                "type": "text"
            },
            {
                "path": "security.login_max_failures",
                "label": "Maximum login failures",
                "type": "number",
                "min": 1
            },
            {
                "path": "security.admin_cidrs",
                "label": "Admin CIDRs (one per line)",
                "type": "list"
            }
        ]
    },
    {
        "title": "Transport and limits",
        "fields": [
            {
                "path": "transport.dial_timeout",
                "label": "Dial timeout",
                "type": "text"
            },
            {
                "path": "transport.keep_alive",
                "label": "TCP keepalive",
                "type": "text"
            },
            {
                "path": "transport.tls_handshake_timeout",
                "label": "TLS handshake timeout",
                "type": "text"
            },
            {
                "path": "transport.response_header_timeout",
                "label": "Response header timeout",
                "type": "text"
            },
            {
                "path": "transport.idle_connection_timeout",
                "label": "Idle connection timeout",
                "type": "text"
            },
            {
                "path": "transport.max_idle_connections",
                "label": "Maximum idle connections",
                "type": "number",
                "min": 1
            },
            {
                "path": "transport.max_idle_connections_per_host",
                "label": "Maximum idle connections per host",
                "type": "number",
                "min": 1
            },
            {
                "path": "limits.max_total_concurrency",
                "label": "Global concurrency (0 = unlimited)",
                "type": "number",
                "min": 0
            },
            {
                "path": "limits.max_ip_concurrency",
                "label": "Per-IP concurrency (0 = unlimited)",
                "type": "number",
                "min": 0
            },
            {
                "path": "limits.bandwidth_limit_bps",
                "label": "Global bandwidth B/s (0 = unlimited)",
                "type": "number",
                "min": 0
            }
        ]
    },
    {
        "title": "Logging and lifecycle",
        "fields": [
            {
                "path": "logging.queue_size",
                "label": "Log queue size",
                "type": "number",
                "min": 1
            },
            {
                "path": "logging.max_size_mb",
                "label": "Log file maximum MiB",
                "type": "number",
                "min": 1
            },
            {
                "path": "logging.keep_days",
                "label": "Log retention days",
                "type": "number",
                "min": 1
            },
            {
                "path": "health.worker_interval",
                "label": "Health worker interval",
                "type": "text"
            },
            {
                "path": "shutdown.grace_period",
                "label": "Graceful shutdown window",
                "type": "text"
            }
        ]
    },
    {
        "title": "Managed Upstream Nginx",
        "fields": [
            {
                "path": "upstream_nginx.mode",
                "label": "Mode",
                "type": "select",
                "options": [
                    [
                        "managed",
                        "Managed"
                    ],
                    [
                        "external",
                        "External advanced mode"
                    ],
                    [
                        "disabled",
                        "Disabled"
                    ]
                ]
            },
            {
                "path": "upstream_nginx.upstream_unix_socket_enabled",
                "label": "Use upstream Unix socket",
                "type": "boolean"
            },
            {
                "path": "upstream_nginx.upstream_local_port",
                "label": "Upstream loopback port",
                "type": "number",
                "min": 1,
                "max": 65535
            },
            {
                "path": "upstream_nginx.tls_verify_depth",
                "label": "TLS verification depth",
                "type": "number",
                "min": 1,
                "max": 20
            },
            {
                "path": "upstream_nginx.resolver",
                "label": "DNS resolvers (space separated)",
                "type": "text"
            },
            {
                "path": "upstream_nginx.resolver_refresh",
                "label": "Resolver refresh",
                "type": "text"
            },
            {
                "path": "upstream_nginx.history_limit",
                "label": "Configuration history limit",
                "type": "number",
                "min": 1
            },
            {
                "path": "upstream_nginx.restart_max_failures",
                "label": "Restart maximum failures",
                "type": "number",
                "min": 1
            },
            {
                "path": "upstream_nginx.restart_window",
                "label": "Restart failure window",
                "type": "text"
            },
            {
                "path": "upstream_nginx.restart_initial_backoff",
                "label": "Initial restart backoff",
                "type": "text"
            },
            {
                "path": "upstream_nginx.restart_max_backoff",
                "label": "Maximum restart backoff",
                "type": "text"
            },
            {
                "path": "upstream_nginx.worker_processes",
                "label": "Worker processes",
                "type": "text"
            },
            {
                "path": "upstream_nginx.worker_user",
                "label": "Worker user (empty is allowed)",
                "type": "text"
            },
            {
                "path": "upstream_nginx.worker_connections",
                "label": "Worker connections",
                "type": "number",
                "min": 1
            },
            {
                "path": "upstream_nginx.stop_on_mirrorrelay_exit",
                "label": "Stop Nginx when MirrorRelay exits",
                "type": "boolean"
            }
        ]
    }
],
  duration: function(days, hours, minutes) {
    return days + 'd ' + hours + 'h ' + minutes + 'm';
  },
  exitSummary: function(dateStr, code, reason) {
    var codeStr = code === -1 ? 'exit code unknown' : 'exit code ' + (code !== null && code !== undefined ? code : '—');
    return dateStr + ' · ' + codeStr + ' · ' + (reason || '');
  },
  strings: {
    "exit code unknown": "exit code unknown",
    "exit code ${status.last_exit_code ?? '—'}": "exit code ${status.last_exit_code ?? '—'}",
    "Managed Upstream Nginx": "Managed Upstream Nginx",
    "Repositories / enabled": "Repositories / enabled",
    "Healthy / unhealthy": "Healthy / unhealthy",
    "Active requests": "Active requests",
    "Requests today": "Requests today",
    "Traffic today": "Traffic today",
    "Traffic / 24 h": "Traffic / 24 h",
    "Traffic / 7 d": "Traffic / 7 d",
    "Cache hit rate": "Cache hit rate",
    "Cache usage": "Cache usage",
    "files": "files",
    "MirrorRelay and Managed Upstream Nginx": "MirrorRelay and Managed Upstream Nginx",
    "Managed Upstream Nginx PID": "Managed Upstream Nginx PID",
    "Managed Upstream Nginx version": "Managed Upstream Nginx version",
    "Managed Upstream Nginx build ID": "Managed Upstream Nginx build ID",
    "Managed Upstream Nginx architecture": "Managed Upstream Nginx architecture",
    "Managed Upstream Nginx uptime": "Managed Upstream Nginx uptime",
    "MirrorRelay version": "MirrorRelay version",
    "MirrorRelay build ID": "MirrorRelay build ID",
    "MirrorRelay architecture": "MirrorRelay architecture",
    "Active config": "Active config",
    "MirrorRelay uptime": "MirrorRelay uptime",
    "Repository statistics today": "Repository statistics today",
    "Repository": "Repository",
    "Health": "Health",
    "Requests": "Requests",
    "Traffic": "Traffic",
    "Latency": "Latency",
    "Cache HIT / MISS": "Cache HIT / MISS",
    "Upstream errors": "Upstream errors",
    "No repositories yet.": "No repositories yet.",
    "Custom": "Custom",
    "latest": "latest",
    "Profiles are versioned defaults. Every field remains editable after applying a profile, and existing repositories stay pinned until an explicit upgrade.": "Profiles are versioned defaults. Every field remains editable after applying a profile, and existing repositories stay pinned until an explicit upgrade.",
    "Profile": "Profile",
    "Version": "Version",
    "Type": "Type",
    "Upstream": "Upstream",
    "Mode": "Mode",
    "Cache / rewrite": "Cache / rewrite",
    "Latest stable": "Latest stable",
    "Cache": "Cache",
    "Rewrite": "Rewrite",
    "Details": "Details",
    "Copy URL": "Copy URL",
    "Test": "Test",
    "Config": "Config",
    "Purge": "Purge",
    "Edit": "Edit",
    "Disable": "Disable",
    "Enable": "Enable",
    "Delete": "Delete",
    "Name": "Name",
    "Type / profile": "Type / profile",
    "Public URL": "Public URL",
    "Active upstream": "Active upstream",
    "Health / latency": "Health / latency",
    "Desired state": "Desired state",
    "Actions": "Actions",
    "Edit repository": "Edit repository",
    "Add repository": "Add repository",
    "Invalid header line: ${line}": "Invalid header line: ${line}",
    "Invalid upstream line: ${line}": "Invalid upstream line: ${line}",
    "Candidate validated and activated with a graceful reload.": "Candidate validated and activated with a graceful reload.",
    "Repository URL copied.": "Repository URL copied.",
    "Clipboard access is unavailable.": "Clipboard access is unavailable.",
    "Checking upstreams…": "Checking upstreams…",
    "All upstreams are healthy.": "All upstreams are healthy.",
    "One or more upstreams are unhealthy.": "One or more upstreams are unhealthy.",
    "Repository enabled.": "Repository enabled.",
    "Repository disabled.": "Repository disabled.",
    "Delete this repository and logically invalidate its cache? This cannot be undone.": "Delete this repository and logically invalidate its cache? This cannot be undone.",
    "Repository deleted.": "Repository deleted.",
    "Preview upgrade to ${latest.version}": "Preview upgrade to ${latest.version}",
    "Active state": "Active state",
    "Published": "Published",
    "Not active": "Not active",
    "Effective config": "Effective config",
    "Observed cache traffic": "Observed cache traffic",
    "Preview config": "Preview config",
    "Purge cache": "Purge cache",
    "Desired configuration": "Desired configuration",
    "Active routing snapshot": "Active routing snapshot",
    "No active version. The desired configuration may have failed validation or activation.": "No active version. The desired configuration may have failed validation or activation.",
    "Upstreams": "Upstreams",
    "Priority": "Priority",
    "Last check": "Last check",
    "Client configuration examples": "Client configuration examples",
    "Copy": "Copy",
    "Copied.": "Copied.",
    "Type / mode": "Type / mode",
    "authenticated enabled": "authenticated enabled",
    "anonymous only": "anonymous only",
    "Disabled": "Disabled",
    "Browsable HTML URL rewrite": "Browsable HTML URL rewrite",
    "Enabled": "Enabled",
    "Rewrite hosts": "Rewrite hosts",
    "Generated repository configuration": "Generated repository configuration",
    "Effective Managed Upstream Nginx configuration": "Effective Managed Upstream Nginx configuration",
    "Profile upgrade preview": "Profile upgrade preview",
    "Field": "Field",
    "Before": "Before",
    "After": "After",
    "Apply upgrade": "Apply upgrade",
    "Profile upgrade activated.": "Profile upgrade activated.",
    "Optional object path. Leave empty to purge the whole repository cache.": "Optional object path. Leave empty to purge the whole repository cache.",
    "Logical purge completed; physical reclaim: ${result.physical_reclaim}.": "Logical purge completed; physical reclaim: ${result.physical_reclaim}.",
    "State": "State",
    "Uptime": "Uptime",
    "Config version": "Config version",
    "Build ID": "Build ID",
    "Architecture": "Architecture",
    "Integration snippet": "Integration snippet",
    "Regenerate, validate and reload": "Regenerate, validate and reload",
    "Configuration history": "Configuration history",
    "Time": "Time",
    "Operator": "Operator",
    "Description": "Description",
    "Active": "Active",
    "History": "History",
    "Rollback": "Rollback",
    "Runtime and build": "Runtime and build",
    "Last reload": "Last reload",
    "Reload result": "Reload result",
    "Last exit": "Last exit",
    "Build options unavailable.": "Build options unavailable.",
    "Effective configuration": "Effective configuration",
    "Validation passed and Managed Upstream Nginx reloaded.": "Validation passed and Managed Upstream Nginx reloaded.",
    "Rollback repositories and custom configuration to v${version}?": "Rollback repositories and custom configuration to v${version}?",
    "Rolled back through a validated graceful reload.": "Rolled back through a validated graceful reload.",
    "These directives apply only to Managed Upstream Nginx. Dangerous process, filesystem and context-escape directives are rejected.": "These directives apply only to Managed Upstream Nginx. Dangerous process, filesystem and context-escape directives are rejected.",
    "Context": "Context",
    "Last validation": "Last validation",
    "Global": "Global",
    "Edit custom Managed Upstream Nginx configuration": "Edit custom Managed Upstream Nginx configuration",
    "Add custom Managed Upstream Nginx configuration": "Add custom Managed Upstream Nginx configuration",
    "Delete this custom configuration and reload Managed Upstream Nginx?": "Delete this custom configuration and reload Managed Upstream Nginx?",
    "Custom configuration deleted.": "Custom configuration deleted.",
    "Custom configuration validated and activated.": "Custom configuration validated and activated.",
    "Ingress mode": "Ingress mode",
    "Frontend network": "Frontend network",
    "Frontend address": "Frontend address",
    "External Shared Nginx integration snippet": "External Shared Nginx integration snippet",
    "The generated file is a scoped deployment aid. MirrorRelay does not own or reload the External Shared Nginx process.": "The generated file is a scoped deployment aid. MirrorRelay does not own or reload the External Shared Nginx process.",
    "Cache files": "Cache files",
    "Used space": "Used space",
    "Maximum space": "Maximum space",
    "Global generation": "Global generation",
    "Cache storage": "Cache storage",
    "Path": "Path",
    "Maximum files": "Maximum files",
    "Minimum free space": "Minimum free space",
    "Inactive window": "Inactive window",
    "Global logical purge": "Global logical purge",
    "Logical invalidation is immediate. Physical files remain until the asynchronous Nginx cache manager completes its inactive/max_size cleanup window.": "Logical invalidation is immediate. Physical files remain until the asynchronous Nginx cache manager completes its inactive/max_size cleanup window.",
    "Repository cache traffic today": "Repository cache traffic today",
    "Nginx cache files are content-keyed; this table reports observed cache-served traffic, not guessed physical ownership.": "Nginx cache files are content-keyed; this table reports observed cache-served traffic, not guessed physical ownership.",
    "Cache-served bytes": "Cache-served bytes",
    "Purge / reclaim jobs": "Purge / reclaim jobs",
    "Scope": "Scope",
    "Logical purge": "Logical purge",
    "Physical reclaim": "Physical reclaim",
    "Reclaimed": "Reclaimed",
    "Completed": "Completed",
    "Invalidate every existing cache namespace?": "Invalidate every existing cache namespace?",
    "Logical purge completed; physical reclaim is ${result.physical_reclaim}.": "Logical purge completed; physical reclaim is ${result.physical_reclaim}.",
    "Frontend endpoint": "Frontend endpoint",
    "External Shared Nginx": "External Shared Nginx",
    "Upstream endpoint": "Upstream endpoint",
    "Repositories": "Repositories",
    "Refresh": "Refresh",
    "No access records.": "No access records.",
    "User": "User",
    "Client": "Client",
    "Action": "Action",
    "Object / detail": "Object / detail",
    "Result": "Result",
    "Success": "Success",
    "Failed": "Failed",
    "Restart MirrorRelay service now? The application will reconnect automatically once ready.": "Restart MirrorRelay service now? The application will reconnect automatically once ready.",
    "Requesting service restart...": "Requesting service restart...",
    "MirrorRelay is restarting, reconnecting...": "MirrorRelay is restarting, reconnecting...",
    "Restarting...": "Restarting...",
    "MirrorRelay restarted successfully.": "MirrorRelay restarted successfully.",
    "Restart timed out. Please check server status.": "Restart timed out. Please check server status.",
    "Restart service": "Restart service",
    "Program version": "Program version",
    "Go version": "Go version",
    "Public base URL": "Public base URL",
    "Not configured": "Not configured",
    "Runtime resources": "Runtime resources",
    "Go heap allocated": "Go heap allocated",
    "Go heap in use": "Go heap in use",
    "Go heap objects": "Go heap objects",
    "Total allocations": "Total allocations",
    "Goroutines": "Goroutines",
    "Open file descriptors": "Open file descriptors",
    "GC cycles": "GC cycles",
    "GC pause total": "GC pause total",
    "GC CPU fraction": "GC CPU fraction",
    "HTTPS listen": "HTTPS listen",
    "Minimum TLS": "Minimum TLS",
    "Certificate": "Certificate",
    "Private key": "Private key",
    "Repository changes become active only after candidate generation, nginx -t, atomic publication and graceful reload.": "Repository changes become active only after candidate generation, nginx -t, atomic publication and graceful reload.",
    "field.label[0]": "field.label[0]",
    "option[1]": "option[1]",
    "Saved values differ from the running process. Restart MirrorRelay to apply them.": "Saved values differ from the running process. Restart MirrorRelay to apply them.",
    "Restart now": "Restart now",
    "The running process matches the saved settings.": "The running process matches the saved settings.",
    "group.title[0]": "group.title[0]",
    "These operational settings are stored in SQLite, strictly validated, and override the matching YAML values after restart. Repository changes continue to use the immediate Desired/Active validation workflow.": "These operational settings are stored in SQLite, strictly validated, and override the matching YAML values after restart. Repository changes continue to use the immediate Desired/Active validation workflow.",
    "Source": "Source",
    "Web UI override": "Web UI override",
    "Configuration file": "Configuration file",
    "File-only bootstrap settings:": "File-only bootstrap settings:",
    "Reset to YAML after restart": "Reset to YAML after restart",
    "Restart MirrorRelay": "Restart MirrorRelay",
    "Validate and save": "Validate and save",
    "Settings saved; restart MirrorRelay to apply them.": "Settings saved; restart MirrorRelay to apply them.",
    "Settings already match the running process.": "Settings already match the running process.",
    "Discard the Web UI override and restore YAML values after restart?": "Discard the Web UI override and restore YAML values after restart?",
    "Web UI override removed; restart MirrorRelay.": "Web UI override removed; restart MirrorRelay.",
    "Cluster role": "Cluster role",
    "Cluster status": "Cluster status",
    "Total nodes": "Total nodes",
    "Healthy nodes": "Healthy nodes",
    "Routable nodes": "Routable nodes",
    "Routing mode": "Routing mode",
    "Cluster Fingerprint": "Cluster Fingerprint",
    "Not initialized": "Not initialized",
    "Check": "Check",
    "Edge nodes": "Edge nodes",
    "URL": "URL",
    "Region": "Region",
    "Priority / Weight": "Priority / Weight",
    "Fingerprint": "Fingerprint",
    "No edge nodes registered yet.": "No edge nodes registered yet.",
    "Add edge node": "Add edge node",
    "Node updated.": "Node updated.",
    "Node added.": "Node added.",
    "Reset the cluster configuration fingerprint? It will reinitialize from active nodes.": "Reset the cluster configuration fingerprint? It will reinitialize from active nodes.",
    "Cluster fingerprint reset.": "Cluster fingerprint reset.",
    "Node probe completed.": "Node probe completed.",
    "Edit edge node": "Edit edge node",
    "Node disabled.": "Node disabled.",
    "Node enabled.": "Node enabled.",
    "Delete this edge node?": "Delete this edge node?",
    "Node deleted.": "Node deleted.",
    "Add administrator": "Add administrator",
    "Username": "Username",
    "Initial password": "Initial password",
    "Create user": "Create user",
    "User list": "User list",
    "Created": "Created",
    "Updated": "Updated",
    "User created.": "User created.",
    "Delete this administrator account?": "Delete this administrator account?",
    "User deleted.": "User deleted.",
    "Change password": "Change password",
    "Current password": "Current password",
    "New password (at least 10 characters)": "New password (at least 10 characters)",
    "Update password": "Update password",
    "Password updated.": "Password updated.",
    "Unknown action.": "Unknown action."
}
};
