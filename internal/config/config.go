package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server        ServerConfig        `yaml:"server"`
	Runtime       RuntimeConfig       `yaml:"runtime"`
	Ingress       IngressConfig       `yaml:"ingress"`
	Performance   PerformanceConfig   `yaml:"performance"`
	Metadata      MetadataConfig      `yaml:"metadata"`
	Redirect      RedirectConfig      `yaml:"redirect"`
	HTTP          HTTPConfig          `yaml:"http"`
	TLS           TLSConfig           `yaml:"tls"`
	Database      DatabaseConfig      `yaml:"database"`
	Cache         CacheConfig         `yaml:"cache"`
	Logging       LoggingConfig       `yaml:"logging"`
	Security      SecurityConfig      `yaml:"security"`
	Transport     TransportConfig     `yaml:"transport"`
	Limits        LimitsConfig        `yaml:"limits"`
	Health        HealthConfig        `yaml:"health"`
	Admin         AdminConfig         `yaml:"admin"`
	Shutdown      ShutdownConfig      `yaml:"shutdown"`
	UpstreamNginx UpstreamNginxConfig `yaml:"upstream_nginx"`
	Distributed   DistributedConfig   `yaml:"distributed"`
}

type ServerConfig struct {
	UnixSocketEnabled      bool        `yaml:"unix_socket_enabled"`
	FrontendSocket         string      `yaml:"frontend_socket"`
	FrontendSocketMode     os.FileMode `yaml:"-"`
	FrontendSocketModeText string      `yaml:"frontend_socket_mode"`
	LocalPort              int         `yaml:"local_port"`
}

type RuntimeConfig struct {
	Root   string `yaml:"root"`
	RunDir string `yaml:"run_dir"`
}

type IngressConfig struct {
	Mode            string `yaml:"mode"`
	GenerateSnippet bool   `yaml:"generate_snippet"`
	SnippetPath     string `yaml:"snippet_path"`
}

type PerformanceConfig struct {
	StreamBufferSize int   `yaml:"stream_buffer_size_bytes"`
	GoMemoryLimit    int64 `yaml:"go_memory_limit_bytes"`
	GOGC             int   `yaml:"gogc"`
}

type MetadataConfig struct {
	RewriteBufferLimit int64  `yaml:"rewrite_buffer_limit_bytes"`
	OutputCompression  string `yaml:"output_compression"`
	GzipMinLength      int    `yaml:"gzip_min_length_bytes"`
	ValidatorEntries   int    `yaml:"validator_entries"`
}

type RedirectConfig struct {
	MaxHops           int  `yaml:"max_hops"`
	PinValidatedIP    bool `yaml:"pin_validated_ip"`
	RejectMixedResult bool `yaml:"reject_mixed_dns_result"`
}

type UpstreamNginxConfig struct {
	Mode                   string        `yaml:"mode"`
	Binary                 string        `yaml:"binary"`
	Prefix                 string        `yaml:"prefix"`
	PID                    string        `yaml:"pid"`
	LogPath                string        `yaml:"log_path"`
	UpstreamSocketEnabled  bool          `yaml:"upstream_unix_socket_enabled"`
	UpstreamSocket         string        `yaml:"upstream_socket"`
	UpstreamSocketModeText string        `yaml:"upstream_socket_mode"`
	UpstreamSocketMode     os.FileMode   `yaml:"-"`
	UpstreamLocalPort      int           `yaml:"upstream_local_port"`
	CABundle               string        `yaml:"ca_bundle"`
	TLSVerifyDepth         int           `yaml:"tls_verify_depth"`
	Resolver               string        `yaml:"resolver"`
	ResolverRefresh        time.Duration `yaml:"resolver_refresh"`
	HistoryLimit           int           `yaml:"history_limit"`
	RestartMaxFailures     int           `yaml:"restart_max_failures"`
	RestartWindow          time.Duration `yaml:"restart_window"`
	RestartInitialBackoff  time.Duration `yaml:"restart_initial_backoff"`
	RestartMaxBackoff      time.Duration `yaml:"restart_max_backoff"`
	WorkerProcesses        string        `yaml:"worker_processes"`
	WorkerUser             string        `yaml:"worker_user"`
	WorkerConnections      int           `yaml:"worker_connections"`
	StopOnMirrorRelayExit  bool          `yaml:"stop_on_mirrorrelay_exit"`
}

