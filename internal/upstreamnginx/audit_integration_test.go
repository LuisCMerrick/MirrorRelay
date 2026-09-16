package upstreamnginx

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/LuisCMerrick/MirrorRelay/internal/cachectl"
	"github.com/LuisCMerrick/MirrorRelay/internal/config"
	"github.com/LuisCMerrick/MirrorRelay/internal/database"
	"github.com/LuisCMerrick/MirrorRelay/internal/mirror"
	"github.com/LuisCMerrick/MirrorRelay/internal/model"
	"github.com/LuisCMerrick/MirrorRelay/internal/proxy"
	"github.com/LuisCMerrick/MirrorRelay/internal/security"
	"github.com/LuisCMerrick/MirrorRelay/internal/stats"
)

func runRegressionNginx(t *testing.T, block string, port int) string {
	t.Helper()
	binary := os.Getenv("MIRRORRELAY_TEST_UPSTREAM_NGINX")
	if binary == "" {
		t.Skip("set MIRRORRELAY_TEST_UPSTREAM_NGINX for real ingress reproduction")
	}
	root := t.TempDir()
	currentUser, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	content := fmt.Sprintf("%sdaemon off; master_process off; pid %s/nginx.pid; error_log %s/error.log warn; events { worker_connections 64; } http { access_log off; client_body_temp_path %s/client; proxy_temp_path %s/proxy; %s }", workerUserDirective(currentUser.Username, os.Geteuid()), root, root, root, root, block)
	path := filepath.Join(root, "nginx.conf")
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command(binary, "-t", "-e", "stderr", "-p", root+"/", "-c", path).CombinedOutput(); err != nil {
		t.Fatalf("fixture nginx -t: %v %s", err, output)
	}
	cmd := exec.Command(binary, "-e", "stderr", "-p", root+"/", "-c", path)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	t.Cleanup(func() {
		if t.Failed() {
			logs, _ := os.ReadFile(filepath.Join(root, "error.log"))
			t.Logf("Nginx fixture log: %s", logs)
		}
		_ = cmd.Process.Signal(syscall.SIGTERM)
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			_ = cmd.Process.Kill()
			<-done
		}
	})
	address := fmt.Sprintf("127.0.0.1:%d", port)
	deadline := time.Now().Add(3 * time.Second)
	for {
		conn, err := net.DialTimeout("tcp", address, 50*time.Millisecond)
		if err == nil {
			conn.Close()
			break
		}
		if time.Now().After(deadline) {
			logs, _ := os.ReadFile(filepath.Join(root, "error.log"))
			t.Fatalf("Nginx fixture did not listen: %s", logs)
		}
		time.Sleep(10 * time.Millisecond)
	}
	return "http://" + address
}

func regressionUnusedPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	return port
}

