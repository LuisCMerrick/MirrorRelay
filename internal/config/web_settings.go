package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/LuisCMerrick/MirrorRelay/internal/model"
	"gopkg.in/yaml.v3"
)

const WebSettingsKey = "web_settings_v1"

type WebSettings struct {
	Server        WebServerSettings         `json:"server"`
	Runtime       WebRuntimeSettings        `json:"runtime"`
	Ingress       WebIngressSettings        `json:"ingress"`
	Performance   WebPerformanceSettings    `json:"performance"`
	Metadata      WebMetadataSettings       `json:"metadata"`
	Redirect      WebRedirectSettings       `json:"redirect"`
	HTTP          WebHTTPSettings           `json:"http"`
	TLS           WebTLSSettings            `json:"tls"`
	Database      WebDatabaseSettings       `json:"database"`
	Cache         WebCacheSettings          `json:"cache"`
	Logging       WebLoggingSettings        `json:"logging"`
	Security      WebSecuritySettings       `json:"security"`
	Transport     WebTransportSettings      `json:"transport"`
	Limits        WebLimitSettings          `json:"limits"`
	Health        WebHealthSettings         `json:"health"`
	Admin         WebAdminSettings          `json:"admin"`
	Shutdown      WebShutdownSettings       `json:"shutdown"`
	UpstreamNginx WebUpstreamNginxSettings  `json:"upstream_nginx"`
	Distributed   WebDistributedSettings    `json:"distributed"`
	UIEnhancement model.UIEnhancementConfig `json:"ui_enhancement"`
	Webhook       *WebWebhookSettings       `json:"webhook,omitempty"`
	Warmup        WebWarmupSettings         `json:"warmup"`
}

type WebServerSettings struct {
	UnixSocketEnabled      bool   `json:"unix_socket_enabled"`
	FrontendSocket         string `json:"frontend_socket"`
	FrontendSocketModeText string `json:"frontend_socket_mode"`
	LocalAddress           string `json:"local_address"`
	LocalPort              int    `json:"local_port"`
}

type WebRuntimeSettings struct {
	Root   string `json:"root"`
	RunDir string `json:"run_dir"`
}

type WebIngressSettings struct {
	Mode            string `json:"mode"`
	GenerateSnippet bool   `json:"generate_snippet"`
	SnippetPath     string `json:"snippet_path"`
}

type WebPerformanceSettings struct {
	StreamBufferSize int   `json:"stream_buffer_size_bytes"`
	GoMemoryLimit    int64 `json:"go_memory_limit_bytes"`
	GOGC             int   `json:"gogc"`
	ZeroCopyBypass   bool  `json:"zero_copy_bypass"`
}

type WebMetadataSettings struct {
	RewriteBufferLimit int64  `json:"rewrite_buffer_limit_bytes"`
	OutputCompression  string `json:"output_compression"`
	GzipMinLength      int    `json:"gzip_min_length_bytes"`
	ValidatorEntries   int    `json:"validator_entries"`
}

type WebRedirectSettings struct {
	MaxHops           int  `json:"max_hops"`
	PinValidatedIP    bool `json:"pin_validated_ip"`
	RejectMixedResult bool `json:"reject_mixed_dns_result"`
}

type WebHTTPSettings struct {
	Listen        string `json:"listen"`
	HTTPSListen   string `json:"https_listen"`
	PublicBaseURL string `json:"public_base_url"`
	ReadTimeout   string `json:"read_timeout"`
	WriteTimeout  string `json:"write_timeout"`
	IdleTimeout   string `json:"idle_timeout"`
}

type WebTLSSettings struct {
	Certificate string `json:"certificate"`
	PrivateKey  string `json:"private_key"`
	MinVersion  string `json:"min_version"`
}

type WebDatabaseSettings struct {
	Path string `json:"path"`
}

type WebCacheSettings struct {
	Path             string `json:"path"`
	MaxSizeBytes     int64  `json:"max_size_bytes"`
	MaxFiles         int    `json:"max_files"`
	Inactive         string `json:"inactive"`
	MetadataTTL      string `json:"metadata_ttl"`
	PackageTTL       string `json:"package_ttl"`
	CleanupInterval  string `json:"cleanup_interval"`
	WaitForFill      string `json:"wait_for_fill"`
	MinimumFreeBytes int64  `json:"minimum_free_bytes"`
}

type WebLoggingSettings struct {
	Path      string `json:"path"`
	QueueSize int    `json:"queue_size"`
	MaxSizeMB int64  `json:"max_size_mb"`
	KeepDays  int    `json:"keep_days"`
}

type WebSecuritySettings struct {
	AllowHTTPUpstream    bool     `json:"allow_http_upstream"`
	AllowPrivateUpstream bool     `json:"allow_private_upstream"`
	ExposeClientIP       bool     `json:"expose_client_ip"`
	TrustedProxyCIDRs    []string `json:"trusted_proxy_cidrs"`
	SessionTimeout       string   `json:"session_timeout"`
	LoginWindow          string   `json:"login_window"`
	LoginMaxFailures     int      `json:"login_max_failures"`
	AdminCIDRs           []string `json:"admin_cidrs"`
}

type WebTransportSettings struct {
	DialTimeout           string `json:"dial_timeout"`
	KeepAlive             string `json:"keep_alive"`
	TLSHandshakeTimeout   string `json:"tls_handshake_timeout"`
	ResponseHeaderTimeout string `json:"response_header_timeout"`
	IdleConnTimeout       string `json:"idle_connection_timeout"`
	MaxIdleConns          int    `json:"max_idle_connections"`
	MaxIdleConnsPerHost   int    `json:"max_idle_connections_per_host"`
}

type WebLimitSettings struct {
	MaxTotalConcurrency int   `json:"max_total_concurrency"`
	MaxIPConcurrency    int   `json:"max_ip_concurrency"`
	BandwidthLimitBPS   int64 `json:"bandwidth_limit_bps"`
}

type WebHealthSettings struct {
	WorkerInterval string `json:"worker_interval"`
}

type WebAdminSettings struct {
	Host string `json:"host"`
	Path string `json:"path"`
}

type WebShutdownSettings struct {
	GracePeriod string `json:"grace_period"`
}

