package upstreamnginx

import (
	"context"
	"net/netip"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LuisCMerrick/MirrorRelay/internal/config"
	"github.com/LuisCMerrick/MirrorRelay/internal/model"
)

type fixedResolver map[string][]netip.Addr

func (r fixedResolver) LookupNetIP(_ context.Context, _, host string) ([]netip.Addr, error) {
	return r[host], nil
}

func TestWorkerUserDirectiveRequiresRootMaster(t *testing.T) {
	if got := workerUserDirective("mirrorrelay", 1000); got != "" {
		t.Fatalf("non-root master received a user directive: %q", got)
	}
	if got := workerUserDirective("mirrorrelay", 0); got != "user mirrorrelay;\n" {
		t.Fatalf("root master user directive = %q", got)
	}
	if got := workerUserDirective("", 0); got != "" {
		t.Fatalf("empty worker user generated a directive: %q", got)
	}
}

func TestGeneratePinsValidatedConfiguredAndDynamicTargets(t *testing.T) {
	cfg := config.Default()
	cfg.UpstreamNginx.Prefix = "/var/lib/mm/upstream-nginx"
	cfg.UpstreamNginx.PID = "/run/mm/upstream-nginx.pid"
	cfg.UpstreamNginx.UpstreamSocket = "/run/mm/upstream.sock"
	cfg.Cache.Path = "/var/cache/mm"
	cfg.Server.FrontendSocket = "/run/mm/frontend.sock"
	resolver := fixedResolver{
		"repo.example":     {netip.MustParseAddr("2606:4700:4700::1111"), netip.MustParseAddr("8.8.8.8")},
		"registry.example": {netip.MustParseAddr("8.8.4.4")},
	}
	repositories := []model.Mirror{
		{ID: 1, Name: "APT", Slug: "apt", Type: "apt", Enabled: true, HTMLRewriteEnabled: true, PublicMode: "path", PublicPath: "/apt/", ProxyMode: "transparent", CacheEnabled: true, CacheProfile: "packages", Upstreams: []model.Upstream{{ID: 11, URL: "https://repo.example/base/", Host: "repo.example", Enabled: true}}},
		{ID: 2, Name: "Registry", Slug: "registry", Type: "docker-registry", Enabled: true, PublicMode: "host", PublicHost: "docker.example.com", ProxyMode: "registry", AuthMode: "full_proxy", BlobRedirectMode: "full_proxy", Upstreams: []model.Upstream{{ID: 22, URL: "https://registry.example/", Host: "registry.example", Enabled: true}}},
	}
	generated, err := NewGenerator(cfg, resolver).Generate(context.Background(), repositories, nil)
	if err != nil {
		t.Fatal(err)
	}
	routes := generated.Files["repositories.conf"]
	upstreamGroups := generated.Files["upstreams.conf"]
	if !strings.Contains(upstreamGroups, `server 8.8.8.8:443`) || !strings.Contains(upstreamGroups, `server [2606:4700:4700::1111]:443`) || !strings.Contains(routes, `set $repo_1_origin "https://mirrorrelay_repo_1_11"`) {
		t.Fatalf("configured upstream must use the validated pinned address:\n%s", routes)
	}
	if !strings.Contains(routes, "proxy_ssl_name repo.example") || !strings.Contains(routes, "proxy_set_header Host repo.example") {
		t.Fatalf("pinned connection must retain original TLS SNI and Host:\n%s", routes)
	}
	if !strings.Contains(routes, "proxy_pass https://$http_x_mirror_internal_upstream_address$http_x_mirror_internal_upstream_uri") {
		t.Fatalf("dynamic target pinning route missing:\n%s", routes)
	}
	if !strings.Contains(routes, "proxy_cache_key $http_x_mirror_internal_cache_key") || !strings.Contains(routes, "/_repo/1/11/metadata/") || !strings.Contains(routes, "/_repo/1/11/package/") {
		t.Fatalf("classed cache route missing:\n%s", routes)
	}
	if !strings.Contains(routes, "/_repo_aux/1/11/metadata/") || !strings.Contains(routes, `rewrite ^/_repo_aux/1/11/metadata/(.*)$ "/$1" break;`) {
		t.Fatalf("browsable HTML auxiliary upstream route missing:\n%s", routes)
	}
	if !strings.Contains(generated.Main, "listen unix:/run/mm/upstream.sock") || !strings.Contains(generated.Main, "pid /run/mm/upstream-nginx.pid") {
		t.Fatalf("runtime sockets/pid missing:\n%s", generated.Main)
	}
	if strings.Contains(generated.Main, "%!(EXTRA") || !strings.Contains(generated.Main, "max_size=536870912000 min_free=1073741824") || !strings.Contains(generated.Main, "resolver 1.1.1.1 8.8.8.8 valid=300s") {
		t.Fatalf("main configuration arguments are misaligned:\n%s", generated.Main)
	}
	if strings.Contains(generated.Main, `uri="$request_uri"`) || !strings.Contains(generated.Main, `uri="$uri"`) {
		t.Fatalf("Managed Upstream Nginx access log can persist raw query values:\n%s", generated.Main)
	}
	if !strings.Contains(generated.Files["external-nginx-integration.conf"], "proxy_pass http://unix:/run/mm/frontend.sock:") || !strings.Contains(generated.Files["external-nginx-integration.conf"], "X-Mirror-Internal-Upstream-Address") {
		t.Fatalf("External Shared Nginx integration security snippet missing:\n%s", generated.Files["external-nginx-integration.conf"])
	}
	if !strings.Contains(generated.Files["external-nginx-integration.conf"], "server_name docker.example.com;") || !strings.Contains(generated.Files["external-nginx-integration.conf"], `location ^~ "/apt/"`) || !strings.Contains(generated.Files["external-nginx-integration.conf"], `location ^~ "/admin/"`) || !strings.Contains(generated.Files["external-nginx-integration.conf"], `location ^~ "/_mirrorrelay/upstream/"`) || !strings.Contains(generated.Files["external-nginx-integration.conf"], `location = "/"`) {
		t.Fatalf("External Shared Nginx integration must generate host and path routes:\n%s", generated.Files["external-nginx-integration.conf"])
	}
}

