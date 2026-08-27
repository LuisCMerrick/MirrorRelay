// Shared settings schema. Labels are English source strings translated through i18n.
export const settingsGroups = [
  {
    "title": "Local endpoints and ingress",
    "effect": "restart",
    "fields": [
      {
        "path": "server.unix_socket_enabled",
        "label": "Enable frontend Unix socket",
        "type": "boolean"
      },
      {
        "path": "server.frontend_socket",
        "label": "Frontend Unix socket path",
        "type": "text",
        "placeholder": "/run/mirrorrelay/frontend.sock"
      },
      {
        "path": "server.frontend_socket_mode",
        "label": "Frontend socket file mode",
        "type": "text",
        "placeholder": "0660"
      },
      {
        "path": "server.local_address",
        "label": "Frontend listen IP",
        "type": "text",
        "placeholder": "127.0.0.1"
      },
      {
        "path": "server.local_port",
        "label": "Frontend listen port",
        "type": "number",
        "min": 1,
        "max": 65535
      },
      {
        "path": "runtime.root",
        "label": "Runtime root directory",
        "type": "text",
        "placeholder": "/var/lib/mirrorrelay/runtime"
      },
      {
        "path": "runtime.run_dir",
        "label": "Runtime PID and socket directory",
        "type": "text",
        "placeholder": "/run/mirrorrelay"
      }
    ]
  },
  {
    "title": "Ingress and standalone HTTP/TLS",
    "effect": "restart",
    "fields": [
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
        "path": "ingress.snippet_path",
        "label": "Ingress snippet output path",
        "type": "text",
        "placeholder": "/var/lib/mirrorrelay/integration/external-nginx"
      },
      {
        "path": "http.listen",
        "label": "Standalone HTTP listen",
        "type": "text",
        "placeholder": ":80"
      },
      {
        "path": "http.https_listen",
        "label": "Standalone HTTPS listen",
        "type": "text",
        "placeholder": ":443"
      },
      {
        "path": "http.public_base_url",
        "label": "Public base URL",
        "type": "text",
        "placeholder": "https://mirror.example.com"
      },
      {
        "path": "tls.certificate",
        "label": "TLS certificate file path",
        "type": "text",
        "placeholder": "/etc/mirrorrelay/certs/fullchain.pem"
      },
      {
        "path": "tls.private_key",
        "label": "TLS private key file path",
        "type": "text",
        "placeholder": "/etc/mirrorrelay/certs/privkey.pem"
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
    "title": "Performance, metadata, and redirects",
    "effect": "restart",
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
        "path": "performance.zero_copy_bypass",
        "label": "Zero-Copy X-Accel Acceleration (Pure binary bypass)",
        "type": "boolean"
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
        "path": "redirect.pin_validated_ip",
        "label": "Pin validated IP across redirects",
        "type": "boolean"
      },
      {
        "path": "redirect.reject_mixed_dns_result",
        "label": "Reject mixed permitted/forbidden DNS results",
        "type": "boolean"
      }
    ]
  },
  {
    "title": "Database and cache defaults",
    "effect": "restart",
    "fields": [
      {
        "path": "database.path",
        "label": "SQLite database file path",
        "type": "text",
        "placeholder": "/var/lib/mirrorrelay/mirrorrelay.db"
      },
      {
        "path": "cache.path",
        "label": "Cache directory path",
        "type": "text",
        "placeholder": "/var/cache/mirrorrelay"
      },
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
    "title": "Security and access control",
    "effect": "restart",
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
        "path": "security.trusted_proxy_cidrs",
        "label": "Trusted ingress CIDRs (one per line)",
        "type": "list"
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
      },
      {
        "path": "admin.host",
        "label": "Dedicated administration host (blank allows all hosts)",
        "type": "text"
      },
      {
        "path": "admin.path",
        "label": "Administration path prefix",
        "type": "text",
        "placeholder": "/admin/"
      },
      {
        "path": "admin.passkey.enabled",
        "label": "Enable Passkey authentication",
        "type": "boolean"
      },
      {
        "path": "admin.passkey.rp_name",
        "label": "Passkey relying party name",
        "type": "text",
        "placeholder": "MirrorRelay"
      },
      {
        "path": "admin.passkey.rp_id",
        "label": "Passkey relying party ID",
        "type": "text",
        "placeholder": "admin.example.com"
      },
      {
        "path": "admin.passkey.origins",
        "label": "Allowed Passkey origins (one per line)",
        "type": "list"
      }
    ]
  },
  {
    "title": "Network transport and concurrency limits",
    "effect": "restart",
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
    "effect": "restart",
    "fields": [
      {
        "path": "logging.path",
        "label": "Application log directory",
        "type": "text",
        "placeholder": "/var/log/mirrorrelay"
      },
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
    "effect": "restart",
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
        "path": "upstream_nginx.binary",
        "label": "Nginx executable path",
        "type": "text",
        "placeholder": "/usr/lib/mirrorrelay/nginx/nginx"
      },
      {
        "path": "upstream_nginx.prefix",
        "label": "Nginx runtime prefix directory",
        "type": "text",
        "placeholder": "/var/lib/mirrorrelay/runtime/upstream-nginx"
      },
      {
        "path": "upstream_nginx.pid",
        "label": "Nginx PID file path",
        "type": "text",
        "placeholder": "/run/mirrorrelay/upstream-nginx.pid"
      },
      {
        "path": "upstream_nginx.log_path",
        "label": "Nginx log directory",
        "type": "text",
        "placeholder": "/var/log/mirrorrelay/upstream-nginx"
      },
      {
        "path": "upstream_nginx.upstream_unix_socket_enabled",
        "label": "Use upstream Unix socket",
        "type": "boolean"
      },
      {
        "path": "upstream_nginx.upstream_socket",
        "label": "Upstream Unix socket path",
        "type": "text",
        "placeholder": "/run/mirrorrelay/upstream.sock"
      },
      {
        "path": "upstream_nginx.upstream_socket_mode",
        "label": "Upstream socket file mode",
        "type": "text",
        "placeholder": "0660"
      },
      {
        "path": "upstream_nginx.upstream_local_port",
        "label": "Upstream loopback port",
        "type": "number",
        "min": 1,
        "max": 65535
      },
      {
        "path": "upstream_nginx.ca_bundle",
        "label": "Upstream CA bundle path",
        "type": "text",
        "placeholder": "/etc/ssl/certs/ca-certificates.crt"
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
  },
  {
    "title": "Distributed cluster and routing",
    "effect": "restart",
    "fields": [
      {
        "path": "distributed.enabled",
        "label": "Enable distributed cluster support",
        "type": "boolean"
      },
      {
        "path": "distributed.role",
        "label": "Cluster node role",
        "type": "select",
        "options": [
          [
            "standalone",
            "Standalone"
          ],
          [
            "coordinator",
            "Coordinator"
          ],
          [
            "edge",
            "Edge"
          ]
        ]
      },
      {
        "path": "distributed.token",
        "label": "Cluster communication token (edge registration)",
        "type": "password",
        "placeholder": "Leave blank to keep the current cluster token"
      },
      {
        "path": "distributed.mutation_token",
        "label": "Mutation token (edge write and sync authentication)",
        "type": "password",
        "placeholder": "Leave blank to keep the current mutation token"
      },
      {
        "path": "distributed.mutation_token_key_files",
        "label": "Mutation-token keyring files (one per line)",
        "type": "list"
      },
      {
        "path": "distributed.coordinator_id",
        "label": "Coordinator instance ID",
        "type": "text"
      },
      {
        "path": "distributed.allow_http",
        "label": "Allow plaintext HTTP between cluster nodes",
        "type": "boolean"
      },
      {
        "path": "distributed.node.name",
        "label": "Current node name",
        "type": "text",
        "placeholder": "node-01"
      },
      {
        "path": "distributed.node.public_base_url",
        "label": "Current node public base URL",
        "type": "text",
        "placeholder": "https://node.example.com"
      },
      {
        "path": "distributed.node.region",
        "label": "Current node region",
        "type": "text",
        "placeholder": "default"
      },
      {
        "path": "distributed.node.country",
        "label": "Current node country code",
        "type": "text",
        "placeholder": "CN"
      },
      {
        "path": "distributed.routing.mode",
        "label": "Client traffic routing mode",
        "type": "select",
        "options": [
          [
            "hybrid",
            "Hybrid (CIDR > geo > priority/weight)"
          ],
          [
            "cidr",
            "CIDR client networks only"
          ],
          [
            "geo",
            "Geographic region and country only"
          ],
          [
            "priority",
            "Node priority and weight only"
          ]
        ]
      },
      {
        "path": "distributed.health_check.interval",
        "label": "Cluster node health-check interval",
        "type": "text"
      },
      {
        "path": "distributed.health_check.timeout",
        "label": "Cluster node health-check timeout",
        "type": "text"
      },
      {
        "path": "distributed.health_check.unhealthy_threshold",
        "label": "Consecutive failures before unhealthy",
        "type": "number",
        "min": 1
      },
      {
        "path": "distributed.health_check.healthy_threshold",
        "label": "Consecutive successes before healthy",
        "type": "number",
        "min": 1
      }
    ]
  },
  {
    "title": "Webhook alerts — single destination",
    "effect": "restart",
    "fields": [
      {
        "path": "webhook.enabled",
        "label": "Enable Webhook notifications",
        "type": "boolean"
      },
      {
        "path": "webhook.url",
        "label": "Single Webhook destination URL (provider format is auto-detected)",
        "type": "text"
      },
      {
        "path": "webhook.secret",
        "label": "Secret token (for HMAC-SHA256 signature)",
        "type": "password",
        "placeholder": "Leave blank to keep the current signing secret"
      },
      {
        "path": "webhook.events",
        "label": "Enabled event names (one per line)",
        "type": "list"
      },
      {
        "path": "webhook.timeout",
        "label": "Request timeout",
        "type": "text"
      },
      {
        "path": "webhook.allow_http",
        "label": "Allow plaintext HTTP for this webhook",
        "type": "boolean"
      },
      {
        "path": "webhook.allow_private",
        "label": "Allow private or local addresses for this webhook",
        "type": "boolean"
      }
    ]
  },
  {
    "title": "Cache warm-up and predictive prefetch",
    "effect": "restart",
    "fields": [
      {
        "path": "warmup.enabled",
        "label": "Enable Smart Cache Warm-Up Engine",
        "type": "boolean"
      },
      {
        "path": "warmup.max_concurrency",
        "label": "Maximum warm-up concurrency",
        "type": "number",
        "min": 1,
        "max": 64
      },
      {
        "path": "warmup.bandwidth_limit_bps",
        "label": "Warm-up bandwidth limit in bytes/s (0 = unlimited)",
        "type": "number",
        "min": 0
      },
      {
        "path": "warmup.timeout",
        "label": "Warm-up job timeout",
        "type": "text"
      },
      {
        "path": "warmup.metadata_depth",
        "label": "Metadata package extraction depth (0 = direct only, 1 = parse packages)",
        "type": "number",
        "min": 0,
        "max": 5
      },
      {
        "path": "warmup.retry_count",
        "label": "Retry count on failure",
        "type": "number",
        "min": 0,
        "max": 10
      }
    ]
  }
];
