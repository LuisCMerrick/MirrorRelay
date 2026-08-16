package upstreamnginx

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/LuisCMerrick/MirrorRelay/internal/config"
	"github.com/LuisCMerrick/MirrorRelay/internal/mirror"
	"github.com/LuisCMerrick/MirrorRelay/internal/model"
	"github.com/LuisCMerrick/MirrorRelay/internal/security"
)

type Generated struct {
	Main      string            `json:"main"`
	Files     map[string]string `json:"files"`
	Effective string            `json:"effective"`
	Hash      string            `json:"hash"`
}

type Generator struct {
	cfg      config.Config
	resolver security.Resolver
}

func NewGenerator(cfg config.Config, resolver security.Resolver) *Generator {
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	return &Generator{cfg: cfg, resolver: resolver}
}

func (g *Generator) Generate(ctx context.Context, repositories []model.Mirror, custom []model.CustomConfig) (Generated, error) {
	if err := mirror.ValidateRouteConflicts(repositories, g.cfg.Admin.Path, g.cfg.HTTP.PublicBaseURL, g.cfg.Admin.Host); err != nil {
		return Generated{}, err
	}
	fragments, err := classifyCustom(custom)
	if err != nil {
		return Generated{}, err
	}
	repositoryIDs := make(map[int64]bool, len(repositories))
	for _, repository := range repositories {
		repositoryIDs[repository.ID] = true
	}
	for repositoryID := range fragments.byRepository {
		if !repositoryIDs[repositoryID] {
			return Generated{}, fmt.Errorf("custom configuration references unknown repository %d", repositoryID)
		}
	}
	for repositoryID := range fragments.byUpstreamRepository {
		if !repositoryIDs[repositoryID] {
			return Generated{}, fmt.Errorf("custom upstream configuration references unknown repository %d", repositoryID)
		}
	}
	var routes strings.Builder
	var upstreamGroups strings.Builder
	seenHosts := make(map[string]string)
	seenPaths := make(map[string]string)
	for _, repository := range repositories {
		if !repository.Enabled {
			continue
		}
		if repository.PublicMode == "host" {
			key := strings.ToLower(repository.PublicHost)
			if existing := seenHosts[key]; existing != "" {
				return Generated{}, fmt.Errorf("public host %s is used by both %s and %s", key, existing, repository.Slug)
			}
			seenHosts[key] = repository.Slug
		} else {
			key := strings.TrimSuffix(repository.PublicPath, "/") + "/"
			if existing := seenPaths[key]; existing != "" {
				return Generated{}, fmt.Errorf("public path %s is used by both %s and %s", key, existing, repository.Slug)
			}
			seenPaths[key] = repository.Slug
		}
		route, groups, routeErr := g.repository(ctx, repository, fragments.byRepository[repository.ID], fragments.byUpstreamRepository[repository.ID])
		if routeErr != nil {
			return Generated{}, fmt.Errorf("repository %s: %w", repository.Slug, routeErr)
		}
		routes.WriteString(route)
		upstreamGroups.WriteString(groups)
	}
	routes.WriteString(fragments.server)

	files := map[string]string{
		"repositories.conf":               routes.String(),
		"upstreams.conf":                  upstreamGroups.String(),
		"external-nginx-integration.conf": g.integrationSnippet(repositories),
	}
	main := g.mainConfig(fragments.http)
	var effective strings.Builder
	effective.WriteString("# BEGIN SYSTEM\n")
	effective.WriteString(main)
	effective.WriteString("# END SYSTEM\n")
	keys := make([]string, 0, len(files))
	for key := range files {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		fmt.Fprintf(&effective, "\n# BEGIN GENERATED: %s\n%s# END GENERATED\n", key, files[key])
	}
	sum := sha256.Sum256([]byte(effective.String()))
	return Generated{
		Main:      main,
		Files:     files,
		Effective: effective.String(),
		Hash:      hex.EncodeToString(sum[:]),
	}, nil
}

type customFragments struct {
	http                 string
	server               string
	byRepository         map[int64]string
	byUpstreamRepository map[int64]string
}

