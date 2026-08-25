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
	var body strings.Builder
	fmt.Fprintf(&body, `<!doctype html><html lang="en" data-theme="%s"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>%s Repository Index</title><style>body{font:15px/1.5 system-ui,sans-serif;max-width:1100px;margin:3rem auto;padding:0 1.25rem;color:#20242b}h1{margin-bottom:.25rem}p{color:#667085}table{width:100%%;border-collapse:collapse;margin-top:2rem}th,td{text-align:left;padding:.7rem;border-bottom:1px solid #dfe3e8}a{color:#0969da;text-decoration:none}a:hover{text-decoration:underline}code{font-family:ui-monospace,monospace}.btn-help{display:inline-block;padding:.2rem .5rem;font-size:12px;background:rgba(9,105,218,0.1);border-radius:4px;margin-left:.5rem}@media(prefers-color-scheme:dark){body{background:#11151b;color:#e6edf3}p{color:#9da7b3}th,td{border-color:#30363d}a{color:#58a6ff}}</style></head><body><h1>%s Repository Index</h1><p>Available repositories / 可用仓库</p><table><thead><tr><th>Repository / 仓库</th><th>Type / 类型</th><th>Description / 说明</th></tr></thead><tbody>`,
		html.EscapeString(themeAttr), html.EscapeString(siteTitle), html.EscapeString(siteTitle))
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
			helpBadge = fmt.Sprintf(`<a href="/help/%s/" class="btn-help" title="Help / 配置说明">Help</a>`, html.EscapeString(repository.Slug))
		}
		fmt.Fprintf(&body, `<tr><td><a href="%s"><strong>%s</strong></a>%s<br><code>%s</code></td><td>%s</td><td>%s</td></tr>`,
			html.EscapeString(href), html.EscapeString(repository.Name), helpBadge, html.EscapeString(label),
			html.EscapeString(repository.Type), html.EscapeString(repository.Description))
		visible++
	}
	if visible == 0 {
		body.WriteString(`<tr><td colspan="3">No repositories are currently available. / 当前没有可用仓库。</td></tr>`)
	}
	body.WriteString(`</tbody></table></body></html>`)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Content-Length", strconv.Itoa(body.Len()))
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodGet {
		_, _ = io.WriteString(w, body.String())
	}
}
