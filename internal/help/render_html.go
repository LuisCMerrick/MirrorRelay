package help

import (
	"bytes"
	"fmt"
	"html"
	"sort"
	"strings"

	"github.com/LuisCMerrick/MirrorRelay/internal/model"
)

// Simple and safe Markdown to HTML converter supporting headings, code blocks with copy buttons, lists, links, paragraphs.
func renderMarkdownToHTML(md string) string {
	var buf bytes.Buffer
	lines := strings.Split(md, "\n")
	inCodeBlock := false
	var codeBuf bytes.Buffer

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "```") {
			if inCodeBlock {
				// End code block
				rawCode := codeBuf.String()
				buf.WriteString("<div class=\"code-block-wrapper\">")
				buf.WriteString("<button class=\"copy-btn\" onclick=\"copyCode(this)\" title=\"Copy to clipboard\">Copy</button>")
				buf.WriteString("<pre><code>")
				buf.WriteString(html.EscapeString(strings.TrimRight(rawCode, "\n")))
				buf.WriteString("</code></pre></div>\n")
				codeBuf.Reset()
				inCodeBlock = false
			} else {
				// Start code block
				inCodeBlock = true
				codeBuf.Reset()
			}
			continue
		}

		if inCodeBlock {
			codeBuf.WriteString(line)
			codeBuf.WriteString("\n")
			continue
		}

		if strings.HasPrefix(trimmed, "### ") {
			title := strings.TrimPrefix(trimmed, "### ")
			buf.WriteString(fmt.Sprintf("<h3>%s</h3>\n", html.EscapeString(title)))
		} else if strings.HasPrefix(trimmed, "## ") {
			title := strings.TrimPrefix(trimmed, "## ")
			buf.WriteString(fmt.Sprintf("<h2>%s</h2>\n", html.EscapeString(title)))
		} else if strings.HasPrefix(trimmed, "# ") {
			title := strings.TrimPrefix(trimmed, "# ")
			buf.WriteString(fmt.Sprintf("<h1>%s</h1>\n", html.EscapeString(title)))
		} else if strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ") {
			item := strings.TrimPrefix(strings.TrimPrefix(trimmed, "- "), "* ")
			buf.WriteString(fmt.Sprintf("<li>%s</li>\n", renderInlineMarkdown(item)))
		} else if trimmed == "" {
			buf.WriteString("\n")
		} else {
			buf.WriteString(fmt.Sprintf("<p>%s</p>\n", renderInlineMarkdown(trimmed)))
		}
	}

	if inCodeBlock {
		rawCode := codeBuf.String()
		buf.WriteString("<div class=\"code-block-wrapper\"><pre><code>")
		buf.WriteString(html.EscapeString(strings.TrimRight(rawCode, "\n")))
		buf.WriteString("</code></pre></div>\n")
	}

	return buf.String()
}

func renderInlineMarkdown(s string) string {
	escaped := html.EscapeString(s)
	// Convert `code` to <code>code</code>
	parts := strings.Split(escaped, "`")
	if len(parts) >= 3 && len(parts)%2 == 1 {
		var out strings.Builder
		for i, part := range parts {
			if i%2 == 1 {
				out.WriteString("<code>")
				out.WriteString(part)
				out.WriteString("</code>")
			} else {
				out.WriteString(part)
			}
		}
		escaped = out.String()
	}
	return escaped
}