func classifyCustom(values []model.CustomConfig) (customFragments, error) {
	result := customFragments{byRepository: make(map[int64]string), byUpstreamRepository: make(map[int64]string)}
	for _, value := range values {
		if !value.Enabled {
			continue
		}
		if err := ValidateCustomName(value.Name); err != nil {
			return result, fmt.Errorf("custom configuration name: %w", err)
		}
		if err := ValidateCustom(value.Context, value.Content); err != nil {
			return result, fmt.Errorf("custom %s: %w", value.Name, err)
		}
		fragment := "        # BEGIN CUSTOM: " + value.Name + "\n" + indent(value.Content, "        ") + "\n        # END CUSTOM: " + value.Name + "\n"
		switch value.Context {
		case "http":
			if value.RepositoryID != 0 {
				return result, fmt.Errorf("custom %s: http context must be global", value.Name)
			}
			result.http += fragment
		case "upstream":
			if value.RepositoryID <= 0 {
				return result, fmt.Errorf("custom %s: upstream context requires repository_id", value.Name)
			}
			result.byUpstreamRepository[value.RepositoryID] += fragment
		case "server":
			if value.RepositoryID != 0 {
				return result, fmt.Errorf("custom %s: server context must be global", value.Name)
			}
			result.server += fragment
		case "location", "repository":
			if value.RepositoryID <= 0 {
				return result, fmt.Errorf("custom %s: %s context requires repository_id", value.Name, value.Context)
			}
			result.byRepository[value.RepositoryID] += fragment
		default:
			return result, fmt.Errorf("custom %s: unsupported context %s", value.Name, value.Context)
		}
	}
	return result, nil
}

func (g *Generator) mainConfig(customHTTP string) string {
	prefix := absolutePath(g.cfg.UpstreamNginx.Prefix)
	logPath := absolutePath(g.cfg.UpstreamNginx.LogPath)
	cachePath := absolutePath(g.cfg.Cache.Path)
	frontendServer := nginxEndpoint(g.cfg.FrontendEndpoint())
	upstreamListen := nginxEndpoint(g.cfg.UpstreamEndpoint())
	workerUser := workerUserDirective(g.cfg.UpstreamNginx.WorkerUser, os.Geteuid())
	standalone := ""
	if g.cfg.Ingress.Mode == "managed-standalone" {
		tlsProtocols := "TLSv1.2 TLSv1.3"
		if g.cfg.TLS.MinVersion == "1.3" {
			tlsProtocols = "TLSv1.3"
		}
		standalone = fmt.Sprintf(`
    upstream mirrorrelay_frontend {
        server %s;
        keepalive 64;
    }
    server {
        listen %s default_server;
        return 308 https://$host$request_uri;
    }
    server {
        listen %s ssl http2 default_server;
        server_name _;
        ssl_certificate %s;
        ssl_certificate_key %s;
        ssl_protocols %s;
        location / {
            proxy_pass http://mirrorrelay_frontend;
            proxy_http_version 1.1;
            proxy_request_buffering off;
            proxy_buffering off;
            proxy_set_header Host $host;
            proxy_set_header X-Real-IP $remote_addr;
            proxy_set_header X-Forwarded-For $remote_addr;
            proxy_set_header X-Forwarded-Proto https;
%s
            proxy_read_timeout 1h;
            proxy_send_timeout 1h;
            gzip off;
        }
    }
`, frontendServer, nginxListen(g.cfg.HTTP.Listen), nginxListen(g.cfg.HTTP.HTTPSListen),
			absolutePath(g.cfg.TLS.Certificate), absolutePath(g.cfg.TLS.PrivateKey), tlsProtocols, indent(internalHeaderClears(), "            "))
	}

	return fmt.Sprintf(`# Generated by MirrorRelay. Manual edits are overwritten.
%sworker_processes %s;
pid %s;
lock_file %s/run/nginx.lock;
error_log %s/error.log warn;

events { worker_connections %d; }

http {
    default_type application/octet-stream;
    sendfile on;
    tcp_nopush on;
    keepalive_timeout 65;
    server_tokens off;
    client_max_body_size 1m;
    client_body_temp_path %s/temp/client;
    proxy_temp_path %s/temp/proxy;
    proxy_cache_path %s levels=1:2 keys_zone=mirrorrelay_cache:64m inactive=%s max_size=%d min_free=%d use_temp_path=off;
    log_format mirrorrelay '$time_iso8601 request_id=$http_x_mirror_internal_request_id client_ip=$http_x_mirror_internal_client_ip repo=$http_x_mirror_internal_repository_id host=$host method=$request_method uri="$request_uri" status=$status bytes=$body_bytes_sent request_time=$request_time upstream="$upstream_addr" upstream_status="$upstream_status" upstream_time="$upstream_response_time" cache=$upstream_cache_status';
    access_log %s/access.log mirrorrelay;
    resolver %s valid=%s ipv6=on;

%s
    include %s/current/generated/upstreams.conf;
    server {
        listen %s;
        server_name mirrorrelay-upstream-nginx-internal;
        access_log %s/access.log mirrorrelay;
        include %s/current/generated/repositories.conf;
    }
%s}
`, workerUser, g.cfg.UpstreamNginx.WorkerProcesses, absolutePath(g.cfg.UpstreamNginx.PID), prefix, logPath,
		g.cfg.UpstreamNginx.WorkerConnections, prefix, prefix, cachePath, nginxDuration(g.cfg.Cache.Inactive),
		g.cfg.Cache.MaxSizeBytes, g.cfg.Cache.MinimumFreeBytes, logPath, g.cfg.UpstreamNginx.Resolver, nginxDuration(g.cfg.UpstreamNginx.ResolverRefresh),
		customHTTP, prefix, upstreamListen, logPath, prefix, standalone)
}

