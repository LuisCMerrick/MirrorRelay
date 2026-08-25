package proxy

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/LuisCMerrick/MirrorRelay/internal/config"
	"github.com/LuisCMerrick/MirrorRelay/internal/mirror"
	"github.com/LuisCMerrick/MirrorRelay/internal/model"
)

func TestRegistryBlobRedirectModeOverridesGenericPolicy(t *testing.T) {
	repository := model.Mirror{ProxyMode: "registry", RedirectMode: "full_proxy", BlobRedirectMode: "pass"}
	if shouldFollowRedirects(repository, "blob", false) {
		t.Fatal("pass-through Registry blob redirect was followed")
	}
	if shouldRewriteRedirect(repository, "blob") {
		t.Fatal("pass-through Registry blob redirect was rewritten")
	}
	repository.RedirectMode = "pass"
	repository.BlobRedirectMode = "full_proxy"
	if !shouldFollowRedirects(repository, "blob", false) {
		t.Fatal("full-proxy Registry blob redirect was not followed")
	}
	if !shouldFollowRedirects(repository, "metadata", true) {
		t.Fatal("full-proxy token redirect was not followed")
	}
}

func TestMetadataInternalRequestIgnoresRangesAndUpstreamValidators(t *testing.T) {
	header := make(http.Header)
	header.Set("Accept-Encoding", "gzip")
	for _, name := range []string{"If-None-Match", "If-Modified-Since", "Range", "If-Range"} {
		header.Set(name, "client-value")
	}
	prepareMetadataRequestHeaders(header)
	if header.Get("Accept-Encoding") != "identity" {
		t.Fatalf("Accept-Encoding = %q", header.Get("Accept-Encoding"))
	}
	for _, name := range []string{"If-None-Match", "If-Modified-Since", "Range", "If-Range"} {
		if header.Get(name) != "" {
			t.Fatalf("metadata request retained %s", name)
		}
	}
}

func TestRegistryMetadataCacheKeyIncludesAcceptRepresentation(t *testing.T) {
	repository := model.Mirror{ProxyMode: "registry"}
	manifestV2 := representationCacheKey("base", repository, "metadata", " application/vnd.docker.distribution.manifest.v2+json ")
	manifestV2Canonical := representationCacheKey("base", repository, "metadata", "APPLICATION/VND.DOCKER.DISTRIBUTION.MANIFEST.V2+JSON")
	manifestList := representationCacheKey("base", repository, "metadata", "application/vnd.docker.distribution.manifest.list.v2+json")
	if manifestV2 != manifestV2Canonical {
		t.Fatalf("equivalent Accept values produced different keys: %q and %q", manifestV2, manifestV2Canonical)
	}
	if manifestV2 == manifestList {
		t.Fatal("different Registry manifest representations produced the same cache key")
	}
	if got := representationCacheKey("base", repository, "blob", "application/json"); got != "base" {
		t.Fatalf("Registry blob key was changed: %q", got)
	}
	if got := representationCacheKey("base", model.Mirror{ProxyMode: "proxy"}, "metadata", "application/json"); got != "base" {
		t.Fatalf("non-Registry key was changed: %q", got)
	}
}

func TestSameOriginIncludesSchemeHostAndEffectivePort(t *testing.T) {
	parse := func(value string) *url.URL {
		parsed, err := url.Parse(value)
		if err != nil {
			t.Fatal(err)
		}
		return parsed
	}
	if !sameOrigin(parse("https://Registry.Example/v2/"), parse("https://registry.example:443/token")) {
		t.Fatal("equivalent HTTPS origins were treated as different")
	}
	for _, target := range []string{"http://registry.example/token", "https://registry.example:444/token", "https://cdn.example/token"} {
		if sameOrigin(parse("https://registry.example/v2/"), parse(target)) {
			t.Fatalf("cross-origin target was treated as same-origin: %s", target)
		}
	}
}

