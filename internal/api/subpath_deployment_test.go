package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/LuisCMerrick/MirrorRelay/internal/config"
	"github.com/LuisCMerrick/MirrorRelay/internal/mirror"
	"github.com/LuisCMerrick/MirrorRelay/internal/model"
	"github.com/LuisCMerrick/MirrorRelay/internal/upstreamnginx"
)

type dummyLoader struct {
	mirrors []model.Mirror
}

func (d *dummyLoader) ListMirrors(_ context.Context) ([]model.Mirror, error) {
	return d.mirrors, nil
}

func TestSubpathDeploymentRoutingAndIndex(t *testing.T) {
	cfg := config.Default()
	cfg.HTTP.BasePath = "/mirrors"
	cfg.HTTP.PublicBaseURL = "https://mirror.example.com/mirrors"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("cfg.Validate failed: %v", err)
	}
	if err := cfg.NormalizeRuntime(); err != nil {
		t.Fatalf("cfg.NormalizeRuntime failed: %v", err)
	}

	debianRepo := model.Mirror{
		ID:         1,
		Name:       "Debian",
		Slug:       "debian",
		Type:       "apt",
		PublicMode: "path",
		PublicPath: "/debian/",
		Enabled:    true,
		Upstreams: []model.Upstream{
			{ID: 1, URL: "https://deb.debian.org/debian", Enabled: true},
		},
		Help: model.HelpConfig{
			Enabled:  true,
			Template: "debian",
		},
	}

	reg := mirror.NewRegistry(&dummyLoader{mirrors: []model.Mirror{debianRepo}})
	reg.Replace([]model.Mirror{debianRepo})
	if bp := cfg.BasePath(); bp != "" {
		reg.SetBasePath(bp)
	}

	server := &Server{
		cfg:      cfg,
		registry: reg,
		web: fstest.MapFS{
			"index.html": {Data: []byte("<!doctype html><title>MirrorRelay Control Plane</title>")},
		},
	}

	var proxiedPath string
	dummyProxy := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxiedPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("proxied ok"))
	})

	handler := server.Handler(dummyProxy)

	// 1. /mirrors without slash must redirect to /mirrors/
	{
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/mirrors", nil)
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusMovedPermanently {
			t.Fatalf("expected 301 for /mirrors, got %d", rec.Code)
		}
		if loc := rec.Header().Get("Location"); loc != "/mirrors/" {
			t.Fatalf("expected redirect to /mirrors/, got %q", loc)
		}
	}

	// 2. /mirrors/ must serve repository index HTML with subpath links
	{
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/mirrors/", nil)
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 for /mirrors/, got %d", rec.Code)
		}
		body := rec.Body.String()
		if !strings.Contains(body, `href="/mirrors/debian/"`) {
			t.Errorf("repository index HTML missing subpath link href=\"/mirrors/debian/\", got:\n%s", body)
		}
		if !strings.Contains(body, `href="/mirrors/help/debian/"`) {
			t.Errorf("repository index HTML missing subpath help link href=\"/mirrors/help/debian/\", got:\n%s", body)
		}
		if !strings.Contains(body, `href="/mirrors/" class="site-brand"`) {
			t.Errorf("repository index HTML missing subpath brand link href=\"/mirrors/\", got:\n%s", body)
		}
	}

	// 3. /mirrors/admin/ must serve the admin web UI
	{
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/mirrors/admin/", nil)
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 for /mirrors/admin/, got %d", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "MirrorRelay Control Plane") {
			t.Fatalf("expected admin index HTML, got:\n%s", rec.Body.String())
		}
	}

	// 4. /mirrors/help/debian/ must serve help detail
	{
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/mirrors/help/debian/", nil)
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 for /mirrors/help/debian/, got %d", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "Debian") {
			t.Fatalf("expected debian help content, got:\n%s", rec.Body.String())
		}
	}

	// 5. /mirrors/debian/... must route to proxy
	{
		proxiedPath = ""
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/mirrors/debian/dists/bookworm/Release", nil)
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 for proxied request, got %d", rec.Code)
		}
		if proxiedPath != "/mirrors/debian/dists/bookworm/Release" {
			t.Fatalf("expected proxy to receive request, got %q", proxiedPath)
		}
	}

	// 6. External Shared Nginx integration snippet includes subpath locations
	{
		generator := upstreamnginx.NewGenerator(cfg, nil)
		generated, err := generator.Generate(context.Background(), []model.Mirror{debianRepo}, nil)
		if err != nil {
			t.Fatalf("generator.Generate failed: %v", err)
		}
		snippet := generated.Files["external-nginx-integration.conf"]
		if !strings.Contains(snippet, `location = "/mirrors"`) || !strings.Contains(snippet, `location = "/mirrors/"`) {
			t.Errorf("nginx snippet missing subpath index locations, got:\n%s", snippet)
		}
		if !strings.Contains(snippet, `location ^~ "/mirrors/admin/"`) {
			t.Errorf("nginx snippet missing subpath admin location, got:\n%s", snippet)
		}
		if !strings.Contains(snippet, `location ^~ "/mirrors/debian/"`) {
			t.Errorf("nginx snippet missing subpath debian repository location, got:\n%s", snippet)
		}
	}
}
