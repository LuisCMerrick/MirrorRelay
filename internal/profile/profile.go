// Package profile contains pre-defined repository configurations.
package profile

import (
	"errors"
	"strings"

	"github.com/LuisCMerrick/MirrorRelay/internal/model"
)

type Profile struct {
	Name               string            `json:"name"`
	Version            string            `json:"version"`
	LatestStable       bool              `json:"latest_stable"`
	Type               string            `json:"type"`
	ProxyMode          string            `json:"proxy_mode"`
	Upstream           string            `json:"upstream"`
	HealthPath         string            `json:"health_path"`
	CacheEnabled       bool              `json:"cache_enabled"`
	CacheProfile       string            `json:"cache_profile"`
	CacheAuthenticated bool              `json:"cache_authenticated"`
	Rewrite            bool              `json:"rewrite_enabled"`
	HTMLRewrite        bool              `json:"html_rewrite_enabled"`
	RewriteProfile     string            `json:"rewrite_profile"`
	RewriteHosts       []string          `json:"rewrite_hosts"`
	PublicMode         string            `json:"public_mode"`
	AuthMode           string            `json:"auth_mode"`
	RedirectMode       string            `json:"blob_redirect_mode"`
	HeaderAdd          map[string]string `json:"header_add"`
	HeaderRemove       []string          `json:"header_remove"`
	ConnectTimeoutSec  int               `json:"connect_timeout_sec"`
	ReadTimeoutSec     int               `json:"read_timeout_sec"`
	SendTimeoutSec     int               `json:"send_timeout_sec"`
	MetadataLimitBytes int64             `json:"metadata_rewrite_limit_bytes"`
	MetadataTTLSec     int               `json:"metadata_ttl_sec"`
	PackageTTLSec      int               `json:"package_ttl_sec"`
	ImmutableTTLSec    int               `json:"immutable_ttl_sec"`
	BlobTTLSec         int               `json:"blob_ttl_sec"`
	Help               model.HelpConfig  `json:"help,omitempty"`
}