type HTTPConfig struct {
	Listen        string        `yaml:"listen"`
	HTTPSListen   string        `yaml:"https_listen"`
	PublicBaseURL string        `yaml:"public_base_url"`
	ReadTimeout   time.Duration `yaml:"read_timeout"`
	WriteTimeout  time.Duration `yaml:"write_timeout"`
	IdleTimeout   time.Duration `yaml:"idle_timeout"`
}

type TLSConfig struct {
	Certificate string `yaml:"certificate"`
	PrivateKey  string `yaml:"private_key"`
	MinVersion  string `yaml:"min_version"`
}

type DatabaseConfig struct {
	Path string `yaml:"path"`
}

type CacheConfig struct {
	Path             string        `yaml:"path"`
	MaxSizeBytes     int64         `yaml:"max_size_bytes"`
	MaxFiles         int           `yaml:"max_files"`
	Inactive         time.Duration `yaml:"inactive"`
	MetadataTTL      time.Duration `yaml:"metadata_ttl"`
	PackageTTL       time.Duration `yaml:"package_ttl"`
	CleanupInterval  time.Duration `yaml:"cleanup_interval"`
	WaitForFill      time.Duration `yaml:"wait_for_fill"`
	MinimumFreeBytes int64         `yaml:"minimum_free_bytes"`
}

type LoggingConfig struct {
	Path      string `yaml:"path"`
	QueueSize int    `yaml:"queue_size"`
	MaxSizeMB int64  `yaml:"max_size_mb"`
	KeepDays  int    `yaml:"keep_days"`
}

type SecurityConfig struct {
	AllowHTTPUpstream    bool          `yaml:"allow_http_upstream"`
	AllowPrivateUpstream bool          `yaml:"allow_private_upstream"`
	ExposeClientIP       bool          `yaml:"expose_client_ip"`
	SessionTimeout       time.Duration `yaml:"session_timeout"`
	LoginWindow          time.Duration `yaml:"login_window"`
	LoginMaxFailures     int           `yaml:"login_max_failures"`
	AdminCIDRs           []string      `yaml:"admin_cidrs"`
}

type TransportConfig struct {
	DialTimeout           time.Duration `yaml:"dial_timeout"`
	KeepAlive             time.Duration `yaml:"keep_alive"`
	TLSHandshakeTimeout   time.Duration `yaml:"tls_handshake_timeout"`
	ResponseHeaderTimeout time.Duration `yaml:"response_header_timeout"`
	IdleConnTimeout       time.Duration `yaml:"idle_connection_timeout"`
	MaxIdleConns          int           `yaml:"max_idle_connections"`
	MaxIdleConnsPerHost   int           `yaml:"max_idle_connections_per_host"`
}

type LimitsConfig struct {
	MaxTotalConcurrency int   `yaml:"max_total_concurrency"`
	MaxIPConcurrency    int   `yaml:"max_ip_concurrency"`
	BandwidthLimitBPS   int64 `yaml:"bandwidth_limit_bps"`
}

type HealthConfig struct {
	WorkerInterval time.Duration `yaml:"worker_interval"`
}

type AdminConfig struct {
	Host            string `yaml:"host"`
	Path            string `yaml:"path"`
	InitialUsername string `yaml:"initial_username"`
	InitialPassword string `yaml:"initial_password"`
}

type ShutdownConfig struct {
	GracePeriod time.Duration `yaml:"grace_period"`
}

type DistributedConfig struct {
	Enabled     bool                     `yaml:"enabled"`
	Role        string                   `yaml:"role"`
	Token       string                   `yaml:"token"`
	Node        DistributedNodeConfig    `yaml:"node"`
	Routing     DistributedRoutingConfig `yaml:"routing"`
	HealthCheck DistributedHealthConfig  `yaml:"health_check"`
	Nodes       []DistributedNodeSeed    `yaml:"nodes"`
}

type DistributedNodeConfig struct {
	Name          string `yaml:"name"`
	PublicBaseURL string `yaml:"public_base_url"`
	Region        string `yaml:"region"`
	Country       string `yaml:"country"`
}

type DistributedRoutingConfig struct {
	Mode           string                 `yaml:"mode"`
	ClientNetworks []ClientNetworkMapping `yaml:"client_networks"`
	Regions        []RegionMapping        `yaml:"regions"`
}

