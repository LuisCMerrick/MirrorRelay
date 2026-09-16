package proxy

import (
	"context"
	"encoding/base64"
	"fmt"
	"github.com/LuisCMerrick/MirrorRelay/internal/security"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LuisCMerrick/MirrorRelay/internal/cachectl"
	"github.com/LuisCMerrick/MirrorRelay/internal/config"
	"github.com/LuisCMerrick/MirrorRelay/internal/database"
	"github.com/LuisCMerrick/MirrorRelay/internal/mirror"
	"github.com/LuisCMerrick/MirrorRelay/internal/model"
	"github.com/LuisCMerrick/MirrorRelay/internal/stats"
)

type regressionResolver struct{}

func (regressionResolver) LookupNetIP(context.Context, string, string) ([]netip.Addr, error) {
	return []netip.Addr{netip.MustParseAddr("8.8.8.8")}, nil
}

func regressionProxyFixture(t *testing.T, repo model.Mirror, handler http.HandlerFunc) (*Engine, *cachectl.Manager, model.Mirror) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	cfg := config.Default()
	cfg.Performance.ZeroCopyBypass = false
	cfg.UpstreamNginx.UpstreamSocketEnabled = false
	cfg.UpstreamNginx.UpstreamLocalPort = server.Listener.Addr().(*net.TCPAddr).Port
	cfg.HTTP.PublicBaseURL = "https://mirror.example"
	if err := mirror.NormalizeAndValidate(&repo, false, false); err != nil {
		t.Fatal(err)
	}
	db, err := database.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	repo, err = db.CreateMirror(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	registry := mirror.NewRegistry(db)
	registry.Replace([]model.Mirror{repo})
	cache := cachectl.New(cfg, db)
	if err := cache.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	engine := newEngine(cfg, registry, cache, stats.New(), nil, regressionResolver{}, testAuxiliarySigningKey)
	t.Cleanup(engine.CloseIdleConnections)
	return engine, cache, repo
}

func regressionRepository() model.Mirror {
	return model.Mirror{Name: "Audit", Slug: "audit", Type: "generic", Enabled: true,
		PublicMode: "path", PublicPath: "/audit/", ProxyMode: "transparent", CacheEnabled: true,
		Upstreams: []model.Upstream{{URL: "https://repo.example/base/", Enabled: true}}}
}

func TestPackagePolicyAppliesToDecodedAndAuxiliaryTargets(t *testing.T) {
	for _, policy := range []string{"basename", "relative-path", "whitelist"} {
		t.Run(policy, func(t *testing.T) {
			repo := regressionRepository()
			repo.HTMLRewriteEnabled = true
			switch policy {
			case "basename":
				repo.BlockedPackages = []string{"blocked.zip"}
			case "relative-path":
				repo.BlockedPackages = []string{"^dir/blocked[.]zip$"}
			case "whitelist":
				repo.AllowedPackages = []string{"allowed.zip"}
			}
			var calls atomic.Int64
			e, _, saved := regressionProxyFixture(t, repo, func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				fmt.Fprint(w, "allowed package")
			})
			origin := base64.RawURLEncoding.EncodeToString([]byte("https://repo.example"))
			paths := []string{
				"/audit/dir/blocked.zip",
				publicFetchPath(saved, "https://repo.example/base/dir/blocked.zip"),
				publicFetchPath(saved, "https://repo.example/base/dir/blocked%2Ezip"),
				"/audit/__fetch_template/" + origin + "/base/dir/blocked.zip",
			}
			// A valid signature does not exempt auxiliary resources from policy.
			target, _ := url.Parse("https://repo.example/dir/blocked.zip")
			base, _ := effectiveRepositoryBaseURL(saved, saved.Upstreams[0])
			aux, ok := mapBrowsableURL(saved, saved.Upstreams[0], base, target, testAuxiliarySigningKey)
			if !ok {
				t.Fatal("failed to generate auxiliary URL")
			}
			paths = append(paths, aux)
			for _, path := range paths {
				response := httptest.NewRecorder()
				e.ServeHTTP(response, httptest.NewRequest("GET", "https://mirror.example"+path, nil))
				if response.Code != http.StatusForbidden || calls.Load() != 0 {
					t.Fatalf("policy bypass via %s: status=%d calls=%d", path, response.Code, calls.Load())
				}
			}
			for _, path := range []string{"/audit/allowed.zip", publicFetchPath(saved, "https://repo.example/base/allowed.zip"), "/audit/__fetch_template/" + origin + "/base/allowed.zip"} {
				response := httptest.NewRecorder()
				e.ServeHTTP(response, httptest.NewRequest("GET", "https://mirror.example"+path, nil))
				if response.Code != http.StatusOK {
					t.Fatalf("allowed package rejected via %s: %d %s", path, response.Code, response.Body.String())
				}
			}
		})
	}
}

