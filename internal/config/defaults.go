package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"time"

	"github.com/LuisCMerrick/MirrorRelay/internal/model"
)

// Default returns a Config populated with production defaults.
func Default() Config {
	return Config{
		Server: ServerConfig{
			UnixSocketEnabled:      false,
			FrontendSocket:         "/run/mirrorrelay/frontend.sock",
			FrontendSocketModeText: "0660",
			FrontendSocketMode:     0o660,
			LocalAddress:           "127.0.0.1",
			LocalPort:              9081,
		},
		Runtime: RuntimeConfig{
			Root:   "/var/lib/mirrorrelay/runtime",
			RunDir: "/run/mirrorrelay",
		},
		Ingress: IngressConfig{
			Mode:            "external",
			GenerateSnippet: true,
			SnippetPath:     "/var/lib/mirrorrelay/integration/external-nginx",
		},
		Performance: PerformanceConfig{
			StreamBufferSize: 64 << 10,
			GOGC:             100,
			ZeroCopyBypass:   true,
		},
		Metadata: MetadataConfig{
			RewriteBufferLimit: 8 << 20,
			OutputCompression:  "auto",
			GzipMinLength:      1024,
			ValidatorEntries:   2048,
		},
		Redirect: RedirectConfig{
			MaxHops:           5,
			PinValidatedIP:    true,
			RejectMixedResult: true,
		},
		HTTP: HTTPConfig{
			Listen:      ":80",
			HTTPSListen: ":443",
			ReadTimeout: 15 * time.Second,
			IdleTimeout: 2 * time.Minute,
		},
		TLS: TLSConfig{
			Certificate: "/etc/mirrorrelay/certs/fullchain.pem",
			PrivateKey:  "/etc/mirrorrelay/certs/privkey.pem",
			MinVersion:  "1.2",
		},
		Database: DatabaseConfig{
			Path: "/var/lib/mirrorrelay/mirrorrelay.db",
		},
		Cache: CacheConfig{
			Path:             "/var/cache/mirrorrelay",
			MaxSizeBytes:     500 << 30,
			MaxFiles:         1_000_000,
			Inactive:         30 * 24 * time.Hour,
			MetadataTTL:      5 * time.Minute,
			PackageTTL:       30 * 24 * time.Hour,
			CleanupInterval:  10 * time.Minute,
			WaitForFill:      30 * time.Minute,
			MinimumFreeBytes: 1 << 30,
		},
		Logging: LoggingConfig{
			Path:      "/var/log/mirrorrelay",
			QueueSize: 8192,
			MaxSizeMB: 1024,
			KeepDays:  30,
		},
		Security: SecurityConfig{
			AllowHTTPUpstream:    false,
			AllowPrivateUpstream: false,
			ExposeClientIP:       false,
			TrustedProxyCIDRs:    []string{"127.0.0.1/32", "::1/128"},
			SessionTimeout:       12 * time.Hour,
			LoginWindow:          10 * time.Minute,
			LoginMaxFailures:     5,
			AdminCIDRs:           []string{"127.0.0.1/32", "::1/128"},
		},
		Transport: TransportConfig{
			DialTimeout:           5 * time.Second,
			KeepAlive:             30 * time.Second,
			TLSHandshakeTimeout:   5 * time.Second,
			ResponseHeaderTimeout: 60 * time.Second,
			IdleConnTimeout:       90 * time.Second,
			MaxIdleConns:          1024,
			MaxIdleConnsPerHost:   128,
		},
		Limits: LimitsConfig{
			MaxTotalConcurrency: 4096,
			MaxIPConcurrency:    256,
			BandwidthLimitBPS:   0,
		},
		Health: HealthConfig{
			WorkerInterval: 60 * time.Second,
		},
		Admin: AdminConfig{
			Host: "",
			Path: "/admin/",
		},
		Shutdown: ShutdownConfig{
			GracePeriod: 30 * time.Second,
		},
		UpstreamNginx: UpstreamNginxConfig{
			Mode:                   "managed",
			Binary:                 "/usr/lib/mirrorrelay/nginx/nginx",
			Prefix:                 "/var/lib/mirrorrelay/runtime/upstream-nginx",
			PID:                    "/run/mirrorrelay/upstream-nginx.pid",
			LogPath:                "/var/log/mirrorrelay/upstream-nginx",
			UpstreamSocketEnabled:  true,
			UpstreamSocket:         "/run/mirrorrelay/upstream.sock",
			UpstreamSocketModeText: "0660",
			UpstreamSocketMode:     0o660,
			UpstreamLocalPort:      9082,
			CABundle:               "/etc/ssl/certs/ca-certificates.crt",
			TLSVerifyDepth:         3,
			Resolver:               "1.1.1.1 8.8.8.8",
			ResolverRefresh:        5 * time.Minute,
			HistoryLimit:           20,
			RestartMaxFailures:     3,
			RestartWindow:          5 * time.Minute,
			RestartInitialBackoff:  1 * time.Second,
			RestartMaxBackoff:      30 * time.Second,
			WorkerProcesses:        "auto",
			WorkerUser:             "mirrorrelay",
			WorkerConnections:      4096,
			StopOnMirrorRelayExit:  true,
		},
		Distributed: DistributedConfig{
			Enabled:       false,
			Role:          "standalone",
			Token:         "",
			MutationToken: "",
			CoordinatorID: "",
			AllowHTTP:     false,
			Node: DistributedNodeConfig{
				Name:          "",
				PublicBaseURL: "",
				Region:        "default",
				Country:       "",
			},
			Routing: DistributedRoutingConfig{
				Mode:           "hybrid",
				ClientNetworks: []ClientNetworkMapping{},
				Regions:        []RegionMapping{},
			},
			HealthCheck: DistributedHealthConfig{
				Interval:           15 * time.Second,
				Timeout:            3 * time.Second,
				UnhealthyThreshold: 2,
				HealthyThreshold:   2,
			},
			Nodes: []DistributedNodeSeed{},
		},
		UIEnhancement: model.UIEnhancementConfig{
			Enabled:     false,
			Theme:       "system",
			AccentColor: "#2563eb",
			Branding: model.BrandingConfig{
				Title:   "MirrorRelay",
				Logo:    "",
				Favicon: "",
			},
			Login: model.LoginBrandingConfig{
				Title:    "MirrorRelay",
				Subtitle: "Repository Proxy Service",
			},
			CustomCSS: model.CustomCSSConfig{
				Enabled: false,
				File:    "/var/lib/mirrorrelay/ui/custom.css",
			},
			RepositoryBrowser: model.RepositoryBrowserConfig{
				Enabled: true,
			},
		},
		Webhook: model.WebhookConfig{
			Enabled: false,
			URL:     "",
			Secret:  "",
			Events:  []string{"upstream_status", "cache_threshold", "config_change", "security_alert"},
			Timeout: 5 * time.Second,
		},
		Warmup: model.WarmupConfig{
			Enabled:        false,
			MaxConcurrency: 4,
			BandwidthLimit: 0,
			Timeout:        30 * time.Minute,
			RetryCount:     2,
			MetadataDepth:  1,
		},
	}
}

