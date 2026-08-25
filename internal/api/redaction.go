package api

import (
	"errors"
	"net/url"
	"slices"

	"github.com/LuisCMerrick/MirrorRelay/internal/health"
	"github.com/LuisCMerrick/MirrorRelay/internal/model"
	"github.com/LuisCMerrick/MirrorRelay/internal/upstreamnginx"
)

const redactedValue = "[REDACTED]"

func mirrorsForRole(values []model.Mirror, role string) []model.Mirror {
	if role == "admin" {
		return values
	}
	redacted := make([]model.Mirror, len(values))
	for index, value := range values {
		redacted[index] = mirrorForRole(value, role)
	}
	return redacted
}

func mirrorForRole(value model.Mirror, role string) model.Mirror {
	if role == "admin" {
		return value
	}
	if value.HeaderAdd != nil {
		headers := make(map[string]string, len(value.HeaderAdd))
		for name := range value.HeaderAdd {
			headers[name] = redactedValue
		}
		value.HeaderAdd = headers
	}
	value.Upstreams = append([]model.Upstream(nil), value.Upstreams...)
	for index := range value.Upstreams {
		value.Upstreams[index].URL = redactURLQuery(value.Upstreams[index].URL)
		value.Upstreams[index].LastError = ""
	}
	if value.TokenUpstream != "" {
		value.TokenUpstream = redactedValue
	}
	value.ConfigError = ""
	return value
}

func healthResultsForRole(values []health.Result, role string) []health.Result {
	if role == "admin" {
		return values
	}
	redacted := make([]health.Result, len(values))
	copy(redacted, values)
	for index := range redacted {
		redacted[index].URL = redactURLQuery(redacted[index].URL)
		if redacted[index].Error != "" {
			redacted[index].Error = "health check failed; detailed diagnostics require an administrator"
		}
	}
	return redacted
}

// restoreRedactedMirrorSecrets lets an operator edit non-secret repository
// fields without replacing values that the API deliberately did not reveal.
// Static request headers and token endpoints are administrator-managed: an
// operator must send the returned sentinel for existing values and cannot add,
// remove, or rotate them.
func restoreRedactedMirrorSecrets(current model.Mirror, candidate *model.Mirror) error {
	if candidate == nil {
		return errors.New("repository is required")
	}
	if current.TokenUpstream == "" {
		if candidate.TokenUpstream != "" {
			return errors.New("repository token endpoint may only be changed by an administrator")
		}
	} else if candidate.TokenUpstream != redactedValue {
		return errors.New("repository token endpoint may only be changed by an administrator")
	}
	if len(candidate.HeaderAdd) != len(current.HeaderAdd) {
		return errors.New("repository static request headers may only be changed by an administrator")
	}
	for name := range current.HeaderAdd {
		if value, found := candidate.HeaderAdd[name]; !found || value != redactedValue {
			return errors.New("repository static request headers may only be changed by an administrator")
		}
	}
	preserveMirrorSecrets(current, candidate)
	for candidateIndex := range candidate.Upstreams {
		if candidate.Upstreams[candidateIndex].Host != "" {
			continue
		}
		for _, approved := range current.Upstreams {
			if candidate.Upstreams[candidateIndex].URL == approved.URL {
				candidate.Upstreams[candidateIndex].Host = approved.Host
				break
			}
		}
	}
	return nil
}

func preserveMirrorSecrets(current model.Mirror, candidate *model.Mirror) {
	candidate.TokenUpstream = current.TokenUpstream
	if current.HeaderAdd == nil {
		candidate.HeaderAdd = nil
		return
	}
	candidate.HeaderAdd = make(map[string]string, len(current.HeaderAdd))
	for name, value := range current.HeaderAdd {
		candidate.HeaderAdd[name] = value
	}
}

func hasMirrorSecrets(value model.Mirror) bool {
	if value.TokenUpstream != "" || len(value.HeaderAdd) != 0 {
		return true
	}
	for _, upstream := range value.Upstreams {
		parsed, err := url.Parse(upstream.URL)
		if err != nil || parsed.User != nil || parsed.RawQuery != "" {
			return true
		}
	}
	return false
}

