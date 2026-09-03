package profile

import (
	"testing"

	"github.com/LuisCMerrick/MirrorRelay/internal/model"
)

func TestFindDefaultsToLatestStableAndApplyPinsVersion(t *testing.T) {
	p, found := Find("docker hub", "")
	if !found || p.Version != "1.1.0" || !p.LatestStable {
		t.Fatalf("latest stable profile not selected: %+v", p)
	}
	m := model.Mirror{}
	if err := Apply(&m, "Docker Hub", ""); err != nil {
		t.Fatal(err)
	}
	if m.ProfileVersion != "1.1.0" || !m.CacheAuthenticated || len(m.RewriteHosts) != 3 {
		t.Fatalf("profile defaults were not pinned completely: %+v", m)
	}
}

func TestApplyDoesNotShareMutableHelpProfileState(t *testing.T) {
	var first, second model.Mirror
	if err := Apply(&first, "Debian", ""); err != nil {
		t.Fatal(err)
	}
	if err := Apply(&second, "Debian", ""); err != nil {
		t.Fatal(err)
	}
	first.Help.Variants[0].Label = "Mutated"
	first.Help.Formats[0].Label = "Mutated"
	if second.Help.Variants[0].Label == "Mutated" || second.Help.Formats[0].Label == "Mutated" {
		t.Fatal("profile application shared mutable Help slices")
	}
}
