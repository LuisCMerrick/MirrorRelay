package help

import (
	"strings"
	"testing"

	"github.com/LuisCMerrick/MirrorRelay/internal/model"
)

func TestRenderHelpDebian(t *testing.T) {
	repo := model.Mirror{
		Name:       "Debian",
		Slug:       "debian",
		Type:       "apt",
		PublicMode: "path",
		PublicPath: "/debian/",
		Upstreams: []model.Upstream{
			{URL: "https://user:password@deb.debian.org/debian/"},
		},
		Help: model.HelpConfig{
			Enabled:         true,
			Title:           "Debian",
			Summary:         "Debian 软件源使用说明",
			Template:        "builtin://help/debian.md",
			TemplateVersion: 1,
			Variants: []model.HelpVariant{
				{Key: "bookworm", Label: "Debian 12 (bookworm)", Codename: "bookworm", Default: true},
				{Key: "bullseye", Label: "Debian 11 (bullseye)", Codename: "bullseye"},
			},
			Formats: []model.HelpFormat{
				{Key: "sources.list", Label: "传统格式", Default: true},
				{Key: "deb822", Label: "DEB822 格式"},
			},
		},
	}

	// 1. Render default variant and format
	res, err := Render(repo, "https://mirrors.example.com", "", "", "My Mirrors")
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}
	if !strings.Contains(res.Content, "https://mirrors.example.com/debian/") {
		t.Errorf("expected public repository url in content")
	}
	if strings.Contains(res.Content, "password") {
		t.Errorf("upstream password was not scrubbed!")
	}
	if !strings.Contains(res.Content, "bookworm") {
		t.Errorf("expected default variant bookworm in content")
	}

	// 2. Render specific format (deb822)
	resDeb822, err := Render(repo, "https://mirrors.example.com", "bullseye", "deb822", "My Mirrors")
	if err != nil {
		t.Fatalf("Render deb822 failed: %v", err)
	}
	if !strings.Contains(resDeb822.Content, "Suites: bullseye") {
		t.Errorf("expected Suites: bullseye in deb822 content")
	}

	// 3. Render HTML
	htmlOut := RenderDetailHTML(res, model.BrandingConfig{Title: "My Mirrors"}, "dark", false)
	if !strings.Contains(htmlOut, "Debian 使用帮助") {
		t.Errorf("expected title in rendered HTML")
	}
	if !strings.Contains(htmlOut, "data-theme=\"dark\"") {
		t.Errorf("expected dark theme attribute")
	}
}

func TestRenderOverviewHTML(t *testing.T) {
	repos := []model.Mirror{
		{
			Name:    "Debian",
			Slug:    "debian",
			Type:    "apt",
			Enabled: true,
			Help: model.HelpConfig{
				Enabled:  true,
				Template: "builtin://help/debian.md",
				Summary:  "Debian apt source",
			},
		},
		{
			Name:    "Disabled Repo",
			Slug:    "disabled",
			Type:    "generic",
			Enabled: false,
			Help: model.HelpConfig{
				Enabled:  true,
				Template: "builtin://help/debian.md",
			},
		},
		{
			Name:    "No Help Repo",
			Slug:    "no-help",
			Type:    "generic",
			Enabled: true,
			Help: model.HelpConfig{
				Enabled: false,
			},
		},
	}

	htmlOut := RenderOverviewHTML(repos, "https://mirrors.example.com", model.BrandingConfig{Title: "Test Mirrors"}, "system")
	if !strings.Contains(htmlOut, "Debian") {
		t.Errorf("expected Debian in overview HTML")
	}
	if strings.Contains(htmlOut, "Disabled Repo") {
		t.Errorf("disabled repo should not be in overview")
	}
	if strings.Contains(htmlOut, "No Help Repo") {
		t.Errorf("repo without help should not be in overview")
	}
}