type WebUpstreamNginxSettings struct {
	Mode                   string `json:"mode"`
	Binary                 string `json:"binary"`
	Prefix                 string `json:"prefix"`
	PID                    string `json:"pid"`
	LogPath                string `json:"log_path"`
	UpstreamSocketEnabled  bool   `json:"upstream_unix_socket_enabled"`
	UpstreamSocket         string `json:"upstream_socket"`
	UpstreamSocketModeText string `json:"upstream_socket_mode"`
	UpstreamLocalPort      int    `json:"upstream_local_port"`
	CABundle               string `json:"ca_bundle"`
	TLSVerifyDepth         int    `json:"tls_verify_depth"`
	Resolver               string `json:"resolver"`
	ResolverRefresh        string `json:"resolver_refresh"`
	HistoryLimit           int    `json:"history_limit"`
	RestartMaxFailures     int    `json:"restart_max_failures"`
	RestartWindow          string `json:"restart_window"`
	RestartInitialBackoff  string `json:"restart_initial_backoff"`
	RestartMaxBackoff      string `json:"restart_max_backoff"`
	WorkerProcesses        string `json:"worker_processes"`
	WorkerUser             string `json:"worker_user"`
	WorkerConnections      int    `json:"worker_connections"`
	StopOnMirrorRelayExit  bool   `json:"stop_on_mirrorrelay_exit"`
}

type WebDistributedSettings struct {
	Enabled                 bool                         `json:"enabled"`
	Role                    string                       `json:"role"`
	Token                   string                       `json:"token,omitempty"`
	TokenConfigured         bool                         `json:"token_configured,omitempty"`
	MutationToken           string                       `json:"mutation_token,omitempty"`
	MutationTokenConfigured bool                         `json:"mutation_token_configured,omitempty"`
	MutationTokenKeyFiles   []string                     `json:"mutation_token_key_files"`
	CoordinatorID           string                       `json:"coordinator_id"`
	AllowHTTP               bool                         `json:"allow_http"`
	Node                    DistributedNodeConfig        `json:"node"`
	Routing                 DistributedRoutingConfig     `json:"routing"`
	HealthCheck             WebDistributedHealthSettings `json:"health_check"`
	Nodes                   []DistributedNodeSeed        `json:"nodes"`
}

type WebDistributedHealthSettings struct {
	Interval           string `json:"interval"`
	Timeout            string `json:"timeout"`
	UnhealthyThreshold int    `json:"unhealthy_threshold"`
	HealthyThreshold   int    `json:"healthy_threshold"`
}

type WebWebhookSettings struct {
	Enabled          bool     `json:"enabled"`
	URL              string   `json:"url"`
	Secret           string   `json:"secret,omitempty"`
	SecretConfigured bool     `json:"secret_configured,omitempty"`
	Events           []string `json:"events"`
	Timeout          string   `json:"timeout"`
	AllowHTTP        bool     `json:"allow_http"`
	AllowPrivate     bool     `json:"allow_private"`
}

type WebWarmupSettings struct {
	Enabled        bool   `json:"enabled"`
	MaxConcurrency int    `json:"max_concurrency"`
	BandwidthLimit int64  `json:"bandwidth_limit_bps"`
	Timeout        string `json:"timeout"`
	RetryCount     int    `json:"retry_count"`
	MetadataDepth  int    `json:"metadata_depth"`
}

func cloneRegions(regions []RegionMapping) []RegionMapping {
	if regions == nil {
		return []RegionMapping{}
	}
	out := make([]RegionMapping, len(regions))
	for i, r := range regions {
		out[i] = RegionMapping{
			Code:      r.Code,
			Countries: append([]string{}, r.Countries...),
		}
	}
	return out
}

