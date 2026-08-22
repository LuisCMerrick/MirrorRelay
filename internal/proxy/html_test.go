package proxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/LuisCMerrick/MirrorRelay/internal/config"
	"github.com/LuisCMerrick/MirrorRelay/internal/mirror"
	"github.com/LuisCMerrick/MirrorRelay/internal/model"
)

var testAuxiliarySigningKey = []byte("0123456789abcdef0123456789abcdef")

func TestBrowsableHTMLRewritesRepositoryAndAuxiliaryURLs(t *testing.T) {
	repository := model.Mirror{
		ID: 7, Slug: "debian", PublicMode: "path", PublicPath: "/debian/",
	}
	upstream := model.Upstream{ID: 70, URL: "https://deb.debian.org/debian/"}
	pageURL := mustParseURL(t, "https://deb.debian.org/debian/pool/")
	source := []byte(`<!doctype html><html><head><base href="/debian/pool/"><link href="?C=N&O=D"></head><body>
<a href="../dists/">dists</a><a href="/debian/a%20b">escaped</a><img src="/icons/folder.gif"><form action="./?search=1"></form>
<img srcset="/icons/small.png 1x, ../large.png 2x"><script src="https://cdn.example/app.js"></script><a href="#top">top</a>
</body></html>`)

	output, changed, err := rewriteHTMLDocument(source, repository, upstream, pageURL, testAuxiliarySigningKey)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("browsable HTML was not rewritten")
	}
	actual := string(output)
	repositoryBase, _ := effectiveRepositoryBaseURL(repository, upstream)
	iconURL, _ := mapBrowsableURL(repository, upstream, repositoryBase, mustParseURL(t, "https://deb.debian.org/icons/folder.gif"), testAuxiliarySigningKey)
	smallURL, _ := mapBrowsableURL(repository, upstream, repositoryBase, mustParseURL(t, "https://deb.debian.org/icons/small.png"), testAuxiliarySigningKey)
	for _, expected := range []string{
		`href="/debian/pool/"`,
		`href="/debian/pool/?C=N&amp;O=D"`,
		`href="/debian/dists/"`,
		`href="/debian/a%20b"`,
		`src="` + iconURL + `"`,
		`action="/debian/pool/?search=1"`,
		`srcset="` + smallURL + ` 1x, /debian/large.png 2x"`,
		`src="https://cdn.example/app.js"`,
		`href="#top"`,
	} {
		if !strings.Contains(actual, expected) {
			t.Fatalf("rewritten HTML is missing %q:\n%s", expected, actual)
		}
	}
}

func TestBrowsableHTMLUsesAuxiliaryScopeOutsideRepositoryBase(t *testing.T) {
	repository := model.Mirror{
		ID: 9, Slug: "repo", PublicMode: "path", PublicPath: "/repo/", StripPrefix: "/incoming", AddPrefix: "nested",
	}
	upstream := model.Upstream{ID: 90, URL: "https://origin.example/base/"}
	pageURL := mustParseURL(t, "https://origin.example/icons/index.html")
	source := []byte(`<a href="folder.gif">icon</a><a href="/base/nested/packages/">packages</a>`)

	output, changed, err := rewriteHTMLDocument(source, repository, upstream, pageURL, testAuxiliarySigningKey)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("auxiliary HTML was not rewritten")
	}
	actual := string(output)
	if !strings.Contains(actual, `href="/_mirrorrelay/upstream/9/90/`) {
		t.Fatalf("relative auxiliary URL was not scoped: %s", actual)
	}
	if !strings.Contains(actual, `href="/repo/incoming/packages/"`) {
		t.Fatalf("repository URL did not retain the public strip prefix: %s", actual)
	}
}

func TestBrowsableHTMLRewritesNormalCandidateBesideDataSrcset(t *testing.T) {
	repository := model.Mirror{ID: 12, Slug: "repo", PublicMode: "path", PublicPath: "/repo/"}
	upstream := model.Upstream{ID: 120, URL: "https://origin.example/repo/"}
	source := []byte(`<img srcset="data:image/svg+xml,%3Csvg%3E%3C/svg%3E 1x, /icons/normal.png 2x">`)
	output, changed, err := rewriteHTMLDocument(source, repository, upstream,
		mustParseURL(t, "https://origin.example/repo/index.html"), testAuxiliarySigningKey)
	if err != nil {
		t.Fatal(err)
	}
	actual := string(output)
	if !changed || !strings.Contains(actual, "data:image/svg+xml,%3Csvg%3E%3C/svg%3E 1x") ||
		!strings.Contains(actual, "/_mirrorrelay/upstream/12/120/") {
		t.Fatalf("mixed data/normal srcset was not rewritten candidate-by-candidate: %s", actual)
	}
}

