package api

import (
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/LuisCMerrick/MirrorRelay/internal/browser"
	"github.com/LuisCMerrick/MirrorRelay/internal/help"
	"github.com/LuisCMerrick/MirrorRelay/internal/model"
	"github.com/LuisCMerrick/MirrorRelay/internal/security"
)

func requestHostname(raw string) string {
	if host, _, err := net.SplitHostPort(raw); err == nil {
		raw = host
	}
	return strings.ToLower(strings.Trim(strings.TrimSuffix(raw, "."), "[]"))
}

func (s *Server) requestPublicBase(request *http.Request) (string, error) {
	if s.cfg.HTTP.PublicBaseURL != "" {
		return strings.TrimRight(s.cfg.HTTP.PublicBaseURL, "/"), nil
	}
	if request == nil || !security.ValidRequestAuthority(request.Host) {
		return "", fmt.Errorf("invalid request host")
	}
	return "https://" + request.Host, nil
}

func (s *Server) publicHandler(proxy http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.hostRepository(r.Host) {
			if r.URL.Path == "/ui/custom.css" {
				s.serveCustomCSS(w, r)
				return
			}
			if strings.HasPrefix(r.URL.Path, "/ui/icons/") {
				s.serveUIIcon(w, r)
				return
			}
			if r.URL.Path == "/help" || r.URL.Path == "/help/" {
				s.helpOverview(w, r)
				return
			}
			if strings.HasPrefix(r.URL.Path, "/help/") {
				s.helpDetail(w, r, strings.TrimPrefix(r.URL.Path, "/help/"))
				return
			}
		}

		if s.cfg.Distributed.Enabled && s.cfg.Distributed.Role == "coordinator" {
			if r.URL.Path == "/" && !s.hostRepository(r.Host) {
				s.repositoryIndex(w, r)
				return
			}
			repo, _, matched := s.registry.Route(r.Host, r.URL.Path)
			if matched {
				if repo.Type == "docker-registry" || repo.Type == "oci-registry" {
					writeJSON(w, http.StatusNotImplemented, map[string]string{
						"error":   "distributed_registry_not_supported",
						"message": "Distributed Registry Not Supported",
					})
					return
				}
				clientIP := s.requestClientIP(r)
				fp := ""
				if s.clusterChecker != nil {
					fp = s.clusterChecker.ClusterFingerprint()
				}
				if s.clusterRouter != nil {
					node, err := s.clusterRouter.SelectNode(clientIP, repo, fp)
					if err != nil {
						if s.clusterMetrics != nil {
							s.clusterMetrics.IncNoAvailableEdge()
						}
						writeJSON(w, http.StatusServiceUnavailable, map[string]string{
							"error":   "no_available_edge",
							"message": "No healthy MirrorRelay edge node is available",
						})
						return
					}
					if s.clusterMetrics != nil {
						s.clusterMetrics.IncRedirect(node.Name, node.Region)
					}
					dest, err := edgeRedirectLocation(node.URL, r.URL, s.cfg.Distributed.AllowHTTP)
					if err != nil {
						writeJSON(w, http.StatusServiceUnavailable, map[string]string{
							"error": "invalid_edge_url", "message": "The selected edge node URL is invalid",
						})
						return
					}
					http.Redirect(w, r, dest, http.StatusTemporaryRedirect)
					return
				}
			}
		}

		if r.URL.Path != "/" || s.hostRepository(r.Host) {
			proxy.ServeHTTP(w, r)
			return
		}
		s.repositoryIndex(w, r)
	})
}

func edgeRedirectLocation(rawOrigin string, requestURL *url.URL, allowHTTP bool) (string, error) {
	origin, err := security.ParseOriginURL(rawOrigin, allowHTTP)
	if err != nil {
		return "", err
	}
	origin.Path = requestURL.Path
	origin.RawPath = requestURL.EscapedPath()
	origin.RawQuery = requestURL.RawQuery
	origin.ForceQuery = requestURL.ForceQuery
	return origin.String(), nil
}

func (s *Server) hostRepository(requestHost string) bool {
	if s.registry == nil {
		return false
	}
	if host, _, err := net.SplitHostPort(requestHost); err == nil {
		requestHost = host
	}
	requestHost = strings.ToLower(strings.Trim(requestHost, "[]"))
	for _, repository := range s.registry.List() {
		if repository.Enabled && repository.PublicMode == "host" && strings.EqualFold(repository.PublicHost, requestHost) {
			return true
		}
	}
	return false
}

func (s *Server) serveCustomCSS(w http.ResponseWriter, r *http.Request) {
	appearance := s.appearanceConfig()
	if !appearance.Enabled || !appearance.CustomCSS.Enabled || appearance.CustomCSS.File == "" {
		http.NotFound(w, r)
		return
	}
	content, err := os.ReadFile(appearance.CustomCSS.File)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=60")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(content)
}

