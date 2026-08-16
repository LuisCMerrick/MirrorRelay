// Package upstreamnginxlog processes traffic logs from Managed Upstream Nginx.
package upstreamnginxlog

import (
	"testing"

	"github.com/LuisCMerrick/MirrorRelay/internal/model"
	"github.com/LuisCMerrick/MirrorRelay/internal/stats"
)

type fixedResolver struct{ mirror model.Mirror }

func (r fixedResolver) ResolveRequest(_, _ string) (model.Mirror, bool) { return r.mirror, true }

func TestConsumeRecordsManagedUpstreamNginxTrafficAndSkipsAdapter(t *testing.T) {
	metric := stats.New()
	tailer := New("", "127.0.0.1:9080", fixedResolver{model.Mirror{ID: 42}}, metric)
	tailer.consume(`2026-08-12T12:00:00+09:00 192.0.2.1 host=repo.example method=GET uri="/debian/pkg.deb" status=200 bytes=1024 request_time=0.200 upstream="203.0.113.10:443" upstream_status="200" upstream_time="0.150" cache=MISS`)
	tailer.consume(`2026-08-12T12:00:01+09:00 192.0.2.1 host=repo.example method=GET uri="/debian/pkg.deb" status=200 bytes=1024 request_time=0.001 upstream="203.0.113.10:443" upstream_status="200" upstream_time="0.000" cache=HIT`)
	tailer.consume(`2026-08-12T12:00:02+09:00 192.0.2.1 host=repo.example method=GET uri="/debian/" status=200 bytes=10 request_time=0.001 upstream="127.0.0.1:9080" upstream_status="200" upstream_time="0.001" cache=-`)
	snapshot := metric.Snapshot()
	counters := snapshot.ByMirror[42]
	if counters.Requests != 2 || counters.Bytes != 2048 || counters.CacheHits != 1 || counters.CacheMisses != 1 || counters.UpstreamBytes != 1024 || counters.CacheBytes != 1024 {
		t.Fatalf("unexpected counters: %+v", counters)
	}
}
