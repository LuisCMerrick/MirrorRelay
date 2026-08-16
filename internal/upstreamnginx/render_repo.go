package upstreamnginx

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/LuisCMerrick/MirrorRelay/internal/config"
	"github.com/LuisCMerrick/MirrorRelay/internal/model"
	"github.com/LuisCMerrick/MirrorRelay/internal/security"
)

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

func indent(content, prefix string) string {
	lines := strings.Split(strings.TrimSpace(content), "\n")
	for index := range lines {
		lines[index] = prefix + lines[index]
	}
	return strings.Join(lines, "\n")
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