// RenderOverviewHTML generates the complete HTML page for /help/ listing all repos with help enabled.
func RenderOverviewHTML(repos []model.Mirror, publicBaseURL string, branding model.BrandingConfig, theme, accentColor string) string {
	title := branding.Title
	if title == "" {
		title = "MirrorRelay"
	}
	if accentColor == "" {
		accentColor = "#2563eb"
	}
	brandMarkup := html.EscapeString(title)
	if branding.Logo != "" {
		brandMarkup = fmt.Sprintf(`<span class="brand-heading"><img src="%s" alt=""><span>%s</span></span>`, html.EscapeString(branding.Logo), html.EscapeString(title))
	}
	faviconLink := ""
	if branding.Favicon != "" {
		faviconLink = fmt.Sprintf(`<link rel="icon" href="%s">`, html.EscapeString(branding.Favicon))
	}

	// Filter only repositories that have help enabled and valid template
	var available []model.Mirror
	for _, m := range repos {
		if m.Enabled && m.Help.Enabled && m.Help.Template != "" {
			available = append(available, m)
		}
	}
	sort.Slice(available, func(i, j int) bool {
		return strings.ToLower(available[i].Name) < strings.ToLower(available[j].Name)
	})

	var rows strings.Builder
	for _, m := range available {
		summary := m.Help.Summary
		if summary == "" {
			summary = m.Description
		}
		helpHref := "/help/" + m.Slug + "/"
		browseHref := m.PublicPath
		if m.PublicMode == "host" {
			browseHref = "https://" + m.PublicHost + "/"
		} else if browseHref == "" {
			browseHref = "/" + m.Slug + "/"
		}

		fmt.Fprintf(&rows, `<tr data-type="%s">
			<td><a href="%s" class="repo-name"><strong>%s</strong></a></td>
			<td><span class="type-badge">%s</span></td>
			<td>%s</td>
			<td class="action-cell">
				<a href="%s" class="btn btn-sm btn-primary">Help / 说明</a>
				<a href="%s" class="btn btn-sm btn-secondary">Browse / 浏览</a>
			</td>
		</tr>`,
			html.EscapeString(m.Type),
			html.EscapeString(helpHref), html.EscapeString(m.Name),
			html.EscapeString(m.Type),
			html.EscapeString(summary),
			html.EscapeString(helpHref),
			html.EscapeString(browseHref),
		)
	}

	emptyState := ""
	if len(available) == 0 {
		emptyState = `<div class="empty-state">
			<p>No repository help is currently available. / 当前暂无仓库使用帮助。</p>
		</div>`
	}

	return fmt.Sprintf(`<!doctype html>
<html lang="en" data-theme="%s">
<head>
	<meta charset="utf-8">
	<meta name="viewport" content="width=device-width,initial-scale=1">
	<title>Repository Help / 仓库使用帮助 - %s</title>
	%s
	<style>
		:root {
			--mr-primary: %s;
			--mr-primary-hover: %s;
			--mr-bg: #f8fafc;
			--mr-surface: #ffffff;
			--mr-text: #0f172a;
			--mr-muted: #64748b;
			--mr-border: #e2e8f0;
			--mr-radius: 8px;
		}
		[data-theme="dark"] {
			--mr-bg: #0f172a;
			--mr-surface: #1e293b;
			--mr-text: #f8fafc;
			--mr-muted: #94a3b8;
			--mr-border: #334155;
		}
		@media (prefers-color-scheme: dark) {
			[data-theme="system"] {
				--mr-bg: #0f172a;
				--mr-surface: #1e293b;
				--mr-text: #f8fafc;
				--mr-muted: #94a3b8;
				--mr-border: #334155;
			}
		}
		body {
			font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif;
			background: var(--mr-bg);
			color: var(--mr-text);
			margin: 0;
			padding: 0;
			line-height: 1.5;
		}
		.container {
			max-width: 1080px;
			margin: 2rem auto;
			padding: 0 1.25rem;
		}
		header {
			display: flex;
			align-items: center;
			justify-content: space-between;
			margin-bottom: 2rem;
			padding-bottom: 1rem;
			border-bottom: 1px solid var(--mr-border);
		}
		.header-title {
			display: flex;
			align-items: center;
			gap: 0.75rem;
		}
		.header-title h1 {
			margin: 0;
			font-size: 1.5rem;
		}
		.brand-heading {
			display: inline-flex;
			align-items: center;
			gap: 0.65rem;
		}
		.brand-heading img {
			width: 36px;
			height: 36px;
			object-fit: contain;
			border-radius: var(--mr-radius);
		}
		.search-box {
			width: 100%%;
			max-width: 320px;
			padding: 0.6rem 0.9rem;
			border: 1px solid var(--mr-border);
			border-radius: var(--mr-radius);
			background: var(--mr-surface);
			color: var(--mr-text);
			font-size: 0.95rem;
		}
		.search-box:focus {
			outline: 2px solid var(--mr-primary);
			outline-offset: 2px;
		}
		.table-card {
			background: var(--mr-surface);
			border: 1px solid var(--mr-border);
			border-radius: var(--mr-radius);
			overflow-x: auto;
			-webkit-overflow-scrolling: touch;
			box-shadow: 0 1px 3px rgba(0,0,0,0.05);
		}
		table {
			width: 100%%;
			min-width: 760px;
			border-collapse: collapse;
			text-align: left;
		}
		th, td {
			padding: 0.85rem 1.25rem;
			border-bottom: 1px solid var(--mr-border);
		}
		th {
			background: rgba(0,0,0,0.02);
			font-size: 0.85rem;
			text-transform: uppercase;
			color: var(--mr-muted);
			letter-spacing: 0.05em;
		}
		tr:last-child td {
			border-bottom: none;
		}
		.repo-name {
			color: var(--mr-primary);
			text-decoration: none;
			font-size: 1.05rem;
		}
		.repo-name:hover {
			text-decoration: underline;
		}
		.type-badge {
			display: inline-block;
			padding: 0.2rem 0.5rem;
			font-size: 0.75rem;
			font-weight: 600;
			text-transform: uppercase;
			border-radius: 4px;
			background: rgba(37,99,235,0.1);
			color: var(--mr-primary);
		}
		.action-cell {
			text-align: right;
			white-space: nowrap;
		}
		.btn {
			display: inline-flex;
			align-items: center;
			min-height: 44px;
			padding: 0.4rem 0.8rem;
			font-size: 0.85rem;
			font-weight: 500;
			text-decoration: none;
			border-radius: var(--mr-radius);
			cursor: pointer;
			border: none;
		}
		.btn-primary {
			background: var(--mr-primary);
			color: #fff;
		}
		.btn-primary:hover {
			background: var(--mr-primary-hover);
		}
		.btn-secondary {
			background: rgba(0,0,0,0.05);
			color: var(--mr-text);
			margin-left: 0.5rem;
		}
		.btn-secondary:hover {
			background: rgba(0,0,0,0.1);
		}
		.empty-state {
			padding: 3rem;
			text-align: center;
			color: var(--mr-muted);
		}
		footer {
			margin-top: 3rem;
			text-align: center;
			font-size: 0.85rem;
			color: var(--mr-muted);
		}
		footer a {
			color: var(--mr-muted);
			text-decoration: none;
		}
		a:focus-visible,
		input:focus-visible {
			outline: 3px solid var(--mr-primary);
			outline-offset: 2px;
		}
		@media (max-width: 700px) {
			.container { margin: 1rem auto; padding: 0 0.75rem; }
			header { align-items: stretch; flex-direction: column; }
			.search-box { max-width: none; min-height: 44px; box-sizing: border-box; }
		}
		@media (prefers-reduced-motion: reduce) {
			*, *::before, *::after { transition-duration: 0.01ms !important; }
		}
	</style>
</head>
<body data-app="mirrorrelay">
	<div class="container">
		<header data-ui="header">
			<div class="header-title">
				<a href="/" style="color:inherit;text-decoration:none;"><h1>%s</h1></a>
				<span style="color:var(--mr-muted);">/ 仓库使用帮助 (Help)</span>
			</div>
			<input type="search" id="searchInput" class="search-box" placeholder="Search repositories / 搜索仓库..." aria-label="Search repositories / 搜索仓库" oninput="filterRepos()">
		</header>
		<main data-ui="content">
			<div class="table-card">
				<table id="repoTable">
					<thead>
						<tr>
							<th>Repository / 仓库</th>
							<th>Type / 类型</th>
							<th>Summary / 说明</th>
							<th style="text-align:right;">Actions / 操作</th>
						</tr>
					</thead>
					<tbody>
						%s
					</tbody>
				</table>
				%s
			</div>
		</main>
		<footer>
			<p><a href="/">← Return to Repository Index / 返回镜像列表</a> &bull; Powered by <a href="https://github.com/LuisCMerrick/MirrorRelay" target="_blank" rel="noopener noreferrer">%s</a></p>
		</footer>
	</div>
	<script>
		function filterRepos() {
			var input = document.getElementById('searchInput').value.toLowerCase();
			var rows = document.querySelectorAll('#repoTable tbody tr');
			rows.forEach(function(row) {
				var text = row.innerText.toLowerCase();
				row.style.display = text.indexOf(input) > -1 ? '' : 'none';
			});
		}
	</script>
</body>
</html>`,
		html.EscapeString(theme),
		html.EscapeString(title),
		faviconLink,
		html.EscapeString(accentColor),
		html.EscapeString(accentColor),
		brandMarkup,
		rows.String(),
		emptyState,
		html.EscapeString(title),
	)
}