func workerUserDirective(value string, effectiveUID int) string {
	value = strings.TrimSpace(value)
	if effectiveUID != 0 || value == "" {
		return ""
	}
	return "user " + value + ";\n"
}

var cacheClasses = []struct {
	name string
}{
	{name: "metadata"},
	{name: "package"},
	{name: "immutable"},
	{name: "blob"},
}

func (g *Generator) repository(ctx context.Context, repository model.Mirror, custom, upstreamCustom string) (string, string, error) {
	if _, err := activeUpstream(repository.Upstreams); err != nil {
		return "", "", err
	}
	var route strings.Builder
	var groups strings.Builder
	fmt.Fprintf(&route, "        # BEGIN REPOSITORY: %s [GENERATED]\n", repository.Name)
	for _, upstream := range repository.Upstreams {
		if !upstream.Enabled {
			continue
		}
		targets, err := g.resolveUpstream(ctx, repository, upstream)
		if err != nil {
			return "", "", fmt.Errorf("upstream %d: %w", upstream.ID, err)
		}
		g.writeUpstreamGroup(&groups, repository, upstream, targets, upstreamCustom)
		for _, class := range cacheClasses {
			g.writeRepositoryLocation(&route, repository, upstream, class.name, cacheTTL(g.cfg, repository, class.name), custom)
			if repository.HTMLRewriteEnabled {
				g.writeAuxiliaryLocation(&route, repository, upstream, class.name, cacheTTL(g.cfg, repository, class.name), custom)
			}
		}
		g.writeHealthLocation(&route, repository, upstream)
	}
	for _, class := range cacheClasses {
		g.writeDynamicTargetLocation(&route, repository, class.name, cacheTTL(g.cfg, repository, class.name))
	}
	route.WriteString("        # END REPOSITORY\n")
	return route.String(), groups.String(), nil
}

func (g *Generator) resolveUpstream(ctx context.Context, repository model.Mirror, upstream model.Upstream) ([]security.ApprovedTarget, error) {
	allowHTTP := g.cfg.Security.AllowHTTPUpstream && repository.AllowHTTP
	targets, err := security.ResolveApprovedTargets(ctx, upstream.URL, allowHTTP,
		g.cfg.Security.AllowPrivateUpstream && repository.AllowPrivate,
		g.cfg.Redirect.RejectMixedResult, g.resolver)
	if err != nil {
		return nil, err
	}
	parsed, err := url.Parse(upstream.URL)
	if err != nil {
		return nil, err
	}
	for _, value := range []string{parsed.Scheme, parsed.Host, parsed.Hostname(), upstream.Host, repository.HostRewrite} {
		if err := validateNginxValue(value); err != nil {
			return nil, err
		}
	}
	return targets, nil
}

func upstreamGroupName(repository model.Mirror, upstream model.Upstream) string {
	return "mirrorrelay_repo_" + strconv.FormatInt(repository.ID, 10) + "_" + strconv.FormatInt(upstream.ID, 10)
}

func (g *Generator) writeUpstreamGroup(out *strings.Builder, repository model.Mirror, upstream model.Upstream, targets []security.ApprovedTarget, custom string) {
	fmt.Fprintf(out, "    upstream %s {\n", upstreamGroupName(repository, upstream))
	for _, target := range targets {
		fmt.Fprintf(out, "        server %s max_fails=2 fail_timeout=10s;\n", target.Address)
	}
	out.WriteString("        keepalive 32;\n")
	out.WriteString(custom)
	out.WriteString("    }\n")
}

