package api

import (
	"fmt"
	"strings"

	"github.com/LuisCMerrick/MirrorRelay/internal/config"
	"github.com/LuisCMerrick/MirrorRelay/internal/model"
)

func profileDiff(before, after model.Mirror) map[string]map[string]any {
	result := make(map[string]map[string]any)
	add := func(name string, oldValue, newValue any) {
		if fmt.Sprint(oldValue) != fmt.Sprint(newValue) {
			result[name] = map[string]any{"before": oldValue, "after": newValue}
		}
	}
	add("profile_name", before.ProfileName, after.ProfileName)
	add("profile_version", before.ProfileVersion, after.ProfileVersion)
	add("type", before.Type, after.Type)
	add("proxy_mode", before.ProxyMode, after.ProxyMode)
	add("public_mode", before.PublicMode, after.PublicMode)
	add("cache_enabled", before.CacheEnabled, after.CacheEnabled)
	add("cache_profile", before.CacheProfile, after.CacheProfile)
	add("rewrite_enabled", before.RewriteEnabled, after.RewriteEnabled)
	add("rewrite_profile", before.RewriteProfile, after.RewriteProfile)
	add("auth_mode", before.AuthMode, after.AuthMode)
	add("blob_redirect_mode", before.BlobRedirectMode, after.BlobRedirectMode)
	add("rewrite_hosts", before.RewriteHosts, after.RewriteHosts)
	add("header_add", before.HeaderAdd, after.HeaderAdd)
	add("header_remove", before.HeaderRemove, after.HeaderRemove)
	add("connect_timeout_sec", before.ConnectTimeoutSec, after.ConnectTimeoutSec)
	add("read_timeout_sec", before.ReadTimeoutSec, after.ReadTimeoutSec)
	add("send_timeout_sec", before.SendTimeoutSec, after.SendTimeoutSec)
	add("metadata_rewrite_limit_bytes", before.MetadataLimitBytes, after.MetadataLimitBytes)
	add("cache_authenticated", before.CacheAuthenticated, after.CacheAuthenticated)
	add("health_check_path", before.HealthCheckPath, after.HealthCheckPath)
	return result
}

func clientExamples(cfg config.Config, repository model.Mirror) []clientExample {
	base := strings.TrimRight(cfg.HTTP.PublicBaseURL, "/")
	if repository.PublicMode == "host" {
		base = "https://" + repository.PublicHost
	} else {
		if base == "" {
			base = "https://mirror.example.com"
		}
		base += "/" + strings.Trim(repository.PublicPath, "/")
	}
	switch repository.Type {
	case "apt":
		base = strings.TrimRight(base, "/") + "/"
		fileBase := "mirrorrelay"
		if repository.Slug != "" {
			fileBase += "-" + repository.Slug
		}
		listPath := "/etc/apt/sources.list.d/" + fileBase + ".list"
		deb822Path := "/etc/apt/sources.list.d/" + fileBase + ".sources"
		profileName := strings.ToLower(repository.ProfileName + " " + repository.Name)
		suites := []string{"bookworm", "bookworm-updates", "bookworm-backports"}
		components := "main contrib non-free non-free-firmware"
		keyring := "/usr/share/keyrings/debian-archive-keyring.gpg"
		if strings.Contains(profileName, "debian security") || strings.Contains(profileName, "debian-security") {
			suites = []string{"bookworm-security"}
		} else if strings.Contains(profileName, "ubuntu") {
			suites = []string{"noble", "noble-updates", "noble-backports", "noble-security"}
			components = "main restricted universe multiverse"
			keyring = "/usr/share/keyrings/ubuntu-archive-keyring.gpg"
		}
		lines := make([]string, 0, len(suites))
		for _, suite := range suites {
			lines = append(lines, "deb [signed-by="+keyring+"] "+base+" "+suite+" "+components)
		}
		return []clientExample{
			{
				Title:       "APT sources.list one-line format",
				Description: "Save as " + listPath,
				Command:     strings.Join(lines, "\n"),
				Format:      "sources.list",
				FilePath:    listPath,
			},
			{
				Title:       "APT DEB822 format",
				Description: "Save as " + deb822Path,
				Command:     "Types: deb\nURIs: " + base + "\nSuites: " + strings.Join(suites, " ") + "\nComponents: " + components + "\nSigned-By: " + keyring,
				Format:      "deb822",
				FilePath:    deb822Path,
			},
		}
	case "rpm":
		return []clientExample{{Title: "DNF/YUM baseurl", Command: "baseurl=" + base + "/$releasever/BaseOS/$basearch/os/"}}
	case "apk":
		return []clientExample{{Title: "Alpine repository", Command: base + "/v3.x/main"}}
	case "opkg":
		return []clientExample{{Title: "OpenWrt feed", Command: "src/gz mirror " + base + "/releases/<version>/packages/<arch>/base"}}
	case "pypi":
		return []clientExample{{Title: "pip", Command: "pip config set global.index-url " + base + "/simple/"}}
	case "npm":
		return []clientExample{{Title: "npm", Command: "npm config set registry " + base + "/"}}
	case "maven":
		return []clientExample{{Title: "Maven URL", Command: base + "/"}}
	case "goproxy":
		return []clientExample{{Title: "Go", Command: "go env -w GOPROXY=" + base + ",direct"}}
	case "nuget":
		return []clientExample{{Title: "NuGet", Command: "dotnet nuget add source " + base + "/v3/index.json -n mirror"}}
	case "cargo":
		return []clientExample{{Title: "Cargo registry index", Command: "registry = \"" + base + "/\""}}
	case "conda":
		return []clientExample{{Title: "Conda channel", Command: "conda config --add channels " + base}}
	case "docker-registry", "oci-registry":
		host := strings.TrimPrefix(base, "https://")
		return []clientExample{
			{Title: "Docker", Command: "docker pull " + host + "/library/nginx:latest"},
			{Title: "Podman", Command: "podman pull " + host + "/library/alpine:latest"},
		}
	default:
		return []clientExample{{Title: "HTTP", Command: "curl -fLO " + base + "/path/to/object"}}
	}
}
