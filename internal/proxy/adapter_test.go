package proxy

import (
	"compress/gzip"
	"encoding/base64"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/LuisCMerrick/RepoGate/internal/config"
	"github.com/LuisCMerrick/RepoGate/internal/model"
)

func TestRewriteResponseBodyOnlyAllowedHostsAndOwnsValidator(t *testing.T) {
	repository := model.Mirror{
		Slug: "pypi", PublicMode: "path", PublicPath: "/pypi/", ProfileVersion: "1.0.0",
		RewriteProfile: "pypi", RewriteHosts: []string{"files.pythonhosted.org"},
	}
	body := `<a href="https://files.pythonhosted.org/packages/a.whl#sha256=abc">ok</a><a href="https://evil.example/x">no</a>`
	response := &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), ContentLength: int64(len(body))}
	response.Header.Set("Content-Type", "text/html")
	response.Header.Set("ETag", `"upstream"`)
	response.Header.Set("Last-Modified", "Tue, 12 Aug 2025 08:00:00 GMT")
	response.Header.Set("Digest", "sha-256=upstream")
	cfg := config.Default().Metadata
	cfg.GzipMinLength = 1
	validator, err := rewriteResponseBody(response, repository, "https://mirror.example.com", cfg, true, &gzipPool{})
	if err != nil {
		t.Fatal(err)
	}
	reader, err := gzip.NewReader(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	rewritten, _ := io.ReadAll(reader)
	text := string(rewritten)
	if strings.Contains(text, `href="https://files.pythonhosted.org`) || !strings.Contains(text, "https://mirror.example.com/pypi/__fetch/") {
		t.Fatalf("allowed URL not rewritten: %s", text)
	}
	if !strings.Contains(text, `href="https://evil.example/x"`) {
		t.Fatalf("unapproved URL changed: %s", text)
	}
	if response.Header.Get("ETag") != validator.ETag || !strings.HasPrefix(validator.ETag, `"repogate-v5-`) {
		t.Fatalf("proxy ETag missing: response=%q validator=%q", response.Header.Get("ETag"), validator.ETag)
	}
	if response.Header.Get("Last-Modified") != "" || response.Header.Get("Digest") != "" || validator.LastModified != "" {
		t.Fatalf("unsafe upstream validators survived metadata rewrite: %#v", response.Header)
	}
	if response.Header.Get("Content-Encoding") != "gzip" || !strings.Contains(response.Header.Get("Vary"), "Accept-Encoding") {
		t.Fatalf("gzip representation headers incorrect: %#v", response.Header)
	}
}

func TestRewrittenMetadataHeadOmitsUnknownRepresentationValidators(t *testing.T) {
	response := &http.Response{Header: make(http.Header), ContentLength: 1234}
	for _, name := range []string{"Content-Length", "Content-Encoding", "Content-Range", "Accept-Ranges", "ETag", "Last-Modified", "Digest"} {
		response.Header.Set(name, "upstream")
	}
	sanitizeRewrittenMetadataHead(response)
	if response.ContentLength != -1 {
		t.Fatalf("HEAD content length = %d", response.ContentLength)
	}
	for _, name := range []string{"Content-Length", "Content-Encoding", "Content-Range", "Accept-Ranges", "ETag", "Last-Modified", "Digest"} {
		if response.Header.Get(name) != "" {
			t.Fatalf("HEAD retained %s: %#v", name, response.Header)
		}
	}
	if !strings.Contains(response.Header.Get("Vary"), "Accept-Encoding") {
		t.Fatalf("HEAD response does not describe representation variance: %#v", response.Header)
	}
}

func TestRewriteResponseRejectsUnexpectedCompressedUpstream(t *testing.T) {
	response := &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("compressed")), ContentLength: 10}
	response.Header.Set("Content-Type", "application/json")
	response.Header.Set("Content-Encoding", "br")
	if _, err := rewriteResponseBody(response, model.Mirror{}, "https://mirror.example", config.Default().Metadata, false, &gzipPool{}); err == nil {
		t.Fatal("compressed upstream metadata was accepted for identity rewrite")
	}
}

func TestBearerChallengePreservesServiceScopeAndUnknownParameter(t *testing.T) {
	challenge, err := parseBearerChallenge(`Bearer vendor="x,y",scope="repository:library/alpine:pull",realm="https://auth.docker.io/token",service="registry.docker.io"`)
	if err != nil {
		t.Fatal(err)
	}
	challenge.set("realm", "https://docker.example.com/_mirror_auth/7/token")
	encoded := challenge.String()
	for _, expected := range []string{`realm="https://docker.example.com/_mirror_auth/7/token"`, `service="registry.docker.io"`, `scope="repository:library/alpine:pull"`, `vendor="x,y"`} {
		if !strings.Contains(encoded, expected) {
			t.Fatalf("challenge semantics changed: %s", encoded)
		}
	}
}