func TestJoinPath(t *testing.T) {
	cases := []struct{ base, relative, want string }{{"/debian/", "/dists/stable", "/debian/dists/stable"}, {"/", "/file", "/file"}, {"/repo", "/", "/repo/"}}
	for _, tc := range cases {
		if got := joinPath(tc.base, tc.relative); got != tc.want {
			t.Errorf("joinPath(%q,%q)=%q want %q", tc.base, tc.relative, got, tc.want)
		}
	}
}

func TestUnsafeRepositoryPathRejectsTraversalAndBackslashes(t *testing.T) {
	for _, value := range []string{"/../secret", "/pool/./file", `/pool\file`, "/pool/\x00file"} {
		if !unsafeRepositoryPath(value) {
			t.Fatalf("unsafe path accepted: %q", value)
		}
	}
	for _, value := range []string{"/pool/file.deb", "/pool/..file", "/a//b"} {
		if unsafeRepositoryPath(value) {
			t.Fatalf("safe path rejected: %q", value)
		}
	}
}

func TestRequestPublicBaseUsesConfiguredRepositoryIdentity(t *testing.T) {
	cfg := config.Default()
	cfg.HTTP.PublicBaseURL = "https://mirror.example.com"
	request := httptest.NewRequest("GET", "http://attacker.invalid/repo/file", nil)
	request.Header.Set("X-Forwarded-Proto", "https")
	base, err := requestPublicBase(cfg, model.Mirror{PublicMode: "path"}, request)
	if err != nil || base != "https://mirror.example.com" {
		t.Fatalf("path-mode public base = %q, %v", base, err)
	}
	base, err = requestPublicBase(cfg, model.Mirror{PublicMode: "host", PublicHost: "registry.example.com"}, request)
	if err != nil || base != "https://registry.example.com" {
		t.Fatalf("host-mode public base = %q, %v", base, err)
	}
	request.Header.Set("X-Forwarded-Proto", "http")
	base, err = requestPublicBase(cfg, model.Mirror{PublicMode: "host", PublicHost: "registry.example.com"}, request)
	if err != nil || base != "https://registry.example.com" {
		t.Fatalf("untrusted forwarded protocol changed public base to %q, %v", base, err)
	}
	cfg.HTTP.PublicBaseURL = ""
	request.Host = "unsafe.example;return"
	if _, err := requestPublicBase(cfg, model.Mirror{PublicMode: "path"}, request); err == nil {
		t.Fatal("unsafe request Host was accepted")
	}
}

func TestRewriteClassificationOnlyBuffersMetadata(t *testing.T) {
	repository := model.Mirror{Type: "pypi", ProxyMode: "rewrite", RewriteEnabled: true}
	if got := classifyObject(repository, "/simple/demo/", nil); got != "metadata" {
		t.Fatalf("metadata classified as %q", got)
	}
	for _, object := range []string{"/files/demo.whl", "/files/source.tar.gz"} {
		if got := classifyObject(repository, object, nil); got != "package" {
			t.Fatalf("package %s classified as %q", object, got)
		}
	}
	if got := classifyObject(model.Mirror{ProxyMode: "registry"}, "/v2/demo/blobs/sha256:abc", nil); got != "blob" {
		t.Fatalf("blob classified as %q", got)
	}
}

func TestOrderedRepositoryUpstreamsPrefersHealthThenPriority(t *testing.T) {
	values := []model.Upstream{
		{ID: 1, Enabled: true, Priority: 10, HealthStatus: "unhealthy"},
		{ID: 2, Enabled: true, Priority: 30, HealthStatus: "healthy"},
		{ID: 3, Enabled: true, Priority: 20, HealthStatus: "healthy"},
		{ID: 4, Enabled: false, Priority: 1, HealthStatus: "healthy"},
	}
	ordered := orderedRepositoryUpstreams(values)
	if len(ordered) != 3 || ordered[0].ID != 3 || ordered[1].ID != 2 || ordered[2].ID != 1 {
		t.Fatalf("unexpected upstream order: %#v", ordered)
	}
}

