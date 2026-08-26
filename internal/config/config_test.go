package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
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
	if cfg.HTTP.HTTPSListen != "127.0.0.1:8443" || cfg.Admin.Path != "/admin/" {
		t.Fatalf("development defaults not applied: %#v", cfg)
	}
	if cfg.Security.ExposeClientIP {
		t.Fatal("client IP exposure should default to disabled")
	}
}

func TestDevDefaultsUseRepositoryNginxFixtureFromRepositoryRoot(t *testing.T) {
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("the checked-in development fixture is linux/amd64 only")
	}
	workingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	repositoryRoot := filepath.Clean(filepath.Join(workingDirectory, "..", ".."))
	t.Chdir(repositoryRoot)

	cfg := Default()
	applyDevDefaults(&cfg)
	if cfg.UpstreamNginx.Binary != filepath.Join("nginx", "sbin", "nginx") {
		t.Fatalf("development Nginx binary = %q", cfg.UpstreamNginx.Binary)
	}
}

func TestLegacyBootstrapCredentialsAreRejectedAsUnknownConfiguration(t *testing.T) {
	for _, key := range []string{"initial_username", "initial_password"} {
		path := filepath.Join(t.TempDir(), "config.yaml")
		data := []byte("admin:\n  " + key + ": legacy-value\n")
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(path, false); err == nil {
			t.Fatalf("legacy admin key %q was accepted", key)
		}
	}
}

func TestValidateRequires0660ForBothUnixSockets(t *testing.T) {
	for _, mutate := range []func(*Config){
		func(cfg *Config) {
			cfg.Server.UnixSocketEnabled = true
			cfg.Server.FrontendSocketModeText = "0600"
		},
		func(cfg *Config) { cfg.UpstreamNginx.UpstreamSocketModeText = "0600" },
	} {
		cfg := Default()
		mutate(&cfg)
		if err := cfg.Validate(); err == nil {
			t.Fatal("Unix socket mode 0600 was accepted")
		}
	}
}