// RenderDetailHTML generates the interactive repository help page with selectors and copy buttons.
func RenderDetailHTML(res *RenderResult, branding model.BrandingConfig, theme, accentColor string, safeUI bool) string {
	title := branding.Title
	if title == "" {
		title = "MirrorRelay"
	}
	if accentColor == "" {
		accentColor = "#2563eb"
	}
	brandMarkup := html.EscapeString(title)
	if branding.Logo != "" {
		brandMarkup = fmt.Sprintf(`<span class="brand-heading"><img src="%s" alt=""><span>%s</span></span>`, html.EscapeString(branding.Logo), html.EscapeString(title))
	}
	faviconLink := ""
	if branding.Favicon != "" {
		faviconLink = fmt.Sprintf(`<link rel="icon" href="%s">`, html.EscapeString(branding.Favicon))
	}

	var variantOptions strings.Builder
	for _, v := range res.Variants {
		selected := ""
		if v.Key == res.SelectedVariant {
			selected = "selected"
		}
		label := v.Label
		if label == "" {
			label = v.Key
		}
		fmt.Fprintf(&variantOptions, `<option value="%s" %s>%s</option>`, html.EscapeString(v.Key), selected, html.EscapeString(label))
	}

	var formatOptions strings.Builder
	for _, f := range res.Formats {
		selected := ""
		if f.Key == res.SelectedFormat {
			selected = "selected"
		}
		label := f.Label
		if label == "" {
			label = f.Key
		}
		fmt.Fprintf(&formatOptions, `<option value="%s" %s>%s</option>`, html.EscapeString(f.Key), selected, html.EscapeString(label))
	}

	customCSSLink := ""
	if !safeUI {
		customCSSLink = `<link rel="stylesheet" href="/ui/custom.css">`
	}

	return fmt.Sprintf(`<!doctype html>
<html lang="en" data-theme="%s">
<head>
	<meta charset="utf-8">
	<meta name="viewport" content="width=device-width,initial-scale=1">
	<title>%s 使用帮助 - %s</title>
	%s
	%s
	<style>
		:root {
			--mr-primary: %s;
			--mr-primary-hover: %s;
			--mr-bg: #f8fafc;
			--mr-surface: #ffffff;
			--mr-text: #0f172a;
			--mr-muted: #64748b;
			--mr-border: #e2e8f0;
			--mr-radius: 8px;
		}
		[data-theme="dark"] {
			--mr-bg: #0f172a;
			--mr-surface: #1e293b;
			--mr-text: #f8fafc;
			--mr-muted: #94a3b8;
			--mr-border: #334155;
		}
		@media (prefers-color-scheme: dark) {
			[data-theme="system"] {
				--mr-bg: #0f172a;
				--mr-surface: #1e293b;
				--mr-text: #f8fafc;
				--mr-muted: #94a3b8;
				--mr-border: #334155;
			}
		}
		body {
			font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif;
			background: var(--mr-bg);
			color: var(--mr-text);
			margin: 0;
			padding: 0;
			line-height: 1.6;
		}
		.container {
			max-width: 960px;
			margin: 2rem auto;
			padding: 0 1.25rem;
		}
		header {
			display: flex;
			align-items: center;
			justify-content: space-between;
			margin-bottom: 2rem;
			padding-bottom: 1rem;
			border-bottom: 1px solid var(--mr-border);
		}
		.header-title {
			display: flex;
			align-items: center;
			gap: 0.75rem;
		}
		.header-title h1 {
			margin: 0;
			font-size: 1.5rem;
		}
		.brand-heading {
			display: inline-flex;
			align-items: center;
			gap: 0.65rem;
		}
		.brand-heading img {
			width: 36px;
			height: 36px;
			object-fit: contain;
			border-radius: var(--mr-radius);
		}
		.breadcrumb {
			font-size: 0.9rem;
			color: var(--mr-muted);
			margin-bottom: 1.5rem;
		}
		.breadcrumb a {
			color: var(--mr-primary);
			text-decoration: none;
		}
		.selectors-card {
			background: var(--mr-surface);
			border: 1px solid var(--mr-border);
			border-radius: var(--mr-radius);
			padding: 1.25rem;
			margin-bottom: 2rem;
			display: flex;
			flex-wrap: wrap;
			gap: 1.5rem;
			align-items: center;
		}
		.selector-group {
			display: flex;
			align-items: center;
			gap: 0.5rem;
		}
		.selector-group label {
			font-size: 0.9rem;
			font-weight: 600;
			color: var(--mr-muted);
		}
		select {
			min-height: 44px;
			padding: 0.4rem 0.8rem;
			border: 1px solid var(--mr-border);
			border-radius: var(--mr-radius);
			background: var(--mr-bg);
			color: var(--mr-text);
			font-size: 0.95rem;
			cursor: pointer;
		}
		.content-card {
			background: var(--mr-surface);
			border: 1px solid var(--mr-border);
			border-radius: var(--mr-radius);
			padding: 2rem;
			box-shadow: 0 1px 3px rgba(0,0,0,0.05);
		}
		.content-card h1, .content-card h2, .content-card h3 {
			margin-top: 1.5rem;
			margin-bottom: 0.75rem;
			color: var(--mr-text);
		}
		.content-card h1:first-child, .content-card h2:first-child {
			margin-top: 0;
		}
		.code-block-wrapper {
			position: relative;
			margin: 1rem 0;
		}
		pre {
			background: #1e293b;
			color: #f8fafc;
			padding: 1rem 1.25rem;
			border-radius: var(--mr-radius);
			overflow-x: auto;
			font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
			font-size: 0.9rem;
			line-height: 1.5;
			margin: 0;
		}
		code {
			font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
			font-size: 0.9em;
			background: rgba(0,0,0,0.05);
			padding: 0.15rem 0.35rem;
			border-radius: 4px;
		}
		pre code {
			background: none;
			padding: 0;
		}
		.copy-btn {
			position: absolute;
			top: 0.5rem;
			right: 0.5rem;
			min-height: 36px;
			padding: 0.25rem 0.6rem;
			font-size: 0.75rem;
			font-weight: 500;
			background: rgba(255,255,255,0.15);
			color: #f8fafc;
			border: 1px solid rgba(255,255,255,0.2);
			border-radius: 4px;
			cursor: pointer;
			transition: background 0.15s;
		}
		.copy-btn:hover {
			background: rgba(255,255,255,0.3);
		}
		.copy-btn.copied {
			background: #16a34a;
			border-color: #16a34a;
		}
		footer {
			margin-top: 3rem;
			text-align: center;
			font-size: 0.85rem;
			color: var(--mr-muted);
		}
		footer a {
			color: var(--mr-muted);
			text-decoration: none;
		}
		a:focus-visible,
		button:focus-visible,
		select:focus-visible {
			outline: 3px solid var(--mr-primary);
			outline-offset: 2px;
		}
		@media (max-width: 700px) {
			.container { margin: 1rem auto; padding: 0 0.75rem; }
			header { align-items: flex-start; flex-direction: column; }
			.content-card { padding: 1rem; }
			.selector-group { align-items: flex-start; flex-direction: column; width: 100%%; }
			.selector-group select { width: 100%%; }
		}
		@media (prefers-reduced-motion: reduce) {
			*, *::before, *::after { transition-duration: 0.01ms !important; }
		}
	</style>
</head>
<body data-app="mirrorrelay">
	<div class="container">
		<nav class="breadcrumb">
			<a href="/">Home</a> &rsaquo; <a href="/help/">Help</a> &rsaquo; %s
		</nav>
		<header data-ui="header">
			<div class="header-title">
				<a href="/" style="color:inherit;text-decoration:none;"><h1>%s</h1></a>
				<span style="color:var(--mr-muted);">/ %s Help</span>
			</div>
			<div>
				<a href="/help/" class="btn btn-secondary">All Help / 全部帮助</a>
			</div>
		</header>

		%s

		<main data-ui="content">
			<section data-ui="repository-help" class="content-card">
				%s
			</section>
		</main>

		<footer>
			<p><a href="/help/">← All Repository Help / 全部仓库帮助</a> &bull; Powered by <a href="https://github.com/LuisCMerrick/MirrorRelay" target="_blank" rel="noopener noreferrer">%s</a></p>
		</footer>
	</div>

	<script>
		function copyCode(btn) {
			var pre = btn.nextElementSibling;
			var code = pre.innerText || pre.textContent;
			var copyPromise;
			if (window.isSecureContext && navigator.clipboard && navigator.clipboard.writeText) {
				copyPromise = navigator.clipboard.writeText(code);
			} else {
				var input = document.createElement('textarea');
				input.value = code;
				input.setAttribute('readonly', '');
				input.style.position = 'fixed';
				input.style.opacity = '0';
				document.body.appendChild(input);
				input.select();
				var copied = document.execCommand('copy');
				input.remove();
				copyPromise = copied ? Promise.resolve() : Promise.reject(new Error('copy unavailable'));
			}
			copyPromise.then(function() {
				var originalText = btn.innerText;
				btn.innerText = "Copied! / 已复制";
				btn.classList.add("copied");
				setTimeout(function() {
					btn.innerText = originalText;
					btn.classList.remove("copied");
				}, 2000);
			}).catch(function() {
				alert("Copy failed, please copy manually.");
			});
		}

		function onSelectorChange() {
			var variantEl = document.getElementById('variantSelect');
			var formatEl = document.getElementById('formatSelect');
			var params = new URLSearchParams(window.location.search);
			if (variantEl) params.set('variant', variantEl.value);
			if (formatEl) params.set('format', formatEl.value);
			window.location.search = params.toString();
		}
	</script>
</body>
</html>`,
		html.EscapeString(theme),
		html.EscapeString(res.RepositoryName),
		html.EscapeString(title),
		faviconLink,
		customCSSLink,
		html.EscapeString(accentColor),
		html.EscapeString(accentColor),
		html.EscapeString(res.RepositoryName),
		brandMarkup,
		html.EscapeString(res.RepositoryName),
		renderSelectorsCard(variantOptions.String(), formatOptions.String()),
		res.HTMLContent,
		html.EscapeString(title),
	)
}

func renderSelectorsCard(variantOptions, formatOptions string) string {
	if variantOptions == "" && formatOptions == "" {
		return ""
	}
	var buf strings.Builder
	buf.WriteString(`<div class="selectors-card">`)
	if variantOptions != "" {
		buf.WriteString(`<div class="selector-group">
			<label for="variantSelect">Version / 版本:</label>
			<select id="variantSelect" onchange="onSelectorChange()">`)
		buf.WriteString(variantOptions)
		buf.WriteString(`</select></div>`)
	}
	if formatOptions != "" {
		buf.WriteString(`<div class="selector-group">
			<label for="formatSelect">Format / 配置格式:</label>
			<select id="formatSelect" onchange="onSelectorChange()">`)
		buf.WriteString(formatOptions)
		buf.WriteString(`</select></div>`)
	}
	buf.WriteString(`</div>`)
	return buf.String()
}
