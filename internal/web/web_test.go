package web

import (
	"errors"
	"io/fs"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func TestEmbeddedUIHasEnglishDefaultAndAutomaticManualLanguageSelection(t *testing.T) {
	assets := FS()
	if _, err := fs.Stat(assets, "app.js"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("unused legacy UI bundle is still embedded: %v", err)
	}
	index, err := fs.ReadFile(assets, "index.html")
	if err != nil {
		t.Fatal(err)
	}
	mainScript, err := fs.ReadFile(assets, "js/main.js")
	if err != nil {
		t.Fatal(err)
	}
	themeBootstrap, err := fs.ReadFile(assets, "js/theme-bootstrap.js")
	if err != nil {
		t.Fatal(err)
	}
	themeScript, err := fs.ReadFile(assets, "js/theme.js")
	if err != nil {
		t.Fatal(err)
	}
	settingsScript, err := fs.ReadFile(assets, "js/pages/settings.js")
	if err != nil {
		t.Fatal(err)
	}
	settingsSchema, err := fs.ReadFile(assets, "js/settings-schema.js")
	if err != nil {
		t.Fatal(err)
	}
	usersScript, err := fs.ReadFile(assets, "js/pages/users.js")
	if err != nil {
		t.Fatal(err)
	}
	mirrorDetailScript, err := fs.ReadFile(assets, "js/pages/mirrorDetail.js")
	if err != nil {
		t.Fatal(err)
	}
	style, err := fs.ReadFile(assets, "app.css")
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
			`href="app.css"`, `src="js/main.js"`, `src="js/theme-bootstrap.js"`,
			`data-theme-mode="light"`, `data-theme-mode="dark"`, `data-theme-mode="auto"`,
			`id="login-password-confirmation"`, `id="login-mode-title"`, `class="login-github-link login-github-corner"`,
		}},
		"localeEn": {content: string(localeEn), expected: []string{
			"export default", "Linux repository reverse-proxy gateway",
			"Local endpoints and ingress", "Enable frontend Unix socket", "Frontend listen IP",
		}},
		"localeZh": {content: string(localeZh), expected: []string{
			"export default", "Linux 软件仓库反向代理网关",
			"本地端点与入口", "启用前端 Unix Socket", "前端监听 IP",
		}},
		"mainScript": {content: string(mainScript), expected: []string{
			"import", "boot()", "applyLanguage", "triggerRestart", "initThemeControls", "/auth/bootstrap",
		}},
		"themeBootstrap": {content: string(themeBootstrap), expected: []string{
			"mirrorrelay.theme", "prefers-color-scheme: dark", "dataset.theme",
		}},
		"themeScript": {content: string(themeScript), expected: []string{
			"applyInstanceTheme", "aria-pressed", "addEventListener('change'",
		}},
		"style": {content: string(style), expected: []string{
			`:root[data-theme="light"]`, ".theme-switch", "--login-card-bg", ".login-github-link:focus-visible", ".login-github-corner",
		}},
		"settingsScript": {content: string(settingsScript), expected: []string{
			"One active destination", "Running configuration", "oapi.dingtalk.com",
			"open.feishu.cn", "qyapi.weixin.qq.com", "hooks.slack.com", "Custom JSON webhook", "data-settings-group",
		}},
		"settingsSchema": {content: string(settingsSchema), expected: []string{
			`"id": "passkey"`, `"title": "Passkey authentication"`, `"path": "admin.passkey.enabled"`,
			`"path": "admin.passkey.rp_name"`, `"path": "admin.passkey.rp_id"`, `"path": "admin.passkey.origins"`,
		}},
		"usersScript": {content: string(usersScript), expected: []string{
			"configure-passkey-btn", "Configure Passkey authentication", "state.settingsSection = 'passkey'",
		}},
		"mirrorDetailScript": {content: string(mirrorDetailScript), expected: []string{
			"DEB822 (.sources)", "sources.list one-line format", "playground-format",
			"debian-security-12", "ubuntu-archive-keyring.gpg",
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
	formEnd := strings.Index(string(index), "</form>")
	githubLink := strings.Index(string(index), `class="login-github-link login-github-corner"`)
	if formEnd < 0 || githubLink < formEnd {
		t.Fatal("login GitHub link must be outside the authentication form")
	}
	if strings.Contains(string(index), "login-footer") || strings.Contains(string(style), ".login-footer") {
		t.Fatal("login GitHub link still uses the below-form footer layout")
	}
	for _, assetContent := range []string{string(index), string(mainScript), string(themeBootstrap), string(themeScript), string(settingsScript), string(settingsSchema), string(usersScript), string(mirrorDetailScript), string(localeEn), string(localeZh)} {
		if strings.Contains(assetContent, "onclick=") {
			t.Fatal("strict-CSP Web UI contains an inline click handler")
		}
	}
	for _, assetContent := range []string{string(index), string(mainScript), string(themeBootstrap), string(themeScript), string(settingsScript), string(usersScript), string(mirrorDetailScript), string(localeEn), string(localeZh)} {
		if strings.Contains(assetContent, "/admin/") || strings.Contains(assetContent, "fetch('/api/v1") {
			t.Fatal("embedded assets contain a fixed administration path")
		}
	}
}

func TestEmbeddedUIUsesProgressiveDisclosureForTechnicalAndMaintenanceDetails(t *testing.T) {
	assets := FS()
	read := func(name string) string {
		t.Helper()
		value, err := fs.ReadFile(assets, name)
		if err != nil {
			t.Fatal(err)
		}
		return string(value)
	}

	style := read("app.css")
	upstream := read("js/pages/upstreamNginx.js")
	pages := map[string]struct {
		asset    string
		required []string
	}{
		"dashboard":  {"js/pages/dashboard.js", []string{"disclosure(L('Request path')", "compact-cards"}},
		"system":     {"js/pages/system.js", []string{"disclosure-stack", "Managed Upstream Nginx lifecycle"}},
		"health":     {"js/pages/health.js", []string{"Component and endpoint details", "compact-cards"}},
		"cluster":    {"js/pages/cluster.js", []string{"Cluster configuration details", "compact-cards"}},
		"cache":      {"js/pages/cache.js", []string{`<details class="disclosure-panel">`, "Targeted Cache Object Invalidation"}},
		"ingress":    {"js/pages/ingress.js", []string{"ingress-snippet-disclosure", "Hidden by default. Expand to inspect and copy."}},
		"settings":   {"js/pages/settings.js", []string{"settings-section", "const open = index === 0"}},
		"appearance": {"js/pages/appearance.js", []string{"settings-section", "disclosure-chevron"}},
	}

	for _, expected := range []string{
		".disclosure-panel", ".disclosure-panel[open]", "> summary:focus-visible",
		"@media (prefers-reduced-motion: reduce)", ".cards.compact-cards",
	} {
		if !strings.Contains(style, expected) {
			t.Fatalf("shared UI style missing %q", expected)
		}
	}

	for name, page := range pages {
		t.Run(name, func(t *testing.T) {
			content := read(page.asset)
			for _, expected := range page.required {
				if !strings.Contains(content, expected) {
					t.Fatalf("%s missing %q", page.asset, expected)
				}
			}
		})
	}

	for _, expected := range []string{
		"upstream-technical-details", "Managed Upstream Nginx technical details",
		"upstream-config-disclosure", "copy-upstream-config", "Configuration copied.",
		"state.role === 'admin' || state.role === 'operator'",
	} {
		if !strings.Contains(upstream, expected) {
			t.Fatalf("Managed Upstream Nginx page missing %q", expected)
		}
	}
	if strings.Contains(upstream, "card(L('Build ID')") || strings.Contains(upstream, "card('PID'") {
		t.Fatal("Managed Upstream Nginx first-level summary exposes secondary binary/process details")
	}
	toggle := strings.Index(upstream, "configDisclosure?.addEventListener('toggle'")
	configFetch := strings.Index(upstream, "api('/upstream-nginx/config')")
	if toggle < 0 || configFetch < toggle {
		t.Fatal("effective Managed Upstream Nginx configuration is not lazily fetched after disclosure")
	}
}

func TestEmbeddedClientConfigurationNeverDisablesTLSVerification(t *testing.T) {
	value, err := fs.ReadFile(FS(), "js/pages/mirrorDetail.js")
	if err != nil {
		t.Fatal(err)
	}
	content := string(value)
	for _, prohibited := range []string{"trusted-host", "strict-ssl=false", "insecure-registries"} {
		if strings.Contains(content, prohibited) {
			t.Fatalf("client configuration generator contains TLS bypass %q", prohibited)
		}
	}
}

func TestEmbeddedUIKeepsRoleRestrictedControlsOutOfLowerPrivilegeViews(t *testing.T) {
	assets := FS()
	read := func(name string) string {
		t.Helper()
		value, err := fs.ReadFile(assets, name)
		if err != nil {
			t.Fatal(err)
		}
		return string(value)
	}

	for name, expected := range map[string][]string{
		"index.html":               {"requires-admin", "requires-operator", `id="user-role"`, `data-page="ingress" class="requires-admin"`},
		"app.css":                  {`:root[data-role="viewer"] .requires-operator`, `:root:not([data-role="admin"]) .requires-admin`},
		"js/main.js":               {"document.documentElement.dataset.role", "refreshRoleUI()"},
		"js/pages/mirrors.js":      {`class="requires-operator" data-action="check-mirror"`, `class="requires-admin" data-action="preview-repository-config"`},
		"js/pages/mirrorDetail.js": {`class="requires-admin" data-action="view-effective-config"`},
		"js/pages/cluster.js":      {"const canOperate = state.role === 'admin' || state.role === 'operator'", "const canManageNodes = state.role === 'admin'"},
		"js/pages/custom.js":       {`class="requires-admin" data-action="edit-custom"`},
		"js/pages/cache.js":        {"const canManage = state.role === 'admin' || state.role === 'operator'"},
		"js/pages/system.js":       {`class="btn-restart-inline requires-admin"`},
	} {
		content := read(name)
		for _, value := range expected {
			if !strings.Contains(content, value) {
				t.Fatalf("%s missing role-aware UI marker %q", name, value)
			}
		}
	}
}

func TestEmbeddedUILocaleResourcesCoverStaticReferences(t *testing.T) {
	assets := FS()
	read := func(name string) string {
		t.Helper()
		value, err := fs.ReadFile(assets, name)
		if err != nil {
			t.Fatal(err)
		}
		return strings.ReplaceAll(string(value), "\r\n", "\n")
	}
	sectionKeys := func(content, section, nextSection string) map[string]struct{} {
		t.Helper()
		startMarker := "\n  " + section + ": {"
		start := strings.Index(content, startMarker)
		if start < 0 {
			t.Fatalf("locale is missing %s section", section)
		}
		start += len(startMarker)
		endMarker := "\n}\n};"
		if nextSection != "" {
			endMarker = "\n  " + nextSection + ":"
		}
		end := strings.Index(content[start:], endMarker)
		if end < 0 {
			t.Fatalf("locale %s section has no terminator", section)
		}
		keys := make(map[string]struct{})
		keyPattern := regexp.MustCompile(`(?m)^    ("(?:\\.|[^"\\])*"):`)
		for _, match := range keyPattern.FindAllStringSubmatch(content[start:start+end], -1) {
			key, err := strconv.Unquote(match[1])
			if err != nil {
				t.Fatalf("decode locale key %s: %v", match[1], err)
			}
			if _, exists := keys[key]; exists {
				t.Fatalf("locale %s section contains duplicate key %q", section, key)
			}
			keys[key] = struct{}{}
		}
		return keys
	}

	en := read("locales/en.js")
	zh := read("locales/zh.js")
	enDictionary := sectionKeys(en, "dictionary", "pageMeta")
	zhDictionary := sectionKeys(zh, "dictionary", "pageMeta")
	enStrings := sectionKeys(en, "strings", "")
	zhStrings := sectionKeys(zh, "strings", "")
	requireParity := func(name string, left, right map[string]struct{}) {
		t.Helper()
		for key := range left {
			if _, exists := right[key]; !exists {
				t.Errorf("%s is missing locale key %q", name, key)
			}
		}
	}
	requireParity("Chinese dictionary", enDictionary, zhDictionary)
	requireParity("English dictionary", zhDictionary, enDictionary)
	requireParity("Chinese strings", enStrings, zhStrings)
	requireParity("English strings", zhStrings, enStrings)

	staticPattern := regexp.MustCompile(`data-i18n="([^"]+)"`)
	singleQuotePattern := regexp.MustCompile(`\bL\('([^']+)'`)
	doubleQuotePattern := regexp.MustCompile(`\bL\("([^"]+)"`)
	checkReferences := func(name, content string) {
		t.Helper()
		for _, match := range staticPattern.FindAllStringSubmatch(content, -1) {
			if _, exists := enDictionary[match[1]]; !exists {
				t.Errorf("%s references untranslated data-i18n key %q", name, match[1])
			}
		}
		for _, pattern := range []*regexp.Regexp{singleQuotePattern, doubleQuotePattern} {
			for _, match := range pattern.FindAllStringSubmatch(content, -1) {
				if _, exists := enStrings[match[1]]; !exists {
					t.Errorf("%s references untranslated L key %q", name, match[1])
				}
			}
		}
	}
	checkReferences("index.html", read("index.html"))
	if err := fs.WalkDir(assets, "js", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && strings.HasSuffix(path, ".js") {
			checkReferences(path, read(path))
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