func validateOperatorMirrorSecretBindings(current, candidate model.Mirror) error {
	if !hasMirrorSecrets(current) {
		return nil
	}
	approvedUpstreams := make(map[string]bool, len(current.Upstreams))
	for _, upstream := range current.Upstreams {
		approvedUpstreams[upstream.URL+"\x00"+upstream.Host] = true
	}
	for _, upstream := range candidate.Upstreams {
		if !approvedUpstreams[upstream.URL+"\x00"+upstream.Host] {
			return errors.New("credentialed repository upstream bindings may only be changed by an administrator")
		}
	}
	if candidate.Slug != current.Slug || candidate.Type != current.Type ||
		candidate.PublicMode != current.PublicMode || candidate.PublicHost != current.PublicHost || candidate.PublicPath != current.PublicPath ||
		candidate.ProxyMode != current.ProxyMode || candidate.CacheEnabled != current.CacheEnabled || candidate.CacheProfile != current.CacheProfile ||
		candidate.RewriteEnabled != current.RewriteEnabled || candidate.HTMLRewriteEnabled != current.HTMLRewriteEnabled ||
		candidate.RewriteProfile != current.RewriteProfile || !slices.Equal(candidate.RewriteHosts, current.RewriteHosts) ||
		candidate.HealthCheckPath != current.HealthCheckPath || candidate.HealthMethod != current.HealthMethod ||
		candidate.RedirectMode != current.RedirectMode || candidate.AccessPolicy != current.AccessPolicy ||
		candidate.StripPrefix != current.StripPrefix || candidate.AddPrefix != current.AddPrefix || candidate.HostRewrite != current.HostRewrite ||
		!slices.Equal(candidate.HeaderRemove, current.HeaderRemove) || candidate.MetadataLimitBytes != current.MetadataLimitBytes ||
		candidate.MetadataTTLSec != current.MetadataTTLSec || candidate.PackageTTLSec != current.PackageTTLSec ||
		candidate.ImmutableTTLSec != current.ImmutableTTLSec || candidate.BlobTTLSec != current.BlobTTLSec ||
		candidate.CacheAuthenticated != current.CacheAuthenticated || candidate.AuthMode != current.AuthMode ||
		candidate.BlobRedirectMode != current.BlobRedirectMode || candidate.PullOnly != current.PullOnly ||
		candidate.AllowHTTP != current.AllowHTTP || candidate.AllowPrivate != current.AllowPrivate ||
		!slices.Equal(candidate.BlockedPackages, current.BlockedPackages) || !slices.Equal(candidate.AllowedPackages, current.AllowedPackages) {
		return errors.New("credentialed repository security and routing bindings may only be changed by an administrator")
	}
	return nil
}

func configVersionForRole(value model.ConfigVersion, role string) model.ConfigVersion {
	if role != "admin" {
		value.Configuration = ""
		value.ValidationResult = ""
	}
	return value
}

func configVersionsForRole(values []model.ConfigVersion, role string) []model.ConfigVersion {
	if role == "admin" {
		return values
	}
	redacted := make([]model.ConfigVersion, len(values))
	for index, value := range values {
		redacted[index] = configVersionForRole(value, role)
	}
	return redacted
}

func upstreamNginxStatusForRole(value upstreamnginx.Status, role string) upstreamnginx.Status {
	if role != "admin" {
		value.PID = 0
		value.IntegrationSnippet = ""
		value.LastReloadResult = ""
		value.LastError = ""
		value.LastExitReason = ""
		value.IntegrationResult = ""
	}
	return value
}

func auditEntriesForRole(values []model.AuditEntry, role string) []model.AuditEntry {
	if role == "admin" {
		return values
	}
	redacted := make([]model.AuditEntry, len(values))
	copy(redacted, values)
	for index := range redacted {
		if !redacted[index].Succeeded {
			redacted[index].Detail = ""
		}
	}
	return redacted
}

func redactURLQuery(value string) string {
	parsed, err := url.Parse(value)
	if err != nil {
		return redactedValue
	}
	if parsed.User != nil {
		parsed.User = url.User(redactedValue)
	}
	if parsed.RawQuery != "" {
		parsed.RawQuery = "redacted=" + url.QueryEscape(redactedValue)
	}
	return parsed.String()
}
