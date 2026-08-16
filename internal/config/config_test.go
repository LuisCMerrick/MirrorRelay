package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestLoadDurationsAndDevDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	data := []byte("http:\n  read_timeout: 20s\ncache:\n  metadata_ttl: 7m\n")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path, true)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTP.ReadTimeout != 20*time.Second || cfg.Cache.MetadataTTL != 7*time.Minute {
		t.Fatalf("durations not parsed: %#v", cfg)
	}
	if cfg.HTTP.HTTPSListen != "127.0.0.1:8443" || cfg.Admin.Path != "/admin/" || cfg.Admin.InitialPassword != "adminadmin" {
		t.Fatalf("development defaults not applied: %#v", cfg)
	}
}

func TestWebSettingsApplyOperationalValuesAndPreserveFileOnlyPaths(t *testing.T) {
	base := Default()
	settings := WebSettingsFrom(base)
	settings.Server.UnixSocketEnabled = false
	settings.Server.LocalPort = 19081
	settings.Ingress.Mode = "managed-standalone"
	settings.Ingress.GenerateSnippet = false
	settings.Performance.StreamBufferSize = 32 << 10
	settings.Performance.GoMemoryLimit = 1 << 30
	settings.Performance.GOGC = 75
	settings.Metadata.RewriteBufferLimit = 16 << 20
	settings.Metadata.OutputCompression = "gzip"
	settings.Metadata.GzipMinLength = 2048
	settings.Metadata.ValidatorEntries = 4096
	settings.Redirect.MaxHops = 7
	settings.Redirect.RejectMixedResult = false
	settings.HTTP.Listen = "127.0.0.1:18080"
	settings.HTTP.HTTPSListen = "127.0.0.1:18443"
	settings.HTTP.ReadTimeout = "20s"
	settings.HTTP.WriteTimeout = "1h0m0s"
	settings.HTTP.IdleTimeout = "3m0s"
	settings.UpstreamNginx.UpstreamSocketEnabled = false
	settings.UpstreamNginx.UpstreamLocalPort = 19082
	settings.HTTP.PublicBaseURL = "https://mirror.example.com"
	settings.TLS.MinVersion = "1.3"
	settings.Cache.MaxSizeBytes = 64 << 30
	settings.Cache.MaxFiles = 200000
	settings.Cache.Inactive = "48h0m0s"
	settings.Cache.MetadataTTL = "10m0s"
	settings.Cache.PackageTTL = "168h0m0s"
	settings.Cache.CleanupInterval = "20m0s"
	settings.Cache.WaitForFill = "45m0s"
	settings.Cache.MinimumFreeBytes = 2 << 30
	settings.Logging.QueueSize = 4096
	settings.Logging.MaxSizeMB = 512
	settings.Logging.KeepDays = 14
	settings.Security.AllowHTTPUpstream = true
	settings.Security.AllowPrivateUpstream = true
	settings.Security.ExposeClientIP = true
	settings.Security.SessionTimeout = "8h0m0s"
	settings.Security.LoginWindow = "10m0s"
	settings.Security.LoginMaxFailures = 8
	settings.Security.AdminCIDRs = []string{"10.0.0.0/8"}
	settings.Transport.DialTimeout = "8s"
	settings.Transport.KeepAlive = "45s"
	settings.Transport.TLSHandshakeTimeout = "12s"
	settings.Transport.ResponseHeaderTimeout = "45s"
	settings.Transport.IdleConnTimeout = "2m0s"
	settings.Transport.MaxIdleConns = 256
	settings.Transport.MaxIdleConnsPerHost = 32
	settings.Limits.MaxTotalConcurrency = 1024
	settings.Limits.MaxIPConcurrency = 64
	settings.Limits.BandwidthLimitBPS = 100 << 20
	settings.Health.WorkerInterval = "30s"
	settings.Shutdown.GracePeriod = "15m0s"
	settings.UpstreamNginx.Mode = "managed"
	settings.UpstreamNginx.TLSVerifyDepth = 8
	settings.UpstreamNginx.Resolver = "9.9.9.9 1.1.1.1"
	settings.UpstreamNginx.ResolverRefresh = "10m0s"
	settings.UpstreamNginx.HistoryLimit = 40
	settings.UpstreamNginx.RestartMaxFailures = 8
	settings.UpstreamNginx.RestartWindow = "3m0s"
	settings.UpstreamNginx.RestartInitialBackoff = "2s"
	settings.UpstreamNginx.RestartMaxBackoff = "1m0s"
	settings.UpstreamNginx.WorkerProcesses = "2"
	settings.UpstreamNginx.WorkerUser = ""
	settings.UpstreamNginx.WorkerConnections = 2048
	settings.UpstreamNginx.StopOnMirrorRelayExit = true

	applied, err := settings.Apply(base)
	if err != nil {
		t.Fatal(err)
	}
	if network, address := applied.FrontendEndpoint(); network != "tcp" || address != "127.0.0.1:19081" {
		t.Fatalf("frontend endpoint = %s %s", network, address)
	}
	if applied.Cache.MaxSizeBytes != 64<<30 || applied.Cache.Inactive != 48*time.Hour || applied.HTTP.PublicBaseURL != "https://mirror.example.com" {
		t.Fatalf("Web settings were not applied: %+v", applied)
	}
	if got := WebSettingsFrom(applied); !reflect.DeepEqual(got, settings) {
		t.Fatalf("Web settings did not round trip through Config:\n got: %#v\nwant: %#v", got, settings)
	}
	if applied.Database.Path != base.Database.Path || applied.Cache.Path != base.Cache.Path || applied.Admin.Path != base.Admin.Path || applied.UpstreamNginx.Binary != base.UpstreamNginx.Binary {
		t.Fatalf("Web settings changed a file-only path: %+v", applied)
	}

	encoded, err := json.Marshal(settings)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeWebSettings(encoded)
	if err != nil || decoded.HTTP.PublicBaseURL != settings.HTTP.PublicBaseURL {
		t.Fatalf("Web settings round trip failed: %+v %v", decoded, err)
	}
}

