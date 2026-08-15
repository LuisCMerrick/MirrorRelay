package proxy

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/LuisCMerrick/RepoGate/internal/config"
	"github.com/LuisCMerrick/RepoGate/internal/model"
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
