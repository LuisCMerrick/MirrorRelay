package mirror

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/LuisCMerrick/RepoGate/internal/model"
)

type publicPathRoute struct {
	path string
	slug string
}

func ValidateRouteConflicts(repositories []model.Mirror, adminPath, publicBaseURL string, adminHost ...string) error {
	sharedHost := ""
	if publicBaseURL != "" {
		if parsed, err := url.Parse(publicBaseURL); err == nil {
			sharedHost = strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
		}
	}
	adminH := ""
	if len(adminHost) > 0 && adminHost[0] != "" {
		adminH = strings.ToLower(strings.TrimSuffix(adminHost[0], "."))
	}

	seenHosts := make(map[string]string)
	paths := make([]publicPathRoute, 0, len(repositories))
	reserved := []publicPathRoute{
		{path: "/healthz/", slug: "health endpoint"},
		{path: "/metrics/", slug: "metrics endpoint"},
		{path: "/_mirror_auth/", slug: "registry authentication endpoint"},
		{path: "/_repogate/", slug: "upstream auxiliary resource endpoint"},
	}
	if adminPath != "" {
		reserved = append(reserved, publicPathRoute{path: adminPath, slug: "administration"})
	}

	for _, repository := range repositories {
		if repository.PublicMode == "host" {
			host := strings.ToLower(strings.TrimSuffix(repository.PublicHost, "."))
			if host == "" {
				continue
			}
			if existing := seenHosts[host]; existing != "" {
				return fmt.Errorf("public host %s is used by both %s and %s", host, existing, repository.Slug)
			}
			if sharedHost != "" && host == sharedHost {
				return fmt.Errorf("public host %s conflicts with the shared host from http.public_base_url", host)
			}
			if adminH != "" && host == adminH {
				return fmt.Errorf("public host %s conflicts with admin.host", host)
			}
			seenHosts[host] = repository.Slug
			continue
		}

		path := repository.PublicPath
		if path == "" {
			path = "/" + repository.Slug + "/"
		}
		if path == "/" || path == "//" {
			return fmt.Errorf("public path %s conflicts with the repository index", path)
		}
		for _, system := range reserved {
			if routePathsOverlap(path, system.path) {
				return fmt.Errorf("public path %s for %s conflicts with the reserved %s path %s", path, repository.Slug, system.slug, system.path)
			}
		}
		for _, existing := range paths {
			if routePathsOverlap(path, existing.path) {
				return fmt.Errorf("public path %s for %s overlaps public path %s for %s", path, repository.Slug, existing.path, existing.slug)
			}
		}
		paths = append(paths, publicPathRoute{path: path, slug: repository.Slug})
	}
	return nil
}

func routePathsOverlap(left, right string) bool {
	left = strings.TrimSuffix(left, "/") + "/"
	right = strings.TrimSuffix(right, "/") + "/"
	return strings.HasPrefix(left, right) || strings.HasPrefix(right, left)
}
