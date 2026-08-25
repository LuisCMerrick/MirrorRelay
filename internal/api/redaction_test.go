package api

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/LuisCMerrick/MirrorRelay/internal/config"
	"github.com/LuisCMerrick/MirrorRelay/internal/health"
	"github.com/LuisCMerrick/MirrorRelay/internal/model"
	"github.com/LuisCMerrick/MirrorRelay/internal/upstreamnginx"
)

func TestViewerTokenUpstreamIsAlwaysHidden(t *testing.T) {
	for _, tokenUpstream := range []string{
		"https://tokens.example/token?access_token=secret",
		"https://tokens.example/token/path-secret",
		"https://tokens.example/token/%73ecret",
		":// malformed token endpoint",
	} {
		redacted := mirrorForRole(model.Mirror{TokenUpstream: tokenUpstream}, "viewer")
		if redacted.TokenUpstream != redactedValue {
			t.Fatalf("viewer received token endpoint %q as %q", tokenUpstream, redacted.TokenUpstream)
		}
		operator := mirrorForRole(model.Mirror{TokenUpstream: tokenUpstream}, "operator")
		if operator.TokenUpstream != redactedValue {
			t.Fatalf("operator received token endpoint %q as %q", tokenUpstream, operator.TokenUpstream)
		}
		admin := mirrorForRole(model.Mirror{TokenUpstream: tokenUpstream}, "admin")
		if admin.TokenUpstream != tokenUpstream {
			t.Fatalf("admin token endpoint was unexpectedly redacted: %q", admin.TokenUpstream)
		}
	}
}

func TestNonAdminUpstreamCredentialsFailClosed(t *testing.T) {
	for _, upstream := range []struct {
		value      string
		prohibited []string
	}{
		{value: "https://user:password@packages.example/archive", prohibited: []string{"user", "password"}},
		{value: "https://packages.example/archive?signature=query-secret", prohibited: []string{"query-secret", "signature="}},
		{value: ":// malformed-secret", prohibited: []string{"malformed-secret"}},
	} {
		original := model.Mirror{Upstreams: []model.Upstream{{URL: upstream.value, LastError: "dial /secret/runtime/upstream.sock"}}}
		redacted := mirrorForRole(original, "operator")
		for _, secret := range upstream.prohibited {
			if strings.Contains(redacted.Upstreams[0].URL, secret) {
				t.Fatalf("upstream credential %q leaked from %q as %q", secret, upstream.value, redacted.Upstreams[0].URL)
			}
		}
		if original.Upstreams[0].URL != upstream.value {
			t.Fatalf("redaction mutated the stored upstream: got %q, want %q", original.Upstreams[0].URL, upstream.value)
		}
		if redacted.Upstreams[0].LastError != "" {
			t.Fatalf("operator received raw upstream diagnostics: %q", redacted.Upstreams[0].LastError)
		}
	}
}

func TestNonAdminHealthCheckResultsAreRedacted(t *testing.T) {
	values := []health.Result{{
		URL:   "https://packages.example/archive?signature=query-secret",
		Error: "dial unix /secret/runtime/upstream.sock: connect: permission denied",
	}}
	redacted := healthResultsForRole(values, "operator")
	if strings.Contains(redacted[0].URL, "query-secret") || strings.Contains(redacted[0].Error, "/secret/runtime") {
		t.Fatalf("operator health result leaked credentials or diagnostics: %+v", redacted[0])
	}
	if values[0].URL == redacted[0].URL || values[0].Error == redacted[0].Error {
		t.Fatalf("operator health result was not redacted: original=%+v redacted=%+v", values[0], redacted[0])
	}
	if admin := healthResultsForRole(values, "admin"); admin[0] != values[0] {
		t.Fatalf("administrator health result was unexpectedly redacted: %+v", admin[0])
	}
}

func TestNonAdminConfigurationDiagnosticsAreRedacted(t *testing.T) {
	repository := model.Mirror{ConfigState: "failed", ConfigError: "nginx -t failed in /secret/runtime/version/nginx.conf"}
	if redacted := mirrorForRole(repository, "operator"); redacted.ConfigState != "failed" || redacted.ConfigError != "" {
		t.Fatalf("operator repository diagnostics were not minimized: %+v", redacted)
	}
	version := model.ConfigVersion{Configuration: "proxy_set_header Authorization secret;", ValidationResult: "configuration file /secret/runtime/nginx.conf test failed"}
	redactedVersion := configVersionForRole(version, "viewer")
	if redactedVersion.Configuration != "" || redactedVersion.ValidationResult != "" {
		t.Fatalf("viewer configuration history leaked diagnostics: %+v", redactedVersion)
	}
	if admin := configVersionForRole(version, "admin"); admin.Configuration != version.Configuration || admin.ValidationResult != version.ValidationResult {
		t.Fatalf("administrator configuration diagnostics were unexpectedly redacted: %+v", admin)
	}
	status := upstreamnginx.Status{
		PID: 42, LastReloadResult: "nginx -t used /secret/runtime/nginx.conf", LastError: "secret failure",
		LastExitReason: "secret exit", IntegrationSnippet: "secret snippet", IntegrationResult: "secret integration path",
	}
	redactedStatus := upstreamNginxStatusForRole(status, "operator")
	if redactedStatus.PID != 0 || redactedStatus.LastReloadResult != "" || redactedStatus.LastError != "" ||
		redactedStatus.LastExitReason != "" || redactedStatus.IntegrationSnippet != "" || redactedStatus.IntegrationResult != "" {
		t.Fatalf("operator Nginx status leaked raw diagnostics: %+v", redactedStatus)
	}

	failure := errors.New("nginx -t failed in /secret/runtime/nginx.conf")
	if message := validationMessageForRole("secret validation output", failure, "operator"); strings.Contains(message, "secret") {
		t.Fatalf("operator validation error leaked diagnostics: %q", message)
	}
	if message := activationMessageForRole("reload failed", failure, "admin"); !strings.Contains(message, "/secret/runtime") {
		t.Fatalf("administrator activation error lost diagnostics: %q", message)
	}

	audit := []model.AuditEntry{
		{Detail: "failed in /secret/runtime", Succeeded: false},
		{Detail: "version 17", Succeeded: true},
	}
	redactedAudit := auditEntriesForRole(audit, "viewer")
	if redactedAudit[0].Detail != "" || redactedAudit[1].Detail != audit[1].Detail || audit[0].Detail == "" {
		t.Fatalf("non-admin audit redaction is incorrect: original=%+v redacted=%+v", audit, redactedAudit)
	}
}