func (g *Generator) writeRepositoryLocation(out *strings.Builder, repository model.Mirror, upstream model.Upstream, class string, ttl time.Duration, custom string) {
	parsed, _ := url.Parse(upstream.URL)
	origin := parsed.Scheme + "://" + upstreamGroupName(repository, upstream)
	basePath := strings.TrimRight(parsed.EscapedPath(), "/")
	if repository.AddPrefix != "" {
		basePath += "/" + strings.Trim(repository.AddPrefix, "/")
	}
	if basePath == "" {
		basePath = "/"
	} else {
		basePath += "/"
	}
	host := upstream.Host
	if repository.HostRewrite != "" {
		host = repository.HostRewrite
	}
	if host == "" {
		host = parsed.Host
	}
	variable := "$repo_" + strconv.FormatInt(repository.ID, 10) + "_origin"
	fmt.Fprintf(out, "        location ^~ /_repo/%d/%d/%s/ {\n", repository.ID, upstream.ID, class)
	out.WriteString("            if ($request_method !~ ^(GET|HEAD)$) { return 405; }\n")
	fmt.Fprintf(out, "            set %s %q;\n", variable, origin)
	fmt.Fprintf(out, "            rewrite ^/_repo/%d/%d/%s/(.*)$ %s break;\n", repository.ID, upstream.ID, class, strconv.Quote(basePath+"$1"))
	fmt.Fprintf(out, "            proxy_pass %s;\n", variable)
	g.writeProxyCommon(out, repository, parsed, host, true)
	g.writeCache(out, repository, class, ttl)
	out.WriteString(custom)
	out.WriteString("        }\n")
}

func (g *Generator) writeHealthLocation(out *strings.Builder, repository model.Mirror, upstream model.Upstream) {
	parsed, _ := url.Parse(upstream.URL)
	origin := parsed.Scheme + "://" + upstreamGroupName(repository, upstream)
	basePath := strings.TrimRight(parsed.EscapedPath(), "/") + "/"
	host := upstream.Host
	if host == "" {
		host = parsed.Host
	}
	variable := "$health_" + strconv.FormatInt(repository.ID, 10) + "_" + strconv.FormatInt(upstream.ID, 10)
	fmt.Fprintf(out, "        location ^~ /_health/%d/%d/ {\n", repository.ID, upstream.ID)
	out.WriteString("            if ($request_method !~ ^(GET|HEAD)$) { return 405; }\n")
	fmt.Fprintf(out, "            set %s %q;\n", variable, origin)
	fmt.Fprintf(out, "            rewrite ^/_health/%d/%d/(.*)$ %s break;\n", repository.ID, upstream.ID, strconv.Quote(basePath+"$1"))
	fmt.Fprintf(out, "            proxy_pass %s;\n", variable)
	g.writeProxyCommon(out, repository, parsed, host, true)
	out.WriteString("            proxy_buffering off;\n            proxy_cache off;\n")
	out.WriteString("        }\n")
}

func (g *Generator) writeAuxiliaryLocation(out *strings.Builder, repository model.Mirror, upstream model.Upstream, class string, ttl time.Duration, custom string) {
	parsed, _ := url.Parse(upstream.URL)
	origin := parsed.Scheme + "://" + upstreamGroupName(repository, upstream)
	host := upstream.Host
	if repository.HostRewrite != "" {
		host = repository.HostRewrite
	}
	if host == "" {
		host = parsed.Host
	}
	variable := "$repo_aux_" + strconv.FormatInt(repository.ID, 10) + "_origin"
	fmt.Fprintf(out, "        location ^~ /_repo_aux/%d/%d/%s/ {\n", repository.ID, upstream.ID, class)
	out.WriteString("            if ($request_method !~ ^(GET|HEAD)$) { return 405; }\n")
	fmt.Fprintf(out, "            set %s %q;\n", variable, origin)
	fmt.Fprintf(out, "            rewrite ^/_repo_aux/%d/%d/%s/(.*)$ %s break;\n", repository.ID, upstream.ID, class, strconv.Quote("/$1"))
	fmt.Fprintf(out, "            proxy_pass %s;\n", variable)
	g.writeProxyCommon(out, repository, parsed, host, false)
	g.writeCache(out, repository, class, ttl)
	out.WriteString(custom)
	out.WriteString("        }\n")
}

func (g *Generator) writeDynamicTargetLocation(out *strings.Builder, repository model.Mirror, class string, ttl time.Duration) {
	fmt.Fprintf(out, "        location ^~ /_target/%d/%s/https/ {\n", repository.ID, class)
	out.WriteString("            if ($request_method !~ ^(GET|HEAD)$) { return 405; }\n")
	out.WriteString("            proxy_pass https://$http_x_mirror_internal_upstream_address$http_x_mirror_internal_upstream_uri;\n")
	out.WriteString("            proxy_ssl_server_name on;\n            proxy_ssl_name $http_x_mirror_internal_upstream_host;\n")
	out.WriteString("            proxy_ssl_verify on;\n")
	fmt.Fprintf(out, "            proxy_ssl_trusted_certificate %s;\n", strconv.Quote(absolutePath(g.cfg.UpstreamNginx.CABundle)))
	fmt.Fprintf(out, "            proxy_ssl_verify_depth %d;\n", g.cfg.UpstreamNginx.TLSVerifyDepth)
	out.WriteString("            proxy_set_header Host $http_x_mirror_internal_upstream_authority;\n")
	g.writeProxyHeaders(out)
	g.writeRepositoryHeaders(out, repository, false)
	g.writeCache(out, repository, class, ttl)
	out.WriteString("        }\n")
	if g.cfg.Security.AllowHTTPUpstream && repository.AllowHTTP {
		fmt.Fprintf(out, "        location ^~ /_target/%d/%s/http/ {\n", repository.ID, class)
		out.WriteString("            if ($request_method !~ ^(GET|HEAD)$) { return 405; }\n")
		out.WriteString("            proxy_pass http://$http_x_mirror_internal_upstream_address$http_x_mirror_internal_upstream_uri;\n")
		out.WriteString("            proxy_set_header Host $http_x_mirror_internal_upstream_authority;\n")
		g.writeProxyHeaders(out)
		g.writeRepositoryHeaders(out, repository, false)
		g.writeCache(out, repository, class, ttl)
		out.WriteString("        }\n")
	}
}