func WebSettingsFrom(c Config) WebSettings {
	return WebSettings{
		Server: WebServerSettings{
			UnixSocketEnabled:      c.Server.UnixSocketEnabled,
			FrontendSocket:         c.Server.FrontendSocket,
			FrontendSocketModeText: c.Server.FrontendSocketModeText,
			LocalAddress:           c.Server.LocalAddress,
			LocalPort:              c.Server.LocalPort,
		},
		Runtime: WebRuntimeSettings{
			Root:   c.Runtime.Root,
			RunDir: c.Runtime.RunDir,
		},
		Ingress: WebIngressSettings{
			Mode:            c.Ingress.Mode,
			GenerateSnippet: c.Ingress.GenerateSnippet,
			SnippetPath:     c.Ingress.SnippetPath,
		},
		Performance: WebPerformanceSettings{
			StreamBufferSize: c.Performance.StreamBufferSize,
			GoMemoryLimit:    c.Performance.GoMemoryLimit,
			GOGC:             c.Performance.GOGC,
			ZeroCopyBypass:   c.Performance.ZeroCopyBypass,
		},
		Metadata: WebMetadataSettings{
			RewriteBufferLimit: c.Metadata.RewriteBufferLimit,
			OutputCompression:  c.Metadata.OutputCompression,
			GzipMinLength:      c.Metadata.GzipMinLength,
			ValidatorEntries:   c.Metadata.ValidatorEntries,
		},
		Redirect: WebRedirectSettings{
			MaxHops:           c.Redirect.MaxHops,
			PinValidatedIP:    c.Redirect.PinValidatedIP,
			RejectMixedResult: c.Redirect.RejectMixedResult,
		},
		HTTP: WebHTTPSettings{
			Listen:        c.HTTP.Listen,
			HTTPSListen:   c.HTTP.HTTPSListen,
			PublicBaseURL: c.HTTP.PublicBaseURL,
			ReadTimeout:   c.HTTP.ReadTimeout.String(),
			WriteTimeout:  c.HTTP.WriteTimeout.String(),
			IdleTimeout:   c.HTTP.IdleTimeout.String(),
		},
		TLS: WebTLSSettings{
			Certificate: c.TLS.Certificate,
			PrivateKey:  c.TLS.PrivateKey,
			MinVersion:  c.TLS.MinVersion,
		},
		Database: WebDatabaseSettings{
			Path: c.Database.Path,
		},
		Cache: WebCacheSettings{
			Path:             c.Cache.Path,
			MaxSizeBytes:     c.Cache.MaxSizeBytes,
			MaxFiles:         c.Cache.MaxFiles,
			Inactive:         c.Cache.Inactive.String(),
			MetadataTTL:      c.Cache.MetadataTTL.String(),
			PackageTTL:       c.Cache.PackageTTL.String(),
			CleanupInterval:  c.Cache.CleanupInterval.String(),
			WaitForFill:      c.Cache.WaitForFill.String(),
			MinimumFreeBytes: c.Cache.MinimumFreeBytes,
		},
		Logging: WebLoggingSettings{
			Path:      c.Logging.Path,
			QueueSize: c.Logging.QueueSize,
			MaxSizeMB: c.Logging.MaxSizeMB,
			KeepDays:  c.Logging.KeepDays,
		},
		Security: WebSecuritySettings{
			AllowHTTPUpstream:    c.Security.AllowHTTPUpstream,
			AllowPrivateUpstream: c.Security.AllowPrivateUpstream,
			ExposeClientIP:       c.Security.ExposeClientIP,
			TrustedProxyCIDRs:    append([]string{}, c.Security.TrustedProxyCIDRs...),
			SessionTimeout:       c.Security.SessionTimeout.String(),
			LoginWindow:          c.Security.LoginWindow.String(),
			LoginMaxFailures:     c.Security.LoginMaxFailures,
			AdminCIDRs:           append([]string{}, c.Security.AdminCIDRs...),
		},
		Transport: WebTransportSettings{
			DialTimeout:           c.Transport.DialTimeout.String(),
			KeepAlive:             c.Transport.KeepAlive.String(),
			TLSHandshakeTimeout:   c.Transport.TLSHandshakeTimeout.String(),
			ResponseHeaderTimeout: c.Transport.ResponseHeaderTimeout.String(),
			IdleConnTimeout:       c.Transport.IdleConnTimeout.String(),
			MaxIdleConns:          c.Transport.MaxIdleConns,
			MaxIdleConnsPerHost:   c.Transport.MaxIdleConnsPerHost,
		},
		Limits: WebLimitSettings{
			MaxTotalConcurrency: c.Limits.MaxTotalConcurrency,
			MaxIPConcurrency:    c.Limits.MaxIPConcurrency,
			BandwidthLimitBPS:   c.Limits.BandwidthLimitBPS,
		},
		Health: WebHealthSettings{
			WorkerInterval: c.Health.WorkerInterval.String(),
		},
		Admin: WebAdminSettings{
			Host: c.Admin.Host,
			Path: c.Admin.Path,
		},
		Shutdown: WebShutdownSettings{
			GracePeriod: c.Shutdown.GracePeriod.String(),
		},
		UpstreamNginx: WebUpstreamNginxSettings{
			Mode:                   c.UpstreamNginx.Mode,
			Binary:                 c.UpstreamNginx.Binary,
			Prefix:                 c.UpstreamNginx.Prefix,
			PID:                    c.UpstreamNginx.PID,
			LogPath:                c.UpstreamNginx.LogPath,
			UpstreamSocketEnabled:  c.UpstreamNginx.UpstreamSocketEnabled,
			UpstreamSocket:         c.UpstreamNginx.UpstreamSocket,
			UpstreamSocketModeText: c.UpstreamNginx.UpstreamSocketModeText,
			UpstreamLocalPort:      c.UpstreamNginx.UpstreamLocalPort,
			CABundle:               c.UpstreamNginx.CABundle,
			TLSVerifyDepth:         c.UpstreamNginx.TLSVerifyDepth,
			Resolver:               c.UpstreamNginx.Resolver,
			ResolverRefresh:        c.UpstreamNginx.ResolverRefresh.String(),
			HistoryLimit:           c.UpstreamNginx.HistoryLimit,
			RestartMaxFailures:     c.UpstreamNginx.RestartMaxFailures,
			RestartWindow:          c.UpstreamNginx.RestartWindow.String(),
			RestartInitialBackoff:  c.UpstreamNginx.RestartInitialBackoff.String(),
			RestartMaxBackoff:      c.UpstreamNginx.RestartMaxBackoff.String(),
			WorkerProcesses:        c.UpstreamNginx.WorkerProcesses,
			WorkerUser:             c.UpstreamNginx.WorkerUser,
			WorkerConnections:      c.UpstreamNginx.WorkerConnections,
			StopOnMirrorRelayExit:  c.UpstreamNginx.StopOnMirrorRelayExit,
		},
		Distributed: WebDistributedSettings{
			Enabled:                 c.Distributed.Enabled,
			Role:                    c.Distributed.Role,
			Token:                   c.Distributed.Token,
			TokenConfigured:         c.Distributed.Token != "",
			MutationToken:           c.Distributed.MutationToken,
			MutationTokenConfigured: c.Distributed.MutationToken != "",
			MutationTokenKeyFiles:   append([]string{}, c.Distributed.MutationTokenKeyFiles...),
			CoordinatorID:           c.Distributed.CoordinatorID,
			AllowHTTP:               c.Distributed.AllowHTTP,
			Node:                    c.Distributed.Node,
			Routing: DistributedRoutingConfig{
				Mode:           c.Distributed.Routing.Mode,
				ClientNetworks: append([]ClientNetworkMapping{}, c.Distributed.Routing.ClientNetworks...),
				Regions:        cloneRegions(c.Distributed.Routing.Regions),
			},
			HealthCheck: WebDistributedHealthSettings{
				Interval:           c.Distributed.HealthCheck.Interval.String(),
				Timeout:            c.Distributed.HealthCheck.Timeout.String(),
				UnhealthyThreshold: c.Distributed.HealthCheck.UnhealthyThreshold,
				HealthyThreshold:   c.Distributed.HealthCheck.HealthyThreshold,
			},
			Nodes: append([]DistributedNodeSeed{}, c.Distributed.Nodes...),
		},
		UIEnhancement: c.UIEnhancement,
		Webhook: &WebWebhookSettings{
			Enabled:          c.Webhook.Enabled,
			URL:              c.Webhook.URL,
			Secret:           c.Webhook.Secret,
			SecretConfigured: c.Webhook.Secret != "",
			Events:           append([]string{}, c.Webhook.Events...),
			Timeout:          c.Webhook.Timeout.String(),
			AllowHTTP:        c.Webhook.AllowHTTP,
			AllowPrivate:     c.Webhook.AllowPrivate,
		},
		Warmup: WebWarmupSettings{
			Enabled:        c.Warmup.Enabled,
			MaxConcurrency: c.Warmup.MaxConcurrency,
			BandwidthLimit: c.Warmup.BandwidthLimit,
			Timeout:        c.Warmup.Timeout.String(),
			RetryCount:     c.Warmup.RetryCount,
			MetadataDepth:  c.Warmup.MetadataDepth,
		},
	}
}

func mergeNodeSeeds(existing, input []DistributedNodeSeed) []DistributedNodeSeed {
	out := make([]DistributedNodeSeed, len(input))
	for i, seed := range input {
		out[i] = seed
		if seed.MutationToken == "" || seed.MutationToken == "[REDACTED]" {
			for _, prev := range existing {
				if prev.URL == seed.URL || prev.Name == seed.Name {
					out[i].MutationToken = prev.MutationToken
					break
				}
			}
		}
	}
	return out
}

