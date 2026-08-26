// Package model defines the data structures used across the application.
package model

import (
	"regexp"
	"time"
)

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
	BlockedPackages    []string          `json:"blocked_packages,omitempty" yaml:"blocked_packages,omitempty"`
	AllowedPackages    []string          `json:"allowed_packages,omitempty" yaml:"allowed_packages,omitempty"`
	PackagePolicy      *PackagePolicy    `json:"-" yaml:"-"`
	Help               HelpConfig        `json:"help"`
	CreatedAt          time.Time         `json:"created_at"`
	UpdatedAt          time.Time         `json:"updated_at"`
	Upstreams          []Upstream        `json:"upstreams"`
}

type PackagePattern struct {
	Pattern string
	Glob    bool
	Regexp  *regexp.Regexp
}

type PackagePolicy struct {
	Blocked []PackagePattern
	Allowed []PackagePattern
	Invalid string
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

type SettingVersion struct {
	ID          int64     `json:"id"`
	Version     int64     `json:"version"`
	CreatedAt   time.Time `json:"created_at"`
	Operator    string    `json:"operator"`
	Source      string    `json:"source"`
	Description string    `json:"description"`
	DiffSummary string    `json:"diff_summary"`
	Settings    string    `json:"settings,omitempty"`
}

type SettingDiffEntry struct {
	Path     string `json:"path"`
	OldValue string `json:"old_value"`
	NewValue string `json:"new_value"`
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
	ID                    int64     `json:"id"`
	Username              string    `json:"username"`
	PasswordHash          string    `json:"-"`
	Role                  string    `json:"role"`
	PasswordLoginDisabled bool      `json:"password_login_disabled"`
	CreatedAt             time.Time `json:"created_at"`
	UpdatedAt             time.Time `json:"updated_at"`
}

type PasskeyCredential struct {
	ID             int64      `json:"id"`
	UserID         int64      `json:"user_id"`
	CredentialID   string     `json:"credential_id"`
	PublicKey      string     `json:"public_key"`
	SignCount      uint32     `json:"sign_count"`
	AAGUID         string     `json:"aaguid,omitempty"`
	Transports     []string   `json:"transports,omitempty"`
	BackupEligible bool       `json:"backup_eligible"`
	BackupState    bool       `json:"backup_state"`
	DisplayName    string     `json:"display_name"`
	CreatedAt      time.Time  `json:"created_at"`
	LastUsedAt     *time.Time `json:"last_used_at,omitempty"`
}

type RecoveryCode struct {
	ID        int64      `json:"id"`
	UserID    int64      `json:"user_id"`
	CodeHash  string     `json:"-"`
	CreatedAt time.Time  `json:"created_at"`
	UsedAt    *time.Time `json:"used_at,omitempty"`
}

type PasskeyConfig struct {
	Enabled bool     `json:"enabled" yaml:"enabled"`
	RPName  string   `json:"rp_name,omitempty" yaml:"rp_name,omitempty"`
	RPID    string   `json:"rp_id,omitempty" yaml:"rp_id,omitempty"`
	Origins []string `json:"origins,omitempty" yaml:"origins,omitempty"`
}

type WebhookConfig struct {
	Enabled      bool          `json:"enabled" yaml:"enabled"`
	URL          string        `json:"url" yaml:"url"`
	Secret       string        `json:"secret,omitempty" yaml:"secret,omitempty"`
	Events       []string      `json:"events" yaml:"events"`
	Timeout      time.Duration `json:"timeout" yaml:"timeout"`
	AllowHTTP    bool          `json:"allow_http" yaml:"allow_http"`
	AllowPrivate bool          `json:"allow_private" yaml:"allow_private"`
}

type WebhookPayload struct {
	Event     string         `json:"event"`
	Timestamp time.Time      `json:"timestamp"`
	Title     string         `json:"title"`
	Message   string         `json:"message"`
	Data      map[string]any `json:"data,omitempty"`
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

type ClusterNode struct {
	ID                      int64           `json:"id"`
	Name                    string          `json:"name"`
	URL                     string          `json:"url"`
	Region                  string          `json:"region"`
	Country                 string          `json:"country,omitempty"`
	Priority                int             `json:"priority"`
	Weight                  int             `json:"weight"`
	Enabled                 bool            `json:"enabled"`
	MutationToken           string          `json:"mutation_token,omitempty"`
	MutationTokenConfigured bool            `json:"mutation_token_configured"`
	HealthStatus            string          `json:"health_status"`
	ConfigStatus            string          `json:"config_status"`
	ConfigFingerprint       string          `json:"config_fingerprint,omitempty"`
	ConfigGeneration        int64           `json:"config_generation,omitempty"`
	NodeID                  string          `json:"node_id,omitempty"`
	CoordinatorID           string          `json:"coordinator_id,omitempty"`
	CoordinatorEpoch        string          `json:"coordinator_epoch,omitempty"`
	Version                 string          `json:"version,omitempty"`
	ProtocolVersion         int             `json:"protocol_version"`
	Capabilities            []string        `json:"capabilities,omitempty"`
	RepositoryHealth        map[string]bool `json:"repository_health,omitempty"`
	LatencyMS               int64           `json:"latency_ms,omitempty"`
	LastCheck               time.Time       `json:"last_check,omitempty"`
	LastError               string          `json:"last_error,omitempty"`
	CreatedAt               time.Time       `json:"created_at"`
	UpdatedAt               time.Time       `json:"updated_at"`
}

type ClusterManifest struct {
	ProtocolVersion    int      `json:"protocol_version"`
	MirrorRelayVersion string   `json:"mirrorrelay_version"`
	NodeID             string   `json:"node_id"`
	CoordinatorID      string   `json:"coordinator_id"`
	CoordinatorEpoch   string   `json:"coordinator_epoch"`
	ConfigGeneration   int64    `json:"config_generation"`
	ConfigFingerprint  string   `json:"config_fingerprint"`
	Capabilities       []string `json:"capabilities"`
}

type ClusterSyncRequest struct {
	Manifest      ClusterManifest `json:"manifest"`
	Repositories  []Mirror        `json:"repositories"`
	CustomConfigs []CustomConfig  `json:"custom_configs"`
}

type ClusterSyncResponse struct {
	Status             string   `json:"status"`
	Fingerprint        string   `json:"fingerprint"`
	ProtocolVersion    int      `json:"protocol_version"`
	ConfigGeneration   int64    `json:"config_generation"`
	MirrorRelayVersion string   `json:"mirrorrelay_version"`
	Capabilities       []string `json:"capabilities"`
}

type ClusterPurgeRequest struct {
	CoordinatorID    string `json:"coordinator_id"`
	CoordinatorEpoch string `json:"coordinator_epoch"`
	Scope            string `json:"scope"`
	RepositorySlug   string `json:"repository_slug,omitempty"`
	ObjectID         string `json:"object_id,omitempty"`
	ObjectPath       string `json:"object_path,omitempty"`
}

type ClusterPurgeResponse struct {
	Status          string `json:"status"`
	Scope           string `json:"scope"`
	RepositorySlug  string `json:"repository_slug,omitempty"`
	ObjectID        string `json:"object_id,omitempty"`
	Generation      int64  `json:"generation"`
	PhysicalReclaim string `json:"physical_reclaim"`
}

type ClusterHealth struct {
	Status            string          `json:"status"`
	Version           string          `json:"version"`
	ConfigGeneration  int64           `json:"config_generation"`
	ConfigFingerprint string          `json:"config_fingerprint"`
	Repositories      map[string]bool `json:"repositories,omitempty"`
}

type ClusterSyncState struct {
	CoordinatorID     string   `json:"coordinator_id"`
	CoordinatorEpoch  string   `json:"coordinator_epoch"`
	ConfigGeneration  int64    `json:"config_generation"`
	ConfigFingerprint string   `json:"config_fingerprint"`
	Status            string   `json:"status"`
	RetiredEpochs     []string `json:"retired_epochs,omitempty"`
}

type ClusterOverview struct {
	Role               string `json:"role"`
	Enabled            bool   `json:"enabled"`
	ClusterFingerprint string `json:"cluster_fingerprint,omitempty"`
	TotalNodes         int    `json:"total_nodes"`
	HealthyNodes       int    `json:"healthy_nodes"`
	RoutableNodes      int    `json:"routable_nodes"`
	RoutingMode        string `json:"routing_mode"`
}

type HelpConfig struct {
	Enabled         bool          `json:"enabled" yaml:"enabled"`
	Title           string        `json:"title,omitempty" yaml:"title,omitempty"`
	Summary         string        `json:"summary,omitempty" yaml:"summary,omitempty"`
	Template        string        `json:"template,omitempty" yaml:"template,omitempty"`
	TemplateVersion int           `json:"template_version,omitempty" yaml:"template_version,omitempty"`
	Variants        []HelpVariant `json:"variants,omitempty" yaml:"variants,omitempty"`
	Formats         []HelpFormat  `json:"formats,omitempty" yaml:"formats,omitempty"`
}

type HelpVariant struct {
	Key      string `json:"key" yaml:"key"`
	Label    string `json:"label" yaml:"label"`
	Codename string `json:"codename,omitempty" yaml:"codename,omitempty"`
	Default  bool   `json:"default,omitempty" yaml:"default,omitempty"`
}

type HelpFormat struct {
	Key       string `json:"key" yaml:"key"`
	Label     string `json:"label" yaml:"label"`
	Extension string `json:"extension,omitempty" yaml:"extension,omitempty"`
	Default   bool   `json:"default,omitempty" yaml:"default,omitempty"`
}

type UIEnhancementConfig struct {
	Enabled           bool                    `json:"enabled" yaml:"enabled"`
	Theme             string                  `json:"theme" yaml:"theme"`
	AccentColor       string                  `json:"accent_color" yaml:"accent_color"`
	Branding          BrandingConfig          `json:"branding" yaml:"branding"`
	Login             LoginBrandingConfig     `json:"login" yaml:"login"`
	CustomCSS         CustomCSSConfig         `json:"custom_css" yaml:"custom_css"`
	RepositoryBrowser RepositoryBrowserConfig `json:"repository_browser" yaml:"repository_browser"`
}

type BrandingConfig struct {
	Title   string `json:"title" yaml:"title"`
	Logo    string `json:"logo" yaml:"logo"`
	Favicon string `json:"favicon" yaml:"favicon"`
}

type LoginBrandingConfig struct {
	Title    string `json:"title" yaml:"title"`
	Subtitle string `json:"subtitle" yaml:"subtitle"`
}

type CustomCSSConfig struct {
	Enabled bool   `json:"enabled" yaml:"enabled"`
	File    string `json:"file" yaml:"file"`
}

type RepositoryBrowserConfig struct {
	Enabled bool `json:"enabled" yaml:"enabled"`
}

type WarmupConfig struct {
	Enabled        bool          `json:"enabled" yaml:"enabled"`
	MaxConcurrency int           `json:"max_concurrency" yaml:"max_concurrency"`
	BandwidthLimit int64         `json:"bandwidth_limit_bps" yaml:"bandwidth_limit_bps"`
	Timeout        time.Duration `json:"timeout" yaml:"timeout"`
	RetryCount     int           `json:"retry_count" yaml:"retry_count"`
	MetadataDepth  int           `json:"metadata_depth" yaml:"metadata_depth"`
}

type WarmupJob struct {
	ID              int64     `json:"id"`
	MirrorID        int64     `json:"mirror_id"`
	MirrorName      string    `json:"mirror_name,omitempty"`
	MirrorSlug      string    `json:"mirror_slug,omitempty"`
	Name            string    `json:"name"`
	CronExpression  string    `json:"cron_expression"`
	URLPatterns     []string  `json:"url_patterns"`
	Status          string    `json:"status"`
	TotalItems      int       `json:"total_items"`
	CompletedItems  int       `json:"completed_items"`
	FailedItems     int       `json:"failed_items"`
	BytesDownloaded int64     `json:"bytes_downloaded"`
	ErrorMessage    string    `json:"error_message,omitempty"`
	LastRunAt       string    `json:"last_run_at,omitempty"`
	NextRunAt       string    `json:"next_run_at,omitempty"`
	Enabled         bool      `json:"enabled"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}
