// Package stats collects and aggregates proxy performance metrics.
package stats

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type MirrorCounters struct {
	Requests       uint64 `json:"requests"`
	Bytes          uint64 `json:"bytes"`
	UpstreamBytes  uint64 `json:"upstream_bytes"`
	CacheBytes     uint64 `json:"cache_bytes"`
	CacheHits      uint64 `json:"cache_hits"`
	CacheMisses    uint64 `json:"cache_misses"`
	UpstreamErrors uint64 `json:"upstream_errors"`
	Status2xx      uint64 `json:"status_2xx"`
	Status3xx      uint64 `json:"status_3xx"`
	Status4xx      uint64 `json:"status_4xx"`
	Status5xx      uint64 `json:"status_5xx"`
}

type Snapshot struct {
	StartedAt    time.Time                `json:"started_at"`
	Active       int64                    `json:"active_requests"`
	Total        MirrorCounters           `json:"total"`
	Today        MirrorCounters           `json:"today"`
	Last24Hours  MirrorCounters           `json:"last_24_hours"`
	Last7Days    MirrorCounters           `json:"last_7_days"`
	ByMirror     map[int64]MirrorCounters `json:"by_mirror"`
	Status       map[int]uint64           `json:"status"`
	LatencySumMS uint64                   `json:"upstream_latency_sum_ms"`
	LatencyCount uint64                   `json:"upstream_latency_count"`
	Runtime      RuntimeMetrics           `json:"runtime"`
}

type RuntimeMetrics struct {
	HeapAllocBytes  uint64  `json:"heap_alloc_bytes"`
	HeapInUseBytes  uint64  `json:"heap_inuse_bytes"`
	HeapSystemBytes uint64  `json:"heap_system_bytes"`
	HeapObjects     uint64  `json:"heap_objects"`
	StackInUseBytes uint64  `json:"stack_inuse_bytes"`
	TotalAllocBytes uint64  `json:"total_alloc_bytes"`
	Mallocs         uint64  `json:"mallocs"`
	Frees           uint64  `json:"frees"`
	RSSBytes        uint64  `json:"rss_bytes"`
	Goroutines      int     `json:"goroutines"`
	OpenFDs         int     `json:"open_fds"`
	GCCount         uint32  `json:"gc_count"`
	GCPauseTotalNS  uint64  `json:"gc_pause_total_ns"`
	GCCPUFraction   float64 `json:"gc_cpu_fraction"`
}

type PersistentRecord struct {
	Hour     string
	MirrorID int64
	Counters MirrorCounters
}

type Store interface {
	LoadStatsHourly(context.Context, string) ([]PersistentRecord, error)
	SaveStatsHourly(context.Context, []PersistentRecord, string) error
}

type bucket struct {
	date     string
	counters MirrorCounters
	byMirror map[int64]MirrorCounters
	status   map[int]uint64
}

type Stats struct {
	started      time.Time
	active       atomic.Int64
	mu           sync.RWMutex
	total        MirrorCounters
	today        bucket
	hourly       map[string]MirrorCounters
	hourlyByID   map[string]map[int64]MirrorCounters
	daily        map[string]MirrorCounters
	latencySumMS uint64
	latencyCount uint64
	store        Store
}

func New() *Stats {
	now := time.Now()
	return &Stats{started: now, today: bucket{date: now.Format("2006-01-02"), byMirror: make(map[int64]MirrorCounters), status: make(map[int]uint64)},
		hourly: make(map[string]MirrorCounters), hourlyByID: make(map[string]map[int64]MirrorCounters), daily: make(map[string]MirrorCounters)}
}

func (s *Stats) Load(ctx context.Context, store Store) error {
	cutoff := time.Now().AddDate(0, 0, -8).Format("2006-01-02T15")
	records, err := store.LoadStatsHourly(ctx, cutoff)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.store = store
	today := time.Now().Format("2006-01-02")
	for _, record := range records {
		if len(record.Hour) < len("2006-01-02T15") {
			continue
		}
		hour := s.hourly[record.Hour]
		merge(&hour, record.Counters)
		s.hourly[record.Hour] = hour
		if s.hourlyByID[record.Hour] == nil {
			s.hourlyByID[record.Hour] = make(map[int64]MirrorCounters)
		}
		s.hourlyByID[record.Hour][record.MirrorID] = record.Counters
		dayKey := record.Hour[:10]
		day := s.daily[dayKey]
		merge(&day, record.Counters)
		s.daily[dayKey] = day
		merge(&s.total, record.Counters)
		if dayKey == today {
			mirror := s.today.byMirror[record.MirrorID]
			merge(&mirror, record.Counters)
			s.today.byMirror[record.MirrorID] = mirror
			merge(&s.today.counters, record.Counters)
		}
	}
	return nil
}

