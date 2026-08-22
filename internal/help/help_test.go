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
	if !strings.Contains(resDeb822.Content, "Signed-By: /usr/share/keyrings/debian-archive-keyring.gpg") {
		t.Errorf("expected Debian archive keyring in deb822 content")
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

func TestRenderHelpAPTFormatsForUbuntuAndDebianSecurity(t *testing.T) {
	tests := []struct {
		name             string
		template         string
		variant          string
		wantSuite        string
		wantComponents   string
		wantKeyring      string
		unwantedFragment string
	}{
		{
			name: "Ubuntu", template: "builtin://help/ubuntu.md", variant: "noble",
			wantSuite:      "noble noble-updates noble-backports noble-security",
			wantComponents: "main restricted universe multiverse",
			wantKeyring:    "/usr/share/keyrings/ubuntu-archive-keyring.gpg",
		},
		{
			name: "Debian Security", template: "builtin://help/debian-security.md", variant: "bookworm-security",
			wantSuite:        "bookworm-security",
			wantComponents:   "main contrib non-free non-free-firmware",
			wantKeyring:      "/usr/share/keyrings/debian-archive-keyring.gpg",
			unwantedFragment: "bookworm-security-security",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repo := model.Mirror{
				Name: test.name, Slug: strings.ToLower(strings.ReplaceAll(test.name, " ", "-")), Type: "apt",
				PublicMode: "path", PublicPath: "/apt/",
				Help: model.HelpConfig{
					Enabled: true, Title: test.name, Template: test.template,
					Variants: []model.HelpVariant{{Key: test.variant, Codename: test.variant, Default: true}},
					Formats:  []model.HelpFormat{{Key: "sources.list", Default: true}, {Key: "deb822"}},
				},
			}
			for _, format := range []string{"sources.list", "deb822"} {
				result, err := Render(repo, "https://mirror.example", test.variant, format, "MirrorRelay")
				if err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(result.Content, test.wantComponents) || !strings.Contains(result.Content, test.wantKeyring) {
					t.Fatalf("%s output missing expected APT fields:\n%s", format, result.Content)
				}
				for _, suite := range strings.Fields(test.wantSuite) {
					if !strings.Contains(result.Content, suite) {
						t.Fatalf("%s output missing suite %q:\n%s", format, suite, result.Content)
					}
				}
				if test.unwantedFragment != "" && strings.Contains(result.Content, test.unwantedFragment) {
					t.Fatalf("%s output contains invalid suite %q", format, test.unwantedFragment)
				}
			}
		})
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