func TestPackagePolicyAppliesToEveryRedirect(t *testing.T) {
	for _, target := range []string{"https://repo.example/base/blocked.zip", "https://repo.example/base/blocked%2Ezip", "https://cdn.example/blocked.zip"} {
		t.Run(target, func(t *testing.T) {
			repo := regressionRepository()
			repo.RedirectMode = "follow"
			repo.BlockedPackages = []string{"blocked.zip"}
			repo.RewriteHosts = []string{"cdn.example"}
			var calls atomic.Int64
			e, _, _ := regressionProxyFixture(t, repo, func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.Header().Set("Location", target)
				w.WriteHeader(http.StatusTemporaryRedirect)
			})
			e.cfg.Performance.ZeroCopyBypass = true
			request := httptest.NewRequest("GET", "https://mirror.example/audit/allowed.zip", nil)
			request.Header.Set("X-Accel-Supported", "1")
			response := httptest.NewRecorder()
			e.ServeHTTP(response, request)
			if response.Code != http.StatusForbidden || calls.Load() != 1 {
				t.Fatalf("blocked redirect reached target: status=%d calls=%d", response.Code, calls.Load())
			}
		})
	}
}

func TestDecodedTargetRejectsUnsafeSegments(t *testing.T) {
	e, _, saved := regressionProxyFixture(t, regressionRepository(), func(w http.ResponseWriter, r *http.Request) { t.Error("unsafe target reached data plane") })
	for _, target := range []string{"https://repo.example/base/../blocked.zip", "https://repo.example/base/%2e%2e/blocked.zip", "https://repo.example/base/a%5cb.zip"} {
		response := httptest.NewRecorder()
		e.ServeHTTP(response, httptest.NewRequest("GET", "https://mirror.example"+publicFetchPath(saved, target), nil))
		if response.Code != http.StatusBadRequest {
			t.Fatalf("unsafe target %s: status=%d", target, response.Code)
		}
	}
}

func TestLocalValidatorNeverSubstitutesForAuthentication(t *testing.T) {
	for _, cacheEnabled := range []bool{false, true} {
		for _, cacheAuthenticated := range []bool{false, true} {
			t.Run(fmt.Sprintf("cache=%v/auth-cache=%v", cacheEnabled, cacheAuthenticated), func(t *testing.T) {
				repo := regressionRepository()
				repo.RewriteEnabled, repo.CacheEnabled, repo.CacheAuthenticated = true, cacheEnabled, cacheAuthenticated
				var calls atomic.Int64
				e, _, _ := regressionProxyFixture(t, repo, func(w http.ResponseWriter, r *http.Request) {
					calls.Add(1)
					if r.Header.Get("Authorization") != "Bearer valid" {
						w.WriteHeader(http.StatusUnauthorized)
						return
					}
					w.Header().Set("Content-Type", "application/json")
					w.Header().Set("Cache-Control", "private, no-store")
					fmt.Fprint(w, `{"private":true}`)
				})
				for i, credential := range []string{"Bearer valid", "", "Bearer invalid", "Bearer valid"} {
					request := httptest.NewRequest("GET", "https://mirror.example/audit/index.json", nil)
					request.Header.Set("Authorization", credential)
					request.Header.Set("If-None-Match", "*")
					response := httptest.NewRecorder()
					e.ServeHTTP(response, request)
					want := http.StatusUnauthorized
					if credential == "Bearer valid" {
						want = http.StatusOK
					}
					if response.Code != want || calls.Load() != int64(i+1) {
						t.Fatalf("credential %q: status=%d calls=%d", credential, response.Code, calls.Load())
					}
				}
			})
		}
	}
}