type ClientNetworkMapping struct {
	CIDR   string `yaml:"cidr"`
	Region string `yaml:"region"`
}

type RegionMapping struct {
	Code      string   `yaml:"code"`
	Countries []string `yaml:"countries"`
}

type DistributedHealthConfig struct {
	Interval           time.Duration `yaml:"interval"`
	Timeout            time.Duration `yaml:"timeout"`
	UnhealthyThreshold int           `yaml:"unhealthy_threshold"`
	HealthyThreshold   int           `yaml:"healthy_threshold"`
}

type DistributedNodeSeed struct {
	Name     string `yaml:"name"`
	URL      string `yaml:"url"`
	Region   string `yaml:"region"`
	Country  string `yaml:"country"`
	Priority int    `yaml:"priority"`
	Weight   int    `yaml:"weight"`
	Enabled  bool   `yaml:"enabled"`
}

func Default() Config {
	return Config{
		Server: ServerConfig{UnixSocketEnabled: true, FrontendSocket: "/run/mirrorrelay/frontend.sock",
			FrontendSocketModeText: "0660", FrontendSocketMode: 0o660, LocalPort: 9081},
		Runtime:     RuntimeConfig{Root: "/var/lib/mirrorrelay/runtime", RunDir: "/run/mirrorrelay"},
		Ingress:     IngressConfig{Mode: "external", GenerateSnippet: true, SnippetPath: "/var/lib/mirrorrelay/integration/external-nginx"},
		Performance: PerformanceConfig{StreamBufferSize: 64 << 10, GOGC: 100},
		Metadata:    MetadataConfig{RewriteBufferLimit: 8 << 20, OutputCompression: "auto", GzipMinLength: 1024, ValidatorEntries: 2048},
		Redirect:    RedirectConfig{MaxHops: 5, PinValidatedIP: true, RejectMixedResult: true},
		HTTP:        HTTPConfig{Listen: ":80", HTTPSListen: ":443", ReadTimeout: 15 * time.Second, IdleTimeout: 2 * time.Minute},
		TLS:         TLSConfig{Certificate: "/etc/mirrorrelay/certs/fullchain.pem", PrivateKey: "/etc/mirrorrelay/certs/privkey.pem", MinVersion: "1.2"},
		Database:    DatabaseConfig{Path: "/var/lib/mirrorrelay/mirrorrelay.db"},
		Cache:       CacheConfig{Path: "/var/cache/mirrorrelay", MaxSizeBytes: 500 << 30, MaxFiles: 1_000_000, Inactive: 30 * 24 * time.Hour, MetadataTTL: 5 * time.Minute, PackageTTL: 30 * 24 * time.Hour, CleanupInterval: 10 * time.Minute, WaitForFill: 30 * time.Minute, MinimumFreeBytes: 1 << 30},
		Logging:     LoggingConfig{Path: "/var/log/mirrorrelay", QueueSize: 8192, MaxSizeMB: 1024, KeepDays: 30},
		Security:    SecurityConfig{SessionTimeout: 12 * time.Hour, LoginWindow: 15 * time.Minute, LoginMaxFailures: 5},
		Transport:   TransportConfig{DialTimeout: 10 * time.Second, KeepAlive: 30 * time.Second, TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 30 * time.Second, IdleConnTimeout: 90 * time.Second, MaxIdleConns: 512, MaxIdleConnsPerHost: 64},
		Limits:      LimitsConfig{MaxTotalConcurrency: 0, MaxIPConcurrency: 0},
		Health:      HealthConfig{WorkerInterval: 15 * time.Second},
		Admin:       AdminConfig{Path: "/admin/", InitialUsername: "admin"},
		Shutdown:    ShutdownConfig{GracePeriod: 30 * time.Minute},
		UpstreamNginx: UpstreamNginxConfig{Mode: "managed", Binary: "/usr/lib/mirrorrelay/nginx/nginx", Prefix: "/var/lib/mirrorrelay/runtime/upstream-nginx",
			PID:                   "/run/mirrorrelay/upstream-nginx.pid",
			LogPath:               "/var/log/mirrorrelay/upstream-nginx",
			UpstreamSocketEnabled: true, UpstreamSocket: "/run/mirrorrelay/upstream.sock",
			UpstreamSocketModeText: "0600", UpstreamSocketMode: 0o600, UpstreamLocalPort: 9082,
			CABundle: "/etc/ssl/certs/ca-certificates.crt", TLSVerifyDepth: 5,
			Resolver: "1.1.1.1 8.8.8.8", ResolverRefresh: 5 * time.Minute,
			HistoryLimit: 20, RestartMaxFailures: 5, RestartWindow: 2 * time.Minute,
			RestartInitialBackoff: time.Second, RestartMaxBackoff: 30 * time.Second, WorkerProcesses: "auto", WorkerUser: "mirrorrelay", WorkerConnections: 4096,
			StopOnMirrorRelayExit: false},
		Distributed: DistributedConfig{
			Enabled: false,
			Role:    "standalone",
			Routing: DistributedRoutingConfig{
				Mode: "hybrid",
			},
			HealthCheck: DistributedHealthConfig{
				Interval:           10 * time.Second,
				Timeout:            3 * time.Second,
				UnhealthyThreshold: 3,
				HealthyThreshold:   2,
			},
		},
	}
}

