package web

import (
	"io/fs"
	"strings"
	"testing"
)

func TestEmbeddedUIHasEnglishDefaultAndAutomaticManualLanguageSelection(t *testing.T) {
	assets := FS()
	index, err := fs.ReadFile(assets, "index.html")
	if err != nil {
		t.Fatal(err)
	}
	script, err := fs.ReadFile(assets, "app.js")
	if err != nil {
		t.Fatal(err)
	}
	for name, test := range map[string]struct {
		content  string
		expected []string
	}{
		"index": {content: string(index), expected: []string{`<html lang="en">`, `data-lang="en"`, `data-lang="zh"`, `data-page="settings"`, `id="html-rewrite-enabled"`, `href="app.css"`, `src="app.js"`}},
		"script": {content: string(script), expected: []string{
			"navigator.languages", "repogate.language", "Linux 软件仓库反向代理网关",
			"fetch('api/v1' + path, request)",
			"const repositories = repositoryValues || [];", "mirrors = (await api('/mirrors')) || [];",
			`data-action="show-repository"`, `data-action="copy-repository-url"`, `data-action="check-mirror"`,
			`data-action="preview-repository-config"`, `data-action="purge-repository"`, `data-action="edit-mirror"`,
			`data-action="toggle-mirror"`, `data-action="delete-mirror"`,
			"html_rewrite_enabled: $('#html-rewrite-enabled').checked",
			"async function loadSettings()", "document.addEventListener('click'",
		}},
	} {
		t.Run(name, func(t *testing.T) {
			for _, value := range test.expected {
				if !strings.Contains(test.content, value) {
					t.Fatalf("embedded asset missing %q", value)
				}
			}
		})
	}
	if strings.Contains(string(index), "onclick=") || strings.Contains(string(script), "onclick=") {
		t.Fatal("strict-CSP Web UI contains an inline click handler")
	}
	if strings.Contains(string(index), "/admin/") || strings.Contains(string(script), "fetch('/api/v1") {
		t.Fatal("embedded assets contain a fixed administration path")
	}
}