func applyDevDefaults(cfg *Config) {
	devRoot, err := filepath.Abs("dev-data")
	if err != nil {
		devRoot = filepath.Clean("dev-data")
	}
	cfg.Ingress.Mode = "managed-standalone"
	if cfg.HTTP.Listen == ":80" {
		cfg.HTTP.Listen = "127.0.0.1:8080"
	}
	if cfg.HTTP.HTTPSListen == ":443" {
		cfg.HTTP.HTTPSListen = "127.0.0.1:8443"
	}
	cfg.Database.Path = filepath.Join(devRoot, "mirrorrelay.db")
	cfg.Cache.Path = filepath.Join(devRoot, "cache")
	cfg.Logging.Path = filepath.Join(devRoot, "logs")
	cfg.TLS.Certificate = filepath.Join(devRoot, "certs", "server.pem")
	cfg.TLS.PrivateKey = filepath.Join(devRoot, "certs", "server-key.pem")
	cfg.Runtime.Root = filepath.Join(devRoot, "runtime")
	cfg.Runtime.RunDir = filepath.Join(devRoot, "run")
	cfg.Server.FrontendSocket = filepath.Join(cfg.Runtime.RunDir, "frontend.sock")
	cfg.UpstreamNginx.UpstreamSocket = filepath.Join(cfg.Runtime.RunDir, "upstream.sock")
	cfg.UpstreamNginx.PID = filepath.Join(cfg.Runtime.RunDir, "upstream-nginx.pid")
	cfg.UpstreamNginx.LogPath = filepath.Join(devRoot, "logs", "upstream-nginx")
	cfg.Ingress.SnippetPath = filepath.Join(devRoot, "integration", "external-nginx")
	if cfg.UpstreamNginx.Prefix == "/var/lib/mirrorrelay/runtime/upstream-nginx" {
		cfg.UpstreamNginx.Prefix = filepath.Join(devRoot, "runtime", "upstream-nginx")
	}
	if cfg.UpstreamNginx.Binary == "/usr/lib/mirrorrelay/nginx/nginx" {
		fixture := filepath.Join("nginx", "sbin", "nginx")
		if info, statErr := os.Stat(fixture); runtime.GOOS == "linux" && runtime.GOARCH == "amd64" && statErr == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0 {
			cfg.UpstreamNginx.Binary = fixture
		} else if info, statErr := os.Stat(cfg.UpstreamNginx.Binary); statErr != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
			cfg.UpstreamNginx.Binary = "nginx"
		}
	}
	if os.Geteuid() == 0 {
		cfg.UpstreamNginx.WorkerUser = "root"
	} else {
		cfg.UpstreamNginx.WorkerUser = ""
	}
}