func TestRestoreRedactedMirrorSecrets(t *testing.T) {
	current := model.Mirror{
		TokenUpstream:   "https://tokens.example/secret",
		HeaderAdd:       map[string]string{"Authorization": "Bearer secret", "X-Existing": "secret"},
		PublicPath:      "/private/",
		ProxyMode:       "transparent",
		RedirectMode:    "follow",
		AccessPolicy:    "admin",
		AuthMode:        "direct",
		PullOnly:        true,
		AllowedPackages: []string{"approved-*"},
		Upstreams:       []model.Upstream{{URL: "https://packages.example/repository/", Host: "packages.example"}},
	}
	candidate := model.Mirror{
		TokenUpstream:   redactedValue,
		PublicPath:      current.PublicPath,
		ProxyMode:       current.ProxyMode,
		RedirectMode:    current.RedirectMode,
		AccessPolicy:    current.AccessPolicy,
		AuthMode:        current.AuthMode,
		PullOnly:        current.PullOnly,
		AllowedPackages: append([]string(nil), current.AllowedPackages...),
		Upstreams:       []model.Upstream{{URL: current.Upstreams[0].URL}},
		HeaderAdd: map[string]string{
			"Authorization": redactedValue,
			"X-Existing":    redactedValue,
		},
	}
	if err := restoreRedactedMirrorSecrets(current, &candidate); err != nil {
		t.Fatal(err)
	}
	if candidate.TokenUpstream != current.TokenUpstream || candidate.HeaderAdd["Authorization"] != current.HeaderAdd["Authorization"] {
		t.Fatalf("redacted values were not restored: %+v", candidate)
	}
	if candidate.HeaderAdd["X-Existing"] != current.HeaderAdd["X-Existing"] {
		t.Fatalf("existing header value was not restored: %+v", candidate.HeaderAdd)
	}
	if candidate.Upstreams[0].Host != current.Upstreams[0].Host {
		t.Fatalf("approved upstream host binding was not restored: %+v", candidate.Upstreams)
	}
	if err := validateOperatorMirrorSecretBindings(current, candidate); err != nil {
		t.Fatalf("unchanged credential binding was rejected: %v", err)
	}

	candidate.HeaderAdd["Authorization"] = "replacement"
	if err := restoreRedactedMirrorSecrets(current, &candidate); err == nil {
		t.Fatal("operator was allowed to rotate a repository credential")
	}
	candidate.HeaderAdd["Authorization"] = redactedValue
	candidate.Upstreams[0] = model.Upstream{URL: "https://attacker.example/repository/", Host: "attacker.example"}
	if err := validateOperatorMirrorSecretBindings(current, candidate); err == nil {
		t.Fatal("operator was allowed to rebind a repository credential to another upstream")
	}

	for name, mutate := range map[string]func(*model.Mirror){
		"public access":         func(value *model.Mirror) { value.AccessPolicy = "public" },
		"public route":          func(value *model.Mirror) { value.PublicPath = "/exposed/" },
		"authenticated caching": func(value *model.Mirror) { value.CacheAuthenticated = true },
		"write methods":         func(value *model.Mirror) { value.PullOnly = false },
		"package policy":        func(value *model.Mirror) { value.AllowedPackages = nil },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := current
			candidate.Upstreams = append([]model.Upstream(nil), current.Upstreams...)
			mutate(&candidate)
			if err := validateOperatorMirrorSecretBindings(current, candidate); err == nil {
				t.Fatalf("operator was allowed to change %s for a credentialed repository", name)
			}
		})
	}
}

func TestUpstreamValidationErrorsNeverEchoTokenEndpoint(t *testing.T) {
	cfg := config.Default()
	cfg.Security.AllowPrivateUpstream = true
	server := &Server{cfg: cfg}
	tokenEndpoint := "https://127.0.0.1/private/token/path?access_token=repository-secret"
	repository := model.Mirror{
		TokenUpstream: tokenEndpoint,
		Upstreams:     []model.Upstream{{URL: "https://example.com/", Enabled: false}},
	}
	err := server.validateUpstreams(context.Background(), repository)
	if err == nil {
		t.Fatal("private token endpoint unexpectedly passed validation")
	}
	if strings.Contains(err.Error(), tokenEndpoint) || strings.Contains(err.Error(), "repository-secret") || strings.Contains(err.Error(), "/private/token/path") {
		t.Fatalf("token endpoint leaked through validation error: %v", err)
	}
}