func TestGenerateUsesLoopbackEndpointsWhenUnixSocketsAreDisabled(t *testing.T) {
	cfg := config.Default()
	cfg.Server.UnixSocketEnabled = false
	cfg.Server.LocalPort = 19081
	cfg.UpstreamNginx.UpstreamSocketEnabled = false
	cfg.UpstreamNginx.UpstreamLocalPort = 19082
	cfg.UpstreamNginx.Prefix = "/var/lib/mm/upstream-nginx"
	cfg.UpstreamNginx.PID = "/run/mm/upstream-nginx.pid"
	cfg.Cache.Path = "/var/cache/mm"
	repositories := []model.Mirror{{
		ID: 1, Name: "Generic", Slug: "generic", Type: "generic", Enabled: true,
		PublicMode: "path", PublicPath: "/generic/", ProxyMode: "transparent",
		Upstreams: []model.Upstream{{ID: 1, URL: "https://repo.example/", Host: "repo.example", Enabled: true}},
	}}
	generated, err := NewGenerator(cfg, fixedResolver{"repo.example": {netip.MustParseAddr("8.8.8.8")}}).Generate(context.Background(), repositories, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(generated.Main, "listen 127.0.0.1:19082;") {
		t.Fatalf("Managed Upstream Nginx loopback listener missing:\n%s", generated.Main)
	}
	if !strings.Contains(generated.Files["external-nginx-integration.conf"], "proxy_pass http://127.0.0.1:19081;") {
		t.Fatalf("frontend loopback endpoint missing:\n%s", generated.Files["external-nginx-integration.conf"])
	}
}

func TestIntegrationSnippetAlwaysIncludesSharedRootIndex(t *testing.T) {
	cfg := config.Default()
	cfg.Admin.Path = "/private-console/"
	generator := NewGenerator(cfg, nil)
	for name, repositories := range map[string][]model.Mirror{
		"empty":     nil,
		"host only": {{ID: 1, Name: "Registry", Enabled: true, PublicMode: "host", PublicHost: "registry.example.com"}},
	} {
		t.Run(name, func(t *testing.T) {
			snippet := generator.integrationSnippet(repositories)
			if strings.Count(snippet, `location = "/"`) != 1 {
				t.Fatalf("shared root index location count is not one:\n%s", snippet)
			}
			if strings.Count(snippet, `location ^~ "/private-console/"`) != 1 {
				t.Fatalf("configured administration location count is not one:\n%s", snippet)
			}
		})
	}
}

func TestGenerateRepositoryOverridesAndSafeCachePolicy(t *testing.T) {
	cfg := config.Default()
	cfg.UpstreamNginx.Prefix = "/var/lib/mm/upstream-nginx"
	cfg.UpstreamNginx.PID = "/run/mm/upstream-nginx.pid"
	cfg.UpstreamNginx.UpstreamSocket = "/run/mm/upstream.sock"
	cfg.Cache.Path = "/var/cache/mm"
	repository := model.Mirror{
		ID: 7, Name: "Packages", Slug: "packages", Type: "generic", Enabled: true,
		PublicMode: "path", PublicPath: "/packages/", ProxyMode: "transparent", CacheEnabled: true,
		HeaderAdd: map[string]string{"X-Repo": "packages"}, HeaderRemove: []string{"X-Legacy"},
		ConnectTimeoutSec: 7, ReadTimeoutSec: 7200, SendTimeoutSec: 7100,
		MetadataTTLSec: 30, PackageTTLSec: 86400,
		Upstreams: []model.Upstream{{ID: 70, URL: "https://repo.example/", Host: "repo.example", Enabled: true}},
	}
	generated, err := NewGenerator(cfg, fixedResolver{"repo.example": {netip.MustParseAddr("8.8.8.8")}}).Generate(context.Background(), []model.Mirror{repository}, nil)
	if err != nil {
		t.Fatal(err)
	}
	routes := generated.Files["repositories.conf"]
	for _, expected := range []string{
		"proxy_connect_timeout 7s;", "proxy_read_timeout 7200s;", "proxy_send_timeout 7100s;",
		"proxy_set_header X-Legacy \"\";", "proxy_set_header X-Repo \"packages\";",
		"proxy_cache_valid 200 30s;", "proxy_cache_valid 200 86400s;",
		"proxy_set_header Range \"\";", "proxy_set_header If-Range \"\";",
		"proxy_cache_bypass $http_x_mirror_internal_cache_bypass $http_authorization $http_cookie;",
		"proxy_cache_lock_timeout 1800s;",
		"proxy_ssl_verify on;",
	} {
		if !strings.Contains(routes, expected) {
			t.Fatalf("missing %q in generated repository configuration:\n%s", expected, routes)
		}
	}
	if !strings.Contains(routes, "proxy_no_cache $http_x_mirror_internal_cache_bypass $http_authorization $http_cookie;") {
		t.Fatal("credential-bearing requests are not excluded from cache storage")
	}
}

func TestStaticCredentialsDisableCacheUnlessExplicitlyAllowed(t *testing.T) {
	cfg := config.Default()
	repository := model.Mirror{CacheEnabled: true, HeaderAdd: map[string]string{"Authorization": "Bearer repository-secret"}}
	var out strings.Builder
	NewGenerator(cfg, nil).writeCache(&out, repository, "package", time.Hour)
	if !strings.Contains(out.String(), "proxy_cache off;") {
		t.Fatalf("static credentials did not disable cache by default:\n%s", out.String())
	}
	repository.CacheAuthenticated = true
	out.Reset()
	NewGenerator(cfg, nil).writeCache(&out, repository, "package", time.Hour)
	if !strings.Contains(out.String(), "proxy_cache mirrorrelay_cache;") || strings.Contains(out.String(), "$http_authorization") || strings.Contains(out.String(), "$http_cookie") {
		t.Fatalf("explicit authenticated-cache policy was not applied:\n%s", out.String())
	}
}

func TestStaticRepositoryCredentialsAreNotForwardedToDynamicTargets(t *testing.T) {
	cfg := config.Default()
	repository := model.Mirror{
		ID: 9, Name: "Credentialed", Slug: "credentialed", Type: "generic", Enabled: true,
		PublicMode: "path", PublicPath: "/credentialed/", ProxyMode: "transparent",
		HeaderAdd: map[string]string{"Authorization": "Bearer repository-secret"},
		Upstreams: []model.Upstream{{ID: 90, URL: "https://repo.example/", Host: "repo.example", Enabled: true}},
	}
	generated, err := NewGenerator(cfg, fixedResolver{"repo.example": {netip.MustParseAddr("8.8.8.8")}}).Generate(context.Background(), []model.Mirror{repository}, nil)
	if err != nil {
		t.Fatal(err)
	}
	routes := generated.Files["repositories.conf"]
	dynamicStart := strings.Index(routes, "location ^~ /_target/")
	if dynamicStart < 0 {
		t.Fatalf("dynamic target locations are missing:\n%s", routes)
	}
	if !strings.Contains(routes[:dynamicStart], "repository-secret") {
		t.Fatal("static repository routes did not receive configured credentials")
	}
	if strings.Contains(routes[dynamicStart:], "repository-secret") {
		t.Fatal("repository credentials leaked to a redirect or adapter-selected target")
	}
}

func TestValidateCustomRejectsEscapesAndDangerousDirectives(t *testing.T) {
	for _, content := range []string{
		"}\nserver { listen 1; }", "load_module modules/evil.so;", "root /;", "listen 0.0.0.0:8080;",
		"proxy_pass https://unapproved.example;", "if ($request_method = GET) { include /etc/passwd; }",
		"location /files/ { alias /srv/files/; }", `"include" /etc/passwd;`, `incl\ude /etc/passwd;`,
		"server { keepalive_timeout 10s; }", "set $repo_7_origin https://unapproved.example;",
		"proxy_ssl_verify off;", "proxy_cache_key $uri;", "add_header X-Mirror-Internal-Upstream-Address exposed;",
		"proxy_store /tmp/response;", "rewrite ^ /_repo/2/ break;", "add_header X-Leak ${http_x_mirror_internal_upstream_host};",
	} {
		if err := ValidateCustom("http", content); err == nil {
			t.Fatalf("expected rejection for %q", content)
		}
	}
	if err := ValidateCustom("http", "map $request_method $allowed { default 0; GET 1; HEAD 1; }"); err != nil {
		t.Fatalf("safe config rejected: %v", err)
	}
	if err := ValidateCustom("location", `add_header X-Literal "quoted { value }"; # root /ignored`); err != nil {
		t.Fatalf("quoted braces or comments were parsed as directives: %v", err)
	}
	if err := ValidateCustomName("unsafe\nname"); err == nil {
		t.Fatal("custom configuration comment injection was accepted")
	}
}

func TestUpstreamCustomConfigurationIsScopedToRepositoryUpstreams(t *testing.T) {
	cfg := config.Default()
	cfg.UpstreamNginx.Prefix = filepath.Join(t.TempDir(), "upstream-nginx")
	cfg.UpstreamNginx.PID = filepath.Join(t.TempDir(), "upstream-nginx.pid")
	cfg.Cache.Path = filepath.Join(t.TempDir(), "cache")
	repository := model.Mirror{ID: 8, Name: "Repository", Slug: "repository", Type: "generic", Enabled: true,
		PublicMode: "path", PublicPath: "/repository/", ProxyMode: "transparent",
		Upstreams: []model.Upstream{{ID: 80, URL: "https://repo.example/", Host: "repo.example", Enabled: true}}}
	resolver := fixedResolver{"repo.example": {netip.MustParseAddr("8.8.8.8")}}
	generated, err := NewGenerator(cfg, resolver).Generate(context.Background(), []model.Mirror{repository}, []model.CustomConfig{{
		Name: "upstream-tuning", Context: "upstream", RepositoryID: repository.ID, Enabled: true, Content: "keepalive_requests 1000;",
	}})
	if err != nil {
		t.Fatal(err)
	}
	groups := generated.Files["upstreams.conf"]
	if !strings.Contains(groups, "# BEGIN CUSTOM: upstream-tuning") || !strings.Contains(groups, "keepalive_requests 1000;") {
		t.Fatalf("upstream custom configuration was not injected into the repository group:\n%s", groups)
	}
	if strings.Contains(generated.Main, "keepalive_requests 1000;") {
		t.Fatal("upstream custom configuration escaped into the http context")
	}
	if _, err := NewGenerator(cfg, resolver).Generate(context.Background(), []model.Mirror{repository}, []model.CustomConfig{{
		Name: "unscoped", Context: "upstream", Enabled: true, Content: "keepalive_requests 1000;",
	}}); err == nil {
		t.Fatal("unscoped upstream custom configuration was accepted")
	}
}

func TestCustomConfigurationRejectsInvalidRepositoryAssociation(t *testing.T) {
	cfg := config.Default()
	cfg.UpstreamNginx.Prefix = filepath.Join(t.TempDir(), "upstream-nginx")
	cfg.UpstreamNginx.PID = filepath.Join(t.TempDir(), "upstream-nginx.pid")
	cfg.Cache.Path = filepath.Join(t.TempDir(), "cache")
	repository := model.Mirror{ID: 1, Name: "Repository", Slug: "repository", Enabled: true,
		PublicMode: "path", PublicPath: "/repository/", ProxyMode: "transparent",
		Upstreams: []model.Upstream{{ID: 11, URL: "https://repo.example/", Enabled: true}}}
	generator := NewGenerator(cfg, fixedResolver{"repo.example": {netip.MustParseAddr("8.8.8.8")}})
	cases := []model.CustomConfig{
		{Name: "orphan", Context: "location", RepositoryID: 999, Enabled: true, Content: "proxy_read_timeout 10s;"},
		{Name: "scoped-http", Context: "http", RepositoryID: 1, Enabled: true, Content: "keepalive_timeout 10s;"},
		{Name: "scoped-server", Context: "server", RepositoryID: 1, Enabled: true, Content: "proxy_read_timeout 10s;"},
	}
	for _, custom := range cases {
		if _, err := generator.Generate(context.Background(), []model.Mirror{repository}, []model.CustomConfig{custom}); err == nil {
			t.Fatalf("invalid custom association was accepted: %#v", custom)
		}
	}
}
