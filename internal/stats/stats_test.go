package stats

import (
	"bytes"
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type memoryStatsStore struct {
	loaded []PersistentRecord
	saved  []PersistentRecord
	before string
}

func (s *memoryStatsStore) LoadStatsHourly(context.Context, string) ([]PersistentRecord, error) {
	return append([]PersistentRecord(nil), s.loaded...), nil
}

func (s *memoryStatsStore) SaveStatsHourly(_ context.Context, records []PersistentRecord, before string) error {
	s.saved = append([]PersistentRecord(nil), records...)
	s.before = before
	return nil
}

func TestHourlyStatisticsSurviveReloadAndFlush(t *testing.T) {
	hour := time.Now().Format("2006-01-02T15")
	store := &memoryStatsStore{loaded: []PersistentRecord{{Hour: hour, MirrorID: 7, Counters: MirrorCounters{Requests: 3, Bytes: 30, CacheHits: 2, Status2xx: 2, Status4xx: 1}}}}
	metric := New()
	if err := metric.Load(context.Background(), store); err != nil {
		t.Fatal(err)
	}
	metric.Record(7, 200, 10, 0, 10, "HIT", false, time.Millisecond)
	snapshot := metric.Snapshot()
	if snapshot.Today.Requests != 4 || snapshot.ByMirror[7].Bytes != 40 || snapshot.ByMirror[7].CacheHits != 3 ||
		snapshot.ByMirror[7].Status2xx != 3 || snapshot.ByMirror[7].Status4xx != 1 {
		t.Fatalf("loaded counters were not merged: %+v", snapshot)
	}
	if err := metric.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(store.saved) != 1 || store.saved[0].Counters.Requests != 4 || store.before == "" {
		t.Fatalf("unexpected persisted records: %+v cutoff=%q", store.saved, store.before)
	}
}

func TestMetricsUseMirrorRelayNamespace(t *testing.T) {
	metric := New()
	metric.Record(7, 200, 10, 0, 10, "HIT", false, time.Millisecond)
	recorder := httptest.NewRecorder()
	metric.Metrics(recorder, map[int64]string{7: "example"})
	body := recorder.Body.String()
	for _, expected := range []string{
		"mirrorrelay_requests_total 1",
		`mirrorrelay_requests_today{repository_id="7",repository="example"} 1`,
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("metrics do not contain %q:\n%s", expected, body)
		}
	}
	if bytes.Contains(recorder.Body.Bytes(), []byte("mirror_")) {
		t.Fatalf("metrics contain the previous namespace:\n%s", body)
	}
}
