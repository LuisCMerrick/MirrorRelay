package profile

import (
	"errors"
	"strings"

	"github.com/LuisCMerrick/RepoGate/internal/model"
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
}

var builtins = []Profile{
	{Name: "Generic HTTP", Version: "1.0.0", LatestStable: true, Type: "generic", ProxyMode: "transparent", Upstream: "https://example.com/", CacheProfile: "standard", PublicMode: "path"},
	{Name: "Debian", Version: "1.0.0", LatestStable: true, Type: "apt", ProxyMode: "transparent", Upstream: "https://deb.debian.org/debian/", HealthPath: "dists/stable/Release", CacheEnabled: true, CacheProfile: "packages", PublicMode: "path"},
	{Name: "Ubuntu", Version: "1.0.0", LatestStable: true, Type: "apt", ProxyMode: "transparent", Upstream: "https://archive.ubuntu.com/ubuntu/", HealthPath: "dists/noble/Release", CacheEnabled: true, CacheProfile: "packages", PublicMode: "path"},
	{Name: "Rocky Linux", Version: "1.0.0", LatestStable: true, Type: "rpm", ProxyMode: "transparent", Upstream: "https://dl.rockylinux.org/pub/rocky/", HealthPath: "9/BaseOS/x86_64/os/repodata/repomd.xml", CacheEnabled: true, CacheProfile: "packages", PublicMode: "path"},
	{Name: "AlmaLinux", Version: "1.0.0", LatestStable: true, Type: "rpm", ProxyMode: "transparent", Upstream: "https://repo.almalinux.org/almalinux/", HealthPath: "9/BaseOS/x86_64/os/repodata/repomd.xml", CacheEnabled: true, CacheProfile: "packages", PublicMode: "path"},
	{Name: "CentOS Stream", Version: "1.0.0", LatestStable: true, Type: "rpm", ProxyMode: "transparent", Upstream: "https://mirror.stream.centos.org/", CacheEnabled: true, CacheProfile: "packages", PublicMode: "path"},
	{Name: "Fedora", Version: "1.0.0", LatestStable: true, Type: "rpm", ProxyMode: "transparent", Upstream: "https://download.fedoraproject.org/pub/fedora/linux/", CacheEnabled: true, CacheProfile: "packages", PublicMode: "path"},
	{Name: "EPEL", Version: "1.0.0", LatestStable: true, Type: "rpm", ProxyMode: "transparent", Upstream: "https://dl.fedoraproject.org/pub/epel/", CacheEnabled: true, CacheProfile: "packages", PublicMode: "path"},
	{Name: "Alpine", Version: "1.0.0", LatestStable: true, Type: "apk", ProxyMode: "transparent", Upstream: "https://dl-cdn.alpinelinux.org/alpine/", CacheEnabled: true, CacheProfile: "packages", PublicMode: "path"},
	{Name: "OpenWrt", Version: "1.0.0", LatestStable: true, Type: "opkg", ProxyMode: "transparent", Upstream: "https://downloads.openwrt.org/", CacheEnabled: true, CacheProfile: "packages", PublicMode: "path"},
	{Name: "Docker CE", Version: "1.0.0", LatestStable: true, Type: "apt", ProxyMode: "transparent", Upstream: "https://download.docker.com/linux/", CacheEnabled: true, CacheProfile: "packages", PublicMode: "path"},
	{Name: "PyPI", Version: "1.0.0", LatestStable: true, Type: "pypi", ProxyMode: "rewrite", Upstream: "https://pypi.org/", HealthPath: "simple/", CacheEnabled: true, CacheProfile: "packages", Rewrite: true, RewriteProfile: "pypi", RewriteHosts: []string{"pypi.org", "files.pythonhosted.org"}, PublicMode: "path"},
	{Name: "npm", Version: "1.0.0", LatestStable: true, Type: "npm", ProxyMode: "rewrite", Upstream: "https://registry.npmjs.org/", CacheEnabled: true, CacheProfile: "packages", Rewrite: true, RewriteProfile: "npm", RewriteHosts: []string{"registry.npmjs.org"}, PublicMode: "host", MetadataLimitBytes: 64 << 20},
	{Name: "Maven Central", Version: "1.0.0", LatestStable: true, Type: "maven", ProxyMode: "transparent", Upstream: "https://repo1.maven.org/maven2/", CacheEnabled: true, CacheProfile: "packages", PublicMode: "path"},
	{Name: "Go Proxy", Version: "1.0.0", LatestStable: true, Type: "goproxy", ProxyMode: "transparent", Upstream: "https://proxy.golang.org/", CacheEnabled: true, CacheProfile: "packages", PublicMode: "host"},
	{Name: "NuGet", Version: "1.0.0", LatestStable: true, Type: "nuget", ProxyMode: "rewrite", Upstream: "https://api.nuget.org/", CacheEnabled: true, CacheProfile: "packages", Rewrite: true, RewriteProfile: "nuget", RewriteHosts: []string{"api.nuget.org", "globalcdn.nuget.org"}, PublicMode: "host"},
	{Name: "Cargo", Version: "1.0.0", LatestStable: true, Type: "cargo", ProxyMode: "rewrite", Upstream: "https://index.crates.io/", CacheEnabled: true, CacheProfile: "packages", Rewrite: true, RewriteProfile: "cargo", RewriteHosts: []string{"index.crates.io", "static.crates.io"}, PublicMode: "host"},
	{Name: "Conda", Version: "1.0.0", LatestStable: true, Type: "conda", ProxyMode: "transparent", Upstream: "https://repo.anaconda.com/pkgs/", CacheEnabled: true, CacheProfile: "packages", PublicMode: "path"},
	{Name: "Docker Registry Generic", Version: "1.0.0", LatestStable: true, Type: "docker-registry", ProxyMode: "registry", Upstream: "https://registry.example.com/", CacheEnabled: true, CacheProfile: "registry", PublicMode: "host", AuthMode: "direct", RedirectMode: "full_proxy"},
	{Name: "Docker Hub", Version: "1.0.0", LatestStable: false, Type: "docker-registry", ProxyMode: "registry", Upstream: "https://registry-1.docker.io/", HealthPath: "v2/", CacheEnabled: true, CacheProfile: "registry", PublicMode: "host", AuthMode: "full_proxy", RedirectMode: "full_proxy"},
	{Name: "Docker Hub", Version: "1.1.0", LatestStable: true, Type: "docker-registry", ProxyMode: "registry", Upstream: "https://registry-1.docker.io/", HealthPath: "v2/", CacheEnabled: true, CacheProfile: "registry", CacheAuthenticated: true, RewriteHosts: []string{"auth.docker.io", "registry-1.docker.io", "production.cloudfront.docker.com"}, PublicMode: "host", AuthMode: "full_proxy", RedirectMode: "full_proxy", BlobTTLSec: 31536000},
	{Name: "OCI Registry Generic", Version: "1.0.0", LatestStable: true, Type: "oci-registry", ProxyMode: "registry", Upstream: "https://registry.example.com/", CacheEnabled: true, CacheProfile: "registry", PublicMode: "host", AuthMode: "direct", RedirectMode: "full_proxy"},
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
			fallback, found = p, true
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
	if len(m.Upstreams) == 0 {
		m.Upstreams = []model.Upstream{{URL: p.Upstream, Priority: 100, Weight: 1, Enabled: true}}
	}
	if m.HealthCheckPath == "" {
		m.HealthCheckPath = p.HealthPath
	}
	return nil
}