func TestBrowsableHTMLRewritesCandidateAfterDescriptorlessDataSrcset(t *testing.T) {
	repository := model.Mirror{ID: 13, Slug: "repo", PublicMode: "path", PublicPath: "/repo/"}
	upstream := model.Upstream{ID: 130, URL: "https://origin.example/repo/"}
	source := []byte(`<img srcset="data:image/png;base64,AAAA, /icons/normal.png 2x">`)
	output, changed, err := rewriteHTMLDocument(source, repository, upstream,
		mustParseURL(t, "https://origin.example/repo/index.html"), testAuxiliarySigningKey)
	if err != nil {
		t.Fatal(err)
	}
	actual := string(output)
	if !changed || !strings.Contains(actual, "data:image/png;base64,AAAA") ||
		!strings.Contains(actual, "/_mirrorrelay/upstream/13/130/") {
		t.Fatalf("descriptor-less data srcset swallowed the following candidate: %s", actual)
	}
}

func TestBrowsableHTMLResponseGetsARepresentationValidator(t *testing.T) {
	source := `<img src="/icons/folder.gif">`
	response := &http.Response{
		StatusCode:    http.StatusOK,
		Header:        make(http.Header),
		Body:          io.NopCloser(strings.NewReader(source)),
		ContentLength: int64(len(source)),
	}
	response.Header.Set("Content-Type", "text/html; charset=utf-8")
	response.Header.Set("Last-Modified", "Sat, 01 Jan 2000 00:00:00 GMT")
	metadataConfig := config.Default().Metadata
	metadataConfig.OutputCompression = "identity"

	validator, changed, err := rewriteHTMLResponseBody(response,
		model.Mirror{ID: 7, Slug: "debian", PublicMode: "path", PublicPath: "/debian/", HTMLRewriteEnabled: true},
		model.Upstream{ID: 70, URL: "https://deb.debian.org/debian/"}, mustParseURL(t, "https://deb.debian.org/debian/"),
		metadataConfig, model.UIEnhancementConfig{}, false, &gzipPool{}, testAuxiliarySigningKey)
	if err != nil {
		t.Fatal(err)
	}
	if !changed || validator.ETag == "" || response.Header.Get("ETag") != validator.ETag {
		t.Fatalf("validator was not installed: changed=%v validator=%+v headers=%v", changed, validator, response.Header)
	}
	if response.Header.Get("Last-Modified") != "" || response.ContentLength <= 0 {
		t.Fatalf("stale representation headers were retained: %v", response.Header)
	}
	body, err := io.ReadAll(response.Body)
	if err != nil || !strings.Contains(string(body), "/_mirrorrelay/upstream/7/70/") {
		t.Fatalf("unexpected rewritten body %q, err=%v", body, err)
	}
}

func TestAuxiliaryResourceRouteIsRepositoryScoped(t *testing.T) {
	cfg := config.Default()
	cfg.HTTP.PublicBaseURL = "https://mirror.example"
	registry := mirror.NewRegistry(nil)
	repository := model.Mirror{
		ID: 7, Slug: "debian", Enabled: true, HTMLRewriteEnabled: true,
		PublicMode: "path", PublicPath: "/debian/",
		Upstreams: []model.Upstream{
			{ID: 70, URL: "https://deb.debian.org/debian/", Enabled: true},
			{ID: 71, URL: "https://deb.debian.org/debian-backup/", Enabled: true},
		},
	}
	registry.Replace([]model.Mirror{repository})
	engine := &Engine{cfg: cfg, registry: registry, auxiliarySigningKey: testAuxiliarySigningKey}

	repositoryBase, _ := effectiveRepositoryBaseURL(repository, repository.Upstreams[0])
	signedPath, ok := mapBrowsableURL(repository, repository.Upstreams[0], repositoryBase,
		mustParseURL(t, "https://deb.debian.org/icons/folder.gif?size=16"), testAuxiliarySigningKey)
	if !ok {
		t.Fatal("failed to create signed auxiliary URL")
	}
	request := httptest.NewRequest(http.MethodGet, "https://mirror.example"+signedPath, nil)
	got, relative, dynamic, auxiliary, routeErr := engine.routeRequest(request)
	if routeErr != nil || got.ID != 7 || relative != "/icons/folder.gif" || dynamic != nil || !auxiliary {
		t.Fatalf("unexpected auxiliary route: repository=%+v relative=%q dynamic=%v auxiliary=%v err=%v", got, relative, dynamic, auxiliary, routeErr)
	}
	logical, err := repositoryURL(got, got.Upstreams[0], relative, request.URL.RawQuery, auxiliary)
	if err != nil || logical.String() != "https://deb.debian.org/icons/folder.gif?size=16" {
		t.Fatalf("unexpected auxiliary target %v, err=%v", logical, err)
	}
	forgedQuery := httptest.NewRequest(http.MethodGet, "https://mirror.example"+strings.Replace(signedPath, "size=16", "size=17", 1), nil)
	if _, _, _, _, routeErr := engine.routeRequest(forgedQuery); routeErr == nil || routeErr.status != http.StatusNotFound {
		t.Fatalf("auxiliary route accepted a query not covered by its signature: %v", routeErr)
	}
	forgedUpstream := httptest.NewRequest(http.MethodGet, "https://mirror.example"+strings.Replace(signedPath, "/70/", "/71/", 1), nil)
	if _, _, _, _, routeErr := engine.routeRequest(forgedUpstream); routeErr == nil || routeErr.status != http.StatusNotFound {
		t.Fatalf("auxiliary route accepted a different upstream: %v", routeErr)
	}

	wrongHost := httptest.NewRequest(http.MethodGet, "https://other.example"+signedPath, nil)
	if _, _, _, _, routeErr := engine.routeRequest(wrongHost); routeErr == nil || routeErr.status != http.StatusNotFound {
		t.Fatalf("path-mode auxiliary route accepted the wrong shared host: %v", routeErr)
	}
	repository.HTMLRewriteEnabled = false
	registry.Replace([]model.Mirror{repository})
	if _, _, _, _, routeErr := engine.routeRequest(request); routeErr == nil || routeErr.status != http.StatusNotFound {
		t.Fatalf("disabled auxiliary route remained available: %v", routeErr)
	}
}