func (s *Server) serveUIIcon(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/ui/icons/"), ".svg")
	svg, ok := browser.Icons[name]
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(svg))
}

func (s *Server) helpOverview(w http.ResponseWriter, r *http.Request) {
	appearance := s.appearanceConfig()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	publicBase, err := s.requestPublicBase(r)
	if err != nil {
		http.Error(w, "invalid request host", http.StatusBadRequest)
		return
	}
	var repoList []model.Mirror
	if s.registry != nil {
		repoList = s.registry.List()
	}
	htmlContent := help.RenderOverviewHTML(repoList, publicBase, appearance.Branding, appearance.Theme)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(htmlContent))
}

func (s *Server) helpDetail(w http.ResponseWriter, r *http.Request, slugPath string) {
	appearance := s.appearanceConfig()
	slug := strings.Trim(slugPath, "/")
	if slug == "" {
		s.helpOverview(w, r)
		return
	}
	if s.registry == nil {
		http.NotFound(w, r)
		return
	}
	repo, found := s.registry.Get(slug)
	if !found || !repo.Enabled || !repo.Help.Enabled || repo.Help.Template == "" {
		http.NotFound(w, r)
		return
	}
	variant := r.URL.Query().Get("variant")
	format := r.URL.Query().Get("format")
	safeUI := r.URL.Query().Get("safe-ui") == "1"

	publicBase, err := s.requestPublicBase(r)
	if err != nil {
		http.Error(w, "invalid request host", http.StatusBadRequest)
		return
	}

	res, err := help.Render(repo, publicBase, variant, format, appearance.Branding.Title)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	htmlContent := help.RenderDetailHTML(res, appearance.Branding, appearance.Theme, safeUI)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(htmlContent))
}

