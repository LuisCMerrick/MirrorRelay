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
	mainScript, err := fs.ReadFile(assets, "js/main.js")
	if err != nil {
		t.Fatal(err)
	}
	localeEn, err := fs.ReadFile(assets, "locales/en.js")
	if err != nil {
		t.Fatal(err)
	}
	localeZh, err := fs.ReadFile(assets, "locales/zh.js")
	if err != nil {
		t.Fatal(err)
	}
	for name, test := range map[string]struct {
		content  string
		expected []string
	}{
		"index": {content: string(index), expected: []string{
			`<html lang="en">`, `data-lang="en"`, `data-lang="zh"`, `data-page="settings"`,
			`id="html-rewrite-enabled"`, `id="restart-header"`, `id="restart-sidebar"`,
			`href="app.css"`, `src="js/main.js"`,
		}},
		"localeEn": {content: string(localeEn), expected: []string{
			"export default", "Linux repository reverse-proxy gateway",
			"Local endpoints and ingress", "Frontend Unix socket",
		}},
		"localeZh": {content: string(localeZh), expected: []string{
			"export default", "Linux 软件仓库反向代理网关",
			"本地端点与入口", "前端 Unix Socket",
		}},
		"mainScript": {content: string(mainScript), expected: []string{
			"import", "boot()", "applyLanguage", "triggerRestart",
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
	for _, assetContent := range []string{string(index), string(mainScript), string(localeEn), string(localeZh)} {
		if strings.Contains(assetContent, "onclick=") {
			t.Fatal("strict-CSP Web UI contains an inline click handler")
		}
		if strings.Contains(assetContent, "/admin/") || strings.Contains(assetContent, "fetch('/api/v1") {
			t.Fatal("embedded assets contain a fixed administration path")
		}
	}
}