func (s *Stats) StartPersistence(ctx context.Context) {
	s.mu.RLock()
	configured := s.store != nil
	s.mu.RUnlock()
	if !configured {
		return
	}
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				flushCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				_ = s.Flush(flushCtx)
				cancel()
				return
			case <-ticker.C:
				flushCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
				_ = s.Flush(flushCtx)
				cancel()
			}
		}
	}()
}

func (s *Stats) Flush(ctx context.Context) error {
	s.mu.RLock()
	store := s.store
	records := make([]PersistentRecord, 0)
	for hour, values := range s.hourlyByID {
		for mirrorID, counters := range values {
			records = append(records, PersistentRecord{Hour: hour, MirrorID: mirrorID, Counters: counters})
		}
	}
	s.mu.RUnlock()
	if store == nil {
		return nil
	}
	sort.Slice(records, func(i, j int) bool {
		if records[i].Hour != records[j].Hour {
			return records[i].Hour < records[j].Hour
		}
		return records[i].MirrorID < records[j].MirrorID
	})
	return store.SaveStatsHourly(ctx, records, time.Now().AddDate(0, 0, -8).Format("2006-01-02T15"))
}

func (s *Stats) Begin() func() {
	s.active.Add(1)
	return func() { s.active.Add(-1) }
}

func add(dst *MirrorCounters, status int, bytes, upstreamBytes, cacheBytes uint64, hit, miss, upstreamErr bool) {
	dst.Requests++
	dst.Bytes += bytes
	dst.UpstreamBytes += upstreamBytes
	dst.CacheBytes += cacheBytes
	if hit {
		dst.CacheHits++
	}
	if miss {
		dst.CacheMisses++
	}
	if upstreamErr {
		dst.UpstreamErrors++
	}
	switch status / 100 {
	case 2:
		dst.Status2xx++
	case 3:
		dst.Status3xx++
	case 4:
		dst.Status4xx++
	case 5:
		dst.Status5xx++
	}
}

func (s *Stats) Record(mirrorID int64, status int, bytes, upstreamBytes, cacheBytes uint64, cacheStatus string, upstreamErr bool, upstreamLatency time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	today := time.Now().Format("2006-01-02")
	if s.today.date != today {
		s.today = bucket{date: today, byMirror: make(map[int64]MirrorCounters), status: make(map[int]uint64)}
	}
	hit, miss := cacheStatus == "HIT", cacheStatus == "MISS"
	add(&s.total, status, bytes, upstreamBytes, cacheBytes, hit, miss, upstreamErr)
	add(&s.today.counters, status, bytes, upstreamBytes, cacheBytes, hit, miss, upstreamErr)
	hourKey := time.Now().Format("2006-01-02T15")
	hour := s.hourly[hourKey]
	add(&hour, status, bytes, upstreamBytes, cacheBytes, hit, miss, upstreamErr)
	s.hourly[hourKey] = hour
	if s.hourlyByID[hourKey] == nil {
		s.hourlyByID[hourKey] = make(map[int64]MirrorCounters)
	}
	persistent := s.hourlyByID[hourKey][mirrorID]
	add(&persistent, status, bytes, upstreamBytes, cacheBytes, hit, miss, upstreamErr)
	s.hourlyByID[hourKey][mirrorID] = persistent
	day := s.daily[today]
	add(&day, status, bytes, upstreamBytes, cacheBytes, hit, miss, upstreamErr)
	s.daily[today] = day
	pruneCounters(s.hourly, time.Now().Add(-25*time.Hour).Format("2006-01-02T15"))
	pruneHourlyByMirror(s.hourlyByID, time.Now().AddDate(0, 0, -8).Format("2006-01-02T15"))
	pruneCounters(s.daily, time.Now().AddDate(0, 0, -8).Format("2006-01-02"))
	m := s.today.byMirror[mirrorID]
	add(&m, status, bytes, upstreamBytes, cacheBytes, hit, miss, upstreamErr)
	s.today.byMirror[mirrorID] = m
	s.today.status[status]++
	if upstreamLatency > 0 {
		s.latencySumMS += uint64(upstreamLatency.Milliseconds())
		s.latencyCount++
	}
}