// Run the generated ingress -> Go -> generated data plane -> local origins.
// TLS listeners alone are replaced by loopback HTTP for this isolated fixture.
func regressionRepositoryChain(t *testing.T, repositories []model.Mirror, zeroCopy bool, overrides ...func(http.ResponseWriter, *http.Request) bool) (string, *proxy.Engine, *atomic.Int64) {
	t.Helper()
	if os.Getenv("MIRRORRELAY_TEST_UPSTREAM_NGINX") == "" {
		t.Skip("set MIRRORRELAY_TEST_UPSTREAM_NGINX for real proxy-chain tests")
	}
	cfg := config.Default()
	cfg.Performance.ZeroCopyBypass = zeroCopy
	cfg.HTTP.PublicBaseURL = "https://mirror.example"
	cfg.Security.AdminCIDRs = []string{"192.0.2.0/24"}
	cfg.Security.AllowHTTPUpstream, cfg.Security.AllowPrivateUpstream = true, true
	cfg.UpstreamNginx.UpstreamSocketEnabled = false
	cfg.UpstreamNginx.UpstreamLocalPort = regressionUnusedPort(t)
	for i := range repositories {
		// Ordinary fixture downloads deliberately permit pass-through redirects
		// so zeroCopy=true actually exercises the acceleration path.
		if repositories[i].ProxyMode != "registry" && repositories[i].RedirectMode == "" {
			repositories[i].RedirectMode = "pass"
		}
		if err := mirror.NormalizeAndValidate(&repositories[i], true, true); err != nil {
			t.Fatal(err)
		}
	}
	g, err := NewGenerator(cfg, nil).Generate(context.Background(), repositories, nil)
	if err != nil {
		t.Fatal(err)
	}
	runRegressionNginx(t, g.Files["upstreams.conf"]+fmt.Sprintf("server { listen 127.0.0.1:%d; %s }", cfg.UpstreamNginx.UpstreamLocalPort, g.Files["repositories.conf"]), cfg.UpstreamNginx.UpstreamLocalPort)
	db, err := database.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	registry := mirror.NewRegistry(db)
	registry.Replace(repositories)
	cache := cachectl.New(cfg, db)
	if err := cache.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	engine := proxy.New(cfg, registry, cache, stats.New(), nil, []byte("0123456789abcdef0123456789abcdef"))
	t.Cleanup(engine.CloseIdleConnections)
	var calls atomic.Int64
	frontend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		for _, override := range overrides {
			if override(w, r) {
				return
			}
		}
		engine.ServeHTTP(w, r)
	}))
	t.Cleanup(frontend.Close)
	cfg.Server.LocalPort = frontend.Listener.Addr().(*net.TCPAddr).Port
	snippet := NewGenerator(cfg, nil).integrationSnippet(repositories)
	port := regressionUnusedPort(t)
	block := fmt.Sprintf("server { listen 127.0.0.1:%d; %s }", port, snippet)
	if repositories[0].PublicMode == "host" {
		start := strings.Index(snippet, "server {\n")
		if start < 0 {
			t.Fatal("generated host server missing")
		}
		end := strings.Index(snippet[start:], "\n}\n")
		if end < 0 {
			t.Fatal("generated host server incomplete")
		}
		block = snippet[start : start+end+3]
		block = strings.Replace(block, "listen 443 ssl http2;", fmt.Sprintf("listen 127.0.0.1:%d;", port), 1)
	}
	return runRegressionNginx(t, block, port), engine, &calls
}

func regressionClientGet(t *testing.T, base, host, path string) (*http.Response, string) {
	t.Helper()
	request, err := http.NewRequest("GET", base+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Host = host
	client := &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return response, string(body)
}

func TestRealManagedUpstreamNginxOriginCannotAccelerateAcrossPolicies(t *testing.T) {
	for _, zeroCopy := range []bool{false, true} {
		for _, target := range []string{"/_repo/2/2/package/private.zip", "/_repo/1/1/package/blocked.zip"} {
			t.Run(fmt.Sprintf("zero-copy=%v/target=%s", zeroCopy, target), func(t *testing.T) {
				var protectedCalls atomic.Int64
				secretOrigin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					protectedCalls.Add(1)
					fmt.Fprint(w, "protected bytes")
				}))
				defer secretOrigin.Close()
				publicOrigin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if strings.HasSuffix(r.URL.Path, "/blocked.zip") {
						protectedCalls.Add(1)
					}
					w.Header().Set("X-Accel-Redirect", target)
					w.Header().Set("Content-Type", "text/html")
					w.Header().Set("Content-Security-Policy", "sandbox allow-scripts allow-same-origin")
					fmt.Fprint(w, "public bytes")
				}))
				defer publicOrigin.Close()
				repositories := []model.Mirror{
					{ID: 1, Name: "Public", Slug: "public", Type: "generic", Enabled: true, PublicMode: "path", PublicPath: "/public/", BlockedPackages: []string{"blocked.zip"}, AllowHTTP: true, AllowPrivate: true, Upstreams: []model.Upstream{{ID: 1, URL: publicOrigin.URL + "/", Enabled: true}}},
					{ID: 2, Name: "Protected", Slug: "protected", Type: "generic", Enabled: true, PublicMode: "path", PublicPath: "/protected/", AccessPolicy: "admin", AllowHTTP: true, AllowPrivate: true, HeaderAdd: map[string]string{"Authorization": "Bearer private-test-credential"}, Upstreams: []model.Upstream{{ID: 2, URL: secretOrigin.URL + "/", Enabled: true}}},
				}
				base, _, frontendCalls := regressionRepositoryChain(t, repositories, zeroCopy)
				for _, path := range []string{"/protected/private.zip", "/public/blocked.zip"} {
					response, _ := regressionClientGet(t, base, "mirror.example", path)
					if response.StatusCode != http.StatusForbidden {
						t.Fatalf("direct access %s: %d", path, response.StatusCode)
					}
				}
				before := frontendCalls.Load()
				response, body := regressionClientGet(t, base, "mirror.example", "/public/file.zip")
				if response.StatusCode != 200 || body != "public bytes" || protectedCalls.Load() != 0 || frontendCalls.Load() != before+1 {
					t.Fatalf("origin crossed policy: status=%d body=%q protected=%d frontend=%d", response.StatusCode, body, protectedCalls.Load(), frontendCalls.Load())
				}
				if !strings.Contains(strings.Join(response.Header.Values("Content-Security-Policy"), ","), security.RepositoryContentSecurityPolicy) || response.Header.Get("X-Content-Type-Options") != "nosniff" {
					t.Fatalf("response escaped sandbox: %v", response.Header)
				}
				if response.Header.Get("X-Accel-Redirect") != "" {
					t.Fatal("origin acceleration header escaped")
				}
			})
		}
	}
}