func TestHostModeAuxiliaryRouteRequiresRepositoryHost(t *testing.T) {
	cfg := config.Default()
	registry := mirror.NewRegistry(nil)
	repository := model.Mirror{
		ID: 11, Slug: "hosted", Enabled: true, HTMLRewriteEnabled: true,
		PublicMode: "host", PublicHost: "repo.example",
		Upstreams: []model.Upstream{{ID: 110, URL: "https://origin.example/repository/", Enabled: true}},
	}
	registry.Replace([]model.Mirror{repository})
	engine := &Engine{cfg: cfg, registry: registry, auxiliarySigningKey: testAuxiliarySigningKey}
	repositoryBase, _ := effectiveRepositoryBaseURL(repository, repository.Upstreams[0])
	signedPath, ok := mapBrowsableURL(repository, repository.Upstreams[0], repositoryBase,
		mustParseURL(t, "https://origin.example/icons/folder.gif"), testAuxiliarySigningKey)
	if !ok {
		t.Fatal("failed to create host-mode signed auxiliary URL")
	}
	valid := httptest.NewRequest(http.MethodGet, "https://repo.example"+signedPath, nil)
	if _, _, _, auxiliary, routeErr := engine.routeRequest(valid); routeErr != nil || !auxiliary {
		t.Fatalf("host-mode auxiliary route failed: auxiliary=%v err=%v", auxiliary, routeErr)
	}
	invalid := httptest.NewRequest(http.MethodGet, "https://elsewhere.example"+signedPath, nil)
	if _, _, _, _, routeErr := engine.routeRequest(invalid); routeErr == nil || routeErr.status != http.StatusNotFound {
		t.Fatalf("host-mode auxiliary route accepted another host: %v", routeErr)
	}
}

func mustParseURL(t *testing.T, value string) *url.URL {
	t.Helper()
	parsed, err := url.Parse(value)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

func TestRepositoryBrowserRewritesDirectoryIndex(t *testing.T) {
	source := `<html><head><title>Index of /debian/</title></head><body><h1>Index of /debian/</h1><hr><pre><a href="../">../</a>
<a href="dists/">dists/</a>                                             20-Jun-2023 15:42                   -
<a href="README">README</a>                                             23-May-2023 11:21                1234
</pre><hr></body></html>`
	response := &http.Response{
		StatusCode:    http.StatusOK,
		Header:        make(http.Header),
		Body:          io.NopCloser(strings.NewReader(source)),
		ContentLength: int64(len(source)),
	}
	response.Header.Set("Content-Type", "text/html; charset=utf-8")
	metadataConfig := config.Default().Metadata
	uiEnhancement := model.UIEnhancementConfig{
		Enabled:           true,
		Theme:             "light",
		RepositoryBrowser: model.RepositoryBrowserConfig{Enabled: true},
	}

	validator, changed, err := rewriteHTMLResponseBody(response,
		model.Mirror{ID: 1, Name: "Debian", Slug: "debian", PublicMode: "path", PublicPath: "/debian/"},
		model.Upstream{URL: "https://deb.debian.org/debian/"}, mustParseURL(t, "https://deb.debian.org/debian/"),
		metadataConfig, uiEnhancement, false, &gzipPool{}, testAuxiliarySigningKey)
	if err != nil {
		t.Fatal(err)
	}
	if !changed || validator.ETag == "" {
		t.Fatalf("expected browser rewrite to succeed, changed=%v", changed)
	}
	body, err := io.ReadAll(response.Body)
	if err != nil || !strings.Contains(string(body), "Index of /debian/") || !strings.Contains(string(body), "file-row") {
		t.Fatalf("unexpected browser rendered output: %s", body)
	}
}