func (s *Stats) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	byMirror := make(map[int64]MirrorCounters, len(s.today.byMirror))
	for k, v := range s.today.byMirror {
		byMirror[k] = v
	}
	statuses := make(map[int]uint64, len(s.today.status))
	for k, v := range s.today.status {
		statuses[k] = v
	}
	var last24, last7 MirrorCounters
	hourCutoff := time.Now().Add(-24 * time.Hour).Format("2006-01-02T15")
	for key, value := range s.hourly {
		if key >= hourCutoff {
			merge(&last24, value)
		}
	}
	dayCutoff := time.Now().AddDate(0, 0, -6).Format("2006-01-02")
	for key, value := range s.daily {
		if key >= dayCutoff {
			merge(&last7, value)
		}
	}
	return Snapshot{StartedAt: s.started, Active: s.active.Load(), Total: s.total, Today: s.today.counters, Last24Hours: last24, Last7Days: last7, ByMirror: byMirror, Status: statuses, LatencySumMS: s.latencySumMS, LatencyCount: s.latencyCount, Runtime: runtimeSnapshot()}
}

func runtimeSnapshot() RuntimeMetrics {
	var memory runtime.MemStats
	runtime.ReadMemStats(&memory)
	return RuntimeMetrics{
		HeapAllocBytes: memory.HeapAlloc, HeapInUseBytes: memory.HeapInuse, HeapSystemBytes: memory.HeapSys,
		HeapObjects: memory.HeapObjects, StackInUseBytes: memory.StackInuse, TotalAllocBytes: memory.TotalAlloc,
		Mallocs: memory.Mallocs, Frees: memory.Frees, RSSBytes: processRSS(), Goroutines: runtime.NumGoroutine(),
		OpenFDs: openFDCount(), GCCount: memory.NumGC, GCPauseTotalNS: memory.PauseTotalNs, GCCPUFraction: memory.GCCPUFraction,
	}
}

func processRSS() uint64 {
	content, err := os.ReadFile("/proc/self/statm")
	if err != nil {
		return 0
	}
	fields := strings.Fields(string(content))
	if len(fields) < 2 {
		return 0
	}
	pages, err := strconv.ParseUint(fields[1], 10, 64)
	if err != nil {
		return 0
	}
	return pages * uint64(os.Getpagesize())
}

func openFDCount() int {
	entries, err := os.ReadDir(filepath.Clean("/proc/self/fd"))
	if err != nil {
		return 0
	}
	return len(entries)
}

func merge(dst *MirrorCounters, value MirrorCounters) {
	dst.Requests += value.Requests
	dst.Bytes += value.Bytes
	dst.UpstreamBytes += value.UpstreamBytes
	dst.CacheBytes += value.CacheBytes
	dst.CacheHits += value.CacheHits
	dst.CacheMisses += value.CacheMisses
	dst.UpstreamErrors += value.UpstreamErrors
	dst.Status2xx += value.Status2xx
	dst.Status3xx += value.Status3xx
	dst.Status4xx += value.Status4xx
	dst.Status5xx += value.Status5xx
}

func pruneCounters(values map[string]MirrorCounters, cutoff string) {
	for key := range values {
		if key < cutoff {
			delete(values, key)
		}
	}
}

func pruneHourlyByMirror(values map[string]map[int64]MirrorCounters, cutoff string) {
	for key := range values {
		if key < cutoff {
			delete(values, key)
		}
	}
}