func (g *Generator) writeProxyCommon(out *strings.Builder, repository model.Mirror, parsed *url.URL, host string, includeCustomHeaders bool) {
	fmt.Fprintf(out, "            proxy_set_header Host %s;\n", host)
	if parsed.Scheme == "https" {
		fmt.Fprintf(out, "            proxy_ssl_name %s;\n", parsed.Hostname())
		out.WriteString("            proxy_ssl_server_name on;\n")
		out.WriteString("            proxy_ssl_verify on;\n")
		fmt.Fprintf(out, "            proxy_ssl_trusted_certificate %s;\n", strconv.Quote(absolutePath(g.cfg.UpstreamNginx.CABundle)))
		fmt.Fprintf(out, "            proxy_ssl_verify_depth %d;\n", g.cfg.UpstreamNginx.TLSVerifyDepth)
	}
	g.writeProxyHeaders(out)
	g.writeRepositoryHeaders(out, repository, includeCustomHeaders)
}

func (g *Generator) writeProxyHeaders(out *strings.Builder) {
	out.WriteString("            proxy_http_version 1.1;\n")
	out.WriteString("            proxy_set_header Connection \"\";\n")
	out.WriteString("            proxy_set_header Accept-Encoding $http_accept_encoding;\n")
	out.WriteString("            proxy_request_buffering off;\n")
	out.WriteString("            proxy_force_ranges on;\n")
	out.WriteString("            proxy_redirect off;\n")
	out.WriteString(indent(internalHeaderClears(), "            "))
	out.WriteByte('\n')
}

func (g *Generator) writeRepositoryHeaders(out *strings.Builder, repository model.Mirror, includeAddedHeaders bool) {
	connectTimeout := repository.ConnectTimeoutSec
	readTimeout := repository.ReadTimeoutSec
	sendTimeout := repository.SendTimeoutSec
	if connectTimeout <= 0 {
		connectTimeout = 10
	}
	if readTimeout <= 0 {
		readTimeout = 3600
	}
	if sendTimeout <= 0 {
		sendTimeout = 3600
	}
	fmt.Fprintf(out, "            proxy_connect_timeout %ds;\n", connectTimeout)
	fmt.Fprintf(out, "            proxy_read_timeout %ds;\n", readTimeout)
	fmt.Fprintf(out, "            proxy_send_timeout %ds;\n", sendTimeout)
	for _, name := range repository.HeaderRemove {
		fmt.Fprintf(out, "            proxy_set_header %s \"\";\n", name)
	}
	if !includeAddedHeaders {
		return
	}
	names := make([]string, 0, len(repository.HeaderAdd))
	for name := range repository.HeaderAdd {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		fmt.Fprintf(out, "            proxy_set_header %s %s;\n", name, strconv.Quote(repository.HeaderAdd[name]))
	}
}

