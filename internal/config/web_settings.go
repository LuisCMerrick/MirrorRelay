package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/LuisCMerrick/MirrorRelay/internal/model"
)

const WebSettingsKey = "web_settings_v1"

type WebSettings struct {
	Server        WebServerSettings         `json:"server"`
	Ingress       WebIngressSettings        `json:"ingress"`
	Performance   WebPerformanceSettings    `json:"performance"`
	Metadata      WebMetadataSettings       `json:"metadata"`
	Redirect      WebRedirectSettings       `json:"redirect"`
	HTTP          WebHTTPSettings           `json:"http"`
	TLS           WebTLSSettings            `json:"tls"`
	Cache         WebCacheSettings          `json:"cache"`
	Logging       WebLoggingSettings        `json:"logging"`
	Security      WebSecuritySettings       `json:"security"`
	Transport     WebTransportSettings      `json:"transport"`
	Limits        WebLimitSettings          `json:"limits"`
	Health        WebHealthSettings         `json:"health"`
	Shutdown      WebShutdownSettings       `json:"shutdown"`
	UpstreamNginx WebUpstreamNginxSettings  `json:"upstream_nginx"`
	UIEnhancement model.UIEnhancementConfig `json:"ui_enhancement"`
}

type WebServerSettings struct {
	UnixSocketEnabled bool `json:"unix_socket_enabled"`
	LocalPort         int  `json:"local_port"`
}

type WebIngressSettings struct {
	Mode            string `json:"mode"`
	GenerateSnippet bool   `json:"generate_snippet"`
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
	MinVersion string `json:"min_version"`
}

type WebCacheSettings struct {
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
	QueueSize int   `json:"queue_size"`
	MaxSizeMB int64 `json:"max_size_mb"`
	KeepDays  int   `json:"keep_days"`
}