func escLabel(v string) string {
	v = strings.ReplaceAll(v, `\`, `\\`)
	v = strings.ReplaceAll(v, `"`, `\"`)
	return strings.ReplaceAll(v, "\n", `\n`)
}

func (s *Stats) Metrics(w http.ResponseWriter, names map[int64]string) {
	snap := s.Snapshot()
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	fmt.Fprintf(w, "# TYPE mirrorrelay_active_requests gauge\nmirrorrelay_active_requests %d\n", snap.Active)
	fmt.Fprintf(w, "# TYPE mirrorrelay_requests_total counter\nmirrorrelay_requests_total %d\n", snap.Total.Requests)
	fmt.Fprintf(w, "# TYPE mirrorrelay_bytes_total counter\nmirrorrelay_bytes_total %d\n", snap.Total.Bytes)
	fmt.Fprintf(w, "# TYPE mirrorrelay_upstream_bytes_total counter\nmirrorrelay_upstream_bytes_total %d\n", snap.Total.UpstreamBytes)
	fmt.Fprintf(w, "# TYPE mirrorrelay_cache_bytes_total counter\nmirrorrelay_cache_bytes_total %d\n", snap.Total.CacheBytes)
	fmt.Fprintf(w, "# TYPE mirrorrelay_upstream_errors_total counter\nmirrorrelay_upstream_errors_total %d\n", snap.Total.UpstreamErrors)
	fmt.Fprintf(w, "# TYPE mirrorrelay_cache_hit_total counter\nmirrorrelay_cache_hit_total %d\n", snap.Total.CacheHits)
	fmt.Fprintf(w, "# TYPE mirrorrelay_cache_miss_total counter\nmirrorrelay_cache_miss_total %d\n", snap.Total.CacheMisses)
	fmt.Fprintf(w, "# TYPE mirrorrelay_go_heap_alloc_bytes gauge\nmirrorrelay_go_heap_alloc_bytes %d\n", snap.Runtime.HeapAllocBytes)
	fmt.Fprintf(w, "# TYPE mirrorrelay_go_allocated_bytes_total counter\nmirrorrelay_go_allocated_bytes_total %d\n", snap.Runtime.TotalAllocBytes)
	fmt.Fprintf(w, "# TYPE mirrorrelay_go_mallocs_total counter\nmirrorrelay_go_mallocs_total %d\n", snap.Runtime.Mallocs)
	fmt.Fprintf(w, "# TYPE mirrorrelay_go_frees_total counter\nmirrorrelay_go_frees_total %d\n", snap.Runtime.Frees)
	fmt.Fprintf(w, "# TYPE mirrorrelay_process_resident_memory_bytes gauge\nmirrorrelay_process_resident_memory_bytes %d\n", snap.Runtime.RSSBytes)
	fmt.Fprintf(w, "# TYPE mirrorrelay_go_goroutines gauge\nmirrorrelay_go_goroutines %d\n", snap.Runtime.Goroutines)
	fmt.Fprintf(w, "# TYPE mirrorrelay_process_open_fds gauge\nmirrorrelay_process_open_fds %d\n", snap.Runtime.OpenFDs)
	fmt.Fprintf(w, "# TYPE mirrorrelay_go_gc_cycles_total counter\nmirrorrelay_go_gc_cycles_total %d\n", snap.Runtime.GCCount)
	fmt.Fprintf(w, "# TYPE mirrorrelay_go_gc_pause_seconds_total counter\nmirrorrelay_go_gc_pause_seconds_total %g\n", float64(snap.Runtime.GCPauseTotalNS)/float64(time.Second))
	fmt.Fprintf(w, "# TYPE mirrorrelay_go_gc_cpu_fraction gauge\nmirrorrelay_go_gc_cpu_fraction %g\n", snap.Runtime.GCCPUFraction)
	ids := make([]int64, 0, len(snap.ByMirror))
	for id := range snap.ByMirror {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		c := snap.ByMirror[id]
		labels := `repository_id="` + strconv.FormatInt(id, 10) + `",repository="` + escLabel(names[id]) + `"`
		fmt.Fprintf(w, "mirrorrelay_requests_today{%s} %d\nmirrorrelay_bytes_today{%s} %d\n", labels, c.Requests, labels, c.Bytes)
		fmt.Fprintf(w, "mirrorrelay_cache_hits_today{%s} %d\nmirrorrelay_cache_misses_today{%s} %d\n", labels, c.CacheHits, labels, c.CacheMisses)
		fmt.Fprintf(w, "mirrorrelay_upstream_errors_today{%s} %d\n", labels, c.UpstreamErrors)
		fmt.Fprintf(w, "mirrorrelay_http_responses_today{%s,class=\"2xx\"} %d\n", labels, c.Status2xx)
		fmt.Fprintf(w, "mirrorrelay_http_responses_today{%s,class=\"3xx\"} %d\n", labels, c.Status3xx)
		fmt.Fprintf(w, "mirrorrelay_http_responses_today{%s,class=\"4xx\"} %d\n", labels, c.Status4xx)
		fmt.Fprintf(w, "mirrorrelay_http_responses_today{%s,class=\"5xx\"} %d\n", labels, c.Status5xx)
	}
}
