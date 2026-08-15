package mirror

import (
	"context"
	"testing"
	"time"

	"github.com/LuisCMerrick/RepoGate/internal/model"
)

type fakeLoader struct{ values []model.Mirror }

func (f fakeLoader) ListMirrors(context.Context) ([]model.Mirror, error) { return f.values, nil }

func TestRegistryResolveExactSegment(t *testing.T) {
	r := NewRegistry(fakeLoader{[]model.Mirror{{ID: 1, Name: "Deb", Slug: "deb", Enabled: true}, {ID: 2, Name: "Debian", Slug: "debian", Enabled: true}}})
	if err := r.Reload(context.Background()); err != nil {
		t.Fatal(err)
	}
	m, path, ok := r.Resolve("/debian/dists/stable/Release")
	if !ok || m.ID != 2 || path != "/dists/stable/Release" {
		t.Fatalf("unexpected resolution: %#v %q %v", m, path, ok)
	}
	if _, _, ok := r.Resolve("/debian-extra/file"); ok {
		t.Fatal("prefix match must not be accepted")
	}
}

func TestNormalizeAndValidate(t *testing.T) {
	m := model.Mirror{Name: " Debian ", Slug: "/Debian/", Upstreams: []model.Upstream{{URL: "https://deb.debian.org/debian", Enabled: true}}}
	if err := NormalizeAndValidate(&m, false, false); err != nil {
		t.Fatal(err)
	}
	if m.Slug != "debian" || m.Upstreams[0].URL != "https://deb.debian.org/debian/" {
		t.Fatalf("not normalized: %#v", m)
	}
	m.Slug = "admin"
	if err := NormalizeAndValidate(&m, false, false); err == nil {
		t.Fatal("reserved slug accepted")
	}
}

func TestNormalizeAndValidatePreservesEscapedUpstreamPath(t *testing.T) {
	m := model.Mirror{Name: "Escaped", Slug: "escaped", Upstreams: []model.Upstream{{URL: "https://repo.example/base%20directory", Enabled: true}}}
	if err := NormalizeAndValidate(&m, false, false); err != nil {
		t.Fatal(err)
	}
	if m.Upstreams[0].URL != "https://repo.example/base%20directory/" {
		t.Fatalf("escaped upstream path changed: %q", m.Upstreams[0].URL)
	}
}

func TestNormalizeAndValidateRejectsGeneratedConfigInjection(t *testing.T) {
	base := model.Mirror{Name: "Safe", Slug: "safe", PublicMode: "host", PublicHost: "repo.example", Upstreams: []model.Upstream{{URL: "https://repo.example/", Enabled: true}}}
	for name, mutate := range map[string]func(*model.Mirror){
		"name newline":           func(m *model.Mirror) { m.Name = "safe\nreturn 200" },
		"public host directive":  func(m *model.Mirror) { m.PublicHost = "repo.example;return" },
		"public path newline":    func(m *model.Mirror) { m.PublicMode, m.PublicPath = "path", "/safe/\nreturn" },
		"public path root":       func(m *model.Mirror) { m.PublicMode, m.PublicPath = "path", "/" },
		"public path traversal":  func(m *model.Mirror) { m.PublicMode, m.PublicPath = "path", "/safe/../other/" },
		"public path escape":     func(m *model.Mirror) { m.PublicMode, m.PublicPath = "path", "/safe%2fother/" },
		"public auxiliary route": func(m *model.Mirror) { m.PublicMode, m.PublicPath = "path", "/_repogate/upstream/" },
		"host rewrite directive": func(m *model.Mirror) { m.HostRewrite = "repo.example;return" },
		"rewrite host directive": func(m *model.Mirror) { m.RewriteHosts = []string{"cdn.example;return"} },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := base
			mutate(&candidate)
			if err := NormalizeAndValidate(&candidate, false, false); err == nil {
				t.Fatal("unsafe value was accepted")
			}
		})
	}
}

func TestActiveSnapshotIsExplicitAndHealthDoesNotImportDesiredState(t *testing.T) {
	loader := &mutableLoader{values: []model.Mirror{{
		ID: 1, Name: "Active", Slug: "repo", Enabled: true,
		Upstreams: []model.Upstream{{ID: 10, URL: "https://active.example/", Enabled: true}},
	}}}
	registry := NewRegistry(loader)
	if err := registry.Reload(context.Background()); err != nil {
		t.Fatal(err)
	}
	loader.values[0].Name = "Desired"
	loader.values[0].Upstreams[0].URL = "https://desired.example/"
	registry.UpdateUpstreamHealth(10, "healthy", 12, "", time.Now())
	active, found := registry.GetByID(1)
	if !found || active.Name != "Active" || active.Upstreams[0].URL != "https://active.example/" || active.Upstreams[0].HealthStatus != "healthy" {
		t.Fatalf("health update imported pending desired configuration: %+v", active)
	}
	if err := registry.Reload(context.Background()); err != nil {
		t.Fatal(err)
	}
	active, _ = registry.GetByID(1)
	if active.Name != "Desired" || active.Upstreams[0].URL != "https://desired.example/" {
		t.Fatalf("explicit publication did not activate desired state: %+v", active)
	}
}

type mutableLoader struct{ values []model.Mirror }

func (m *mutableLoader) ListMirrors(context.Context) ([]model.Mirror, error) { return m.values, nil }
