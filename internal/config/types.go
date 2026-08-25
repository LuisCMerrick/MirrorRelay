package config

import (
	"os"
	"time"

	"github.com/LuisCMerrick/MirrorRelay/internal/model"
)

type Config struct {
	Server        ServerConfig              `yaml:"server"`
	Runtime       RuntimeConfig             `yaml:"runtime"`
	Ingress       IngressConfig             `yaml:"ingress"`
	Performance   PerformanceConfig         `yaml:"performance"`
	Metadata      MetadataConfig            `yaml:"metadata"`
	Redirect      RedirectConfig            `yaml:"redirect"`
	HTTP          HTTPConfig                `yaml:"http"`
	TLS           TLSConfig                 `yaml:"tls"`
	Database      DatabaseConfig            `yaml:"database"`
	Cache         CacheConfig               `yaml:"cache"`
	Logging       LoggingConfig             `yaml:"logging"`
	Security      SecurityConfig            `yaml:"security"`
	Transport     TransportConfig           `yaml:"transport"`
	Limits        LimitsConfig              `yaml:"limits"`
	Health        HealthConfig              `yaml:"health"`
	Admin         AdminConfig               `yaml:"admin"`
	Shutdown      ShutdownConfig            `yaml:"shutdown"`
	UpstreamNginx UpstreamNginxConfig       `yaml:"upstream_nginx"`
	Distributed   DistributedConfig         `yaml:"distributed"`
	UIEnhancement model.UIEnhancementConfig `yaml:"ui_enhancement"`
	Webhook       model.WebhookConfig       `yaml:"webhook"`
	Warmup        model.WarmupConfig        `yaml:"warmup"`
}

type ServerConfig struct {
	UnixSocketEnabled      bool        `yaml:"unix_socket_enabled"`
	FrontendSocket         string      `yaml:"frontend_socket"`
	FrontendSocketMode     os.FileMode `yaml:"-"`
	FrontendSocketModeText string      `yaml:"frontend_socket_mode"`
	LocalAddress           string      `yaml:"local_address"`
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
	ZeroCopyBypass   bool  `yaml:"zero_copy_bypass"`
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
	TrustedProxyCIDRs    []string      `yaml:"trusted_proxy_cidrs"`
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
	Host string `yaml:"host"`
	Path string `yaml:"path"`
}

type ShutdownConfig struct {
	GracePeriod time.Duration `yaml:"grace_period"`
}

type DistributedConfig struct {
	Enabled               bool                     `yaml:"enabled"`
	Role                  string                   `yaml:"role"`
	Token                 string                   `yaml:"token"`
	MutationToken         string                   `yaml:"mutation_token"`
	MutationTokenKeyFiles []string                 `yaml:"mutation_token_key_files"`
	CoordinatorID         string                   `yaml:"coordinator_id"`
	AllowHTTP             bool                     `yaml:"allow_http"`
	Node                  DistributedNodeConfig    `yaml:"node"`
	Routing               DistributedRoutingConfig `yaml:"routing"`
	HealthCheck           DistributedHealthConfig  `yaml:"health_check"`
	Nodes                 []DistributedNodeSeed    `yaml:"nodes"`
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
	Name          string `yaml:"name"`
	URL           string `yaml:"url"`
	MutationToken string `yaml:"mutation_token"`
	Region        string `yaml:"region"`
	Country       string `yaml:"country"`
	Priority      int    `yaml:"priority"`
	Weight        int    `yaml:"weight"`
	Enabled       bool   `yaml:"enabled"`
}
