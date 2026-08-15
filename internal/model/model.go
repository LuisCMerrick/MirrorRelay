package model

import "time"

type Mirror struct {
	ID                 int64             `json:"id"`
	Name               string            `json:"name"`
	Slug               string            `json:"slug"`
	Type               string            `json:"type"`
	Enabled            bool              `json:"enabled"`
	Description        string            `json:"description"`
	PublicMode         string            `json:"public_mode"`
	PublicHost         string            `json:"public_host"`
	PublicPath         string            `json:"public_path"`
	ProxyMode          string            `json:"proxy_mode"`
	CacheEnabled       bool              `json:"cache_enabled"`
	CacheProfile       string            `json:"cache_profile"`
	RewriteEnabled     bool              `json:"rewrite_enabled"`
	HTMLRewriteEnabled bool              `json:"html_rewrite_enabled"`
	RewriteProfile     string            `json:"rewrite_profile"`
	RewriteHosts       []string          `json:"rewrite_hosts"`
	HealthCheckEnabled bool              `json:"health_check_enabled"`
	HealthCheckPath    string            `json:"health_check_path"`
	HealthIntervalSec  int               `json:"health_interval_sec"`
	HealthTimeoutSec   int               `json:"health_timeout_sec"`
	HealthMethod       string            `json:"health_method"`
	HealthExpected     int               `json:"health_expected"`
	RedirectMode       string            `json:"redirect_mode"`
	ProfileName        string            `json:"profile_name"`
	ProfileVersion     string            `json:"profile_version"`
	RateLimitProfile   string            `json:"rate_limit_profile"`
	AccessPolicy       string            `json:"access_policy"`
	StripPrefix        string            `json:"strip_prefix"`
	AddPrefix          string            `json:"add_prefix"`
	HostRewrite        string            `json:"host_rewrite"`
	HeaderAdd          map[string]string `json:"header_add"`
	HeaderRemove       []string          `json:"header_remove"`
	ConnectTimeoutSec  int               `json:"connect_timeout_sec"`
	ReadTimeoutSec     int               `json:"read_timeout_sec"`
	SendTimeoutSec     int               `json:"send_timeout_sec"`
	MetadataLimitBytes int64             `json:"metadata_rewrite_limit_bytes"`
	MetadataTTLSec     int               `json:"metadata_ttl_sec"`
	PackageTTLSec      int               `json:"package_ttl_sec"`
	ImmutableTTLSec    int               `json:"immutable_ttl_sec"`
	BlobTTLSec         int               `json:"blob_ttl_sec"`
	CacheAuthenticated bool              `json:"cache_authenticated"`
	AuthMode           string            `json:"auth_mode"`
	TokenUpstream      string            `json:"token_upstream"`
	BlobRedirectMode   string            `json:"blob_redirect_mode"`
	PullOnly           bool              `json:"pull_only"`
	ConfigState        string            `json:"config_state"`
	ConfigError        string            `json:"config_error,omitempty"`
	AllowHTTP          bool              `json:"allow_http_upstream"`
	AllowPrivate       bool              `json:"allow_private_upstream"`
	InsecureTLS        bool              `json:"insecure_skip_verify"`
	BandwidthLimitBPS  int64             `json:"bandwidth_limit_bps"`
	MaxConcurrency     int               `json:"max_concurrency"`
	CreatedAt          time.Time         `json:"created_at"`
	UpdatedAt          time.Time         `json:"updated_at"`
	Upstreams          []Upstream        `json:"upstreams"`
}

type Upstream struct {
	ID           int64     `json:"id"`
	MirrorID     int64     `json:"mirror_id"`
	URL          string    `json:"url"`
	Host         string    `json:"host"`
	Priority     int       `json:"priority"`
	Weight       int       `json:"weight"`
	Enabled      bool      `json:"enabled"`
	HealthStatus string    `json:"health_status"`
	LastCheck    time.Time `json:"last_check,omitempty"`
	LatencyMS    int64     `json:"latency_ms"`
	LastError    string    `json:"last_error,omitempty"`
}

type ConfigVersion struct {
	ID                int64     `json:"id"`
	Version           int64     `json:"version"`
	CreatedAt         time.Time `json:"created_at"`
	Operator          string    `json:"operator"`
	Description       string    `json:"description"`
	ConfigurationHash string    `json:"configuration_hash"`
	ValidationOK      bool      `json:"validation_ok"`
	ValidationResult  string    `json:"validation_result"`
	Active            bool      `json:"active"`
	Snapshot          string    `json:"-"`
	Configuration     string    `json:"configuration,omitempty"`
}

type CustomConfig struct {
	ID           int64     `json:"id"`
	Name         string    `json:"name"`
	Context      string    `json:"context"`
	RepositoryID int64     `json:"repository_id,omitempty"`
	Enabled      bool      `json:"enabled"`
	Content      string    `json:"content"`
	LastResult   string    `json:"last_validation_result"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type User struct {
	ID           int64     `json:"id"`
	Username     string    `json:"username"`
	PasswordHash string    `json:"-"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type AuditEntry struct {
	ID        int64     `json:"id"`
	Time      time.Time `json:"time"`
	Username  string    `json:"username"`
	ClientIP  string    `json:"client_ip"`
	Action    string    `json:"action"`
	Object    string    `json:"object"`
	Detail    string    `json:"detail"`
	Succeeded bool      `json:"succeeded"`
}

type CacheState struct {
	GlobalGeneration     int64           `json:"global_generation"`
	RepositoryGeneration map[int64]int64 `json:"repository_generation"`
	LogicalPurge         string          `json:"logical_purge"`
	PhysicalReclaim      string          `json:"physical_reclaim"`
}

type PurgeJob struct {
	ID             int64     `json:"id"`
	Scope          string    `json:"scope"`
	RepositoryID   int64     `json:"repository_id,omitempty"`
	ObjectID       string    `json:"object_id,omitempty"`
	OldGeneration  int64     `json:"old_generation"`
	NewGeneration  int64     `json:"new_generation"`
	ReclaimState   string    `json:"reclaim_state"`
	ReclaimedBytes int64     `json:"reclaimed_bytes"`
	Error          string    `json:"error,omitempty"`
	Operator       string    `json:"operator"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type CacheGeneration struct {
	Scope        string
	RepositoryID int64
	ObjectID     string
	Generation   int64
}