func Load(path string, dev bool) (Config, error) {
	cfg := Default()
	b, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return cfg, err
	}
	if err == nil {
		decoder := yaml.NewDecoder(bytes.NewReader(b))
		decoder.KnownFields(true)
		if err := decoder.Decode(&cfg); err != nil {
			return cfg, fmt.Errorf("parse config: %w", err)
		}
		var trailing any
		if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
			if err == nil {
				err = errors.New("multiple YAML documents are not allowed")
			}
			return cfg, fmt.Errorf("parse config: %w", err)
		}
	} else if !dev {
		return cfg, fmt.Errorf("read config %q: %w", path, err)
	}
	if dev {
		applyDevDefaults(&cfg)
	}
	applyEnvironment(&cfg)
	if err := cfg.NormalizeRuntime(); err != nil {
		return cfg, fmt.Errorf("parse socket mode: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return cfg, err
	}
	return cfg, nil
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
		cfg.UpstreamNginx.Binary = "nginx"
	}
	if os.Geteuid() == 0 {
		cfg.UpstreamNginx.WorkerUser = "root"
	} else {
		cfg.UpstreamNginx.WorkerUser = ""
	}
	if cfg.Admin.InitialPassword == "" {
		cfg.Admin.InitialPassword = "adminadmin"
	}
}