func TestWebSettingsApplyOperationalValuesAndPreserveFileOnlyPaths(t *testing.T) {
	base := Default()
	settings := WebSettingsFrom(base)
	settings.Server.UnixSocketEnabled = false
	settings.Server.LocalAddress = "127.0.0.2"
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
	settings.Security.TrustedProxyCIDRs = []string{"127.0.0.2/32"}
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
	settings.Webhook.Enabled = true
	settings.Webhook.URL = "http://127.0.0.1/hooks"
	settings.Webhook.Secret = "webhook-secret"
	settings.Webhook.SecretConfigured = true
	settings.Webhook.Events = []string{"config_change", "security_alert"}
	settings.Webhook.Timeout = "9s"
	settings.Webhook.AllowHTTP = true
	settings.Webhook.AllowPrivate = true

	applied, err := settings.Apply(base)
	if err != nil {
		t.Fatal(err)
	}
	if network, address := applied.FrontendEndpoint(); network != "tcp" || address != "127.0.0.2:19081" {
		t.Fatalf("frontend endpoint = %s %s", network, address)
	}
	if applied.Cache.MaxSizeBytes != 64<<30 || applied.Cache.Inactive != 48*time.Hour || applied.HTTP.PublicBaseURL != "https://mirror.example.com" {
		t.Fatalf("Web settings were not applied: %+v", applied)
	}
	if applied.Webhook.URL != settings.Webhook.URL || applied.Webhook.Secret != settings.Webhook.Secret || applied.Webhook.Timeout != 9*time.Second || !applied.Webhook.AllowHTTP || !applied.Webhook.AllowPrivate {
		t.Fatalf("Webhook Web settings were not applied: %+v", applied.Webhook)
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
	settings = WebSettingsFrom(Default())
	settings.Security.TrustedProxyCIDRs = []string{"not-a-cidr"}
	if _, err := settings.Apply(Default()); err == nil {
		t.Fatal("invalid trusted proxy CIDR was accepted")
	}
	legacy := WebSettingsFrom(Default())
	legacy.Webhook = nil
	applied, err := legacy.Apply(Default())
	if err != nil || applied.Webhook.Timeout != Default().Webhook.Timeout || !reflect.DeepEqual(applied.Webhook.Events, Default().Webhook.Events) {
		t.Fatalf("legacy settings document did not inherit the new webhook section: webhook=%+v err=%v", applied.Webhook, err)
	}
	legacy = WebSettingsFrom(Default())
	legacy.Server.LocalAddress = ""
	base := Default()
	base.Server.LocalAddress = "127.0.0.2"
	applied, err = legacy.Apply(base)
	if err != nil || applied.Server.LocalAddress != base.Server.LocalAddress {
		t.Fatalf("legacy settings document did not inherit server.local_address: address=%q err=%v", applied.Server.LocalAddress, err)
	}
}

func TestDistributedAndWebhookURLsUseIndependentOutboundPolicy(t *testing.T) {
	for _, rawURL := range []string{
		"http://edge.example.com",
		"https://user:pass@edge.example.com",
		"https://edge.example.com/prefix",
		"https://edge.example.com?token=secret",
		"https://edge.example.com#fragment",
	} {
		candidate := Default()
		candidate.Distributed.Enabled = true
		candidate.Distributed.Role = "coordinator"
		candidate.Distributed.Token = "cluster-secret"
		candidate.Distributed.MutationTokenKeyFiles = []string{"/etc/mirrorrelay/cluster-mutation-token.key"}
		candidate.Distributed.Node.Name = "coordinator-1"
		candidate.Distributed.Nodes = []DistributedNodeSeed{{Name: "edge", URL: rawURL, MutationToken: "edge-mutation-secret", Enabled: true}}
		if err := candidate.Validate(); err == nil {
			t.Fatalf("invalid cluster origin %q was accepted", rawURL)
		}
	}

	httpCluster := Default()
	httpCluster.Distributed.Enabled = true
	httpCluster.Distributed.Role = "coordinator"
	httpCluster.Distributed.Token = "cluster-secret"
	httpCluster.Distributed.MutationTokenKeyFiles = []string{"/etc/mirrorrelay/cluster-mutation-token.key"}
	httpCluster.Distributed.Node.Name = "coordinator-1"
	httpCluster.Distributed.AllowHTTP = true
	httpCluster.Distributed.Nodes = []DistributedNodeSeed{{Name: "edge", URL: "http://edge.example.com:8080", MutationToken: "edge-mutation-secret", Enabled: true}}
	if err := httpCluster.Validate(); err != nil {
		t.Fatalf("explicitly allowed HTTP cluster origin was rejected: %v", err)
	}
	duplicateMutation := httpCluster
	duplicateMutation.Distributed.Nodes = []DistributedNodeSeed{
		{Name: "edge-a", URL: "http://edge-a.example.com:8080", MutationToken: "edge-mutation-secret", Enabled: true},
		{Name: "edge-b", URL: "http://edge-b.example.com:8080", MutationToken: "edge-mutation-secret", Enabled: true},
	}
	if err := duplicateMutation.Validate(); err == nil {
		t.Fatal("Coordinator accepted a mutation credential shared by two Edge seeds")
	}
	probeAsMutation := httpCluster
	probeAsMutation.Distributed.Nodes[0].MutationToken = probeAsMutation.Distributed.Token
	if err := probeAsMutation.Validate(); err == nil {
		t.Fatal("Coordinator accepted its read-only probe credential as an Edge mutation credential")
	}

	missingToken := httpCluster
	missingToken.Distributed.Token = ""
	if err := missingToken.Validate(); err == nil {
		t.Fatal("enabled distributed mode accepted an empty cluster token")
	}

	missingKeyring := httpCluster
	missingKeyring.Distributed.MutationTokenKeyFiles = nil
	if err := missingKeyring.Validate(); err == nil {
		t.Fatal("Coordinator accepted an empty mutation-token encryption keyring")
	}
	relativeKey := httpCluster
	relativeKey.Distributed.MutationTokenKeyFiles = []string{"cluster.key"}
	if err := relativeKey.Validate(); err == nil {
		t.Fatal("Coordinator accepted a relative mutation-token encryption key path")
	}

	edge := Default()
	edge.Distributed.Enabled = true
	edge.Distributed.Role = "edge"
	edge.Distributed.Token = "probe-secret"
	edge.Distributed.MutationToken = "edge-mutation-secret"
	edge.Distributed.CoordinatorID = "coordinator-1"
	edge.Distributed.Node.Name = "edge-1"
	if err := edge.Validate(); err != nil {
		t.Fatalf("valid split Edge credentials were rejected: %v", err)
	}
	edge.Distributed.MutationToken = edge.Distributed.Token
	if err := edge.Validate(); err == nil {
		t.Fatal("Edge accepted the probe credential as its mutation credential")
	}
	edge.Distributed.MutationToken = ""
	if err := edge.Validate(); err == nil {
		t.Fatal("Edge accepted an empty mutation credential")
	}

	webhook := Default()
	webhook.Webhook.Enabled = true
	webhook.Webhook.URL = "http://hooks.example.com/notify"
	if err := webhook.Validate(); err == nil {
		t.Fatal("webhook HTTP URL was accepted without its independent opt-in")
	}
	webhook.Webhook.AllowHTTP = true
	if err := webhook.Validate(); err != nil {
		t.Fatalf("explicitly allowed HTTP webhook URL was rejected: %v", err)
	}
}

func TestFrontendTCPAddressAndExplicitUpstreamTCPAreConfigurable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	data := []byte("server:\n  local_address: ::1\n  local_port: 19081\nupstream_nginx:\n  upstream_unix_socket_enabled: false\n  upstream_local_port: 19082\n")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path, true)
	if err != nil {
		t.Fatal(err)
	}
	if network, address := cfg.FrontendEndpoint(); network != "tcp" || address != "[::1]:19081" {
		t.Fatalf("frontend endpoint = %s %s", network, address)
	}
	if network, address := cfg.UpstreamEndpoint(); network != "tcp" || address != "127.0.0.1:19082" {
		t.Fatalf("upstream endpoint = %s %s", network, address)
	}
}