func TestAdminPathIsNormalizedAndValidated(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("admin:\n  path: /private-console\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path, true)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Admin.Path != "/private-console/" || cfg.AdminAPIPath() != "/private-console/api/v1/" {
		t.Fatalf("administration paths were not normalized: %+v", cfg.Admin)
	}
	for _, invalid := range []string{"/", "relative/", "/metrics/private/", "/_mirrorrelay/private/", "/unsafe%2fpath/", "/two//slashes/"} {
		candidate := Default()
		candidate.Admin.Path = invalid
		if err := candidate.NormalizeRuntime(); err == nil {
			t.Fatalf("invalid administration path %q was accepted", invalid)
		}
	}
}

func TestWebSettingsRejectUnknownFieldsAndInvalidValues(t *testing.T) {
	if _, err := DecodeWebSettings([]byte(`{"unknown":true}`)); err == nil {
		t.Fatal("unknown Web settings field was accepted")
	}
	settings := WebSettingsFrom(Default())
	settings.HTTP.ReadTimeout = "not-a-duration"
	if _, err := settings.Apply(Default()); err == nil {
		t.Fatal("invalid Web duration was accepted")
	}
	settings = WebSettingsFrom(Default())
	settings.Security.AdminCIDRs = []string{"not-a-cidr"}
	if _, err := settings.Apply(Default()); err == nil {
		t.Fatal("invalid Web security setting was accepted")
	}
}

func TestUnixSocketsCanBeDisabledWithConfigurableLoopbackPorts(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	data := []byte("server:\n  unix_socket_enabled: false\n  local_port: 19081\nupstream_nginx:\n  upstream_unix_socket_enabled: false\n  upstream_local_port: 19082\n")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path, true)
	if err != nil {
		t.Fatal(err)
	}
	if network, address := cfg.FrontendEndpoint(); network != "tcp" || address != "127.0.0.1:19081" {
		t.Fatalf("frontend endpoint = %s %s", network, address)
	}
	if network, address := cfg.UpstreamEndpoint(); network != "tcp" || address != "127.0.0.1:19082" {
		t.Fatalf("upstream endpoint = %s %s", network, address)
	}
}

