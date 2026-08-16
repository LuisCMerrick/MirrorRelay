package cluster

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/LuisCMerrick/MirrorRelay/internal/buildinfo"
	"github.com/LuisCMerrick/MirrorRelay/internal/config"
	"github.com/LuisCMerrick/MirrorRelay/internal/model"
)

const ClusterProtocolVersion = 1

type canonicalMirror struct {
	Slug               string              `json:"slug"`
	Type               string              `json:"type"`
	Enabled            bool                `json:"enabled"`
	Description        string              `json:"description"`
	PublicPath         string              `json:"public_path"`
	ProxyMode          string              `json:"proxy_mode"`
	CacheEnabled       bool                `json:"cache_enabled"`
	CacheProfile       string              `json:"cache_profile"`
	RewriteEnabled     bool                `json:"rewrite_enabled"`
	HTMLRewriteEnabled bool                `json:"html_rewrite_enabled"`
	RewriteProfile     string              `json:"rewrite_profile"`
	RewriteHosts       []string            `json:"rewrite_hosts"`
	HealthCheckEnabled bool                `json:"health_check_enabled"`
	HealthCheckPath    string              `json:"health_check_path"`
	HealthIntervalSec  int                 `json:"health_interval_sec"`
	HealthTimeoutSec   int                 `json:"health_timeout_sec"`
	HealthMethod       string              `json:"health_method"`
	HealthExpected     int                 `json:"health_expected"`
	RedirectMode       string              `json:"redirect_mode"`
	ProfileName        string              `json:"profile_name"`
	ProfileVersion     string              `json:"profile_version"`
	RateLimitProfile   string              `json:"rate_limit_profile"`
	AccessPolicy       string              `json:"access_policy"`
	StripPrefix        string              `json:"strip_prefix"`
	AddPrefix          string              `json:"add_prefix"`
	HostRewrite        string              `json:"host_rewrite"`
	HeaderAdd          map[string]string   `json:"header_add"`
	HeaderRemove       []string            `json:"header_remove"`
	ConnectTimeoutSec  int                 `json:"connect_timeout_sec"`
	ReadTimeoutSec     int                 `json:"read_timeout_sec"`
	SendTimeoutSec     int                 `json:"send_timeout_sec"`
	MetadataLimitBytes int64               `json:"metadata_rewrite_limit_bytes"`
	MetadataTTLSec     int                 `json:"metadata_ttl_sec"`
	PackageTTLSec      int                 `json:"package_ttl_sec"`
	ImmutableTTLSec    int                 `json:"immutable_ttl_sec"`
	BlobTTLSec         int                 `json:"blob_ttl_sec"`
	CacheAuthenticated bool                `json:"cache_authenticated"`
	AuthMode           string              `json:"auth_mode"`
	TokenUpstream      string              `json:"token_upstream"`
	BlobRedirectMode   string              `json:"blob_redirect_mode"`
	PullOnly           bool                `json:"pull_only"`
	AllowHTTP          bool                `json:"allow_http_upstream"`
	AllowPrivate       bool                `json:"allow_private_upstream"`
	InsecureTLS        bool                `json:"insecure_skip_verify"`
	BandwidthLimitBPS  int64               `json:"bandwidth_limit_bps"`
	MaxConcurrency     int                 `json:"max_concurrency"`
	Upstreams          []canonicalUpstream `json:"upstreams"`
}

type canonicalUpstream struct {
	URL      string `json:"url"`
	Host     string `json:"host"`
	Priority int    `json:"priority"`
	Weight   int    `json:"weight"`
	Enabled  bool   `json:"enabled"`
}