func applyEnvironment(cfg *Config) {
	if v := os.Getenv("MIRRORRELAY_ADMIN_USERNAME"); v != "" {
		cfg.Admin.InitialUsername = v
	}
	if v := os.Getenv("MIRRORRELAY_ADMIN_PASSWORD_FILE"); v != "" {
		if content, err := os.ReadFile(v); err == nil {
			cfg.Admin.InitialPassword = strings.TrimSpace(string(content))
		}
	}
	if v := os.Getenv("MIRRORRELAY_ADMIN_PASSWORD"); v != "" {
		cfg.Admin.InitialPassword = v
	}
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

func (c Config) Validate() error {
	if c.Server.UnixSocketEnabled {
		frontendMode, err := parseSocketMode(c.Server.FrontendSocketModeText)
		if err != nil || frontendMode != 0o660 {
			return errors.New("server.frontend_socket_mode must be 0660")
		}
		if strings.TrimSpace(c.Server.FrontendSocket) == "" {
			return errors.New("server.frontend_socket is required when Unix sockets are enabled")
		}
	} else if !validPort(c.Server.LocalPort) {
		return errors.New("server.local_port must be 1..65535 when Unix sockets are disabled")
	}
	if c.UpstreamNginx.UpstreamSocketEnabled {
		upstreamMode, err := parseSocketMode(c.UpstreamNginx.UpstreamSocketModeText)
		if err != nil || (upstreamMode != 0o600 && upstreamMode != 0o660) {
			return errors.New("upstream_nginx.upstream_socket_mode must be 0600 or 0660")
		}
		if strings.TrimSpace(c.UpstreamNginx.UpstreamSocket) == "" {
			return errors.New("upstream_nginx.upstream_socket is required when Unix sockets are enabled")
		}
	} else if !validPort(c.UpstreamNginx.UpstreamLocalPort) {
		return errors.New("upstream_nginx.upstream_local_port must be 1..65535 when Unix sockets are disabled")
	}
	if c.Server.UnixSocketEnabled && c.UpstreamNginx.UpstreamSocketEnabled && c.Server.FrontendSocket == c.UpstreamNginx.UpstreamSocket {
		return errors.New("distinct frontend and upstream Unix sockets are required")
	}
	if !c.Server.UnixSocketEnabled && !c.UpstreamNginx.UpstreamSocketEnabled && c.Server.LocalPort == c.UpstreamNginx.UpstreamLocalPort {
		return errors.New("server.local_port and upstream_nginx.upstream_local_port must be distinct")
	}
	if c.Ingress.Mode != "external" && c.Ingress.Mode != "managed-standalone" {
		return errors.New("ingress.mode must be external or managed-standalone")
	}
	if c.Ingress.Mode == "managed-standalone" && (c.HTTP.Listen == "" || c.HTTP.HTTPSListen == "" || c.HTTP.Listen == c.HTTP.HTTPSListen) {
		return errors.New("standalone ingress requires distinct HTTP and HTTPS listen addresses")
	}
	if c.Ingress.Mode == "managed-standalone" && (!validListenAddress(c.HTTP.Listen) || !validListenAddress(c.HTTP.HTTPSListen)) {
		return errors.New("standalone ingress listen addresses must contain a valid IP address and port")
	}
	if c.HTTP.PublicBaseURL != "" {
		u, err := url.Parse(c.HTTP.PublicBaseURL)
		if err != nil || u.Scheme != "https" || u.Host == "" || u.Hostname() == "" || u.Opaque != "" || u.User != nil ||
			u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || (u.EscapedPath() != "" && u.EscapedPath() != "/") ||
			!validURLAuthority(u.Host) || strings.ContainsAny(c.HTTP.PublicBaseURL, "\x00\r\n\\{}") {
			return errors.New("http.public_base_url must be an HTTPS origin without credentials, path, query or fragment")
		}
	}
	if c.Admin.Host != "" {
		adminHost := strings.ToLower(strings.TrimSuffix(c.Admin.Host, "."))
		if strings.ContainsAny(adminHost, " /:@\x00\r\n\\{}") || !validURLAuthority(adminHost) {
			return errors.New("admin.host must be a valid hostname without scheme, port or path")
		}
		if c.HTTP.PublicBaseURL != "" {
			if u, err := url.Parse(c.HTTP.PublicBaseURL); err == nil {
				sharedHost := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
				if adminHost == sharedHost {
					return errors.New("admin.host must not be the same origin/host as http.public_base_url")
				}
			}
		}
	}
	if c.Database.Path == "" || c.Cache.Path == "" || c.Logging.Path == "" {
		return errors.New("database, cache and logging paths are required")
	}
	if c.Cache.MaxSizeBytes <= 0 || c.Cache.MaxFiles <= 0 || c.Cache.MinimumFreeBytes < 0 || c.Cache.Inactive <= 0 || c.Cache.MetadataTTL <= 0 || c.Cache.PackageTTL <= 0 || c.Cache.CleanupInterval <= 0 || c.Cache.WaitForFill <= 0 {
		return errors.New("cache sizes, file limit and durations must be positive")
	}
	if c.Logging.QueueSize <= 0 || c.Logging.MaxSizeMB <= 0 || c.Logging.KeepDays <= 0 {
		return errors.New("logging queue, maximum size and retention must be positive")
	}
	if c.HTTP.ReadTimeout <= 0 || c.HTTP.WriteTimeout < 0 || c.HTTP.IdleTimeout <= 0 ||
		c.Security.SessionTimeout <= 0 || c.Security.LoginWindow <= 0 || c.Security.LoginMaxFailures <= 0 ||
		c.Transport.DialTimeout <= 0 || c.Transport.KeepAlive <= 0 || c.Transport.TLSHandshakeTimeout <= 0 ||
		c.Transport.ResponseHeaderTimeout <= 0 || c.Transport.IdleConnTimeout <= 0 || c.Transport.MaxIdleConns <= 0 ||
		c.Transport.MaxIdleConnsPerHost <= 0 || c.Health.WorkerInterval <= 0 || c.Shutdown.GracePeriod <= 0 {
		return errors.New("timeouts must be positive")
	}
	if c.Limits.MaxTotalConcurrency < 0 || c.Limits.MaxIPConcurrency < 0 || c.Limits.BandwidthLimitBPS < 0 {
		return errors.New("global limits cannot be negative")
	}
	if strings.TrimSpace(c.Admin.InitialUsername) == "" {
		return errors.New("admin.initial_username is required")
	}
	adminPath, err := normalizeAdminPath(c.Admin.Path)
	if err != nil || adminPath != c.Admin.Path {
		return errors.New("admin.path must be an absolute URL path with safe segments and a trailing slash")
	}
	for _, cidr := range c.Security.AdminCIDRs {
		if _, _, err := net.ParseCIDR(cidr); err != nil {
			return fmt.Errorf("invalid admin CIDR %q: %w", cidr, err)
		}
	}
	if c.TLS.MinVersion != "1.2" && c.TLS.MinVersion != "1.3" {
		return errors.New("tls.min_version must be 1.2 or 1.3")
	}
	if c.UpstreamNginx.Mode != "managed" && c.UpstreamNginx.Mode != "external" && c.UpstreamNginx.Mode != "disabled" {
		return errors.New("upstream_nginx.mode must be managed, external or disabled")
	}
	if c.Ingress.Mode == "managed-standalone" && c.UpstreamNginx.Mode != "managed" {
		return errors.New("standalone ingress requires upstream_nginx.mode=managed")
	}
	if c.Server.UnixSocketEnabled && !safeNginxPath(c.Server.FrontendSocket) {
		return errors.New("server.frontend_socket contains unsafe characters")
	}
	if c.UpstreamNginx.Mode != "disabled" {
		if strings.TrimSpace(c.UpstreamNginx.Binary) == "" || strings.TrimSpace(c.UpstreamNginx.Prefix) == "" || strings.TrimSpace(c.UpstreamNginx.PID) == "" || strings.TrimSpace(c.UpstreamNginx.LogPath) == "" {
			return errors.New("upstream_nginx binary, prefix, pid and log_path are required")
		}
		if strings.TrimSpace(c.UpstreamNginx.CABundle) == "" || strings.ContainsAny(c.UpstreamNginx.CABundle, "\x00\r\n") || c.UpstreamNginx.TLSVerifyDepth < 1 || c.UpstreamNginx.TLSVerifyDepth > 20 {
			return errors.New("upstream_nginx.ca_bundle is required and upstream_nginx.tls_verify_depth must be 1..20")
		}
		if c.UpstreamNginx.ResolverRefresh <= 0 || c.UpstreamNginx.HistoryLimit <= 0 || c.UpstreamNginx.RestartMaxFailures <= 0 || c.UpstreamNginx.RestartWindow <= 0 || c.UpstreamNginx.RestartInitialBackoff <= 0 || c.UpstreamNginx.RestartMaxBackoff <= 0 || c.UpstreamNginx.WorkerConnections <= 0 {
			return errors.New("upstream_nginx limits and durations must be positive")
		}
		if c.UpstreamNginx.RestartMaxBackoff < c.UpstreamNginx.RestartInitialBackoff {
			return errors.New("upstream_nginx.restart_max_backoff cannot be shorter than restart_initial_backoff")
		}
		if err := validateResolver(c.UpstreamNginx.Resolver); err != nil {
			return err
		}
		if !validWorkerProcesses(c.UpstreamNginx.WorkerProcesses) || !validWorkerUser(c.UpstreamNginx.WorkerUser) {
			return errors.New("upstream_nginx.worker_processes or upstream_nginx.worker_user is invalid")
		}
		for name, value := range map[string]string{
			"upstream_nginx.prefix":    c.UpstreamNginx.Prefix,
			"upstream_nginx.pid":       c.UpstreamNginx.PID,
			"upstream_nginx.log_path":  c.UpstreamNginx.LogPath,
			"upstream_nginx.ca_bundle": c.UpstreamNginx.CABundle,
			"cache.path":               c.Cache.Path,
		} {
			if !safeNginxPath(value) {
				return fmt.Errorf("%s contains characters that are unsafe in generated Nginx configuration", name)
			}
		}
		if c.UpstreamNginx.UpstreamSocketEnabled && !safeNginxPath(c.UpstreamNginx.UpstreamSocket) {
			return errors.New("upstream_nginx.upstream_socket contains unsafe characters")
		}
		if c.Ingress.Mode == "managed-standalone" && (!safeNginxPath(c.TLS.Certificate) || !safeNginxPath(c.TLS.PrivateKey)) {
			return errors.New("standalone TLS paths contain unsafe characters")
		}
	}
	if c.Performance.StreamBufferSize != 32<<10 && c.Performance.StreamBufferSize != 64<<10 && c.Performance.StreamBufferSize != 128<<10 {
		return errors.New("performance.stream_buffer_size_bytes must be 32768, 65536 or 131072")
	}
	if c.Performance.GoMemoryLimit < 0 || c.Performance.GOGC < -1 || c.Performance.GOGC > 10000 {
		return errors.New("performance.go_memory_limit_bytes must be non-negative and gogc must be -1..10000")
	}
	if c.Metadata.RewriteBufferLimit <= 0 || c.Metadata.GzipMinLength < 0 || c.Metadata.ValidatorEntries <= 0 {
		return errors.New("metadata limits must be positive")
	}
	if c.Metadata.OutputCompression != "auto" && c.Metadata.OutputCompression != "identity" && c.Metadata.OutputCompression != "gzip" {
		return errors.New("metadata.output_compression must be auto, identity or gzip")
	}
	if c.Redirect.MaxHops <= 0 || c.Redirect.MaxHops > 20 || !c.Redirect.PinValidatedIP {
		return errors.New("redirect.max_hops must be 1..20 and pin_validated_ip must remain enabled")
	}
	if c.Distributed.Role != "" && c.Distributed.Role != "standalone" && c.Distributed.Role != "coordinator" && c.Distributed.Role != "edge" {
		return errors.New("distributed.role must be standalone, coordinator or edge")
	}
	if c.Distributed.Enabled || c.Distributed.Role != "standalone" {
		if c.Distributed.Routing.Mode != "" && c.Distributed.Routing.Mode != "hybrid" && c.Distributed.Routing.Mode != "cidr" && c.Distributed.Routing.Mode != "geo" && c.Distributed.Routing.Mode != "priority" {
			return errors.New("distributed.routing.mode must be hybrid, cidr, geo or priority")
		}
		for _, netMap := range c.Distributed.Routing.ClientNetworks {
			if _, _, err := net.ParseCIDR(netMap.CIDR); err != nil {
				return fmt.Errorf("invalid distributed client network CIDR %q: %w", netMap.CIDR, err)
			}
			if strings.TrimSpace(netMap.Region) == "" {
				return errors.New("distributed client network region cannot be empty")
			}
		}
		if c.Distributed.Role == "coordinator" {
			if c.Distributed.HealthCheck.Interval <= 0 || c.Distributed.HealthCheck.Timeout <= 0 {
				return errors.New("distributed.health_check interval and timeout must be positive")
			}
			for _, seed := range c.Distributed.Nodes {
				if strings.TrimSpace(seed.URL) == "" {
					return errors.New("distributed node seed URL cannot be empty")
				}
				u, err := url.Parse(seed.URL)
				if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
					return fmt.Errorf("invalid distributed node seed URL %q", seed.URL)
				}
			}
		}
		if c.Distributed.Role == "edge" && c.Distributed.Node.PublicBaseURL != "" {
			u, err := url.Parse(c.Distributed.Node.PublicBaseURL)
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
				return fmt.Errorf("invalid distributed node public_base_url %q", c.Distributed.Node.PublicBaseURL)
			}
		}
	}
	return nil
}