func TestRealManagedUpstreamNginxHostAndPathAcceleration(t *testing.T) {
	for _, mode := range []string{"host", "path"} {
		t.Run(mode, func(t *testing.T) {
			var originCalls atomic.Int64
			origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				originCalls.Add(1)
				fmt.Fprint(w, "package bytes")
			}))
			defer origin.Close()
			repo := model.Mirror{ID: 1, Name: "Packages", Slug: "packages", Type: "generic", Enabled: true, PublicMode: mode, PublicPath: "/packages/", AllowHTTP: true, AllowPrivate: true, Upstreams: []model.Upstream{{ID: 1, URL: origin.URL + "/", Enabled: true}}}
			path := "/packages/file.zip"
			host := "mirror.example"
			if mode == "host" {
				host = "packages.example"
				repo.PublicHost = host
				repo.PublicPath = "/"
				path = "/file.zip"
			}
			base, _, frontendCalls := regressionRepositoryChain(t, []model.Mirror{repo}, true)
			response, body := regressionClientGet(t, base, host, path)
			if response.StatusCode != 200 || body != "package bytes" || frontendCalls.Load() != 1 || originCalls.Load() != 1 {
				t.Fatalf("acceleration failed: status=%d body=%q frontend=%d origin=%d", response.StatusCode, body, frontendCalls.Load(), originCalls.Load())
			}
			response, _ = regressionClientGet(t, base, host, "/_repo/1/1/package/file.zip")
			if response.StatusCode != 404 || originCalls.Load() != 1 {
				t.Fatalf("internal acceleration location is public: status=%d origin=%d", response.StatusCode, originCalls.Load())
			}
		})
	}
}

func TestRealManagedUpstreamNginxFullProxyKeepsRedirectsInternal(t *testing.T) {
	for _, zeroCopy := range []bool{false, true} {
		t.Run(fmt.Sprintf("zero-copy=%v", zeroCopy), func(t *testing.T) {
			var cdnCalls atomic.Int64
			cdn := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				cdnCalls.Add(1)
				if r.Header.Get("Authorization") != "" {
					t.Error("cross-origin credentials reached CDN")
				}
				fmt.Fprint(w, "final blob")
			}))
			defer cdn.Close()
			origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Location", cdn.URL+"/blob.bin")
				w.WriteHeader(http.StatusTemporaryRedirect)
			}))
			defer origin.Close()
			repo := model.Mirror{ID: 1, Name: "Registry", Slug: "registry", Type: "docker-registry", Enabled: true, PublicMode: "host", PublicHost: "registry.example", PublicPath: "/", ProxyMode: "registry", AuthMode: "full_proxy", BlobRedirectMode: "full_proxy", AllowHTTP: true, AllowPrivate: true, HeaderAdd: map[string]string{"Authorization": "Bearer origin-credential"}, Upstreams: []model.Upstream{{ID: 1, URL: origin.URL + "/", Enabled: true}, {ID: 2, URL: cdn.URL + "/", Enabled: true, Priority: 200}}}
			base, _, frontendCalls := regressionRepositoryChain(t, []model.Mirror{repo}, zeroCopy)
			response, body := regressionClientGet(t, base, "registry.example", "/v2/project/blobs/sha256:abcd")
			if response.StatusCode != 200 || body != "final blob" || response.Header.Get("Location") != "" || cdnCalls.Load() != 1 || frontendCalls.Load() != 1 {
				t.Fatalf("full proxy leaked redirect: status=%d body=%q headers=%v CDN=%d frontend=%d", response.StatusCode, body, response.Header, cdnCalls.Load(), frontendCalls.Load())
			}
		})
	}
}
