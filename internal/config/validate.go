package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/LuisCMerrick/MirrorRelay/internal/model"
	"github.com/LuisCMerrick/MirrorRelay/internal/security"
)

func (c Config) Validate() error {
	if c.Server.UnixSocketEnabled {
		frontendMode, err := parseSocketMode(c.Server.FrontendSocketModeText)
		if err != nil || frontendMode != 0o660 {
			return errors.New("server.frontend_socket_mode must be 0660")
		}
		if strings.TrimSpace(c.Server.FrontendSocket) == "" {
			return errors.New("server.frontend_socket is required when Unix sockets are enabled")
		}
	} else {
		if !validLocalAddress(c.Server.LocalAddress) {
			return errors.New("server.local_address must be a valid IP listen address when the frontend Unix socket is disabled")
		}
		if !validPort(c.Server.LocalPort) {
			return errors.New("server.local_port must be 1..65535 when the frontend Unix socket is disabled")
		}
	}
	if c.UpstreamNginx.UpstreamSocketEnabled {
		upstreamMode, err := parseSocketMode(c.UpstreamNginx.UpstreamSocketModeText)
		if err != nil || upstreamMode != 0o660 {
			return errors.New("upstream_nginx.upstream_socket_mode must be 0660")
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
	if !c.Server.UnixSocketEnabled && !c.UpstreamNginx.UpstreamSocketEnabled &&
		c.Server.LocalPort == c.UpstreamNginx.UpstreamLocalPort && frontendAddressConflictsWithUpstreamLoopback(c.Server.LocalAddress) {
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
	if c.Limits.MaxTotalConcurrency < 0 || c.Limits.MaxIPConcurrency < 0 || c.Limits.BandwidthLimitBPS < 0 ||
		c.Warmup.MaxConcurrency < 0 || c.Warmup.BandwidthLimit < 0 || c.Warmup.RetryCount < 0 {
		return errors.New("global and warmup limits cannot be negative")
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
	for _, cidr := range c.Security.TrustedProxyCIDRs {
		if _, _, err := net.ParseCIDR(cidr); err != nil {
			return fmt.Errorf("invalid trusted proxy CIDR %q: %w", cidr, err)
		}
	}
	if len(c.Admin.Passkey.RPName) > 128 {
		return errors.New("admin.passkey.rp_name must not exceed 128 bytes")
	}
	if c.Admin.Passkey.RPID != "" && !validWebAuthnRPID(c.Admin.Passkey.RPID) {
		return errors.New("admin.passkey.rp_id must be a lowercase hostname or IP address without scheme, port, path or trailing dot")
	}
	seenOrigins := make(map[string]bool, len(c.Admin.Passkey.Origins))
	for _, origin := range c.Admin.Passkey.Origins {
		u, err := url.Parse(origin)
		if err != nil || u.Host == "" || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || !validURLAuthority(u.Host) {
			return fmt.Errorf("admin.passkey origin %q must contain only an http or https scheme and authority", origin)
		}
		host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
		if host == "" || (u.Scheme == "http" && !isLoopbackHostname(host)) {
			return fmt.Errorf("admin.passkey origin %q must use https except on a loopback host", origin)
		}
		canonical := strings.ToLower(u.Scheme) + "://" + strings.ToLower(u.Host)
		if seenOrigins[canonical] {
			return fmt.Errorf("duplicate admin.passkey origin %q", origin)
		}
		seenOrigins[canonical] = true
		if c.Admin.Passkey.RPID != "" && !webAuthnHostMatchesRPID(host, c.Admin.Passkey.RPID) {
			return fmt.Errorf("admin.passkey origin host %q is outside rp_id %q", host, c.Admin.Passkey.RPID)
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
	seenKeyFiles := make(map[string]bool, len(c.Distributed.MutationTokenKeyFiles))
	for _, keyFile := range c.Distributed.MutationTokenKeyFiles {
		if strings.TrimSpace(keyFile) != keyFile || !filepath.IsAbs(keyFile) {
			return errors.New("distributed.mutation_token_key_files entries must be absolute paths without surrounding whitespace")
		}
		cleaned := filepath.Clean(keyFile)
		if seenKeyFiles[cleaned] {
			return fmt.Errorf("distributed.mutation_token_key_files contains duplicate path %q", cleaned)
		}
		seenKeyFiles[cleaned] = true
	}
	if c.Distributed.Enabled || c.Distributed.Role != "standalone" {
		probeToken := strings.TrimSpace(c.Distributed.Token)
		if c.Distributed.Enabled && probeToken == "" {
			return errors.New("distributed.token probe credential is required when distributed mode is enabled")
		}
		if c.Distributed.Enabled && probeToken != c.Distributed.Token {
			return errors.New("distributed.token probe credential must not contain surrounding whitespace")
		}
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
			if len(c.Distributed.MutationTokenKeyFiles) == 0 {
				return errors.New("distributed.mutation_token_key_files requires at least one key file for a coordinator")
			}
			if strings.TrimSpace(c.Distributed.Node.Name) == "" {
				return errors.New("distributed.node.name is required for a coordinator")
			}
			if c.Distributed.HealthCheck.Interval <= 0 || c.Distributed.HealthCheck.Timeout <= 0 {
				return errors.New("distributed.health_check interval and timeout must be positive")
			}
			mutationTokens := make(map[string]string, len(c.Distributed.Nodes))
			for _, seed := range c.Distributed.Nodes {
				if strings.TrimSpace(seed.URL) == "" {
					return errors.New("distributed node seed URL cannot be empty")
				}
				if _, err := security.ParseOriginURL(seed.URL, c.Distributed.AllowHTTP); err != nil {
					return fmt.Errorf("invalid distributed node seed URL %q: %w", seed.URL, err)
				}
				mutationToken := strings.TrimSpace(seed.MutationToken)
				if mutationToken == "" {
					return fmt.Errorf("distributed node seed %q requires a unique mutation_token", seed.Name)
				}
				if mutationToken != seed.MutationToken {
					return fmt.Errorf("distributed node seed %q mutation_token must not contain surrounding whitespace", seed.Name)
				}
				if mutationToken == probeToken {
					return fmt.Errorf("distributed node seed %q mutation_token must differ from distributed.token", seed.Name)
				}
				if previous, exists := mutationTokens[mutationToken]; exists {
					return fmt.Errorf("distributed node seeds %q and %q must not share a mutation_token", previous, seed.Name)
				}
				mutationTokens[mutationToken] = seed.Name
			}
		}
		if c.Distributed.Role == "edge" {
			if strings.TrimSpace(c.Distributed.Node.Name) == "" {
				return errors.New("distributed.node.name is required for an edge")
			}
			mutationToken := strings.TrimSpace(c.Distributed.MutationToken)
			if mutationToken == "" {
				return errors.New("distributed.mutation_token is required for an edge")
			}
			if mutationToken != c.Distributed.MutationToken {
				return errors.New("distributed.mutation_token must not contain surrounding whitespace")
			}
			if mutationToken == probeToken {
				return errors.New("distributed.mutation_token must differ from the distributed.token probe credential")
			}
			if strings.TrimSpace(c.Distributed.CoordinatorID) == "" {
				return errors.New("distributed.coordinator_id is required for an edge")
			}
			if c.Distributed.Node.PublicBaseURL != "" {
				if _, err := security.ParseOriginURL(c.Distributed.Node.PublicBaseURL, c.Distributed.AllowHTTP); err != nil {
					return fmt.Errorf("invalid distributed node public_base_url %q: %w", c.Distributed.Node.PublicBaseURL, err)
				}
			}
		}
	}
	if err := ValidateUIEnhancement(&c.UIEnhancement); err != nil {
		return err
	}
	if err := ValidateWebhook(&c.Webhook); err != nil {
		return err
	}
	return nil
}

func ValidateWebhook(w *model.WebhookConfig) error {
	if !w.Enabled {
		return nil
	}
	if w.URL == "" {
		return errors.New("webhook.url is required when webhook is enabled")
	}
	if err := security.ValidateOutboundURLSyntax(w.URL, w.AllowHTTP); err != nil {
		return fmt.Errorf("invalid webhook url %q: %w", w.URL, err)
	}
	if w.Timeout <= 0 {
		w.Timeout = 5 * time.Second
	}
	return nil
}

func ValidateUIEnhancement(c *model.UIEnhancementConfig) error {
	switch c.Theme {
	case "", "system", "light", "dark":
		if c.Theme == "" {
			c.Theme = "system"
		}
	default:
		return fmt.Errorf("ui_enhancement.theme must be system, light or dark, got %q", c.Theme)
	}
	if c.AccentColor == "" {
		c.AccentColor = "#2563eb"
	}
	if !validHexColor(c.AccentColor) {
		return fmt.Errorf("ui_enhancement.accent_color must be a valid hex color code, got %q", c.AccentColor)
	}
	if c.Branding.Title == "" {
		c.Branding.Title = "MirrorRelay"
	}
	if len(c.Branding.Title) > 128 {
		return errors.New("ui_enhancement.branding.title must be at most 128 characters")
	}
	if err := validateBrandAssetPath("ui_enhancement.branding.logo", c.Branding.Logo); err != nil {
		return err
	}
	if err := validateBrandAssetPath("ui_enhancement.branding.favicon", c.Branding.Favicon); err != nil {
		return err
	}
	if c.Login.Title == "" {
		c.Login.Title = "MirrorRelay"
	}
	if len(c.Login.Title) > 128 {
		return errors.New("ui_enhancement.login.title must be at most 128 characters")
	}
	if len(c.Login.Subtitle) > 256 {
		return errors.New("ui_enhancement.login.subtitle must be at most 256 characters")
	}
	if c.CustomCSS.Enabled && c.CustomCSS.File == "" {
		c.CustomCSS.File = "/var/lib/mirrorrelay/ui/custom.css"
	}
	if c.CustomCSS.File != "" {
		if len(c.CustomCSS.File) > 4096 || !filepath.IsAbs(c.CustomCSS.File) || filepath.Clean(c.CustomCSS.File) != c.CustomCSS.File || filepath.Ext(c.CustomCSS.File) != ".css" || strings.ContainsAny(c.CustomCSS.File, "\x00\r\n\t") {
			return errors.New("ui_enhancement.custom_css.file must be a clean absolute path ending in .css")
		}
	}
	return nil
}

func validateBrandAssetPath(name, raw string) error {
	if raw == "" {
		return nil
	}
	if len(raw) > 2048 || !strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, "//") || strings.ContainsAny(raw, "\\\r\n\t") {
		return fmt.Errorf("%s must be an optional same-origin absolute path", name)
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.IsAbs() || parsed.Host != "" || parsed.User != nil || parsed.Fragment != "" || parsed.Path == "" {
		return fmt.Errorf("%s must be an optional same-origin absolute path", name)
	}
	return nil
}

func validHexColor(v string) bool {
	if !strings.HasPrefix(v, "#") {
		return false
	}
	hexStr := strings.TrimPrefix(v, "#")
	if len(hexStr) != 3 && len(hexStr) != 6 {
		return false
	}
	for _, ch := range hexStr {
		if (ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f') || (ch >= 'A' && ch <= 'F') {
			continue
		}
		return false
	}
	return true
}

func validateResolver(value string) error {
	if strings.TrimSpace(value) == "system" {
		return nil
	}
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

func validWebAuthnRPID(value string) bool {
	if value == "" || value != strings.TrimSpace(value) || value != strings.ToLower(value) || strings.HasSuffix(value, ".") || len(value) > 253 {
		return false
	}
	if ip := net.ParseIP(value); ip != nil {
		return true
	}
	for _, label := range strings.Split(value, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, character := range label {
			if (character >= 'a' && character <= 'z') || (character >= '0' && character <= '9') || character == '-' {
				continue
			}
			return false
		}
	}
	return true
}

func isLoopbackHostname(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}

func webAuthnHostMatchesRPID(host, rpID string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	rpID = strings.ToLower(strings.TrimSuffix(rpID, "."))
	if rpIP := net.ParseIP(rpID); rpIP != nil {
		hostIP := net.ParseIP(host)
		return hostIP != nil && hostIP.Equal(rpIP)
	}
	return host == rpID || strings.HasSuffix(host, "."+rpID)
}

func parseSocketMode(value string) (os.FileMode, error) {
	parsed, err := strconv.ParseUint(strings.TrimSpace(value), 8, 32)
	return os.FileMode(parsed), err
}

func validPort(port int) bool { return port >= 1 && port <= 65535 }

func validLocalAddress(address string) bool {
	if address == "" || strings.TrimSpace(address) != address {
		return false
	}
	ip := net.ParseIP(address)
	return ip != nil && !ip.IsMulticast() && !ip.IsLinkLocalUnicast() && !ip.Equal(net.IPv4bcast)
}

func frontendAddressConflictsWithUpstreamLoopback(address string) bool {
	ip := net.ParseIP(address)
	return ip != nil && (ip.IsUnspecified() || ip.Equal(net.IPv4(127, 0, 0, 1)))
}

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