func applyEnvironment(cfg *Config) {
	if v := os.Getenv("MIRRORRELAY_ADMIN_HOST"); v != "" {
		cfg.Admin.Host = v
	}
	if v := os.Getenv("MIRRORRELAY_DISTRIBUTED_ROLE"); v != "" {
		cfg.Distributed.Role = v
		if v == "coordinator" || v == "edge" {
			cfg.Distributed.Enabled = true
		}
	}
	if v := os.Getenv("MIRRORRELAY_DISTRIBUTED_ENABLED"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			cfg.Distributed.Enabled = b
		}
	}
	if v := os.Getenv("MIRRORRELAY_DISTRIBUTED_TOKEN"); v != "" {
		cfg.Distributed.Token = v
	}
	if v := os.Getenv("MIRRORRELAY_DISTRIBUTED_MUTATION_TOKEN"); v != "" {
		cfg.Distributed.MutationToken = v
	}
	if v := os.Getenv("MIRRORRELAY_DISTRIBUTED_MUTATION_TOKEN_KEY_FILES"); v != "" {
		cfg.Distributed.MutationTokenKeyFiles = filepath.SplitList(v)
	}
	if v := os.Getenv("MIRRORRELAY_COORDINATOR_ID"); v != "" {
		cfg.Distributed.CoordinatorID = v
	}
	if v := os.Getenv("MIRRORRELAY_NODE_NAME"); v != "" {
		cfg.Distributed.Node.Name = v
	}
	if v := os.Getenv("MIRRORRELAY_NODE_PUBLIC_BASE_URL"); v != "" {
		cfg.Distributed.Node.PublicBaseURL = v
	}
	if v := os.Getenv("MIRRORRELAY_NODE_REGION"); v != "" {
		cfg.Distributed.Node.Region = v
	}
}

// ApplyEnvironment reapplies documented environment overrides and validates
// the resulting configuration. Callers use this after loading Web UI settings
// so explicit process configuration retains the highest precedence.
func ApplyEnvironment(cfg Config) (Config, error) {
	applyEnvironment(&cfg)
	if err := cfg.NormalizeRuntime(); err != nil {
		return cfg, err
	}
	if err := cfg.Validate(); err != nil {
		return cfg, err
	}
	return cfg, nil
}