func (g *Generator) writeCache(out *strings.Builder, repository model.Mirror, class string, ttl time.Duration) {
	if !repository.CacheEnabled || (!repository.CacheAuthenticated && hasStaticCredentialHeader(repository.HeaderAdd)) {
		out.WriteString("            proxy_buffering off;\n            proxy_cache off;\n")
		return
	}
	out.WriteString("            proxy_buffering on;\n")
	out.WriteString("            proxy_cache mirrorrelay_cache;\n")
	out.WriteString("            proxy_cache_key $http_x_mirror_internal_cache_key;\n")
	out.WriteString("            proxy_cache_methods GET HEAD;\n")
	out.WriteString("            proxy_cache_convert_head on;\n")
	fmt.Fprintf(out, "            proxy_cache_valid 200 %s;\n", nginxDuration(ttl))
	bypass := []string{"$http_x_mirror_internal_cache_bypass"}
	if class == "metadata" {
		bypass = append(bypass, "$http_range")
	} else {
		out.WriteString("            proxy_set_header Range \"\";\n")
		out.WriteString("            proxy_set_header If-Range \"\";\n")
	}
	if !repository.CacheAuthenticated {
		bypass = append(bypass, "$http_authorization", "$http_cookie")
	}
	fmt.Fprintf(out, "            proxy_cache_bypass %s;\n", strings.Join(bypass, " "))
	fmt.Fprintf(out, "            proxy_no_cache %s;\n", strings.Join(bypass, " "))
	out.WriteString("            proxy_cache_lock on;\n")
	fmt.Fprintf(out, "            proxy_cache_lock_timeout %s;\n", nginxDuration(g.cfg.Cache.WaitForFill))
	out.WriteString("            proxy_cache_use_stale error timeout updating http_500 http_502 http_503 http_504;\n")
	out.WriteString("            add_header X-Mirror-Cache $upstream_cache_status always;\n")
	limit := effectiveLimit(g.cfg.Limits.BandwidthLimitBPS, repository.BandwidthLimitBPS)
	if limit > 0 {
		fmt.Fprintf(out, "            limit_rate %d;\n", limit)
	}
}

func hasStaticCredentialHeader(headers map[string]string) bool {
	for name, value := range headers {
		if value != "" && (strings.EqualFold(name, "Authorization") || strings.EqualFold(name, "Cookie")) {
			return true
		}
	}
	return false
}

func cacheTTL(cfg config.Config, repository model.Mirror, class string) time.Duration {
	seconds := 0
	var fallback time.Duration
	switch class {
	case "metadata":
		seconds, fallback = repository.MetadataTTLSec, cfg.Cache.MetadataTTL
	case "package":
		seconds, fallback = repository.PackageTTLSec, cfg.Cache.PackageTTL
	case "immutable":
		seconds, fallback = repository.ImmutableTTLSec, 365*24*time.Hour
	case "blob":
		seconds, fallback = repository.BlobTTLSec, 365*24*time.Hour
	default:
		fallback = cfg.Cache.PackageTTL
	}
	if seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	return fallback
}

func (g *Generator) integrationSnippet(repositories []model.Mirror) string {
	hosts := make([]model.Mirror, 0)
	paths := make([]model.Mirror, 0)
	hasSharedAuxiliaryRoute := false
	for _, repository := range repositories {
		if !repository.Enabled {
			continue
		}
		if repository.PublicMode == "host" {
			hosts = append(hosts, repository)
		} else {
			paths = append(paths, repository)
			if repository.HTMLRewriteEnabled {
				hasSharedAuxiliaryRoute = true
			}
		}
	}
	sort.Slice(hosts, func(i, j int) bool { return hosts[i].PublicHost < hosts[j].PublicHost })
	sort.Slice(paths, func(i, j int) bool { return paths[i].PublicPath < paths[j].PublicPath })

	var out strings.Builder
	out.WriteString("# External Shared Nginx integration generated by MirrorRelay.\n")
	out.WriteString("# Review certificate paths and include the required blocks manually.\n")
	out.WriteString("# MirrorRelay does not include this file or reload External Shared Nginx.\n\n")
	for _, repository := range hosts {
		fmt.Fprintf(&out, "# Host-mode repository: %s\n", repository.Name)
		out.WriteString("server {\n")
		out.WriteString("    listen 443 ssl http2;\n")
		fmt.Fprintf(&out, "    server_name %s;\n", repository.PublicHost)
		out.WriteString("    # Configure ssl_certificate and ssl_certificate_key for this host.\n")
		out.WriteString(integrationLocation("/", nginxProxyPass(g.cfg.FrontendEndpoint()), "    "))
		out.WriteString("}\n\n")
	}
	out.WriteString("# Add this exact location to the shared repository TLS server block.\n")
	out.WriteString("# Shared-host repository index.\n")
	out.WriteString(integrationLocationWithModifier("=", "/", nginxProxyPass(g.cfg.FrontendEndpoint()), ""))
	out.WriteByte('\n')
	out.WriteString("# Administration UI and API. Keep this path private.\n")
	out.WriteString(integrationLocation(g.cfg.Admin.Path, nginxProxyPass(g.cfg.FrontendEndpoint()), ""))
	out.WriteByte('\n')
	if hasSharedAuxiliaryRoute {
		out.WriteString("# Same-origin auxiliary resources for browsable repository HTML.\n")
		out.WriteString(integrationLocation("/_mirrorrelay/upstream/", nginxProxyPass(g.cfg.FrontendEndpoint()), ""))
		out.WriteByte('\n')
	}
	if len(paths) > 0 {
		out.WriteString("# Add these path-mode locations to the same TLS server block.\n")
		for _, repository := range paths {
			fmt.Fprintf(&out, "# Repository: %s\n", repository.Name)
			out.WriteString(integrationLocation(repository.PublicPath, nginxProxyPass(g.cfg.FrontendEndpoint()), ""))
			out.WriteByte('\n')
		}
	}
	return out.String()
}