var builtins = []Profile{
	{Name: "Generic HTTP", Version: "1.0.0", LatestStable: true, Type: "generic", ProxyMode: "transparent", Upstream: "https://example.com/", CacheProfile: "standard", PublicMode: "path"},
	{
		Name: "Debian", Version: "1.0.0", LatestStable: true, Type: "apt", ProxyMode: "transparent", Upstream: "https://deb.debian.org/debian/", HealthPath: "dists/stable/Release", CacheEnabled: true, CacheProfile: "packages", PublicMode: "path",
		Help: model.HelpConfig{
			Enabled: true, Title: "Debian", Summary: "Debian 软件源使用说明", Template: "builtin://help/debian.md", TemplateVersion: 1,
			Variants: []model.HelpVariant{
				{Key: "bookworm", Label: "Debian 12 (bookworm)", Codename: "bookworm", Default: true},
				{Key: "bullseye", Label: "Debian 11 (bullseye)", Codename: "bullseye"},
				{Key: "trixie", Label: "Debian 13 (trixie, testing)", Codename: "trixie"},
				{Key: "sid", Label: "Debian unstable (sid)", Codename: "sid"},
			},
			Formats: []model.HelpFormat{
				{Key: "sources.list", Label: "传统格式 (/etc/apt/sources.list)", Default: true},
				{Key: "deb822", Label: "DEB822 格式 (/etc/apt/sources.list.d/*.sources)", Extension: ".sources"},
			},
		},
	},
	{
		Name: "Debian Security", Version: "1.0.0", LatestStable: true, Type: "apt", ProxyMode: "transparent", Upstream: "https://deb.debian.org/debian-security/", HealthPath: "dists/stable-security/InRelease", CacheEnabled: true, CacheProfile: "packages", PublicMode: "path",
		Help: model.HelpConfig{
			Enabled: true, Title: "Debian Security", Summary: "Debian Security 安全更新源使用说明", Template: "builtin://help/debian-security.md", TemplateVersion: 1,
			Variants: []model.HelpVariant{
				{Key: "bookworm-security", Label: "Debian 12 (bookworm-security)", Codename: "bookworm-security", Default: true},
				{Key: "bullseye-security", Label: "Debian 11 (bullseye-security)", Codename: "bullseye-security"},
			},
		},
	},
	{
		Name: "Ubuntu", Version: "1.0.0", LatestStable: true, Type: "apt", ProxyMode: "transparent", Upstream: "https://archive.ubuntu.com/ubuntu/", HealthPath: "dists/noble/Release", CacheEnabled: true, CacheProfile: "packages", PublicMode: "path",
		Help: model.HelpConfig{
			Enabled: true, Title: "Ubuntu", Summary: "Ubuntu 软件源使用说明", Template: "builtin://help/ubuntu.md", TemplateVersion: 1,
			Variants: []model.HelpVariant{
				{Key: "noble", Label: "Ubuntu 24.04 LTS (noble)", Codename: "noble", Default: true},
				{Key: "jammy", Label: "Ubuntu 22.04 LTS (jammy)", Codename: "jammy"},
				{Key: "focal", Label: "Ubuntu 20.04 LTS (focal)", Codename: "focal"},
			},
			Formats: []model.HelpFormat{
				{Key: "sources.list", Label: "传统格式 (/etc/apt/sources.list)", Default: true},
				{Key: "deb822", Label: "DEB822 格式 (/etc/apt/sources.list.d/ubuntu.sources)", Extension: ".sources"},
			},
		},
	},
	{
		Name: "Rocky Linux", Version: "1.0.0", LatestStable: true, Type: "rpm", ProxyMode: "transparent", Upstream: "https://dl.rockylinux.org/pub/rocky/", HealthPath: "9/BaseOS/x86_64/os/repodata/repomd.xml", CacheEnabled: true, CacheProfile: "packages", PublicMode: "path",
		Help: model.HelpConfig{
			Enabled: true, Title: "Rocky Linux", Summary: "Rocky Linux 软件源使用说明", Template: "builtin://help/rocky.md", TemplateVersion: 1,
			Variants: []model.HelpVariant{
				{Key: "9", Label: "Rocky Linux 9", Codename: "9", Default: true},
				{Key: "8", Label: "Rocky Linux 8", Codename: "8"},
			},
		},
	},
	{
		Name: "AlmaLinux", Version: "1.0.0", LatestStable: true, Type: "rpm", ProxyMode: "transparent", Upstream: "https://repo.almalinux.org/almalinux/", HealthPath: "9/BaseOS/x86_64/os/repodata/repomd.xml", CacheEnabled: true, CacheProfile: "packages", PublicMode: "path",
		Help: model.HelpConfig{
			Enabled: true, Title: "AlmaLinux", Summary: "AlmaLinux 软件源使用说明", Template: "builtin://help/almalinux.md", TemplateVersion: 1,
			Variants: []model.HelpVariant{
				{Key: "9", Label: "AlmaLinux 9", Codename: "9", Default: true},
				{Key: "8", Label: "AlmaLinux 8", Codename: "8"},
			},
		},
	},
	{Name: "CentOS Stream", Version: "1.0.0", LatestStable: true, Type: "rpm", ProxyMode: "transparent", Upstream: "https://mirror.stream.centos.org/", CacheEnabled: true, CacheProfile: "packages", PublicMode: "path"},
	{
		Name: "Fedora", Version: "1.0.0", LatestStable: true, Type: "rpm", ProxyMode: "transparent", Upstream: "https://download.fedoraproject.org/pub/fedora/linux/", CacheEnabled: true, CacheProfile: "packages", PublicMode: "path",
		Help: model.HelpConfig{
			Enabled: true, Title: "Fedora", Summary: "Fedora 软件源使用说明", Template: "builtin://help/fedora.md", TemplateVersion: 1,
			Variants: []model.HelpVariant{
				{Key: "40", Label: "Fedora 40", Codename: "40", Default: true},
				{Key: "39", Label: "Fedora 39", Codename: "39"},
			},
		},
	},
	{
		Name: "EPEL", Version: "1.0.0", LatestStable: true, Type: "rpm", ProxyMode: "transparent", Upstream: "https://dl.fedoraproject.org/pub/epel/", CacheEnabled: true, CacheProfile: "packages", PublicMode: "path",
		Help: model.HelpConfig{
			Enabled: true, Title: "EPEL", Summary: "EPEL 额外软件包仓库使用说明", Template: "builtin://help/epel.md", TemplateVersion: 1,
			Variants: []model.HelpVariant{
				{Key: "9", Label: "EPEL 9", Codename: "9", Default: true},
				{Key: "8", Label: "EPEL 8", Codename: "8"},
				{Key: "7", Label: "EPEL 7", Codename: "7"},
			},
		},
	},
	{
		Name: "Alpine", Version: "1.0.0", LatestStable: true, Type: "apk", ProxyMode: "transparent", Upstream: "https://dl-cdn.alpinelinux.org/alpine/", CacheEnabled: true, CacheProfile: "packages", PublicMode: "path",
		Help: model.HelpConfig{
			Enabled: true, Title: "Alpine Linux", Summary: "Alpine Linux apk 软件源使用说明", Template: "builtin://help/alpine.md", TemplateVersion: 1,
			Variants: []model.HelpVariant{
				{Key: "v3.20", Label: "v3.20", Codename: "v3.20", Default: true},
				{Key: "v3.19", Label: "v3.19", Codename: "v3.19"},
				{Key: "edge", Label: "edge", Codename: "edge"},
			},
		},
	},
	{
		Name: "OpenWrt", Version: "1.0.0", LatestStable: true, Type: "opkg", ProxyMode: "transparent", Upstream: "https://downloads.openwrt.org/", CacheEnabled: true, CacheProfile: "packages", PublicMode: "path",
		Help: model.HelpConfig{
			Enabled: true, Title: "OpenWrt", Summary: "OpenWrt opkg 软件包仓库使用说明", Template: "builtin://help/openwrt.md", TemplateVersion: 1,
		},
	},
	{
		Name: "Docker CE", Version: "1.0.0", LatestStable: true, Type: "apt", ProxyMode: "transparent", Upstream: "https://download.docker.com/linux/", CacheEnabled: true, CacheProfile: "packages", PublicMode: "path",
		Help: model.HelpConfig{
			Enabled: true, Title: "Docker CE", Summary: "Docker CE 社区版安装源使用说明", Template: "builtin://help/docker-ce.md", TemplateVersion: 1,
		},
	},
	{
		Name: "PyPI", Version: "1.0.0", LatestStable: true, Type: "pypi", ProxyMode: "rewrite", Upstream: "https://pypi.org/", HealthPath: "simple/", CacheEnabled: true, CacheProfile: "packages", Rewrite: true, RewriteProfile: "pypi", RewriteHosts: []string{"pypi.org", "files.pythonhosted.org"}, PublicMode: "path",
		Help: model.HelpConfig{
			Enabled: true, Title: "PyPI", Summary: "PyPI Python 镜像源使用说明", Template: "builtin://help/pypi.md", TemplateVersion: 1,
		},
	},
	{
		Name: "npm", Version: "1.0.0", LatestStable: true, Type: "npm", ProxyMode: "rewrite", Upstream: "https://registry.npmjs.org/", CacheEnabled: true, CacheProfile: "packages", Rewrite: true, RewriteProfile: "npm", RewriteHosts: []string{"registry.npmjs.org"}, PublicMode: "host", MetadataLimitBytes: 64 << 20,
		Help: model.HelpConfig{
			Enabled: true, Title: "npm", Summary: "npm 软件包注册表使用说明", Template: "builtin://help/npm.md", TemplateVersion: 1,
		},
	},
	{Name: "Maven Central", Version: "1.0.0", LatestStable: true, Type: "maven", ProxyMode: "transparent", Upstream: "https://repo1.maven.org/maven2/", CacheEnabled: true, CacheProfile: "packages", PublicMode: "path"},
	{Name: "Go Proxy", Version: "1.0.0", LatestStable: true, Type: "goproxy", ProxyMode: "transparent", Upstream: "https://proxy.golang.org/", CacheEnabled: true, CacheProfile: "packages", PublicMode: "host"},
	{Name: "NuGet", Version: "1.0.0", LatestStable: true, Type: "nuget", ProxyMode: "rewrite", Upstream: "https://api.nuget.org/", CacheEnabled: true, CacheProfile: "packages", Rewrite: true, RewriteProfile: "nuget", RewriteHosts: []string{"api.nuget.org", "globalcdn.nuget.org"}, PublicMode: "host"},
	{Name: "Cargo", Version: "1.0.0", LatestStable: true, Type: "cargo", ProxyMode: "rewrite", Upstream: "https://index.crates.io/", CacheEnabled: true, CacheProfile: "packages", Rewrite: true, RewriteProfile: "cargo", RewriteHosts: []string{"index.crates.io", "static.crates.io"}, PublicMode: "host"},
	{Name: "Conda", Version: "1.0.0", LatestStable: true, Type: "conda", ProxyMode: "transparent", Upstream: "https://repo.anaconda.com/pkgs/", CacheEnabled: true, CacheProfile: "packages", PublicMode: "path"},
	{Name: "Docker Registry Generic", Version: "1.0.0", LatestStable: true, Type: "docker-registry", ProxyMode: "registry", Upstream: "https://registry.example.com/", CacheEnabled: true, CacheProfile: "registry", PublicMode: "host", AuthMode: "direct", RedirectMode: "full_proxy"},
	{Name: "Docker Hub", Version: "1.0.0", LatestStable: false, Type: "docker-registry", ProxyMode: "registry", Upstream: "https://registry-1.docker.io/", HealthPath: "v2/", CacheEnabled: true, CacheProfile: "registry", PublicMode: "host", AuthMode: "full_proxy", RedirectMode: "full_proxy"},
	{Name: "Docker Hub", Version: "1.1.0", LatestStable: true, Type: "docker-registry", ProxyMode: "registry", Upstream: "https://registry-1.docker.io/", HealthPath: "v2/", CacheEnabled: true, CacheProfile: "registry", CacheAuthenticated: true, RewriteHosts: []string{"auth.docker.io", "registry-1.docker.io", "production.cloudfront.docker.com"}, PublicMode: "host", AuthMode: "full_proxy", RedirectMode: "full_proxy", BlobTTLSec: 31536000},
	{Name: "OCI Registry Generic", Version: "1.0.0", LatestStable: true, Type: "oci-registry", ProxyMode: "registry", Upstream: "https://registry.example.com/", CacheEnabled: true, CacheProfile: "registry", PublicMode: "host", AuthMode: "direct", RedirectMode: "full_proxy"},
	{
		Name: "Ubuntu Releases (ISO)", Version: "1.0.0", LatestStable: true, Type: "iso", ProxyMode: "transparent", Upstream: "https://releases.ubuntu.com/", HealthPath: "24.04/", CacheEnabled: true, CacheProfile: "packages", PublicMode: "path", HTMLRewrite: true,
		Help: model.HelpConfig{
			Enabled: true, Title: "Ubuntu Releases (ISO)", Summary: "Ubuntu 官方系统安装 ISO 镜像下载", Template: "builtin://help/ubuntu-iso.md", TemplateVersion: 1,
			Variants: []model.HelpVariant{
				{Key: "24.04", Label: "Ubuntu 24.04 LTS (Noble)", Codename: "24.04", Default: true},
				{Key: "22.04", Label: "Ubuntu 22.04 LTS (Jammy)", Codename: "22.04"},
			},
		},
	},
	{
		Name: "Debian CD (ISO)", Version: "1.0.0", LatestStable: true, Type: "iso", ProxyMode: "transparent", Upstream: "https://cdimage.debian.org/debian-cd/", HealthPath: "current/amd64/iso-cd/", CacheEnabled: true, CacheProfile: "packages", PublicMode: "path", HTMLRewrite: true,
		Help: model.HelpConfig{
			Enabled: true, Title: "Debian CD / ISO", Summary: "Debian 官方安装盘与 Live ISO 镜像下载", Template: "builtin://help/debian-cd.md", TemplateVersion: 1,
			Variants: []model.HelpVariant{
				{Key: "12", Label: "Debian 12 (Bookworm)", Codename: "12", Default: true},
				{Key: "11", Label: "Debian 11 (Bullseye)", Codename: "11"},
			},
		},
	},
	{
		Name: "Rocky Linux ISO", Version: "1.0.0", LatestStable: true, Type: "iso", ProxyMode: "transparent", Upstream: "https://download.rockylinux.org/pub/rocky/", HealthPath: "9/isos/x86_64/", CacheEnabled: true, CacheProfile: "packages", PublicMode: "path", HTMLRewrite: true,
		Help: model.HelpConfig{
			Enabled: true, Title: "Rocky Linux ISO", Summary: "Rocky Linux 官方系统安装镜像下载", Template: "builtin://help/rocky-iso.md", TemplateVersion: 1,
			Variants: []model.HelpVariant{
				{Key: "9", Label: "Rocky Linux 9", Codename: "9", Default: true},
				{Key: "8", Label: "Rocky Linux 8", Codename: "8"},
			},
		},
	},
	{
		Name: "Arch Linux ISO", Version: "1.0.0", LatestStable: true, Type: "iso", ProxyMode: "transparent", Upstream: "https://geo.mirror.pkgbuild.com/iso/", HealthPath: "latest/", CacheEnabled: true, CacheProfile: "packages", PublicMode: "path", HTMLRewrite: true,
		Help: model.HelpConfig{
			Enabled: true, Title: "Arch Linux ISO", Summary: "Arch Linux 官方安装镜像下载", Template: "builtin://help/arch-iso.md", TemplateVersion: 1,
		},
	},
	{Name: "Generic ISO / Images", Version: "1.0.0", LatestStable: true, Type: "iso", ProxyMode: "transparent", Upstream: "https://example.com/iso/", CacheEnabled: true, CacheProfile: "packages", PublicMode: "path", HTMLRewrite: true},
	{Name: "Custom", Version: "1.0.0", LatestStable: true, Type: "generic", ProxyMode: "custom", Upstream: "https://example.com/", CacheProfile: "standard", PublicMode: "path"},
}