func (w WebSettings) Apply(base Config) (Config, error) {
	candidate := base

	// Server
	candidate.Server.UnixSocketEnabled = w.Server.UnixSocketEnabled
	candidate.Server.LocalPort = w.Server.LocalPort
	if w.Server.FrontendSocket != "" {
		candidate.Server.FrontendSocket = w.Server.FrontendSocket
	}
	if w.Server.FrontendSocketModeText != "" {
		candidate.Server.FrontendSocketModeText = w.Server.FrontendSocketModeText
	}
	if w.Server.LocalAddress != "" {
		candidate.Server.LocalAddress = w.Server.LocalAddress
	}

	// Runtime
	if w.Runtime.Root != "" {
		candidate.Runtime.Root = w.Runtime.Root
	}
	if w.Runtime.RunDir != "" {
		candidate.Runtime.RunDir = w.Runtime.RunDir
	}

	// Ingress
	if w.Ingress.Mode != "" {
		candidate.Ingress.Mode = w.Ingress.Mode
	}
	candidate.Ingress.GenerateSnippet = w.Ingress.GenerateSnippet
	if w.Ingress.SnippetPath != "" {
		candidate.Ingress.SnippetPath = w.Ingress.SnippetPath
	}

	// Performance
	candidate.Performance.StreamBufferSize = w.Performance.StreamBufferSize
	candidate.Performance.GoMemoryLimit = w.Performance.GoMemoryLimit
	candidate.Performance.GOGC = w.Performance.GOGC
	candidate.Performance.ZeroCopyBypass = w.Performance.ZeroCopyBypass

	// Metadata
	candidate.Metadata.RewriteBufferLimit = w.Metadata.RewriteBufferLimit
	candidate.Metadata.OutputCompression = w.Metadata.OutputCompression
	candidate.Metadata.GzipMinLength = w.Metadata.GzipMinLength
	candidate.Metadata.ValidatorEntries = w.Metadata.ValidatorEntries

	// Redirect
	candidate.Redirect.MaxHops = w.Redirect.MaxHops
	candidate.Redirect.PinValidatedIP = w.Redirect.PinValidatedIP
	candidate.Redirect.RejectMixedResult = w.Redirect.RejectMixedResult

	// HTTP
	candidate.HTTP.Listen = w.HTTP.Listen
	candidate.HTTP.HTTPSListen = w.HTTP.HTTPSListen
	candidate.HTTP.PublicBaseURL = w.HTTP.PublicBaseURL

	// TLS
	if w.TLS.Certificate != "" {
		candidate.TLS.Certificate = w.TLS.Certificate
	}
	if w.TLS.PrivateKey != "" {
		candidate.TLS.PrivateKey = w.TLS.PrivateKey
	}
	if w.TLS.MinVersion != "" {
		candidate.TLS.MinVersion = w.TLS.MinVersion
	}

	// Database
	if w.Database.Path != "" {
		candidate.Database.Path = w.Database.Path
	}

	// Cache
	if w.Cache.Path != "" {
		candidate.Cache.Path = w.Cache.Path
	}
	candidate.Cache.MaxSizeBytes = w.Cache.MaxSizeBytes
	candidate.Cache.MaxFiles = w.Cache.MaxFiles
	candidate.Cache.MinimumFreeBytes = w.Cache.MinimumFreeBytes

	// Logging
	if w.Logging.Path != "" {
		candidate.Logging.Path = w.Logging.Path
	}
	candidate.Logging.QueueSize = w.Logging.QueueSize
	candidate.Logging.MaxSizeMB = w.Logging.MaxSizeMB
	candidate.Logging.KeepDays = w.Logging.KeepDays

	// Security
	candidate.Security.AllowHTTPUpstream = w.Security.AllowHTTPUpstream
	candidate.Security.AllowPrivateUpstream = w.Security.AllowPrivateUpstream
	candidate.Security.ExposeClientIP = w.Security.ExposeClientIP
	candidate.Security.LoginMaxFailures = w.Security.LoginMaxFailures
	if w.Security.TrustedProxyCIDRs != nil {
		candidate.Security.TrustedProxyCIDRs = append([]string{}, w.Security.TrustedProxyCIDRs...)
	}
	if w.Security.AdminCIDRs != nil {
		candidate.Security.AdminCIDRs = append([]string{}, w.Security.AdminCIDRs...)
	}

	// Transport
	candidate.Transport.MaxIdleConns = w.Transport.MaxIdleConns
	candidate.Transport.MaxIdleConnsPerHost = w.Transport.MaxIdleConnsPerHost

	// Limits
	candidate.Limits.MaxTotalConcurrency = w.Limits.MaxTotalConcurrency
	candidate.Limits.MaxIPConcurrency = w.Limits.MaxIPConcurrency
	candidate.Limits.BandwidthLimitBPS = w.Limits.BandwidthLimitBPS

	// Admin
	candidate.Admin.Host = w.Admin.Host
	if w.Admin.Path != "" {
		candidate.Admin.Path = w.Admin.Path
	}

	// Upstream Nginx
	if w.UpstreamNginx.Mode != "" {
		candidate.UpstreamNginx.Mode = w.UpstreamNginx.Mode
	}
	if w.UpstreamNginx.Binary != "" {
		candidate.UpstreamNginx.Binary = w.UpstreamNginx.Binary
	}
	if w.UpstreamNginx.Prefix != "" {
		candidate.UpstreamNginx.Prefix = w.UpstreamNginx.Prefix
	}
	if w.UpstreamNginx.PID != "" {
		candidate.UpstreamNginx.PID = w.UpstreamNginx.PID
	}
	if w.UpstreamNginx.LogPath != "" {
		candidate.UpstreamNginx.LogPath = w.UpstreamNginx.LogPath
	}
	candidate.UpstreamNginx.UpstreamSocketEnabled = w.UpstreamNginx.UpstreamSocketEnabled
	if w.UpstreamNginx.UpstreamSocket != "" {
		candidate.UpstreamNginx.UpstreamSocket = w.UpstreamNginx.UpstreamSocket
	}
	if w.UpstreamNginx.UpstreamSocketModeText != "" {
		candidate.UpstreamNginx.UpstreamSocketModeText = w.UpstreamNginx.UpstreamSocketModeText
	}
	candidate.UpstreamNginx.UpstreamLocalPort = w.UpstreamNginx.UpstreamLocalPort
	if w.UpstreamNginx.CABundle != "" {
		candidate.UpstreamNginx.CABundle = w.UpstreamNginx.CABundle
	}
	candidate.UpstreamNginx.TLSVerifyDepth = w.UpstreamNginx.TLSVerifyDepth
	candidate.UpstreamNginx.Resolver = w.UpstreamNginx.Resolver
	candidate.UpstreamNginx.HistoryLimit = w.UpstreamNginx.HistoryLimit
	candidate.UpstreamNginx.RestartMaxFailures = w.UpstreamNginx.RestartMaxFailures
	candidate.UpstreamNginx.WorkerProcesses = w.UpstreamNginx.WorkerProcesses
	candidate.UpstreamNginx.WorkerUser = w.UpstreamNginx.WorkerUser
	candidate.UpstreamNginx.WorkerConnections = w.UpstreamNginx.WorkerConnections
	candidate.UpstreamNginx.StopOnMirrorRelayExit = w.UpstreamNginx.StopOnMirrorRelayExit

	// Distributed
	candidate.Distributed.Enabled = w.Distributed.Enabled
	if w.Distributed.Role != "" {
		candidate.Distributed.Role = w.Distributed.Role
	}
	if w.Distributed.Token != "" && w.Distributed.Token != "[REDACTED]" {
		candidate.Distributed.Token = w.Distributed.Token
	}
	if w.Distributed.MutationToken != "" && w.Distributed.MutationToken != "[REDACTED]" {
		candidate.Distributed.MutationToken = w.Distributed.MutationToken
	}
	if w.Distributed.MutationTokenKeyFiles != nil {
		candidate.Distributed.MutationTokenKeyFiles = append([]string{}, w.Distributed.MutationTokenKeyFiles...)
	}
	candidate.Distributed.CoordinatorID = w.Distributed.CoordinatorID
	candidate.Distributed.AllowHTTP = w.Distributed.AllowHTTP
	candidate.Distributed.Node = w.Distributed.Node
	if w.Distributed.Routing.Mode != "" {
		candidate.Distributed.Routing.Mode = w.Distributed.Routing.Mode
	}
	if w.Distributed.Routing.ClientNetworks != nil {
		candidate.Distributed.Routing.ClientNetworks = append([]ClientNetworkMapping{}, w.Distributed.Routing.ClientNetworks...)
	}
	if w.Distributed.Routing.Regions != nil {
		candidate.Distributed.Routing.Regions = cloneRegions(w.Distributed.Routing.Regions)
	}
	candidate.Distributed.HealthCheck.UnhealthyThreshold = w.Distributed.HealthCheck.UnhealthyThreshold
	candidate.Distributed.HealthCheck.HealthyThreshold = w.Distributed.HealthCheck.HealthyThreshold
	if w.Distributed.Nodes != nil {
		candidate.Distributed.Nodes = mergeNodeSeeds(base.Distributed.Nodes, w.Distributed.Nodes)
	}

	// UI Enhancement
	candidate.UIEnhancement = w.UIEnhancement

	// Warmup
	candidate.Warmup.Enabled = w.Warmup.Enabled
	candidate.Warmup.MaxConcurrency = w.Warmup.MaxConcurrency
	candidate.Warmup.BandwidthLimit = w.Warmup.BandwidthLimit
	candidate.Warmup.RetryCount = w.Warmup.RetryCount
	candidate.Warmup.MetadataDepth = w.Warmup.MetadataDepth

	durations := []struct {
		name      string
		value     string
		target    *time.Duration
		allowZero bool
	}{
		{"http.read_timeout", w.HTTP.ReadTimeout, &candidate.HTTP.ReadTimeout, false},
		{"http.write_timeout", w.HTTP.WriteTimeout, &candidate.HTTP.WriteTimeout, true},
		{"http.idle_timeout", w.HTTP.IdleTimeout, &candidate.HTTP.IdleTimeout, false},
		{"cache.inactive", w.Cache.Inactive, &candidate.Cache.Inactive, false},
		{"cache.metadata_ttl", w.Cache.MetadataTTL, &candidate.Cache.MetadataTTL, false},
		{"cache.package_ttl", w.Cache.PackageTTL, &candidate.Cache.PackageTTL, false},
		{"cache.cleanup_interval", w.Cache.CleanupInterval, &candidate.Cache.CleanupInterval, false},
		{"cache.wait_for_fill", w.Cache.WaitForFill, &candidate.Cache.WaitForFill, false},
		{"security.session_timeout", w.Security.SessionTimeout, &candidate.Security.SessionTimeout, false},
		{"security.login_window", w.Security.LoginWindow, &candidate.Security.LoginWindow, false},
		{"transport.dial_timeout", w.Transport.DialTimeout, &candidate.Transport.DialTimeout, false},
		{"transport.keep_alive", w.Transport.KeepAlive, &candidate.Transport.KeepAlive, false},
		{"transport.tls_handshake_timeout", w.Transport.TLSHandshakeTimeout, &candidate.Transport.TLSHandshakeTimeout, false},
		{"transport.response_header_timeout", w.Transport.ResponseHeaderTimeout, &candidate.Transport.ResponseHeaderTimeout, false},
		{"transport.idle_connection_timeout", w.Transport.IdleConnTimeout, &candidate.Transport.IdleConnTimeout, false},
		{"health.worker_interval", w.Health.WorkerInterval, &candidate.Health.WorkerInterval, false},
		{"shutdown.grace_period", w.Shutdown.GracePeriod, &candidate.Shutdown.GracePeriod, false},
		{"upstream_nginx.resolver_refresh", w.UpstreamNginx.ResolverRefresh, &candidate.UpstreamNginx.ResolverRefresh, false},
		{"upstream_nginx.restart_window", w.UpstreamNginx.RestartWindow, &candidate.UpstreamNginx.RestartWindow, false},
		{"upstream_nginx.restart_initial_backoff", w.UpstreamNginx.RestartInitialBackoff, &candidate.UpstreamNginx.RestartInitialBackoff, false},
		{"upstream_nginx.restart_max_backoff", w.UpstreamNginx.RestartMaxBackoff, &candidate.UpstreamNginx.RestartMaxBackoff, false},
		{"distributed.health_check.interval", w.Distributed.HealthCheck.Interval, &candidate.Distributed.HealthCheck.Interval, false},
		{"distributed.health_check.timeout", w.Distributed.HealthCheck.Timeout, &candidate.Distributed.HealthCheck.Timeout, false},
	}
	for _, value := range durations {
		if value.value != "" {
			parsed, err := time.ParseDuration(value.value)
			if err != nil || parsed < 0 || (!value.allowZero && parsed == 0) {
				return base, fmt.Errorf("%s must be a valid positive duration", value.name)
			}
			*value.target = parsed
		}
	}

	if w.Warmup.Timeout != "" {
		parsed, err := time.ParseDuration(w.Warmup.Timeout)
		if err != nil || parsed <= 0 {
			return base, errors.New("warmup.timeout must be a valid positive duration")
		}
		candidate.Warmup.Timeout = parsed
	}

	if w.Webhook != nil {
		candidate.Webhook.Enabled = w.Webhook.Enabled
		candidate.Webhook.URL = w.Webhook.URL
		if w.Webhook.Secret != "" && w.Webhook.Secret != "[REDACTED]" {
			candidate.Webhook.Secret = w.Webhook.Secret
		}
		candidate.Webhook.Events = append([]string{}, w.Webhook.Events...)
		candidate.Webhook.AllowHTTP = w.Webhook.AllowHTTP
		candidate.Webhook.AllowPrivate = w.Webhook.AllowPrivate
		if w.Webhook.Timeout != "" {
			parsed, err := time.ParseDuration(w.Webhook.Timeout)
			if err != nil || parsed <= 0 {
				return base, errors.New("webhook.timeout must be a valid positive duration")
			}
			candidate.Webhook.Timeout = parsed
		}
	}

	if err := candidate.NormalizeRuntime(); err != nil {
		return base, err
	}
	if err := candidate.Validate(); err != nil {
		return base, err
	}
	return candidate, nil
}