func (s *Server) repositoryIndex(w http.ResponseWriter, r *http.Request) {
	appearance := s.appearanceConfig()
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Referrer-Policy", "no-referrer")
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	allowAdministrative := s.adminCIDRs.Allows(s.requestClientIP(r))
	var repositories []model.Mirror
	if s.registry != nil {
		repositories = s.registry.List()
	}
	sort.Slice(repositories, func(i, j int) bool {
		return strings.ToLower(repositories[i].Name) < strings.ToLower(repositories[j].Name)
	})
	siteTitle := appearance.Branding.Title
	if siteTitle == "" {
		siteTitle = "MirrorRelay"
	}
	themeAttr := appearance.Theme
	if themeAttr == "" {
		themeAttr = "system"
	}

	customCSSLink := ""
	if appearance.Enabled && appearance.CustomCSS.Enabled && appearance.CustomCSS.File != "" {
		customCSSLink = `<link rel="stylesheet" href="/ui/custom.css">`
	}

	var body strings.Builder
	fmt.Fprintf(&body, `<!doctype html>
<html lang="zh-CN" data-theme="%s">
<head>
	<meta charset="utf-8">
	<meta name="viewport" content="width=device-width,initial-scale=1">
	<title>%s - Repository Index / 镜像列表</title>
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
			font-size: 1.35rem;
			font-weight: 700;
			color: var(--mr-text);
			text-decoration: none;
		}
		.header-right {
			display: flex;
			align-items: center;
			gap: 0.75rem;
		}
		.theme-select {
			padding: 0.4rem 0.6rem;
			border-radius: var(--mr-radius);
			border: 1px solid var(--mr-border);
			background: var(--mr-surface);
			color: var(--mr-text);
			font-size: 0.85rem;
			cursor: pointer;
		}
		.github-link {
			display: inline-flex;
			align-items: center;
			gap: 0.35rem;
			padding: 0.4rem 0.75rem;
			border-radius: var(--mr-radius);
			border: 1px solid var(--mr-border);
			background: var(--mr-surface);
			color: var(--mr-text);
			text-decoration: none;
			font-size: 0.85rem;
			font-weight: 500;
			transition: all 0.15s;
		}
		.github-link:hover {
			border-color: var(--mr-primary);
			color: var(--mr-primary);
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
			padding: 0.85rem 1.25rem;
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
		.type-badge {
			display: inline-block;
			padding: 0.2rem 0.5rem;
			font-size: 0.75rem;
			font-weight: 600;
			text-transform: uppercase;
			background: rgba(37,99,235,0.08);
			color: var(--mr-primary);
			border-radius: 4px;
		}
		.btn-help {
			display: inline-block;
			padding: 0.25rem 0.55rem;
			font-size: 0.8rem;
			font-weight: 500;
			background: rgba(37,99,235,0.1);
			color: var(--mr-primary);
			border-radius: 4px;
			text-decoration: none;
			margin-left: 0.5rem;
			transition: all 0.15s;
		}
		.btn-help:hover {
			background: var(--mr-primary);
			color: #fff;
		}
		.repo-link {
			color: var(--mr-primary);
			text-decoration: none;
			font-weight: 600;
			font-size: 1rem;
		}
		.repo-link:hover {
			text-decoration: underline;
		}
		code {
			font-size: 0.85rem;
			color: var(--mr-muted);
			background: rgba(0,0,0,0.04);
			padding: 0.15rem 0.35rem;
			border-radius: 4px;
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
		footer a:hover {
			color: var(--mr-primary);
		}
	</style>
	<script>
		(function() {
			var saved = localStorage.getItem('mr_public_theme');
			if (saved) {
				document.documentElement.setAttribute('data-theme', saved);
			}
		})();
		function changeTheme(select) {
			var val = select.value;
			localStorage.setItem('mr_public_theme', val);
			document.documentElement.setAttribute('data-theme', val);
		}
	</script>
</head>
<body>
	<div class="container">
		<header>
			<div class="header-left">
				<a href="/" class="site-brand">%s</a>
			</div>
			<div class="header-right">
				<select class="theme-select" onchange="changeTheme(this)" aria-label="Theme">
					<option value="system">Auto / 跟随系统</option>
					<option value="light">Light / 浅色</option>
					<option value="dark">Dark / 深色</option>
				</select>
				<a href="https://github.com/LuisCMerrick/MirrorRelay" target="_blank" rel="noopener noreferrer" class="github-link" title="GitHub Repository">
					<svg width="15" height="15" viewBox="0 0 24 24" fill="currentColor"><path d="M12 0C5.37 0 0 5.37 0 12c0 5.31 3.435 9.795 8.205 11.385.6.105.825-.255.825-.57 0-.285-.015-1.23-.015-2.235-3.015.555-3.795-.735-4.035-1.41-.135-.345-.72-1.41-1.23-1.695-.42-.225-1.02-.78-.015-.795.945-.015 1.62.87 1.845 1.23 1.08 1.815 2.805 1.305 3.495.99.105-.78.42-1.305.765-1.605-2.67-.3-5.46-1.335-5.46-5.925 0-1.305.465-2.385 1.23-3.225-.12-.3-.54-1.53.12-3.18 0 0 1.005-.315 3.3 1.23.96-.27 1.98-.405 3-.405s2.04.135 3 .405c2.295-1.56 3.3-1.23 3.3-1.23.66 1.65.24 2.88.12 3.18.765.84 1.23 1.905 1.23 3.225 0 4.605-2.805 5.625-5.475 5.925.435.375.81 1.095.81 2.22 0 1.605-.015 2.895-.015 3.3 0 .315.225.69.825.57A12.02 12.02 0 0 0 24 12c0-6.63-5.37-12-12-12z"/></svg>
					GitHub
				</a>
			</div>
		</header>

		<main>
			<div class="table-card">
				<table>
					<thead>
						<tr>
							<th>Repository / 仓库</th>
							<th>Type / 类型</th>
							<th>Description / 说明</th>
						</tr>
					</thead>
					<tbody>`,
		html.EscapeString(themeAttr), html.EscapeString(siteTitle), customCSSLink, html.EscapeString(siteTitle))

	visible := 0
	for _, repository := range repositories {
		if !repository.Enabled || (repository.AccessPolicy == "admin" && !allowAdministrative) {
			continue
		}
		href := repository.PublicPath
		label := repository.PublicPath
		if repository.PublicMode == "host" {
			href = "https://" + repository.PublicHost + "/"
			label = repository.PublicHost + "/"
		} else if href == "" {
			href = "/" + repository.Slug + "/"
			label = href
		}
		helpBadge := ""
		if repository.Help.Enabled && repository.Help.Template != "" {
			helpBadge = fmt.Sprintf(`<a href="/help/%s/" class="btn-help" title="Help / 配置说明">Help / 说明</a>`, html.EscapeString(repository.Slug))
		}
		fmt.Fprintf(&body, `<tr>
			<td><a href="%s" class="repo-link">%s</a>%s<br><code>%s</code></td>
			<td><span class="type-badge">%s</span></td>
			<td>%s</td>
		</tr>`,
			html.EscapeString(href), html.EscapeString(repository.Name), helpBadge, html.EscapeString(label),
			html.EscapeString(repository.Type), html.EscapeString(repository.Description))
		visible++
	}
	if visible == 0 {
		body.WriteString(`<tr><td colspan="3">No repositories are currently available. / 当前没有可用仓库。</td></tr>`)
	}
	fmt.Fprintf(&body, `</tbody>
				</table>
			</div>
		</main>

		<footer>
			<p>Powered by <a href="https://github.com/LuisCMerrick/MirrorRelay" target="_blank" rel="noopener noreferrer">%s</a></p>
		</footer>
	</div>
</body>
</html>`, html.EscapeString(siteTitle))

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Content-Length", strconv.Itoa(body.Len()))
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodGet {
		_, _ = io.WriteString(w, body.String())
	}
}
