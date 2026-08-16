package mirror

import (
	"strings"
	"testing"

	"github.com/LuisCMerrick/MirrorRelay/internal/model"
)

func TestValidateRouteConflicts(t *testing.T) {
	base := []model.Mirror{
		{Slug: "packages", PublicMode: "path", PublicPath: "/packages/"},
		{Slug: "registry", PublicMode: "host", PublicHost: "registry.example.com"},
	}
	if err := ValidateRouteConflicts(base, "/private-console/", "https://mirror.example.com"); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name         string
		repositories []model.Mirror
		want         string
	}{
		{name: "administration path", repositories: appendCopy(base, model.Mirror{Slug: "console", PublicMode: "path", PublicPath: "/private-console/"}), want: "reserved administration"},
		{name: "nested administration path", repositories: appendCopy(base, model.Mirror{Slug: "console-child", PublicMode: "path", PublicPath: "/private-console/assets/"}), want: "reserved administration"},
		{name: "system path", repositories: appendCopy(base, model.Mirror{Slug: "auth", PublicMode: "path", PublicPath: "/_mirror_auth/custom/"}), want: "registry authentication endpoint"},
		{name: "auxiliary resource path", repositories: appendCopy(base, model.Mirror{Slug: "auxiliary", PublicMode: "path", PublicPath: "/_mirrorrelay/upstream/"}), want: "upstream auxiliary resource endpoint"},
		{name: "duplicate path", repositories: appendCopy(base, model.Mirror{Slug: "other", PublicMode: "path", PublicPath: "/packages/"}), want: "overlaps"},
		{name: "nested repository path", repositories: appendCopy(base, model.Mirror{Slug: "nested", PublicMode: "path", PublicPath: "/packages/stable/"}), want: "overlaps"},
		{name: "duplicate host", repositories: appendCopy(base, model.Mirror{Slug: "other", PublicMode: "host", PublicHost: "REGISTRY.EXAMPLE.COM"}), want: "used by both"},
		{name: "shared host", repositories: appendCopy(base, model.Mirror{Slug: "shared", PublicMode: "host", PublicHost: "mirror.example.com"}), want: "http.public_base_url"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateRouteConflicts(test.repositories, "/private-console/", "https://mirror.example.com")
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want substring %q", err, test.want)
			}
		})
	}
}

func appendCopy(values []model.Mirror, value model.Mirror) []model.Mirror {
	result := append([]model.Mirror(nil), values...)
	return append(result, value)
}
