package api

import (
	"testing"

	"github.com/LuisCMerrick/MirrorRelay/internal/model"
)

func TestViewerTokenUpstreamIsAlwaysHidden(t *testing.T) {
	for _, tokenUpstream := range []string{
		"https://tokens.example/token?access_token=secret",
		"https://tokens.example/token/path-secret",
		"https://tokens.example/token/%73ecret",
		":// malformed token endpoint",
	} {
		redacted := mirrorForRole(model.Mirror{TokenUpstream: tokenUpstream}, "viewer")
		if redacted.TokenUpstream != "" {
			t.Fatalf("viewer received token endpoint %q as %q", tokenUpstream, redacted.TokenUpstream)
		}
	}
}
