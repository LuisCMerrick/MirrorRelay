package profile

import (
	"testing"

	"github.com/LuisCMerrick/RepoGate/internal/model"
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
