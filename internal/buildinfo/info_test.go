package buildinfo

import (
	"strings"
	"testing"
)

func TestInfoProvidesStableShortAndVerboseOutput(t *testing.T) {
	info := New("1.2.3", "0123456789abcdef", "2026-08-15T00:00:00Z", "")
	if info.Short() != "mirrorrelay 1.2.3 (build 1.2.3-0123456789ab)" || info.BuildID != "1.2.3-0123456789ab" {
		t.Fatalf("unexpected build information: %#v", info)
	}
	for _, expected := range []string{"MirrorRelay Version: 1.2.3", "Git Commit: 0123456789abcdef", "Target Architecture:", "Build ID: 1.2.3-0123456789ab"} {
		if !strings.Contains(info.Verbose(), expected) {
			t.Fatalf("verbose output does not contain %q:\n%s", expected, info.Verbose())
		}
	}
}

func TestEmptyVersionUsesInitialReleaseVersion(t *testing.T) {
	if info := New("", "", "", ""); info.Version != "0.0.1" {
		t.Fatalf("empty version fallback = %q", info.Version)
	}
}