func TestLocalValidatorRespectsResponseCachePolicy(t *testing.T) {
	for _, test := range []struct{ name, value string }{
		{"Cache-Control", "no-store"}, {"Cache-Control", "no-cache"}, {"Cache-Control", "private"},
		{"Cache-Control", "public, max-age=0"}, {"Cache-Control", "public, s-maxage=0"},
		{"Set-Cookie", "upstream=value"}, {"Vary", "Authorization"}, {"Vary", "*"},
		{"Expires", "Thu, 01 Jan 1970 00:00:00 GMT"}, {"Age", "9999"},
	} {
		t.Run(test.name+"/"+test.value, func(t *testing.T) {
			repo := regressionRepository()
			repo.RewriteEnabled = true
			var calls atomic.Int64
			e, _, _ := regressionProxyFixture(t, repo, func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set(test.name, test.value)
				fmt.Fprint(w, `{"version":1}`)
			})
			for i := 0; i < 2; i++ {
				request := httptest.NewRequest("GET", "https://mirror.example/audit/index.json", nil)
				request.Header.Set("If-None-Match", "*")
				response := httptest.NewRecorder()
				e.ServeHTTP(response, request)
				if response.Code != http.StatusOK || calls.Load() != int64(i+1) {
					t.Fatalf("uncacheable response reused: status=%d calls=%d", response.Code, calls.Load())
				}
			}
		})
	}
}

func TestLocalValidatorPartitionsAcceptAndHonorsRequestPolicy(t *testing.T) {
	repo := regressionRepository()
	repo.RewriteEnabled = true
	var calls atomic.Int64
	e, _, _ := regressionProxyFixture(t, repo, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "public, max-age=60")
		w.Header().Set("Vary", "Accept")
		fmt.Fprintf(w, `{"accept":%q}`, r.Header.Get("Accept"))
	})
	request := func(accept, name, value string, status int) {
		t.Helper()
		r := httptest.NewRequest("GET", "https://mirror.example/audit/index.json", nil)
		r.Header.Set("Accept", accept)
		r.Header.Set("If-None-Match", "*")
		if name != "" {
			r.Header.Set(name, value)
		}
		w := httptest.NewRecorder()
		e.ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("%s=%s: status=%d want=%d", name, value, w.Code, status)
		}
		if status == 304 && (w.Header().Get("Cache-Control") != "public, max-age=60" || !strings.Contains(w.Header().Get("Vary"), "Accept") || w.Header().Get("Age") == "") {
			t.Fatalf("304 lost cache headers: %v", w.Header())
		}
	}
	request("application/json", "", "", 200)
	request("application/json", "", "", 304)
	request("text/plain", "", "", 200)
	if calls.Load() != 2 {
		t.Fatalf("representation calls=%d", calls.Load())
	}
	for _, header := range []struct{ name, value string }{
		{"Cache-Control", "no-cache"}, {"Cache-Control", "no-store"}, {"Cache-Control", "max-age=0"},
		{"Pragma", "no-cache"}, {"Range", "bytes=0-2"}, {"Cookie", "upstream=value"}, {"Authorization", "Bearer token"},
	} {
		before := calls.Load()
		request("application/json", header.name, header.value, 200)
		if calls.Load() != before+1 {
			t.Fatalf("request cache policy %s was ignored", header.name)
		}
	}
	// Disabling repository caching invalidates admission even if a validator exists.
	saved, found := e.registry.GetByID(1)
	if !found {
		t.Fatal("fixture repository not found")
	}
	saved.CacheEnabled = false
	e.registry.Replace([]model.Mirror{saved})
	request("text/plain", "", "", 200)
}

func TestLocalValidatorTracksAllPurgeGenerations(t *testing.T) {
	for _, scope := range []string{"global", "repository", "object"} {
		t.Run(scope, func(t *testing.T) {
			repo := regressionRepository()
			repo.RewriteEnabled = true
			var calls atomic.Int64
			e, cache, saved := regressionProxyFixture(t, repo, func(w http.ResponseWriter, r *http.Request) {
				call := calls.Add(1)
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprintf(w, `{"version":%d}`, call)
			})
			first := httptest.NewRecorder()
			e.ServeHTTP(first, httptest.NewRequest("GET", "https://mirror.example/audit/index.json", nil))
			if first.Code != 200 || first.Header().Get("ETag") == "" {
				t.Fatalf("first response: %d", first.Code)
			}
			_, objectID, err := cache.Key(context.Background(), saved.ID, saved.Upstreams[0].URL, "/index.json", "")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := cache.Purge(context.Background(), scope, saved.ID, objectID, "test"); err != nil {
				t.Fatal(err)
			}
			request := httptest.NewRequest("GET", "https://mirror.example/audit/index.json", nil)
			request.Header.Set("If-None-Match", first.Header().Get("ETag"))
			after := httptest.NewRecorder()
			e.ServeHTTP(after, request)
			if after.Code != 200 || calls.Load() != 2 || after.Header().Get("ETag") == first.Header().Get("ETag") {
				t.Fatalf("stale validator after %s purge: status=%d calls=%d", scope, after.Code, calls.Load())
			}
		})
	}
}