func TestDynamicTargetUsesOnlyOneRepositoryCandidate(t *testing.T) {
	meta := requestMeta{dynamicTarget: &url.URL{Scheme: "https", Host: "cdn.example"}, repository: model.Mirror{Upstreams: []model.Upstream{
		{ID: 1, Enabled: true, Priority: 10}, {ID: 2, Enabled: true, Priority: 20},
	}}}
	candidates := orderedRequestUpstreams(meta)
	if len(candidates) != 1 || candidates[0].ID != 1 {
		t.Fatalf("dynamic target candidates = %#v", candidates)
	}
}

func TestRepositoryURLKeepsSelectedUpstreamIdentity(t *testing.T) {
	upstream := model.Upstream{URL: "https://backup.example/base/"}
	got, err := repositoryURL(model.Mirror{AddPrefix: "nested"}, upstream, "/pkg.whl", "b=2&a=1", false)
	if err != nil {
		t.Fatal(err)
	}
	want := &url.URL{Scheme: "https", Host: "backup.example", Path: "/base/nested/pkg.whl", RawQuery: "b=2&a=1"}
	if got.String() != want.String() {
		t.Fatalf("repository URL=%q want %q", got, want)
	}
	auxiliary, err := repositoryURL(model.Mirror{}, upstream, "/icons/folder.gif", "", true)
	if err != nil || auxiliary.String() != "https://backup.example/icons/folder.gif" {
		t.Fatalf("auxiliary repository URL=%q err=%v", auxiliary, err)
	}
}

func TestAuxiliaryRootHasAnIndependentCacheIdentity(t *testing.T) {
	upstream := model.Upstream{URL: "https://repo.example/base/"}
	if normal, auxiliary := repositoryUpstreamIdentity(upstream, false), repositoryUpstreamIdentity(upstream, true); normal == auxiliary {
		t.Fatalf("normal and auxiliary cache identities collided: %q", normal)
	} else if auxiliary != "https://repo.example/" {
		t.Fatalf("unexpected auxiliary cache identity: %q", auxiliary)
	}
}

func TestSanitizeLogURI(t *testing.T) {
	input := "/simple/pkg/?token=secret123&user=alice&signature=sig456&version=1.0"
	got := sanitizeLogURI(input)
	if strings.Contains(got, "secret123") || strings.Contains(got, "sig456") {
		t.Fatalf("sensitive values leaked in log URI: %s", got)
	}
	if !strings.Contains(got, "user=alice") || !strings.Contains(got, "version=1.0") {
		t.Fatalf("innocuous parameters missing in log URI: %s", got)
	}
}

func TestStripUntrustedHeadersSanitizesSessionCookie(t *testing.T) {
	h := http.Header{}
	h.Set("Cookie", "mirrorrelay_session=secret_token; foo=bar; another=value")
	h.Set("X-Mirror-Internal-Foo", "bar")
	h.Set("X-Forwarded-For", "1.2.3.4")
	stripUntrustedHeaders(h)

	if h.Get("X-Mirror-Internal-Foo") != "" || h.Get("X-Forwarded-For") != "" {
		t.Fatal("internal headers not stripped")
	}
	cookie := h.Get("Cookie")
	if strings.Contains(cookie, "mirrorrelay_session") {
		t.Fatalf("mirrorrelay_session was not stripped: %s", cookie)
	}
	if !strings.Contains(cookie, "foo=bar") || !strings.Contains(cookie, "another=value") {
		t.Fatalf("legitimate cookies removed: %s", cookie)
	}
}