func List() []Profile { return append([]Profile(nil), builtins...) }

func Find(name, version string) (Profile, bool) {
	var fallback Profile
	found := false
	for _, p := range builtins {
		if !strings.EqualFold(p.Name, name) {
			continue
		}
		if version != "" && p.Version == version {
			return p, true
		}
		if version == "" && (!found || p.LatestStable) {
			fallback = p
			found = true
			if p.LatestStable {
				return p, true
			}
		}
	}
	return fallback, found
}

func Apply(m *model.Mirror, name, version string) error {
	p, ok := Find(name, version)
	if !ok {
		return errors.New("profile or version not found")
	}
	m.ProfileName, m.ProfileVersion = p.Name, p.Version
	m.Type, m.ProxyMode, m.PublicMode = p.Type, p.ProxyMode, p.PublicMode
	m.CacheEnabled, m.CacheProfile = p.CacheEnabled, p.CacheProfile
	m.RewriteEnabled, m.RewriteProfile = p.Rewrite, p.RewriteProfile
	m.HTMLRewriteEnabled = p.HTMLRewrite
	m.RewriteHosts = append([]string(nil), p.RewriteHosts...)
	m.AuthMode, m.BlobRedirectMode = p.AuthMode, p.RedirectMode
	m.CacheAuthenticated = p.CacheAuthenticated
	m.HeaderAdd = make(map[string]string, len(p.HeaderAdd))
	for key, value := range p.HeaderAdd {
		m.HeaderAdd[key] = value
	}
	m.HeaderRemove = append([]string(nil), p.HeaderRemove...)
	m.ConnectTimeoutSec, m.ReadTimeoutSec, m.SendTimeoutSec = p.ConnectTimeoutSec, p.ReadTimeoutSec, p.SendTimeoutSec
	m.MetadataLimitBytes = p.MetadataLimitBytes
	m.MetadataTTLSec, m.PackageTTLSec = p.MetadataTTLSec, p.PackageTTLSec
	m.ImmutableTTLSec, m.BlobTTLSec = p.ImmutableTTLSec, p.BlobTTLSec
	m.Help = p.Help
	if len(m.Upstreams) == 0 {
		m.Upstreams = []model.Upstream{{URL: p.Upstream, Priority: 100, Weight: 1, Enabled: true}}
	}
	if m.HealthCheckPath == "" {
		m.HealthCheckPath = p.HealthPath
	}
	return nil
}