func validateResolver(value string) error {
	fields := strings.Fields(value)
	if len(fields) == 0 {
		return errors.New("upstream_nginx.resolver must contain at least one IP address")
	}
	for _, field := range fields {
		if net.ParseIP(strings.Trim(field, "[]")) == nil {
			return fmt.Errorf("upstream_nginx.resolver contains invalid address %q", field)
		}
	}
	return nil
}

func validWorkerProcesses(value string) bool {
	if strings.TrimSpace(value) == "auto" {
		return true
	}
	workers, err := strconv.Atoi(strings.TrimSpace(value))
	return err == nil && workers >= 1 && workers <= 1024
}

func validWorkerUser(value string) bool {
	if value == "" {
		return true
	}
	for index, character := range value {
		if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') || character == '_' ||
			(index > 0 && character >= '0' && character <= '9') || (index > 0 && character == '-') {
			continue
		}
		return false
	}
	return true
}

func safeNginxPath(value string) bool {
	return value != "" && !strings.ContainsAny(value, "\x00\r\n\t {};'\"")
}

func parseSocketMode(value string) (os.FileMode, error) {
	parsed, err := strconv.ParseUint(strings.TrimSpace(value), 8, 32)
	return os.FileMode(parsed), err
}

func validPort(port int) bool { return port >= 1 && port <= 65535 }