func integrationLocation(path, proxyPass, prefix string) string {
	return integrationLocationWithModifier("^~", path, proxyPass, prefix)
}

func integrationLocationWithModifier(modifier, path, proxyPass, prefix string) string {
	var out strings.Builder
	fmt.Fprintf(&out, "%slocation %s %s {\n", prefix, modifier, strconv.Quote(path))
	fmt.Fprintf(&out, "%s    proxy_pass %s;\n", prefix, proxyPass)
	fmt.Fprintf(&out, "%s    proxy_http_version 1.1;\n", prefix)
	fmt.Fprintf(&out, "%s    proxy_request_buffering off;\n", prefix)
	fmt.Fprintf(&out, "%s    proxy_buffering off;\n", prefix)
	fmt.Fprintf(&out, "%s    proxy_set_header Host $host;\n", prefix)
	fmt.Fprintf(&out, "%s    proxy_set_header X-Real-IP $remote_addr;\n", prefix)
	fmt.Fprintf(&out, "%s    proxy_set_header X-Forwarded-For $remote_addr;\n", prefix)
	fmt.Fprintf(&out, "%s    proxy_set_header X-Forwarded-Proto https;\n", prefix)
	out.WriteString(indent(internalHeaderClears(), prefix+"    "))
	out.WriteByte('\n')
	fmt.Fprintf(&out, "%s    proxy_read_timeout 1h;\n", prefix)
	fmt.Fprintf(&out, "%s    proxy_send_timeout 1h;\n", prefix)
	fmt.Fprintf(&out, "%s    gzip off;\n", prefix)
	fmt.Fprintf(&out, "%s}\n", prefix)
	return out.String()
}

func internalHeaderClears() string {
	return `proxy_set_header X-Mirror-Internal-Repository-ID "";
proxy_set_header X-Mirror-Internal-Cache-Key "";
proxy_set_header X-Mirror-Internal-Cache-Bypass "";
proxy_set_header X-Mirror-Internal-Client-IP "";
proxy_set_header X-Mirror-Internal-Upstream-IP "";
proxy_set_header X-Mirror-Internal-Upstream-Port "";
proxy_set_header X-Mirror-Internal-Upstream-Address "";
proxy_set_header X-Mirror-Internal-Upstream-Host "";
proxy_set_header X-Mirror-Internal-Upstream-Authority "";
proxy_set_header X-Mirror-Internal-Upstream-URI "";
proxy_set_header X-Mirror-Internal-Request-ID "";`
}

func activeUpstream(values []model.Upstream) (model.Upstream, error) {
	candidates := append([]model.Upstream(nil), values...)
	sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].Priority < candidates[j].Priority })
	for _, status := range []string{"healthy", "unknown", "unhealthy"} {
		for _, value := range candidates {
			actual := value.HealthStatus
			if actual == "" {
				actual = "unknown"
			}
			if value.Enabled && actual == status {
				return value, nil
			}
		}
	}
	return model.Upstream{}, errors.New("no enabled upstream")
}

func validateNginxValue(value string) error {
	if strings.ContainsAny(value, "\x00\r\n{};\"") {
		return fmt.Errorf("value %q cannot be represented safely in generated Nginx configuration", value)
	}
	return nil
}

func nginxListen(value string) string {
	if strings.HasPrefix(value, ":") {
		return strings.TrimPrefix(value, ":")
	}
	return value
}

func nginxEndpoint(network, address string) string {
	if network == "unix" {
		return "unix:" + absolutePath(address)
	}
	return address
}

func nginxProxyPass(network, address string) string {
	if network == "unix" {
		return "http://unix:" + absolutePath(address) + ":"
	}
	return "http://" + address
}

func nginxDuration(duration time.Duration) string {
	return strconv.FormatInt(max(1, int64(duration/time.Second)), 10) + "s"
}

func absolutePath(value string) string {
	path, err := filepath.Abs(value)
	if err != nil {
		return filepath.Clean(value)
	}
	return path
}

func indent(content, prefix string) string {
	lines := strings.Split(strings.TrimSpace(content), "\n")
	for index := range lines {
		lines[index] = prefix + lines[index]
	}
	return strings.Join(lines, "\n")
}

