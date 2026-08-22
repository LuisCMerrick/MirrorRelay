// Package help provides built-in repository help documentation and dynamic client configuration rendering.
package help

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/LuisCMerrick/MirrorRelay/internal/model"
)

// RenderResult represents the rendered output of a help document.
type RenderResult struct {
	Title           string              `json:"title"`
	Summary         string              `json:"summary"`
	RepositoryName  string              `json:"repository_name"`
	RepositoryType  string              `json:"repository_type"`
	RepositoryURL   string              `json:"repository_url"`
	PublicBaseURL   string              `json:"public_base_url"`
	SelectedVariant string              `json:"selected_variant,omitempty"`
	SelectedFormat  string              `json:"selected_format,omitempty"`
	Variants        []model.HelpVariant `json:"variants,omitempty"`
	Formats         []model.HelpFormat  `json:"formats,omitempty"`
	Content         string              `json:"content"`
	HTMLContent     string              `json:"html_content"`
}

// SanitizeUpstreamURL removes credentials and queries from the upstream URL for safe documentation display.
func SanitizeUpstreamURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	u.User = nil
	u.RawQuery = ""
	return u.String()
}

// ComputeRepositoryURL returns the clean canonical public repository URL.
func ComputeRepositoryURL(publicBaseURL string, repo model.Mirror) string {
	if repo.PublicMode == "host" {
		host := repo.PublicHost
		if !strings.Contains(host, "://") {
			return "https://" + strings.TrimRight(host, "/") + "/"
		}
		return strings.TrimRight(host, "/") + "/"
	}
	baseURL := strings.TrimRight(publicBaseURL, "/")
	if baseURL == "" {
		baseURL = "http://localhost"
	}
	publicPath := repo.PublicPath
	if publicPath == "" {
		publicPath = "/" + repo.Slug + "/"
	}
	if !strings.HasPrefix(publicPath, "/") {
		publicPath = "/" + publicPath
	}
	if !strings.HasSuffix(publicPath, "/") {
		publicPath = publicPath + "/"
	}
	return baseURL + publicPath
}

// Render renders a repository help template with substituted dynamic variables and markdown formatting.
func Render(repo model.Mirror, publicBaseURL, variantKey, formatKey, instanceName string) (*RenderResult, error) {
	if !repo.Help.Enabled || repo.Help.Template == "" {
		return nil, fmt.Errorf("repository %q does not have help enabled", repo.Slug)
	}

	tmplText, ok := GetTemplate(repo.Help.Template)
	if !ok {
		return nil, fmt.Errorf("help template %q not found", repo.Help.Template)
	}

	repoURL := ComputeRepositoryURL(publicBaseURL, repo)
	parsedRepoURL, _ := url.Parse(repoURL)
	repoHost := ""
	if parsedRepoURL != nil {
		repoHost = parsedRepoURL.Host
	}

	upstreamURL := ""
	if len(repo.Upstreams) > 0 {
		upstreamURL = SanitizeUpstreamURL(repo.Upstreams[0].URL)
	}

	selectedVariant := variantKey
	if selectedVariant == "" && len(repo.Help.Variants) > 0 {
		selectedVariant = repo.Help.Variants[0].Key
		for _, v := range repo.Help.Variants {
			if v.Default {
				selectedVariant = v.Key
				break
			}
		}
	}

	selectedFormat := formatKey
	if selectedFormat == "" && len(repo.Help.Formats) > 0 {
		selectedFormat = repo.Help.Formats[0].Key
		for _, f := range repo.Help.Formats {
			if f.Default {
				selectedFormat = f.Key
				break
			}
		}
	}

	codename := selectedVariant
	version := selectedVariant
	for _, v := range repo.Help.Variants {
		if v.Key == selectedVariant {
			if v.Codename != "" {
				codename = v.Codename
			}
			break
		}
	}

	if instanceName == "" {
		instanceName = "MirrorRelay"
	}

	configBlock := generateConfigBlock(repo, repoURL, selectedVariant, codename, selectedFormat)

	replacements := map[string]string{
		"{{INSTANCE_NAME}}":   instanceName,
		"{{PUBLIC_BASE_URL}}": strings.TrimRight(publicBaseURL, "/"),
		"{{REPOSITORY_NAME}}": repo.Name,
		"{{REPOSITORY_SLUG}}": repo.Slug,
		"{{REPOSITORY_PATH}}": repo.PublicPath,
		"{{REPOSITORY_URL}}":  repoURL,
		"{{REPOSITORY_HOST}}": repoHost,
		"{{REPOSITORY_TYPE}}": repo.Type,
		"{{UPSTREAM_URL}}":    upstreamURL,
		"{{VERSION}}":         version,
		"{{CODENAME}}":        codename,
		"{{CONFIG_BLOCK}}":    configBlock,
	}

	resultText := tmplText
	for k, v := range replacements {
		resultText = strings.ReplaceAll(resultText, k, v)
	}

	htmlContent := renderMarkdownToHTML(resultText)

	return &RenderResult{
		Title:           repo.Help.Title,
		Summary:         repo.Help.Summary,
		RepositoryName:  repo.Name,
		RepositoryType:  repo.Type,
		RepositoryURL:   repoURL,
		PublicBaseURL:   publicBaseURL,
		SelectedVariant: selectedVariant,
		SelectedFormat:  selectedFormat,
		Variants:        repo.Help.Variants,
		Formats:         repo.Help.Formats,
		Content:         resultText,
		HTMLContent:     htmlContent,
	}, nil
}

func generateConfigBlock(repo model.Mirror, repoURL, variant, codename, format string) string {
	switch repo.Type {
	case "apt":
		suites := []string{codename, codename + "-updates", codename + "-backports"}
		components := "main contrib non-free non-free-firmware"
		keyring := "/usr/share/keyrings/debian-archive-keyring.gpg"
		switch repo.Help.Template {
		case "builtin://help/debian-security.md":
			suites = []string{codename}
		case "builtin://help/ubuntu.md":
			suites = []string{codename, codename + "-updates", codename + "-backports", codename + "-security"}
			components = "main restricted universe multiverse"
			keyring = "/usr/share/keyrings/ubuntu-archive-keyring.gpg"
		}
		if format == "deb822" {
			return fmt.Sprintf("```text\n# /etc/apt/sources.list.d/%s.sources\nTypes: deb\nURIs: %s\nSuites: %s\nComponents: %s\nSigned-By: %s\n```",
				repo.Slug, repoURL, strings.Join(suites, " "), components, keyring)
		}
		lines := make([]string, 0, len(suites))
		for _, suite := range suites {
			lines = append(lines, fmt.Sprintf("deb [signed-by=%s] %s %s %s", keyring, repoURL, suite, components))
		}
		return fmt.Sprintf("```text\n# /etc/apt/sources.list.d/%s.list\n%s\n```", repo.Slug, strings.Join(lines, "\n"))
	case "rpm":
		return fmt.Sprintf("```ini\n[%s-baseos]\nname=%s BaseOS\nbaseurl=%s$releasever/BaseOS/$basearch/os/\nenabled=1\ngpgcheck=1\n```",
			repo.Slug, repo.Name, repoURL)
	case "apk":
		return fmt.Sprintf("```text\n# /etc/apk/repositories\n%s%s/main\n%s%s/community\n```",
			repoURL, variant, repoURL, variant)
	default:
		return fmt.Sprintf("```text\n%s\n```", repoURL)
	}
}