func validListenAddress(address string) bool {
	host, rawPort, err := net.SplitHostPort(address)
	if err != nil {
		return false
	}
	port, err := strconv.Atoi(rawPort)
	if err != nil || !validPort(port) {
		return false
	}
	host = strings.Trim(host, "[]")
	return host == "" || strings.EqualFold(host, "localhost") || net.ParseIP(host) != nil
}

func validURLAuthority(authority string) bool {
	parsed, err := url.Parse("https://" + authority)
	if err != nil || parsed.Host != authority || parsed.Hostname() == "" || parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return false
	}
	if port := parsed.Port(); port != "" {
		value, err := strconv.Atoi(port)
		return err == nil && validPort(value)
	}
	return true
}

func (c Config) FrontendEndpoint() (network, address string) {
	if c.Server.UnixSocketEnabled {
		return "unix", c.Server.FrontendSocket
	}
	return "tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(c.Server.LocalPort))
}

func (c Config) UpstreamEndpoint() (network, address string) {
	if c.UpstreamNginx.UpstreamSocketEnabled {
		return "unix", c.UpstreamNginx.UpstreamSocket
	}
	return "tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(c.UpstreamNginx.UpstreamLocalPort))
}

func (c Config) AdminAPIPath() string {
	return c.Admin.Path + "api/v1/"
}