func TestEndpointDefaultsUseFrontendTCPAndUpstreamUnixSocket(t *testing.T) {
	cfg := Default()
	if cfg.Server.UnixSocketEnabled {
		t.Fatal("frontend Unix socket is enabled by default")
	}
	if network, address := cfg.FrontendEndpoint(); network != "tcp" || address != "127.0.0.1:9081" {
		t.Fatalf("default frontend endpoint = %s %s", network, address)
	}
	if !cfg.UpstreamNginx.UpstreamSocketEnabled {
		t.Fatal("Managed Upstream Nginx Unix socket is disabled by default")
	}
	if network, address := cfg.UpstreamEndpoint(); network != "unix" || address != "/run/mirrorrelay/upstream.sock" {
		t.Fatalf("default upstream endpoint = %s %s", network, address)
	}
}

func TestFrontendTCPAddressMustBeExplicitValidIP(t *testing.T) {
	for _, address := range []string{"", "localhost", "169.254.1.2", "224.0.0.1", "255.255.255.255", " 127.0.0.1"} {
		cfg := Default()
		cfg.Server.LocalAddress = address
		if err := cfg.Validate(); err == nil {
			t.Fatalf("invalid frontend TCP address %q was accepted", address)
		}
	}

	for _, address := range []string{"127.0.0.1", "127.0.0.2", "::1", "0.0.0.0", "::", "192.168.10.8", "2001:db8::8"} {
		cfg := Default()
		cfg.Server.LocalAddress = address
		if err := cfg.Validate(); err != nil {
			t.Fatalf("valid frontend TCP address %q was rejected: %v", address, err)
		}
	}
}

func TestWildcardFrontendListenAddressUsesLoopbackConnectEndpoint(t *testing.T) {
	cfg := Default()
	cfg.Server.LocalAddress = "0.0.0.0"
	if network, address := cfg.FrontendEndpoint(); network != "tcp" || address != "127.0.0.1:9081" {
		t.Fatalf("IPv4 wildcard frontend endpoint = %s %s", network, address)
	}
	cfg.Server.LocalAddress = "::"
	if network, address := cfg.FrontendEndpoint(); network != "tcp" || address != "[::1]:9081" {
		t.Fatalf("IPv6 wildcard frontend endpoint = %s %s", network, address)
	}
}