func TestIsAllowedRewriteOrigin(t *testing.T) {
	repo := model.Mirror{
		Upstreams: []model.Upstream{
			{URL: "https://deb.debian.org/debian/"},
		},
		RewriteHosts: []string{
			"security.debian.org",
			"https://archive.debian.org:8443",
		},
	}
	allowed1, _ := url.Parse("https://deb.debian.org/pool/main/a/a.deb")
	if !isAllowedRewriteOrigin(repo, allowed1) {
		t.Fatal("deb.debian.org should be allowed")
	}

	allowed2, _ := url.Parse("https://security.debian.org/dists/InRelease")
	if !isAllowedRewriteOrigin(repo, allowed2) {
		t.Fatal("security.debian.org should be allowed")
	}

	disallowedPort, _ := url.Parse("https://security.debian.org:8080/dists/InRelease")
	if isAllowedRewriteOrigin(repo, disallowedPort) {
		t.Fatal("security.debian.org on port 8080 should not be allowed")
	}

	disallowedScheme, _ := url.Parse("http://security.debian.org/dists/InRelease")
	if isAllowedRewriteOrigin(repo, disallowedScheme) {
		t.Fatal("security.debian.org over HTTP should not be allowed")
	}

	allowedCustomPort, _ := url.Parse("https://archive.debian.org:8443/pool/main/b/b.deb")
	if !isAllowedRewriteOrigin(repo, allowedCustomPort) {
		t.Fatal("archive.debian.org:8443 should be allowed")
	}
}

func TestCredentialPartitionKey(t *testing.T) {
	repoUnauth := model.Mirror{CacheAuthenticated: false}
	h := http.Header{}
	h.Set("Authorization", "Bearer token1")
	if key := credentialPartitionKey(repoUnauth, h); key != "" {
		t.Fatalf("unauth repo should have empty partition key: %q", key)
	}

	repoAuth := model.Mirror{CacheAuthenticated: true}
	key1 := credentialPartitionKey(repoAuth, h)
	if !strings.HasPrefix(key1, ":auth:") {
		t.Fatalf("auth repo should have :auth: partition key: %q", key1)
	}

	h2 := http.Header{}
	h2.Set("Authorization", "Bearer token2")
	key2 := credentialPartitionKey(repoAuth, h2)
	if key1 == key2 {
		t.Fatal("different tokens must have different partition keys")
	}
}

func TestPackageFilteringGuard(t *testing.T) {
	if blocked, reason := isPackageBlocked(model.Mirror{BlockedPackages: []string{"example-*"}}, "/packages/example.tar.gz"); !blocked || !strings.Contains(reason, "unavailable") {
		t.Fatalf("missing compiled package policy did not fail closed: blocked=%v reason=%q", blocked, reason)
	}
	repo := model.Mirror{
		BlockedPackages: []string{"^malicious-.*", "bad-package-*.tar.gz"},
		AllowedPackages: []string{"^safe-.*", "numpy*", "*.whl"},
	}
	if err := mirror.CompilePackagePolicy(&repo); err != nil {
		t.Fatal(err)
	}

	// Blocked by blacklist
	if blocked, _ := isPackageBlocked(repo, "/packages/malicious-pkg-1.0.tar.gz"); !blocked {
		t.Fatal("malicious-pkg should be blocked by blacklist")
	}
	if blocked, _ := isPackageBlocked(repo, "/bad-package-1.2.3.tar.gz"); !blocked {
		t.Fatal("bad-package-*.tar.gz should be blocked by blacklist")
	}

	// Allowed by whitelist
	if blocked, _ := isPackageBlocked(repo, "/simple/safe-package/"); blocked {
		t.Fatal("safe-package should be allowed by whitelist")
	}
	if blocked, _ := isPackageBlocked(repo, "/packages/numpy-1.24.whl"); blocked {
		t.Fatal("numpy whl should be allowed by whitelist")
	}

	// Blocked because not in whitelist
	if blocked, _ := isPackageBlocked(repo, "/packages/untrusted-tool.rpm"); !blocked {
		t.Fatal("untrusted-tool should be blocked because it is not in whitelist")
	}
}