func (c *Config) NormalizeRuntime() error {
	adminPath, err := normalizeAdminPath(c.Admin.Path)
	if err != nil {
		return err
	}
	c.Admin.Path = adminPath
	c.Server.FrontendSocketMode, err = parseSocketMode(c.Server.FrontendSocketModeText)
	if err != nil {
		return err
	}
	c.UpstreamNginx.UpstreamSocketMode, err = parseSocketMode(c.UpstreamNginx.UpstreamSocketModeText)
	return err
}

func normalizeAdminPath(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || !strings.HasPrefix(value, "/") || value == "/" || len(value) > 256 ||
		strings.Contains(value, "//") || strings.ContainsAny(value, "\\%?#\x00\r\n\t ") {
		return "", errors.New("admin.path must be an absolute URL path with safe segments")
	}
	segments := strings.Split(strings.Trim(value, "/"), "/")
	for _, segment := range segments {
		if segment == "" || segment == "." || segment == ".." || len(segment) > 64 {
			return "", errors.New("admin.path contains an invalid segment")
		}
		for _, character := range segment {
			if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') ||
				(character >= '0' && character <= '9') || strings.ContainsRune("._~-", character) {
				continue
			}
			return "", errors.New("admin.path contains an unsafe character")
		}
	}
	first := strings.ToLower(segments[0])
	if first == "healthz" || first == "metrics" || first == "_mirror_auth" || first == "_mirrorrelay" {
		return "", errors.New("admin.path conflicts with a reserved system path")
	}
	return "/" + strings.Join(segments, "/") + "/", nil
}

func EnsureDirectories(c Config) error {
	snippetDirectory := c.Ingress.SnippetPath
	if strings.EqualFold(filepath.Ext(snippetDirectory), ".conf") {
		snippetDirectory = filepath.Dir(snippetDirectory)
	}
	directories := []string{filepath.Dir(c.Database.Path), c.Cache.Path, c.Logging.Path, c.UpstreamNginx.Prefix, c.UpstreamNginx.LogPath, c.Runtime.Root, c.Runtime.RunDir, snippetDirectory, filepath.Dir(c.UpstreamNginx.PID)}
	if c.Ingress.Mode == "managed-standalone" {
		directories = append(directories, filepath.Dir(c.TLS.Certificate), filepath.Dir(c.TLS.PrivateKey))
	}
	if c.Server.UnixSocketEnabled {
		directories = append(directories, filepath.Dir(c.Server.FrontendSocket))
	}
	if c.UpstreamNginx.UpstreamSocketEnabled {
		directories = append(directories, filepath.Dir(c.UpstreamNginx.UpstreamSocket))
	}
	for _, dir := range directories {
		if dir == "." || strings.TrimSpace(dir) == "" {
			continue
		}
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return err
		}
	}
	return nil
}