func TestPurgeDuringResponseCannotRepopulateCurrentValidator(t *testing.T) {
	repo := regressionRepository()
	repo.RewriteEnabled = true
	started, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int64
	e, cache, saved := regressionProxyFixture(t, repo, func(w http.ResponseWriter, r *http.Request) {
		call := calls.Add(1)
		if call == 1 {
			close(started)
			<-release
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"version":%d}`, call)
	})
	done := make(chan struct{})
	go func() {
		defer close(done)
		e.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "https://mirror.example/audit/index.json", nil))
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		close(release)
		t.Fatal("upstream request did not start")
	}
	_, err := cache.Purge(context.Background(), "repository", saved.ID, "", "test")
	close(release)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("upstream request did not finish")
	}
	request := httptest.NewRequest("GET", "https://mirror.example/audit/index.json", nil)
	request.Header.Set("If-None-Match", "*")
	response := httptest.NewRecorder()
	e.ServeHTTP(response, request)
	if response.Code != 200 || calls.Load() != 2 {
		t.Fatalf("old in-flight validator survived purge: status=%d calls=%d", response.Code, calls.Load())
	}
}

func TestZeroCopyPreservesFullProxyRedirects(t *testing.T) {
	repo := regressionRepository()
	repo.Type, repo.PublicMode, repo.PublicHost, repo.PublicPath = "docker-registry", "host", "registry.example", "/"
	repo.ProxyMode, repo.AuthMode, repo.RedirectMode, repo.BlobRedirectMode = "registry", "full_proxy", "full_proxy", "full_proxy"
	repo.RewriteHosts = []string{"cdn.example"}
	var calls atomic.Int64
	e, _, _ := regressionProxyFixture(t, repo, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if strings.HasPrefix(r.URL.Path, "/_target/") {
			fmt.Fprint(w, "blob data")
			return
		}
		w.Header().Set("Location", "https://cdn.example/blob.bin")
		w.WriteHeader(http.StatusTemporaryRedirect)
	})
	e.cfg.Performance.ZeroCopyBypass = true
	request := httptest.NewRequest("GET", "https://registry.example/v2/audit/blobs/sha256:abcd", nil)
	request.Header.Set("X-Accel-Supported", "1")
	response := httptest.NewRecorder()
	e.ServeHTTP(response, request)
	if response.Code != 200 || response.Body.String() != "blob data" || calls.Load() != 2 || response.Header().Get("X-Accel-Redirect") != "" {
		t.Fatalf("full proxy bypassed: status=%d body=%q calls=%d", response.Code, response.Body.String(), calls.Load())
	}
}

func TestRepositoryResponsesKeepOpaqueSandbox(t *testing.T) {
	for _, contentType := range []string{"text/html", "image/svg+xml", "application/octet-stream"} {
		for _, rewrite := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/rewrite=%v", contentType, rewrite), func(t *testing.T) {
				repo := regressionRepository()
				repo.HTMLRewriteEnabled = rewrite
				e, _, _ := regressionProxyFixture(t, repo, func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", contentType)
					w.Header().Set("Content-Security-Policy", "sandbox allow-same-origin allow-scripts")
					w.Header().Set("X-Accel-Redirect", "/admin/")
					fmt.Fprint(w, `<html><script>window.marker=true;</script><a href="https://repo.example/base/allowed.zip">package</a></html>`)
				})
				for _, suffix := range []string{"", "?safe-ui=1"} {
					response := httptest.NewRecorder()
					e.ServeHTTP(response, httptest.NewRequest("GET", "https://mirror.example/audit/page.html"+suffix, nil))
					if response.Code != 200 || !strings.Contains(strings.Join(response.Header().Values("Content-Security-Policy"), ","), security.RepositoryContentSecurityPolicy) || response.Header().Get("X-Content-Type-Options") != "nosniff" || response.Header().Get("X-Accel-Redirect") != "" {
						t.Fatalf("missing sandbox: status=%d headers=%v", response.Code, response.Header())
					}
				}
			})
		}
	}
}

func TestDecodedPackagePolicyHandlesHostRewriteAndOverlappingBases(t *testing.T) {
	repo := regressionRepository()
	repo.HostRewrite = "logical.example"
	repo.BlockedPackages = []string{"^dir/blocked[.]zip$"}
	repo.Upstreams = append(repo.Upstreams, model.Upstream{URL: "https://repo.example/base/dir/", Enabled: true, Priority: 200})
	e, _, saved := regressionProxyFixture(t, repo, func(w http.ResponseWriter, r *http.Request) { t.Error("blocked target reached data plane") })
	request := httptest.NewRequest("GET", "https://mirror.example"+publicFetchPath(saved, "https://repo.example/base/dir/blocked.zip"), nil)
	response := httptest.NewRecorder()
	e.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("overlapping base/Host rewrite bypass: status=%d", response.Code)
	}
}

func TestZeroCopyRetainsGenericRedirectAndFailoverHandling(t *testing.T) {
	for _, mode := range []string{"follow", "full_proxy", "rewrite", "failover"} {
		t.Run(mode, func(t *testing.T) {
			repo := regressionRepository()
			repo.RewriteHosts = []string{"cdn.example"}
			repo.RedirectMode = mode
			if mode == "failover" {
				repo.RedirectMode = "pass"
				repo.Upstreams = append(repo.Upstreams, model.Upstream{URL: "https://backup.example/", Enabled: true, Priority: 200})
			}
			var calls atomic.Int64
			e, _, _ := regressionProxyFixture(t, repo, func(w http.ResponseWriter, r *http.Request) {
				call := calls.Add(1)
				if mode == "failover" {
					if call == 1 {
						w.WriteHeader(http.StatusServiceUnavailable)
						return
					}
					fmt.Fprint(w, "package bytes")
					return
				}
				if strings.HasPrefix(r.URL.Path, "/_target/") {
					fmt.Fprint(w, "package bytes")
					return
				}
				w.Header().Set("Location", "https://cdn.example/final.zip")
				w.WriteHeader(http.StatusTemporaryRedirect)
			})
			e.cfg.Performance.ZeroCopyBypass = true
			request := httptest.NewRequest("GET", "https://mirror.example/audit/file.zip", nil)
			request.Header.Set("X-Accel-Supported", "1")
			response := httptest.NewRecorder()
			e.ServeHTTP(response, request)
			if response.Header().Get("X-Accel-Redirect") != "" {
				t.Fatal("Go response processing was bypassed")
			}
			if mode == "rewrite" {
				if response.Code != 307 || !strings.HasPrefix(response.Header().Get("Location"), "https://mirror.example/audit/__fetch/") || calls.Load() != 1 {
					t.Fatalf("redirect rewrite failed: status=%d headers=%v calls=%d", response.Code, response.Header(), calls.Load())
				}
			} else if response.Code != 200 || response.Body.String() != "package bytes" || calls.Load() != 2 {
				t.Fatalf("proxy handling failed: status=%d body=%q calls=%d", response.Code, response.Body.String(), calls.Load())
			}
		})
	}
}

func TestStaticCredentialsCannotPopulateLocalValidators(t *testing.T) {
	for _, name := range []string{"Authorization", "Cookie"} {
		t.Run(name, func(t *testing.T) {
			repo := regressionRepository()
			repo.RewriteEnabled = true
			repo.CacheAuthenticated = true
			repo.HeaderAdd = map[string]string{name: "fixture-credential"}
			var calls atomic.Int64
			e, _, _ := regressionProxyFixture(t, repo, func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, `{"private":true}`)
			})
			for i := 0; i < 2; i++ {
				request := httptest.NewRequest("GET", "https://mirror.example/audit/index.json", nil)
				request.Header.Set("If-None-Match", "*")
				response := httptest.NewRecorder()
				e.ServeHTTP(response, request)
				if response.Code != 200 || calls.Load() != int64(i+1) {
					t.Fatalf("static credentials reused locally: status=%d calls=%d", response.Code, calls.Load())
				}
			}
		})
	}
}