func TestExternalIngressDoesNotPrepareUnusedTLSDirectories(t *testing.T) {
	root := t.TempDir()
	cfg := Default()
	cfg.Ingress.Mode = "external"
	cfg.Database.Path = filepath.Join(root, "data", "mirrorrelay.db")
	cfg.Cache.Path = filepath.Join(root, "cache")
	cfg.Logging.Path = filepath.Join(root, "logs")
	cfg.UpstreamNginx.Prefix = filepath.Join(root, "runtime", "upstream-nginx")
	cfg.UpstreamNginx.LogPath = filepath.Join(root, "logs", "upstream-nginx")
	cfg.Runtime.Root = filepath.Join(root, "runtime")
	cfg.Runtime.RunDir = filepath.Join(root, "run")
	cfg.UpstreamNginx.PID = filepath.Join(root, "run", "upstream-nginx.pid")
	cfg.Server.FrontendSocket = filepath.Join(root, "run", "frontend.sock")
	cfg.UpstreamNginx.UpstreamSocket = filepath.Join(root, "run", "upstream.sock")
	cfg.Ingress.SnippetPath = filepath.Join(root, "integration")
	cfg.TLS.Certificate = filepath.Join(root, "unused-tls", "server.pem")
	cfg.TLS.PrivateKey = filepath.Join(root, "unused-tls", "server-key.pem")
	if err := EnsureDirectories(cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "unused-tls")); !os.IsNotExist(err) {
		t.Fatalf("external mode prepared an unused TLS directory: %v", err)
	}
}

func TestValidateRejectsGeneratedNginxInjectionAndInvalidRuntimeLimits(t *testing.T) {
	cases := []func(*Config){
		func(cfg *Config) { cfg.UpstreamNginx.Resolver = "1.1.1.1; include /tmp/evil" },
		func(cfg *Config) { cfg.UpstreamNginx.WorkerProcesses = "auto; load_module evil" },
		func(cfg *Config) { cfg.UpstreamNginx.Prefix = "/var/lib/nginx; include /tmp/evil" },
		func(cfg *Config) { cfg.Logging.KeepDays = 0 },
		func(cfg *Config) { cfg.Cache.MaxSizeBytes = -1 },
		func(cfg *Config) { cfg.Performance.GOGC = -2 },
		func(cfg *Config) { cfg.HTTP.ReadTimeout = 0 },
		func(cfg *Config) { cfg.HTTP.WriteTimeout = -time.Second },
		func(cfg *Config) { cfg.HTTP.IdleTimeout = 0 },
		func(cfg *Config) { cfg.UpstreamNginx.ResolverRefresh = 0 },
		func(cfg *Config) { cfg.HTTP.PublicBaseURL = "https://user@example.com/base" },
		func(cfg *Config) { cfg.HTTP.PublicBaseURL = "https://example.com/base?unsafe=1" },
		func(cfg *Config) { cfg.HTTP.PublicBaseURL = "https://example.com/base" },
		func(cfg *Config) { cfg.Admin.Path = "/unsafe path/" },
		func(cfg *Config) {
			cfg.Ingress.Mode = "managed-standalone"
			cfg.UpstreamNginx.Mode = "disabled"
		},
		func(cfg *Config) {
			cfg.UpstreamNginx.Mode = "disabled"
			cfg.Server.FrontendSocket = "/run/frontend.sock; include evil"
		},
		func(cfg *Config) {
			cfg.Ingress.Mode = "managed-standalone"
			cfg.HTTP.Listen = "127.0.0.1:8080; include evil"
		},
	}
	for index, mutate := range cases {
		cfg := Default()
		mutate(&cfg)
		if err := cfg.Validate(); err == nil {
			t.Fatalf("unsafe configuration case %d was accepted", index)
		}
	}
}

func TestLoadRejectsUnknownKeysAndMultipleDocuments(t *testing.T) {
	for _, content := range []string{
		"server:\n  unix_socket_enable: false\n",
		"nginx: {}\n",
		"server:\n  unix_socket_enabled: true\n---\nserver:\n  unix_socket_enabled: false\n",
	} {
		path := filepath.Join(t.TempDir(), "config.yaml")
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(path, true); err == nil {
			t.Fatalf("unsupported configuration was accepted: %q", content)
		}
	}
}