func TestFetchPathRoundTripKeepsFragmentClientSide(t *testing.T) {
	target := "https://files.example/package.whl?download=1#sha256=abc"
	public := publicFetchPath(model.Mirror{PublicMode: "host"}, target)
	if !strings.HasPrefix(public, "/__fetch/") || !strings.HasSuffix(public, "#sha256=abc") {
		t.Fatalf("unexpected path %q", public)
	}
	encoded := strings.TrimSuffix(strings.TrimPrefix(public, "/__fetch/"), "#sha256=abc")
	decoded, err := base64Decode(encoded)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(decoded)
	if err != nil || parsed.RawQuery != "download=1" || parsed.Fragment != "" {
		t.Fatalf("decoded target invalid: %q, %v", decoded, err)
	}
}

func TestCargoTemplateRewriteLeavesClientPlaceholdersVisible(t *testing.T) {
	repository := model.Mirror{PublicMode: "host"}
	path := publicFetchPath(repository, "https://static.crates.io/crates/{crate}/{crate}-{version}.crate")
	if !strings.Contains(path, "/__fetch_template/") || !strings.Contains(path, "{crate}") || !strings.Contains(path, "{version}") {
		t.Fatalf("Cargo placeholders were hidden from the client: %q", path)
	}
}

func TestFullProxyBearerChallengeFailsClosed(t *testing.T) {
	engine := &Engine{tokenTargets: make(map[int64]*url.URL)}
	meta := requestMeta{repository: model.Mirror{ID: 7, RewriteHosts: []string{"auth.docker.io"}}, publicBase: "https://docker.example"}
	response := &http.Response{Header: make(http.Header)}
	response.Header.Set("WWW-Authenticate", `Bearer service="registry.docker.io"`)
	if err := engine.rewriteBearerChallenges(response, meta); err == nil {
		t.Fatal("Bearer challenge without realm was accepted")
	}
	response.Header.Set("WWW-Authenticate", `Bearer realm="https://127.0.0.1/token"`)
	if err := engine.rewriteBearerChallenges(response, meta); err == nil {
		t.Fatal("Bearer challenge with an unapproved realm was accepted")
	}
	response.Header.Set("WWW-Authenticate", `Basic realm="basic", Bearer realm="https://auth.docker.io/token",service="registry.docker.io"`)
	if err := engine.rewriteBearerChallenges(response, meta); err == nil {
		t.Fatal("secondary Bearer challenge bypassed full-proxy validation")
	}
}

func TestFullProxyBearerChallengeRewritesRealmAndPreservesOtherSchemes(t *testing.T) {
	engine := &Engine{tokenTargets: make(map[int64]*url.URL)}
	meta := requestMeta{repository: model.Mirror{ID: 7, RewriteHosts: []string{"auth.docker.io"}}, publicBase: "https://docker.example"}
	response := &http.Response{Header: make(http.Header)}
	response.Header.Add("WWW-Authenticate", `Basic realm="basic"`)
	response.Header.Add("WWW-Authenticate", `Bearer scope="repository:library/alpine:pull",realm="https://auth.docker.io/token",service="registry.docker.io"`)
	if err := engine.rewriteBearerChallenges(response, meta); err != nil {
		t.Fatal(err)
	}
	values := strings.Join(response.Header.Values("WWW-Authenticate"), "\n")
	if !strings.Contains(values, `Basic realm="basic"`) || !strings.Contains(values, `realm="https://docker.example/_mirror_auth/7/token"`) || !strings.Contains(values, `scope="repository:library/alpine:pull"`) {
		t.Fatalf("challenge rewrite lost semantics: %s", values)
	}
}

func TestStoredTokenTargetIsRevalidatedAgainstActiveRepositoryPolicy(t *testing.T) {
	oldTarget, err := url.Parse("https://old-auth.example/token")
	if err != nil {
		t.Fatal(err)
	}
	engine := &Engine{tokenTargets: map[int64]*url.URL{1: oldTarget}}
	repository := model.Mirror{ID: 1, RewriteHosts: []string{"new-auth.example"}}
	if target := engine.tokenTarget(repository); target != nil {
		t.Fatalf("stale token target remained authorized after an active policy change: %s", target)
	}
	repository.RewriteHosts = append(repository.RewriteHosts, "old-auth.example")
	if target := engine.tokenTarget(repository); target == nil || target.String() != oldTarget.String() {
		t.Fatalf("currently authorized token target was rejected: %v", target)
	}
}

func base64Decode(value string) (string, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	return string(decoded), err
}