type WebSecuritySettings struct {
	AllowHTTPUpstream    bool     `json:"allow_http_upstream"`
	AllowPrivateUpstream bool     `json:"allow_private_upstream"`
	ExposeClientIP       bool     `json:"expose_client_ip"`
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

type WebShutdownSettings struct {
	GracePeriod string `json:"grace_period"`
}

type WebUpstreamNginxSettings struct {
	Mode                  string `json:"mode"`
	UpstreamSocketEnabled bool   `json:"upstream_unix_socket_enabled"`
	UpstreamLocalPort     int    `json:"upstream_local_port"`
	TLSVerifyDepth        int    `json:"tls_verify_depth"`
	Resolver              string `json:"resolver"`
	ResolverRefresh       string `json:"resolver_refresh"`
	HistoryLimit          int    `json:"history_limit"`
	RestartMaxFailures    int    `json:"restart_max_failures"`
	RestartWindow         string `json:"restart_window"`
	RestartInitialBackoff string `json:"restart_initial_backoff"`
	RestartMaxBackoff     string `json:"restart_max_backoff"`
	WorkerProcesses       string `json:"worker_processes"`
	WorkerUser            string `json:"worker_user"`
	WorkerConnections     int    `json:"worker_connections"`
	StopOnMirrorRelayExit bool   `json:"stop_on_mirrorrelay_exit"`
}

func WebSettingsFrom(c Config) WebSettings {
	return WebSettings{
		Server:      WebServerSettings{UnixSocketEnabled: c.Server.UnixSocketEnabled, LocalPort: c.Server.LocalPort},
		Ingress:     WebIngressSettings{Mode: c.Ingress.Mode, GenerateSnippet: c.Ingress.GenerateSnippet},
		Performance: WebPerformanceSettings{StreamBufferSize: c.Performance.StreamBufferSize, GoMemoryLimit: c.Performance.GoMemoryLimit, GOGC: c.Performance.GOGC, ZeroCopyBypass: c.Performance.ZeroCopyBypass},
		Metadata: WebMetadataSettings{RewriteBufferLimit: c.Metadata.RewriteBufferLimit, OutputCompression: c.Metadata.OutputCompression,
			GzipMinLength: c.Metadata.GzipMinLength, ValidatorEntries: c.Metadata.ValidatorEntries},
		Redirect: WebRedirectSettings{MaxHops: c.Redirect.MaxHops, RejectMixedResult: c.Redirect.RejectMixedResult},
		HTTP: WebHTTPSettings{Listen: c.HTTP.Listen, HTTPSListen: c.HTTP.HTTPSListen, PublicBaseURL: c.HTTP.PublicBaseURL,
			ReadTimeout: c.HTTP.ReadTimeout.String(), WriteTimeout: c.HTTP.WriteTimeout.String(), IdleTimeout: c.HTTP.IdleTimeout.String()},
		TLS: WebTLSSettings{MinVersion: c.TLS.MinVersion},
		Cache: WebCacheSettings{MaxSizeBytes: c.Cache.MaxSizeBytes, MaxFiles: c.Cache.MaxFiles, Inactive: c.Cache.Inactive.String(),
			MetadataTTL: c.Cache.MetadataTTL.String(), PackageTTL: c.Cache.PackageTTL.String(), CleanupInterval: c.Cache.CleanupInterval.String(),
			WaitForFill: c.Cache.WaitForFill.String(), MinimumFreeBytes: c.Cache.MinimumFreeBytes},
		Logging: WebLoggingSettings{QueueSize: c.Logging.QueueSize, MaxSizeMB: c.Logging.MaxSizeMB, KeepDays: c.Logging.KeepDays},
		Security: WebSecuritySettings{AllowHTTPUpstream: c.Security.AllowHTTPUpstream, AllowPrivateUpstream: c.Security.AllowPrivateUpstream,
			ExposeClientIP: c.Security.ExposeClientIP, SessionTimeout: c.Security.SessionTimeout.String(), LoginWindow: c.Security.LoginWindow.String(),
			LoginMaxFailures: c.Security.LoginMaxFailures, AdminCIDRs: append([]string(nil), c.Security.AdminCIDRs...)},
		Transport: WebTransportSettings{DialTimeout: c.Transport.DialTimeout.String(), KeepAlive: c.Transport.KeepAlive.String(),
			TLSHandshakeTimeout: c.Transport.TLSHandshakeTimeout.String(), ResponseHeaderTimeout: c.Transport.ResponseHeaderTimeout.String(),
			IdleConnTimeout: c.Transport.IdleConnTimeout.String(), MaxIdleConns: c.Transport.MaxIdleConns, MaxIdleConnsPerHost: c.Transport.MaxIdleConnsPerHost},
		Limits:   WebLimitSettings{MaxTotalConcurrency: c.Limits.MaxTotalConcurrency, MaxIPConcurrency: c.Limits.MaxIPConcurrency, BandwidthLimitBPS: c.Limits.BandwidthLimitBPS},
		Health:   WebHealthSettings{WorkerInterval: c.Health.WorkerInterval.String()},
		Shutdown: WebShutdownSettings{GracePeriod: c.Shutdown.GracePeriod.String()},
		UpstreamNginx: WebUpstreamNginxSettings{Mode: c.UpstreamNginx.Mode, UpstreamSocketEnabled: c.UpstreamNginx.UpstreamSocketEnabled,
			UpstreamLocalPort: c.UpstreamNginx.UpstreamLocalPort, TLSVerifyDepth: c.UpstreamNginx.TLSVerifyDepth, Resolver: c.UpstreamNginx.Resolver,
			ResolverRefresh: c.UpstreamNginx.ResolverRefresh.String(), HistoryLimit: c.UpstreamNginx.HistoryLimit,
			RestartMaxFailures: c.UpstreamNginx.RestartMaxFailures, RestartWindow: c.UpstreamNginx.RestartWindow.String(),
			RestartInitialBackoff: c.UpstreamNginx.RestartInitialBackoff.String(), RestartMaxBackoff: c.UpstreamNginx.RestartMaxBackoff.String(),
			WorkerProcesses: c.UpstreamNginx.WorkerProcesses, WorkerUser: c.UpstreamNginx.WorkerUser,
			WorkerConnections: c.UpstreamNginx.WorkerConnections, StopOnMirrorRelayExit: c.UpstreamNginx.StopOnMirrorRelayExit},
		UIEnhancement: c.UIEnhancement,
	}
}

func (w WebSettings) Apply(base Config) (Config, error) {
	candidate := base
	candidate.Server.UnixSocketEnabled, candidate.Server.LocalPort = w.Server.UnixSocketEnabled, w.Server.LocalPort
	candidate.Ingress.Mode, candidate.Ingress.GenerateSnippet = w.Ingress.Mode, w.Ingress.GenerateSnippet
	candidate.Performance.StreamBufferSize, candidate.Performance.GoMemoryLimit, candidate.Performance.GOGC, candidate.Performance.ZeroCopyBypass = w.Performance.StreamBufferSize, w.Performance.GoMemoryLimit, w.Performance.GOGC, w.Performance.ZeroCopyBypass
	candidate.Metadata.RewriteBufferLimit, candidate.Metadata.OutputCompression = w.Metadata.RewriteBufferLimit, w.Metadata.OutputCompression
	candidate.Metadata.GzipMinLength, candidate.Metadata.ValidatorEntries = w.Metadata.GzipMinLength, w.Metadata.ValidatorEntries
	candidate.Redirect.MaxHops, candidate.Redirect.RejectMixedResult = w.Redirect.MaxHops, w.Redirect.RejectMixedResult
	candidate.HTTP.Listen, candidate.HTTP.HTTPSListen, candidate.HTTP.PublicBaseURL = w.HTTP.Listen, w.HTTP.HTTPSListen, w.HTTP.PublicBaseURL
	candidate.TLS.MinVersion = w.TLS.MinVersion
	candidate.Cache.MaxSizeBytes, candidate.Cache.MaxFiles = w.Cache.MaxSizeBytes, w.Cache.MaxFiles
	candidate.Cache.MinimumFreeBytes = w.Cache.MinimumFreeBytes
	candidate.Logging.QueueSize, candidate.Logging.MaxSizeMB, candidate.Logging.KeepDays = w.Logging.QueueSize, w.Logging.MaxSizeMB, w.Logging.KeepDays
	candidate.Security.AllowHTTPUpstream, candidate.Security.AllowPrivateUpstream = w.Security.AllowHTTPUpstream, w.Security.AllowPrivateUpstream
	candidate.Security.ExposeClientIP, candidate.Security.LoginMaxFailures = w.Security.ExposeClientIP, w.Security.LoginMaxFailures
	candidate.Security.AdminCIDRs = append([]string(nil), w.Security.AdminCIDRs...)
	candidate.Transport.MaxIdleConns, candidate.Transport.MaxIdleConnsPerHost = w.Transport.MaxIdleConns, w.Transport.MaxIdleConnsPerHost
	candidate.Limits.MaxTotalConcurrency, candidate.Limits.MaxIPConcurrency = w.Limits.MaxTotalConcurrency, w.Limits.MaxIPConcurrency
	candidate.Limits.BandwidthLimitBPS = w.Limits.BandwidthLimitBPS
	candidate.UpstreamNginx.Mode = w.UpstreamNginx.Mode
	candidate.UpstreamNginx.UpstreamSocketEnabled, candidate.UpstreamNginx.UpstreamLocalPort = w.UpstreamNginx.UpstreamSocketEnabled, w.UpstreamNginx.UpstreamLocalPort
	candidate.UpstreamNginx.TLSVerifyDepth, candidate.UpstreamNginx.Resolver = w.UpstreamNginx.TLSVerifyDepth, w.UpstreamNginx.Resolver
	candidate.UpstreamNginx.HistoryLimit, candidate.UpstreamNginx.RestartMaxFailures = w.UpstreamNginx.HistoryLimit, w.UpstreamNginx.RestartMaxFailures
	candidate.UpstreamNginx.WorkerProcesses, candidate.UpstreamNginx.WorkerUser = w.UpstreamNginx.WorkerProcesses, w.UpstreamNginx.WorkerUser
	candidate.UpstreamNginx.WorkerConnections, candidate.UpstreamNginx.StopOnMirrorRelayExit = w.UpstreamNginx.WorkerConnections, w.UpstreamNginx.StopOnMirrorRelayExit
	candidate.UIEnhancement = w.UIEnhancement

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
	}
	for _, value := range durations {
		parsed, err := time.ParseDuration(value.value)
		if err != nil || parsed < 0 || (!value.allowZero && parsed == 0) {
			return base, fmt.Errorf("%s must be a valid positive duration", value.name)
		}
		*value.target = parsed
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