func CanonicalFingerprint(repositories []model.Mirror) string {
	if len(repositories) == 0 {
		return "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	}

	canonical := make([]canonicalMirror, 0, len(repositories))
	for _, m := range repositories {
		hosts := append([]string(nil), m.RewriteHosts...)
		sort.Strings(hosts)

		headerRemove := append([]string(nil), m.HeaderRemove...)
		sort.Strings(headerRemove)

		headerAdd := make(map[string]string, len(m.HeaderAdd))
		for k, v := range m.HeaderAdd {
			headerAdd[k] = v
		}

		upstreams := make([]canonicalUpstream, 0, len(m.Upstreams))
		for _, u := range m.Upstreams {
			upstreams = append(upstreams, canonicalUpstream{
				URL:      strings.TrimSpace(u.URL),
				Host:     strings.TrimSpace(u.Host),
				Priority: u.Priority,
				Weight:   u.Weight,
				Enabled:  u.Enabled,
			})
		}
		sort.Slice(upstreams, func(i, j int) bool {
			if upstreams[i].Priority != upstreams[j].Priority {
				return upstreams[i].Priority < upstreams[j].Priority
			}
			return upstreams[i].URL < upstreams[j].URL
		})

		cm := canonicalMirror{
			Slug:               strings.ToLower(strings.TrimSpace(m.Slug)),
			Type:               strings.ToLower(strings.TrimSpace(m.Type)),
			Enabled:            m.Enabled,
			Description:        m.Description,
			PublicPath:         strings.Trim(m.PublicPath, "/"),
			ProxyMode:          m.ProxyMode,
			CacheEnabled:       m.CacheEnabled,
			CacheProfile:       m.CacheProfile,
			RewriteEnabled:     m.RewriteEnabled,
			HTMLRewriteEnabled: m.HTMLRewriteEnabled,
			RewriteProfile:     m.RewriteProfile,
			RewriteHosts:       hosts,
			HealthCheckEnabled: m.HealthCheckEnabled,
			HealthCheckPath:    m.HealthCheckPath,
			HealthIntervalSec:  m.HealthIntervalSec,
			HealthTimeoutSec:   m.HealthTimeoutSec,
			HealthMethod:       m.HealthMethod,
			HealthExpected:     m.HealthExpected,
			RedirectMode:       m.RedirectMode,
			ProfileName:        m.ProfileName,
			ProfileVersion:     m.ProfileVersion,
			RateLimitProfile:   m.RateLimitProfile,
			AccessPolicy:       m.AccessPolicy,
			StripPrefix:        m.StripPrefix,
			AddPrefix:          m.AddPrefix,
			HostRewrite:        m.HostRewrite,
			HeaderAdd:          headerAdd,
			HeaderRemove:       headerRemove,
			ConnectTimeoutSec:  m.ConnectTimeoutSec,
			ReadTimeoutSec:     m.ReadTimeoutSec,
			SendTimeoutSec:     m.SendTimeoutSec,
			MetadataLimitBytes: m.MetadataLimitBytes,
			MetadataTTLSec:     m.MetadataTTLSec,
			PackageTTLSec:      m.PackageTTLSec,
			ImmutableTTLSec:    m.ImmutableTTLSec,
			BlobTTLSec:         m.BlobTTLSec,
			CacheAuthenticated: m.CacheAuthenticated,
			AuthMode:           m.AuthMode,
			TokenUpstream:      m.TokenUpstream,
			BlobRedirectMode:   m.BlobRedirectMode,
			PullOnly:           m.PullOnly,
			AllowHTTP:          m.AllowHTTP,
			AllowPrivate:       m.AllowPrivate,
			InsecureTLS:        m.InsecureTLS,
			BandwidthLimitBPS:  m.BandwidthLimitBPS,
			MaxConcurrency:     m.MaxConcurrency,
			Upstreams:          upstreams,
		}
		canonical = append(canonical, cm)
	}

	sort.Slice(canonical, func(i, j int) bool {
		return canonical[i].Slug < canonical[j].Slug
	})

	data, err := json.Marshal(canonical)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return fmt.Sprintf("sha256:%x", sum)
}

func ExtractCapabilities(repositories []model.Mirror) []string {
	capMap := make(map[string]struct{})
	for _, m := range repositories {
		t := strings.ToLower(strings.TrimSpace(m.Type))
		if t != "" {
			capMap[t] = struct{}{}
		}
	}
	caps := make([]string, 0, len(capMap))
	for k := range capMap {
		caps = append(caps, k)
	}
	sort.Strings(caps)
	return caps
}

func GenerateManifest(cfg config.Config, repositories []model.Mirror, build buildinfo.Info, configGeneration int64) model.ClusterManifest {
	nodeID := cfg.Distributed.Node.Name
	if nodeID == "" {
		nodeID = "mirrorrelay-node"
	}
	return model.ClusterManifest{
		ProtocolVersion:    ClusterProtocolVersion,
		MirrorRelayVersion: build.Version,
		NodeID:             nodeID,
		ConfigGeneration:   configGeneration,
		ConfigFingerprint:  CanonicalFingerprint(repositories),
		Capabilities:       ExtractCapabilities(repositories),
	}
}