func TestWildcardFrontendTCPCannotReuseExplicitUpstreamTCPPort(t *testing.T) {
	cfg := Default()
	cfg.Server.LocalAddress = "0.0.0.0"
	cfg.UpstreamNginx.UpstreamSocketEnabled = false
	cfg.UpstreamNginx.UpstreamLocalPort = cfg.Server.LocalPort
	if err := cfg.Validate(); err == nil {
		t.Fatal("overlapping frontend wildcard and upstream loopback TCP ports were accepted")
	}
}

func TestDistinctFrontendIPCanReuseExplicitUpstreamTCPPort(t *testing.T) {
	for _, address := range []string{"127.0.0.2", "::1"} {
		cfg := Default()
		cfg.Server.LocalAddress = address
		cfg.UpstreamNginx.UpstreamSocketEnabled = false
		cfg.UpstreamNginx.UpstreamLocalPort = cfg.Server.LocalPort
		if err := cfg.Validate(); err != nil {
			t.Fatalf("distinct frontend address %q was rejected: %v", address, err)
		}
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
			cfg.Server.UnixSocketEnabled = true
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

func TestExportYAMLOmissionAndFullBackup(t *testing.T) {
	cfg := Default()
	cfg.HTTP.PublicBaseURL = "https://mirror.example.com"
	cfg.Distributed.Node.PublicBaseURL = "https://node.example.com"
	cfg.Distributed.Token = "super-cluster-token"
	cfg.Distributed.MutationToken = "super-mutation-token"
	cfg.Webhook.Secret = "super-webhook-secret"
	cfg.Distributed.Nodes = []DistributedNodeSeed{
		{
			Name:          "edge-1",
			URL:           "https://edge1.example.com",
			MutationToken: "edge-1-secret",
			Region:        "us-west",
			Priority:      100,
			Weight:        100,
			Enabled:       true,
		},
	}

	// Normal Export (Standard)
	standardYAML, err := ExportYAML(cfg, false)
	if err != nil {
		t.Fatalf("ExportYAML standard error: %v", err)
	}
	if strings.Contains(standardYAML, "https://mirror.example.com") {
		t.Fatal("standard export contains http.public_base_url")
	}
	if strings.Contains(standardYAML, "https://node.example.com") {
		t.Fatal("standard export contains distributed.node.public_base_url")
	}
	if strings.Contains(standardYAML, "super-cluster-token") || strings.Contains(standardYAML, "super-mutation-token") || strings.Contains(standardYAML, "super-webhook-secret") || strings.Contains(standardYAML, "edge-1-secret") {
		t.Fatal("standard export leaked sensitive credentials")
	}
	if !strings.Contains(standardYAML, "https://edge1.example.com") {
		t.Fatal("standard export omitted topology node URL")
	}

	// Full Backup Export
	fullYAML, err := ExportYAML(cfg, true)
	if err != nil {
		t.Fatalf("ExportYAML full error: %v", err)
	}
	if strings.Contains(fullYAML, "https://mirror.example.com") {
		t.Fatal("full export contains http.public_base_url")
	}
	if strings.Contains(fullYAML, "https://node.example.com") {
		t.Fatal("full export contains distributed.node.public_base_url")
	}
	if !strings.Contains(fullYAML, "super-cluster-token") || !strings.Contains(fullYAML, "super-mutation-token") || !strings.Contains(fullYAML, "super-webhook-secret") || !strings.Contains(fullYAML, "edge-1-secret") {
		t.Fatal("full export failed to include sensitive credentials needed for backup")
	}
}

func TestComputeSettingsDiffRedactsSensitiveChanges(t *testing.T) {
	oldWS := WebSettingsFrom(Default())
	newWS := WebSettingsFrom(Default())
	newWS.Server.LocalPort = 19081
	newWS.Distributed.Token = "new-token"
	newWS.Webhook.Secret = "new-secret"

	diff := ComputeSettingsDiff(oldWS, newWS)
	if len(diff) != 3 {
		t.Fatalf("expected 3 diff entries, got %d: %+v", len(diff), diff)
	}
	for _, d := range diff {
		if d.Path == "distributed.token" || d.Path == "webhook.secret" {
			if d.NewValue != "[REDACTED]" {
				t.Fatalf("diff for %s leaked secret: %s", d.Path, d.NewValue)
			}
		}
	}
}
