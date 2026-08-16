package browser

import (
	"fmt"
	"html"
	"path"
	"strings"

	"github.com/LuisCMerrick/MirrorRelay/internal/model"
)

// RenderHTML generates the unified HTML repository directory browser page.
func RenderHTML(listing *ParsedListing, repo model.Mirror, reqPath string, branding model.BrandingConfig, theme string, safeUI bool) string {
	title := branding.Title
	if title == "" {
		title = "MirrorRelay"
	}

	breadcrumbs := buildBreadcrumbs(reqPath, repo)

	var rows strings.Builder
	for _, entry := range listing.Entries {
		iconSVG := GetIconSVG(entry.IconType)
		href := entry.Href
		name := entry.Name
		if entry.IsParent {
			name = "Parent Directory (..)"
		}

		itemClass := "file-row"
		if entry.IsDir {
			itemClass = "dir-row"
		}

		fmt.Fprintf(&rows, `<tr class="%s" data-name="%s">
			<td class="name-cell">
				<span class="icon">%s</span>
				<a href="%s" class="item-link">%s</a>
			</td>
			<td class="time-cell">%s</td>
			<td class="size-cell">%s</td>
		</tr>`,
			itemClass,
			html.EscapeString(strings.ToLower(name)),
			iconSVG,
			html.EscapeString(href),
			html.EscapeString(name),
			html.EscapeString(entry.ModTime),
			html.EscapeString(entry.Size),
		)
	}

	customCSSLink := ""
	if !safeUI {
		customCSSLink = `<link rel="stylesheet" href="/ui/custom.css">`
	}

	helpButton := ""
	if repo.Help.Enabled {
		helpButton = fmt.Sprintf(`<a href="/help/%s/" class="btn btn-help" title="View configuration help">Help / 说明</a>`, html.EscapeString(repo.Slug))
	}

	return fmt.Sprintf(`<!doctype html>
<html lang="en" data-theme="%s">
<head>
	<meta charset="utf-8">
	<meta name="viewport" content="width=device-width,initial-scale=1">
	<title>Index of %s - %s</title>
	<link rel="stylesheet" href="/ui/base.css">
	%s
	<style>
		:root {
			--mr-primary: #2563eb;
			--mr-primary-hover: #1d4ed8;
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
			--mr-primary: #3b82f6;
			--mr-primary-hover: #60a5fa;
		}
		@media (prefers-color-scheme: dark) {
			[data-theme="system"] {
				--mr-bg: #0f172a;
				--mr-surface: #1e293b;
				--mr-text: #f8fafc;
				--mr-muted: #94a3b8;
				--mr-border: #334155;
				--mr-primary: #3b82f6;
				--mr-primary-hover: #60a5fa;
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
			max-width: 1140px;
			margin: 2rem auto;
			padding: 0 1.25rem;
		}
		header {
			display: flex;
			flex-wrap: wrap;
			align-items: center;
			justify-content: space-between;
			gap: 1rem;
			margin-bottom: 1.5rem;
			padding-bottom: 1rem;
			border-bottom: 1px solid var(--mr-border);
		}
		.header-left {
			display: flex;
			align-items: center;
			gap: 1rem;
		}
		.site-brand {
			font-size: 1.3rem;
			font-weight: 700;
			color: var(--mr-text);
			text-decoration: none;
		}
		.header-right {
			display: flex;
			align-items: center;
			gap: 0.75rem;
		}
		.search-input {
			padding: 0.45rem 0.85rem;
			border: 1px solid var(--mr-border);
			border-radius: var(--mr-radius);
			background: var(--mr-surface);
			color: var(--mr-text);
			font-size: 0.9rem;
			width: 240px;
		}
		.search-input:focus {
			outline: 2px solid var(--mr-primary);
		}
		.btn {
			display: inline-block;
			padding: 0.45rem 0.85rem;
			font-size: 0.85rem;
			font-weight: 500;
			text-decoration: none;
			border-radius: var(--mr-radius);
			cursor: pointer;
			border: 1px solid transparent;
		}
		.btn-help {
			background: rgba(37,99,235,0.1);
			color: var(--mr-primary);
			border-color: rgba(37,99,235,0.2);
		}
		.btn-help:hover {
			background: var(--mr-primary);
			color: #fff;
		}
		.breadcrumbs {
			display: flex;
			flex-wrap: wrap;
			align-items: center;
			gap: 0.35rem;
			font-size: 0.95rem;
			margin-bottom: 1.25rem;
			color: var(--mr-muted);
		}
		.breadcrumbs a {
			color: var(--mr-primary);
			text-decoration: none;
		}
		.breadcrumbs a:hover {
			text-decoration: underline;
		}
		.breadcrumbs .separator {
			color: var(--mr-muted);
		}
		.breadcrumbs .current {
			color: var(--mr-text);
			font-weight: 600;
		}
		.table-card {
			background: var(--mr-surface);
			border: 1px solid var(--mr-border);
			border-radius: var(--mr-radius);
			overflow: hidden;
			box-shadow: 0 1px 3px rgba(0,0,0,0.05);
		}
		table {
			width: 100%%;
			border-collapse: collapse;
			text-align: left;
		}
		th, td {
			padding: 0.75rem 1.25rem;
			border-bottom: 1px solid var(--mr-border);
		}
		th {
			background: rgba(0,0,0,0.02);
			font-size: 0.8rem;
			text-transform: uppercase;
			color: var(--mr-muted);
			letter-spacing: 0.05em;
			font-weight: 600;
		}
		tr:last-child td {
			border-bottom: none;
		}
		tr:hover td {
			background: rgba(0,0,0,0.015);
		}
		.name-cell {
			display: flex;
			align-items: center;
			gap: 0.75rem;
		}
		.icon {
			display: flex;
			align-items: center;
			flex-shrink: 0;
		}
		.item-link {
			color: var(--mr-text);
			text-decoration: none;
			word-break: break-all;
		}
		.dir-row .item-link {
			font-weight: 500;
			color: var(--mr-primary);
		}
		.item-link:hover {
			text-decoration: underline;
		}
		.time-cell {
			white-space: nowrap;
			color: var(--mr-muted);
			font-size: 0.85rem;
			width: 220px;
		}
		.size-cell {
			white-space: nowrap;
			color: var(--mr-muted);
			font-size: 0.85rem;
			text-align: right;
			width: 120px;
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
	</style>
</head>
<body data-app="mirrorrelay">
	<div class="container">
		<header data-ui="header">
			<div class="header-left">
				<a href="/" class="site-brand">%s</a>
			</div>
			<div class="header-right">
				<input type="text" id="fileFilter" class="search-input" placeholder="Filter files..." oninput="filterTable()">
				%s
			</div>
		</header>

		<nav class="breadcrumbs" aria-label="Breadcrumb">
			%s
		</nav>

		<main data-ui="browser">
			<div class="table-card">
				<table id="browserTable">
					<thead>
						<tr>
							<th>Name / 文件名</th>
							<th>Last Modified / 修改时间</th>
							<th style="text-align:right;">Size / 大小</th>
						</tr>
					</thead>
					<tbody>
						%s
					</tbody>
				</table>
			</div>
		</main>

		<footer>
			<p><a href="/">← Return to Repository Index / 返回镜像列表</a> &bull; Powered by %s</p>
		</footer>
	</div>

	<script>
		function filterTable() {
			var query = document.getElementById('fileFilter').value.toLowerCase();
			var rows = document.querySelectorAll('#browserTable tbody tr');
			rows.forEach(function(row) {
				var name = row.getAttribute('data-name') || '';
				row.style.display = name.indexOf(query) > -1 ? '' : 'none';
			});
		}
	</script>
</body>
</html>`,
		html.EscapeString(theme),
		html.EscapeString(listing.Title),
		html.EscapeString(title),
		customCSSLink,
		html.EscapeString(title),
		helpButton,
		breadcrumbs,
		rows.String(),
		html.EscapeString(title),
	)
}

func buildBreadcrumbs(reqPath string, repo model.Mirror) string {
	cleanPath := strings.Trim(reqPath, "/")
	parts := strings.Split(cleanPath, "/")
	if len(parts) == 0 || (len(parts) == 1 && parts[0] == "") {
		return fmt.Sprintf(`<a href="/">Home</a> <span class="separator">/</span> <span class="current">%s</span>`, html.EscapeString(repo.Name))
	}

	var buf strings.Builder
	buf.WriteString(`<a href="/">Home</a> <span class="separator">/</span> `)

	currentAcc := "/"
	for i, part := range parts {
		currentAcc = path.Join(currentAcc, part) + "/"
		if i == len(parts)-1 {
			fmt.Fprintf(&buf, `<span class="current">%s</span>`, html.EscapeString(part))
		} else {
			fmt.Fprintf(&buf, `<a href="%s">%s</a> <span class="separator">/</span> `, html.EscapeString(currentAcc), html.EscapeString(part))
		}
	}
	return buf.String()
}
