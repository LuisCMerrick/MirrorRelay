package upstreamnginx

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
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
            proxy_set_header X-Accel-Supported "1";
%s
            proxy_read_timeout 1h;
            proxy_send_timeout 1h;
            gzip off;
        }
        location ^~ "/_repo/" {
            internal;
            proxy_pass %s;
            proxy_http_version 1.1;
            proxy_request_buffering off;
            proxy_buffering off;
            proxy_set_header Host mirrorrelay-upstream-nginx-internal;
            proxy_set_header X-Mirror-Internal-Repository-ID $upstream_http_x_mirror_internal_repository_id;
            proxy_set_header X-Mirror-Internal-Cache-Key $upstream_http_x_mirror_internal_cache_key;
            proxy_set_header X-Mirror-Internal-Client-IP $upstream_http_x_mirror_internal_client_ip;
            proxy_set_header X-Mirror-Internal-Request-ID $upstream_http_x_mirror_internal_request_id;
            proxy_read_timeout 1h;
            proxy_send_timeout 1h;
        }
    }
`, frontendServer, nginxListen(g.cfg.HTTP.Listen), nginxListen(g.cfg.HTTP.HTTPSListen),
			absolutePath(g.cfg.TLS.Certificate), absolutePath(g.cfg.TLS.PrivateKey), tlsProtocols, indent(internalHeaderClears(), "            "),
			nginxProxyPass(g.cfg.UpstreamEndpoint()))
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
    log_format mirrorrelay '$time_iso8601 request_id=$http_x_mirror_internal_request_id client_ip=$http_x_mirror_internal_client_ip repo=$http_x_mirror_internal_repository_id host=$host method=$request_method uri="$uri" status=$status bytes=$body_bytes_sent request_time=$request_time upstream="$upstream_addr" upstream_status="$upstream_status" upstream_time="$upstream_response_time" cache=$upstream_cache_status';
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
	out.WriteString("# Internal accelerated zero-copy location for large immutable binary packages.\n")
	out.WriteString("location ^~ \"/_repo/\" {\n")
	out.WriteString("    internal;\n")
	fmt.Fprintf(&out, "    proxy_pass %s;\n", nginxProxyPass(g.cfg.UpstreamEndpoint()))
	out.WriteString("    proxy_http_version 1.1;\n")
	out.WriteString("    proxy_request_buffering off;\n")
	out.WriteString("    proxy_buffering off;\n")
	out.WriteString("    proxy_set_header Host mirrorrelay-upstream-nginx-internal;\n")
	out.WriteString("    proxy_set_header X-Mirror-Internal-Repository-ID $upstream_http_x_mirror_internal_repository_id;\n")
	out.WriteString("    proxy_set_header X-Mirror-Internal-Cache-Key $upstream_http_x_mirror_internal_cache_key;\n")
	out.WriteString("    proxy_set_header X-Mirror-Internal-Client-IP $upstream_http_x_mirror_internal_client_ip;\n")
	out.WriteString("    proxy_set_header X-Mirror-Internal-Request-ID $upstream_http_x_mirror_internal_request_id;\n")
	out.WriteString("    proxy_read_timeout 1h;\n")
	out.WriteString("    proxy_send_timeout 1h;\n")
	out.WriteString("}\n\n")
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
	fmt.Fprintf(&out, "%s    proxy_set_header X-Accel-Supported \"1\";\n", prefix)
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