func DecodeWebSettings(data []byte) (WebSettings, error) {
	var settings WebSettings
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&settings); err != nil {
		return settings, err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("multiple JSON values are not allowed")
		}
		return settings, err
	}
	return settings, nil
}

func ExportYAML(cfg Config, fullBackup bool) (string, error) {
	c := cfg
	// Do not export instance public base URLs
	c.HTTP.PublicBaseURL = ""
	c.Distributed.Node.PublicBaseURL = ""

	if !fullBackup {
		// Omit sensitive credentials for standard export
		c.Distributed.Token = ""
		c.Distributed.MutationToken = ""
		c.Webhook.Secret = ""
		nodes := make([]DistributedNodeSeed, len(c.Distributed.Nodes))
		for i, n := range c.Distributed.Nodes {
			nodes[i] = n
			nodes[i].MutationToken = ""
		}
		c.Distributed.Nodes = nodes
	}

	var buf bytes.Buffer
	encoder := yaml.NewEncoder(&buf)
	encoder.SetIndent(2)
	if err := encoder.Encode(&c); err != nil {
		return "", err
	}
	if err := encoder.Close(); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func ComputeSettingsDiff(oldWS, newWS WebSettings) []model.SettingDiffEntry {
	var diffs []model.SettingDiffEntry

	check := func(path string, oldVal, newVal any, sensitive bool) {
		oldStr := fmt.Sprintf("%v", oldVal)
		newStr := fmt.Sprintf("%v", newVal)
		if oldStr != newStr {
			if sensitive {
				if oldStr != "" {
					oldStr = "[REDACTED]"
				}
				if newStr != "" {
					newStr = "[REDACTED]"
				}
			}
			diffs = append(diffs, model.SettingDiffEntry{
				Path:     path,
				OldValue: oldStr,
				NewValue: newStr,
			})
		}
	}

	checkSlice := func(path string, oldSlice, newSlice []string, sensitive bool) {
		if !slices.Equal(oldSlice, newSlice) {
			oldStr := strings.Join(oldSlice, ", ")
			newStr := strings.Join(newSlice, ", ")
			if sensitive {
				if len(oldSlice) > 0 {
					oldStr = "[REDACTED]"
				}
				if len(newSlice) > 0 {
					newStr = "[REDACTED]"
				}
			}
			diffs = append(diffs, model.SettingDiffEntry{
				Path:     path,
				OldValue: oldStr,
				NewValue: newStr,
			})
		}
	}

	// Server
	check("server.unix_socket_enabled", oldWS.Server.UnixSocketEnabled, newWS.Server.UnixSocketEnabled, false)
	check("server.frontend_socket", oldWS.Server.FrontendSocket, newWS.Server.FrontendSocket, false)
	check("server.frontend_socket_mode", oldWS.Server.FrontendSocketModeText, newWS.Server.FrontendSocketModeText, false)
	check("server.local_address", oldWS.Server.LocalAddress, newWS.Server.LocalAddress, false)
	check("server.local_port", oldWS.Server.LocalPort, newWS.Server.LocalPort, false)

	// Runtime
	check("runtime.root", oldWS.Runtime.Root, newWS.Runtime.Root, false)
	check("runtime.run_dir", oldWS.Runtime.RunDir, newWS.Runtime.RunDir, false)

	// Ingress
	check("ingress.mode", oldWS.Ingress.Mode, newWS.Ingress.Mode, false)
	check("ingress.generate_snippet", oldWS.Ingress.GenerateSnippet, newWS.Ingress.GenerateSnippet, false)
	check("ingress.snippet_path", oldWS.Ingress.SnippetPath, newWS.Ingress.SnippetPath, false)

	// Performance
	check("performance.stream_buffer_size_bytes", oldWS.Performance.StreamBufferSize, newWS.Performance.StreamBufferSize, false)
	check("performance.go_memory_limit_bytes", oldWS.Performance.GoMemoryLimit, newWS.Performance.GoMemoryLimit, false)
	check("performance.gogc", oldWS.Performance.GOGC, newWS.Performance.GOGC, false)
	check("performance.zero_copy_bypass", oldWS.Performance.ZeroCopyBypass, newWS.Performance.ZeroCopyBypass, false)

	// Metadata
	check("metadata.rewrite_buffer_limit_bytes", oldWS.Metadata.RewriteBufferLimit, newWS.Metadata.RewriteBufferLimit, false)
	check("metadata.output_compression", oldWS.Metadata.OutputCompression, newWS.Metadata.OutputCompression, false)
	check("metadata.gzip_min_length_bytes", oldWS.Metadata.GzipMinLength, newWS.Metadata.GzipMinLength, false)
	check("metadata.validator_entries", oldWS.Metadata.ValidatorEntries, newWS.Metadata.ValidatorEntries, false)

	// Redirect
	check("redirect.max_hops", oldWS.Redirect.MaxHops, newWS.Redirect.MaxHops, false)
	check("redirect.pin_validated_ip", oldWS.Redirect.PinValidatedIP, newWS.Redirect.PinValidatedIP, false)
	check("redirect.reject_mixed_dns_result", oldWS.Redirect.RejectMixedResult, newWS.Redirect.RejectMixedResult, false)

	// HTTP
	check("http.listen", oldWS.HTTP.Listen, newWS.HTTP.Listen, false)
	check("http.https_listen", oldWS.HTTP.HTTPSListen, newWS.HTTP.HTTPSListen, false)
	check("http.public_base_url", oldWS.HTTP.PublicBaseURL, newWS.HTTP.PublicBaseURL, false)
	check("http.read_timeout", oldWS.HTTP.ReadTimeout, newWS.HTTP.ReadTimeout, false)
	check("http.write_timeout", oldWS.HTTP.WriteTimeout, newWS.HTTP.WriteTimeout, false)
	check("http.idle_timeout", oldWS.HTTP.IdleTimeout, newWS.HTTP.IdleTimeout, false)

	// TLS
	check("tls.certificate", oldWS.TLS.Certificate, newWS.TLS.Certificate, false)
	check("tls.private_key", oldWS.TLS.PrivateKey, newWS.TLS.PrivateKey, false)
	check("tls.min_version", oldWS.TLS.MinVersion, newWS.TLS.MinVersion, false)

	// Database
	check("database.path", oldWS.Database.Path, newWS.Database.Path, false)

	// Cache
	check("cache.path", oldWS.Cache.Path, newWS.Cache.Path, false)
	check("cache.max_size_bytes", oldWS.Cache.MaxSizeBytes, newWS.Cache.MaxSizeBytes, false)
	check("cache.max_files", oldWS.Cache.MaxFiles, newWS.Cache.MaxFiles, false)
	check("cache.inactive", oldWS.Cache.Inactive, newWS.Cache.Inactive, false)
	check("cache.metadata_ttl", oldWS.Cache.MetadataTTL, newWS.Cache.MetadataTTL, false)
	check("cache.package_ttl", oldWS.Cache.PackageTTL, newWS.Cache.PackageTTL, false)
	check("cache.cleanup_interval", oldWS.Cache.CleanupInterval, newWS.Cache.CleanupInterval, false)
	check("cache.wait_for_fill", oldWS.Cache.WaitForFill, newWS.Cache.WaitForFill, false)
	check("cache.minimum_free_bytes", oldWS.Cache.MinimumFreeBytes, newWS.Cache.MinimumFreeBytes, false)

	// Logging
	check("logging.path", oldWS.Logging.Path, newWS.Logging.Path, false)
	check("logging.queue_size", oldWS.Logging.QueueSize, newWS.Logging.QueueSize, false)
	check("logging.max_size_mb", oldWS.Logging.MaxSizeMB, newWS.Logging.MaxSizeMB, false)
	check("logging.keep_days", oldWS.Logging.KeepDays, newWS.Logging.KeepDays, false)

	// Security
	check("security.allow_http_upstream", oldWS.Security.AllowHTTPUpstream, newWS.Security.AllowHTTPUpstream, false)
	check("security.allow_private_upstream", oldWS.Security.AllowPrivateUpstream, newWS.Security.AllowPrivateUpstream, false)
	check("security.expose_client_ip", oldWS.Security.ExposeClientIP, newWS.Security.ExposeClientIP, false)
	checkSlice("security.trusted_proxy_cidrs", oldWS.Security.TrustedProxyCIDRs, newWS.Security.TrustedProxyCIDRs, false)
	check("security.session_timeout", oldWS.Security.SessionTimeout, newWS.Security.SessionTimeout, false)
	check("security.login_window", oldWS.Security.LoginWindow, newWS.Security.LoginWindow, false)
	check("security.login_max_failures", oldWS.Security.LoginMaxFailures, newWS.Security.LoginMaxFailures, false)
	checkSlice("security.admin_cidrs", oldWS.Security.AdminCIDRs, newWS.Security.AdminCIDRs, false)

	// Transport
	check("transport.dial_timeout", oldWS.Transport.DialTimeout, newWS.Transport.DialTimeout, false)
	check("transport.keep_alive", oldWS.Transport.KeepAlive, newWS.Transport.KeepAlive, false)
	check("transport.tls_handshake_timeout", oldWS.Transport.TLSHandshakeTimeout, newWS.Transport.TLSHandshakeTimeout, false)
	check("transport.response_header_timeout", oldWS.Transport.ResponseHeaderTimeout, newWS.Transport.ResponseHeaderTimeout, false)
	check("transport.idle_connection_timeout", oldWS.Transport.IdleConnTimeout, newWS.Transport.IdleConnTimeout, false)
	check("transport.max_idle_connections", oldWS.Transport.MaxIdleConns, newWS.Transport.MaxIdleConns, false)
	check("transport.max_idle_connections_per_host", oldWS.Transport.MaxIdleConnsPerHost, newWS.Transport.MaxIdleConnsPerHost, false)

	// Limits
	check("limits.max_total_concurrency", oldWS.Limits.MaxTotalConcurrency, newWS.Limits.MaxTotalConcurrency, false)
	check("limits.max_ip_concurrency", oldWS.Limits.MaxIPConcurrency, newWS.Limits.MaxIPConcurrency, false)
	check("limits.bandwidth_limit_bps", oldWS.Limits.BandwidthLimitBPS, newWS.Limits.BandwidthLimitBPS, false)

	// Health
	check("health.worker_interval", oldWS.Health.WorkerInterval, newWS.Health.WorkerInterval, false)

	// Admin
	check("admin.host", oldWS.Admin.Host, newWS.Admin.Host, false)
	check("admin.path", oldWS.Admin.Path, newWS.Admin.Path, false)

	// Shutdown
	check("shutdown.grace_period", oldWS.Shutdown.GracePeriod, newWS.Shutdown.GracePeriod, false)

	// Upstream Nginx
	check("upstream_nginx.mode", oldWS.UpstreamNginx.Mode, newWS.UpstreamNginx.Mode, false)
	check("upstream_nginx.binary", oldWS.UpstreamNginx.Binary, newWS.UpstreamNginx.Binary, false)
	check("upstream_nginx.prefix", oldWS.UpstreamNginx.Prefix, newWS.UpstreamNginx.Prefix, false)
	check("upstream_nginx.pid", oldWS.UpstreamNginx.PID, newWS.UpstreamNginx.PID, false)
	check("upstream_nginx.log_path", oldWS.UpstreamNginx.LogPath, newWS.UpstreamNginx.LogPath, false)
	check("upstream_nginx.upstream_unix_socket_enabled", oldWS.UpstreamNginx.UpstreamSocketEnabled, newWS.UpstreamNginx.UpstreamSocketEnabled, false)
	check("upstream_nginx.upstream_socket", oldWS.UpstreamNginx.UpstreamSocket, newWS.UpstreamNginx.UpstreamSocket, false)
	check("upstream_nginx.upstream_socket_mode", oldWS.UpstreamNginx.UpstreamSocketModeText, newWS.UpstreamNginx.UpstreamSocketModeText, false)
	check("upstream_nginx.upstream_local_port", oldWS.UpstreamNginx.UpstreamLocalPort, newWS.UpstreamNginx.UpstreamLocalPort, false)
	check("upstream_nginx.ca_bundle", oldWS.UpstreamNginx.CABundle, newWS.UpstreamNginx.CABundle, false)
	check("upstream_nginx.tls_verify_depth", oldWS.UpstreamNginx.TLSVerifyDepth, newWS.UpstreamNginx.TLSVerifyDepth, false)
	check("upstream_nginx.resolver", oldWS.UpstreamNginx.Resolver, newWS.UpstreamNginx.Resolver, false)
	check("upstream_nginx.resolver_refresh", oldWS.UpstreamNginx.ResolverRefresh, newWS.UpstreamNginx.ResolverRefresh, false)
	check("upstream_nginx.history_limit", oldWS.UpstreamNginx.HistoryLimit, newWS.UpstreamNginx.HistoryLimit, false)
	check("upstream_nginx.restart_max_failures", oldWS.UpstreamNginx.RestartMaxFailures, newWS.UpstreamNginx.RestartMaxFailures, false)
	check("upstream_nginx.restart_window", oldWS.UpstreamNginx.RestartWindow, newWS.UpstreamNginx.RestartWindow, false)
	check("upstream_nginx.restart_initial_backoff", oldWS.UpstreamNginx.RestartInitialBackoff, newWS.UpstreamNginx.RestartInitialBackoff, false)
	check("upstream_nginx.restart_max_backoff", oldWS.UpstreamNginx.RestartMaxBackoff, newWS.UpstreamNginx.RestartMaxBackoff, false)
	check("upstream_nginx.worker_processes", oldWS.UpstreamNginx.WorkerProcesses, newWS.UpstreamNginx.WorkerProcesses, false)
	check("upstream_nginx.worker_user", oldWS.UpstreamNginx.WorkerUser, newWS.UpstreamNginx.WorkerUser, false)
	check("upstream_nginx.worker_connections", oldWS.UpstreamNginx.WorkerConnections, newWS.UpstreamNginx.WorkerConnections, false)
	check("upstream_nginx.stop_on_mirrorrelay_exit", oldWS.UpstreamNginx.StopOnMirrorRelayExit, newWS.UpstreamNginx.StopOnMirrorRelayExit, false)

	// Distributed
	check("distributed.enabled", oldWS.Distributed.Enabled, newWS.Distributed.Enabled, false)
	check("distributed.role", oldWS.Distributed.Role, newWS.Distributed.Role, false)
	check("distributed.token", oldWS.Distributed.Token, newWS.Distributed.Token, true)
	check("distributed.mutation_token", oldWS.Distributed.MutationToken, newWS.Distributed.MutationToken, true)
	checkSlice("distributed.mutation_token_key_files", oldWS.Distributed.MutationTokenKeyFiles, newWS.Distributed.MutationTokenKeyFiles, false)
	check("distributed.coordinator_id", oldWS.Distributed.CoordinatorID, newWS.Distributed.CoordinatorID, false)
	check("distributed.allow_http", oldWS.Distributed.AllowHTTP, newWS.Distributed.AllowHTTP, false)
	check("distributed.node.name", oldWS.Distributed.Node.Name, newWS.Distributed.Node.Name, false)
	check("distributed.node.public_base_url", oldWS.Distributed.Node.PublicBaseURL, newWS.Distributed.Node.PublicBaseURL, false)
	check("distributed.node.region", oldWS.Distributed.Node.Region, newWS.Distributed.Node.Region, false)
	check("distributed.node.country", oldWS.Distributed.Node.Country, newWS.Distributed.Node.Country, false)
	check("distributed.routing.mode", oldWS.Distributed.Routing.Mode, newWS.Distributed.Routing.Mode, false)
	check("distributed.health_check.interval", oldWS.Distributed.HealthCheck.Interval, newWS.Distributed.HealthCheck.Interval, false)
	check("distributed.health_check.timeout", oldWS.Distributed.HealthCheck.Timeout, newWS.Distributed.HealthCheck.Timeout, false)
	check("distributed.health_check.unhealthy_threshold", oldWS.Distributed.HealthCheck.UnhealthyThreshold, newWS.Distributed.HealthCheck.UnhealthyThreshold, false)
	check("distributed.health_check.healthy_threshold", oldWS.Distributed.HealthCheck.HealthyThreshold, newWS.Distributed.HealthCheck.HealthyThreshold, false)

	oldNetJSON, _ := json.Marshal(oldWS.Distributed.Routing.ClientNetworks)
	newNetJSON, _ := json.Marshal(newWS.Distributed.Routing.ClientNetworks)
	if string(oldNetJSON) != string(newNetJSON) {
		diffs = append(diffs, model.SettingDiffEntry{
			Path:     "distributed.routing.client_networks",
			OldValue: strconv.Itoa(len(oldWS.Distributed.Routing.ClientNetworks)) + " network(s)",
			NewValue: strconv.Itoa(len(newWS.Distributed.Routing.ClientNetworks)) + " network(s)",
		})
	}

	oldRegJSON, _ := json.Marshal(oldWS.Distributed.Routing.Regions)
	newRegJSON, _ := json.Marshal(newWS.Distributed.Routing.Regions)
	if string(oldRegJSON) != string(newRegJSON) {
		diffs = append(diffs, model.SettingDiffEntry{
			Path:     "distributed.routing.regions",
			OldValue: strconv.Itoa(len(oldWS.Distributed.Routing.Regions)) + " region(s)",
			NewValue: strconv.Itoa(len(newWS.Distributed.Routing.Regions)) + " region(s)",
		})
	}

	// Webhook
	if oldWS.Webhook != nil || newWS.Webhook != nil {
		var oldHook, newHook WebWebhookSettings
		if oldWS.Webhook != nil {
			oldHook = *oldWS.Webhook
		}
		if newWS.Webhook != nil {
			newHook = *newWS.Webhook
		}
		check("webhook.enabled", oldHook.Enabled, newHook.Enabled, false)
		check("webhook.url", oldHook.URL, newHook.URL, false)
		check("webhook.secret", oldHook.Secret, newHook.Secret, true)
		checkSlice("webhook.events", oldHook.Events, newHook.Events, false)
		check("webhook.timeout", oldHook.Timeout, newHook.Timeout, false)
		check("webhook.allow_http", oldHook.AllowHTTP, newHook.AllowHTTP, false)
		check("webhook.allow_private", oldHook.AllowPrivate, newHook.AllowPrivate, false)
	}

	// Warmup
	check("warmup.enabled", oldWS.Warmup.Enabled, newWS.Warmup.Enabled, false)
	check("warmup.max_concurrency", oldWS.Warmup.MaxConcurrency, newWS.Warmup.MaxConcurrency, false)
	check("warmup.bandwidth_limit_bps", oldWS.Warmup.BandwidthLimit, newWS.Warmup.BandwidthLimit, false)
	check("warmup.timeout", oldWS.Warmup.Timeout, newWS.Warmup.Timeout, false)
	check("warmup.retry_count", oldWS.Warmup.RetryCount, newWS.Warmup.RetryCount, false)
	check("warmup.metadata_depth", oldWS.Warmup.MetadataDepth, newWS.Warmup.MetadataDepth, false)

	return diffs
}