var forbiddenDirectives = []string{
	"user", "pid", "daemon", "master_process", "load_module", "env", "working_directory",
	"root", "alias", "perl", "exec", "include", "server", "location", "upstream", "listen",
	"set", "rewrite", "return", "try_files", "error_page", "recursive_error_pages", "auth_request", "mirror",
	"proxy_pass", "grpc_pass",
	"fastcgi_pass", "uwsgi_pass", "scgi_pass", "memcached_pass", "ssl_certificate",
	"ssl_certificate_key", "ssl_session_ticket_key", "auth_basic_user_file", "access_log",
	"error_log", "client_body_temp_path", "proxy_temp_path", "fastcgi_temp_path",
	"uwsgi_temp_path", "scgi_temp_path", "proxy_store", "proxy_store_access", "proxy_cache_path",
	"proxy_cache", "proxy_cache_key", "proxy_cache_bypass", "proxy_no_cache", "proxy_ignore_headers",
}

var forbiddenDirectivePrefixes = []string{"proxy_ssl_"}

var reservedCustomTokens = []string{"$repo_", "$health_", "$http_x_mirror_internal_", "x-mirror-internal-", "mirrorrelay_repo_", "mirrorrelay_cache", "mirrorrelay_frontend"}

func ValidateCustomName(name string) error {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 100 {
		return errors.New("name must contain 1..100 characters")
	}
	if strings.ContainsAny(name, "\x00\r\n") {
		return errors.New("name contains control characters")
	}
	return nil
}

func ValidateCustom(contextName, content string) error {
	contextName = strings.ToLower(strings.TrimSpace(contextName))
	if contextName != "http" && contextName != "server" && contextName != "location" && contextName != "upstream" && contextName != "repository" {
		return errors.New("context must be http, server, location, upstream or repository")
	}
	if len(content) > 1<<20 {
		return errors.New("custom configuration exceeds 1 MiB")
	}
	if strings.Contains(content, "\x00") {
		return errors.New("custom configuration contains NUL")
	}
	depth, directiveStart := 0, true
	var token strings.Builder
	var quote rune
	escaped, comment, variableBrace := false, false, false
	flush := func() error {
		if token.Len() == 0 {
			return nil
		}
		value := strings.ToLower(token.String())
		normalizedValue := strings.NewReplacer("{", "", "}", "").Replace(value)
		token.Reset()
		for _, reserved := range reservedCustomTokens {
			if strings.Contains(normalizedValue, reserved) {
				return fmt.Errorf("token containing %s is reserved for generated configuration", reserved)
			}
		}
		if directiveStart {
			for _, directive := range forbiddenDirectives {
				if value == directive {
					return fmt.Errorf("directive %s is not allowed", directive)
				}
			}
			for _, prefix := range forbiddenDirectivePrefixes {
				if strings.HasPrefix(value, prefix) {
					return fmt.Errorf("directive prefix %s is reserved for generated configuration", prefix)
				}
			}
			directiveStart = false
		}
		return nil
	}
	for _, character := range content {
		if comment {
			if character == '\n' {
				comment = false
			}
			continue
		}
		if quote != 0 {
			if escaped {
				token.WriteRune(character)
				escaped = false
				continue
			}
			if character == '\\' {
				escaped = true
				continue
			}
			if character == quote {
				quote = 0
				continue
			}
			token.WriteRune(character)
			continue
		}
		switch character {
		case '#':
			if err := flush(); err != nil {
				return err
			}
			comment = true
		case '\'', '"':
			quote = character
		case '\\':
			return errors.New("backslash escapes are not allowed in custom configuration outside quoted values")
		case '{':
			if token.Len() > 0 && strings.HasSuffix(token.String(), "$") {
				token.WriteRune(character)
				variableBrace = true
				continue
			}
			if err := flush(); err != nil {
				return err
			}
			depth++
			directiveStart = true
		case '}':
			if variableBrace {
				token.WriteRune(character)
				variableBrace = false
				continue
			}
			if err := flush(); err != nil {
				return err
			}
			depth--
			if depth < 0 {
				return errors.New("custom configuration escapes its context")
			}
			directiveStart = true
		case ';':
			if err := flush(); err != nil {
				return err
			}
			directiveStart = true
		case ' ', '\t', '\r', '\n':
			if err := flush(); err != nil {
				return err
			}
		default:
			token.WriteRune(character)
		}
	}
	if quote != 0 || escaped || variableBrace {
		return errors.New("unterminated quoted string in custom configuration")
	}
	if err := flush(); err != nil {
		return err
	}
	if depth != 0 {
		return errors.New("unbalanced braces in custom configuration")
	}
	return nil
}

func effectiveLimit(global, repository int64) int64 {
	if global <= 0 {
		return repository
	}
	if repository <= 0 || global < repository {
		return global
	}
	return repository
}
