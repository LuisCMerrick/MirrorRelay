package upstreamnginx

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LuisCMerrick/MirrorRelay/internal/model"
)

// A real browser exercises document-origin isolation, not merely header presence.
// All content and credentials are test fixtures on loopback; no production API
// or user browser profile is used. The optional binary is Chromium-compatible.
func TestRealManagedUpstreamNginxBrowserOriginIsolation(t *testing.T) {
	binary := os.Getenv("MIRRORRELAY_TEST_BROWSER")
	if binary == "" {
		t.Skip("set MIRRORRELAY_TEST_BROWSER to a Chromium-compatible headless binary")
	}
	const probe = `(async function() {
  let cookies = false, storage = false, session = false;
  try { void document.cookie; cookies = true; } catch (_) {}
  try { void localStorage.length; storage = true; } catch (_) {}
  try { const r = await fetch('/admin/api/v1/auth/session', {credentials:'include'}); session = r.ok && (await r.json()).csrf_token === 'fixture-csrf'; } catch (_) {}
  document.getElementById('result').textContent = [cookies,storage,session].join(',');
})();`
	html := "<!doctype html><html><body><output id=\"result\">WAIT</output><script>" + probe + "</script></body></html>"
	svg := "<svg xmlns=\"http://www.w3.org/2000/svg\"><text id=\"result\">WAIT</text><script><![CDATA[" + probe + "]]></script></svg>"
	for _, zeroCopy := range []bool{false, true} {
		t.Run(fmt.Sprintf("zero-copy=%v", zeroCopy), func(t *testing.T) {
			origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Security-Policy", "sandbox allow-same-origin allow-scripts")
				if r.URL.Query().Get("kind") == "svg" {
					w.Header().Set("Content-Type", "image/svg+xml")
					fmt.Fprint(w, svg)
				} else {
					w.Header().Set("Content-Type", "text/html")
					fmt.Fprint(w, html)
				}
			}))
			defer origin.Close()
			var sessionCalls atomic.Int64
			adminFixture := func(w http.ResponseWriter, r *http.Request) bool {
				switch r.URL.Path {
				case "/admin/start":
					http.SetCookie(w, &http.Cookie{Name: "fixture-session", Value: "signed-in", Path: "/admin/", HttpOnly: true, SameSite: http.SameSiteStrictMode})
					http.Redirect(w, r, "/public/probe.zip?kind="+r.URL.Query().Get("kind"), http.StatusFound)
				case "/admin/control":
					http.SetCookie(w, &http.Cookie{Name: "fixture-session", Value: "signed-in", Path: "/admin/", HttpOnly: true, SameSite: http.SameSiteStrictMode})
					w.Header().Set("Content-Type", "text/html")
					fmt.Fprint(w, html)
				case "/admin/api/v1/auth/session":
					sessionCalls.Add(1)
					cookie, err := r.Cookie("fixture-session")
					if err != nil || cookie.Value != "signed-in" {
						w.WriteHeader(http.StatusUnauthorized)
						return true
					}
					w.Header().Set("Content-Type", "application/json")
					fmt.Fprint(w, `{"csrf_token":"fixture-csrf"}`)
				default:
					return false
				}
				return true
			}
			repo := model.Mirror{ID: 1, Name: "Public", Slug: "public", Type: "generic", Enabled: true, PublicMode: "path", PublicPath: "/public/", AllowHTTP: true, AllowPrivate: true, Upstreams: []model.Upstream{{ID: 1, URL: origin.URL + "/", Enabled: true}}}
			base, _, _ := regressionRepositoryChain(t, []model.Mirror{repo}, zeroCopy, adminFixture)
			for _, kind := range []string{"control", "html", "svg"} {
				t.Run(kind, func(t *testing.T) {
					target := base + "/admin/start?kind=" + kind
					want := `id="result">false,false,false</`
					if kind == "control" {
						target = base + "/admin/control"
						want = `id="result">true,true,true</`
					}
					ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
					defer cancel()
					args := []string{"--headless", "--disable-gpu", "--disable-dev-shm-usage", "--disable-background-networking", "--no-first-run", "--no-default-browser-check", "--no-proxy-server", "--user-data-dir=" + t.TempDir(), "--dump-dom", "--virtual-time-budget=3000", target}
					if os.Geteuid() == 0 {
						args = append([]string{"--no-sandbox"}, args...)
					}
					output, err := exec.CommandContext(ctx, binary, args...).CombinedOutput()
					if err != nil || !strings.Contains(string(output), want) {
						t.Fatalf("browser isolation probe failed: err=%v output=%s", err, output)
					}
				})
			}
			if sessionCalls.Load() != 1 {
				t.Fatalf("only the trusted positive control may read the session: calls=%d", sessionCalls.Load())
			}
		})
	}
}
