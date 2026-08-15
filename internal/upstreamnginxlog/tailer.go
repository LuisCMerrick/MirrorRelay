package upstreamnginxlog

import (
	"bufio"
	"context"
	"io"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/LuisCMerrick/RepoGate/internal/model"
	"github.com/LuisCMerrick/RepoGate/internal/stats"
)

type Resolver interface {
	ResolveRequest(host, path string) (model.Mirror, bool)
}

type Tailer struct {
	path     string
	resolver Resolver
	stats    *stats.Stats
	adapter  string
	offset   int64
	identity os.FileInfo
}

var linePattern = regexp.MustCompile(`host=([^ ]+) method=([^ ]+) uri="([^"]*)" status=([0-9]+) bytes=([0-9]+) request_time=([^ ]+) upstream="([^"]*)" upstream_status="([^"]*)" upstream_time="([^"]*)" cache=([^ ]+)`)

func New(path, adapter string, resolver Resolver, metric *stats.Stats) *Tailer {
	return &Tailer{path: path, adapter: adapter, resolver: resolver, stats: metric}
}

func (t *Tailer) Start(ctx context.Context) {
	if info, err := os.Stat(t.path); err == nil {
		t.offset, t.identity = info.Size(), info
	}
	go t.run(ctx)
}

func (t *Tailer) run(ctx context.Context) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			t.readAvailable()
		}
	}
}

func (t *Tailer) readAvailable() {
	file, err := os.Open(t.path)
	if err != nil {
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return
	}
	if t.identity != nil && (!os.SameFile(t.identity, info) || info.Size() < t.offset) {
		t.offset = 0
	}
	t.identity = info
	if _, err := file.Seek(t.offset, io.SeekStart); err != nil {
		return
	}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 4096), 1<<20)
	for scanner.Scan() {
		t.consume(scanner.Text())
		t.offset += int64(len(scanner.Bytes()) + 1)
	}
}

func (t *Tailer) consume(line string) {
	match := linePattern.FindStringSubmatch(line)
	if len(match) != 11 {
		return
	}
	if t.adapter != "" && strings.Contains(match[7], t.adapter) {
		return
	}
	parsedURI, err := url.ParseRequestURI(match[3])
	if err != nil {
		return
	}
	repository, ok := t.resolver.ResolveRequest(match[1], parsedURI.Path)
	if !ok {
		return
	}
	status, err1 := strconv.Atoi(match[4])
	bytesSent, err2 := strconv.ParseUint(match[5], 10, 64)
	upstreamSeconds, err3 := strconv.ParseFloat(lastValue(match[9]), 64)
	if err1 != nil || err2 != nil {
		return
	}
	cacheStatus := strings.ToUpper(strings.TrimSpace(match[10]))
	upstreamBytes, cacheBytes := bytesSent, uint64(0)
	if cacheStatus == "HIT" {
		upstreamBytes, cacheBytes = 0, bytesSent
	}
	upstreamError := status >= 500 || strings.HasPrefix(lastValue(match[8]), "5")
	latency := time.Duration(0)
	if err3 == nil {
		latency = time.Duration(upstreamSeconds * float64(time.Second))
	}
	t.stats.Record(repository.ID, status, bytesSent, upstreamBytes, cacheBytes, cacheStatus, upstreamError, latency)
}

func lastValue(value string) string {
	parts := strings.Split(value, ",")
	if len(parts) == 0 {
		return ""
	}
	return strings.TrimSpace(parts[len(parts)-1])
}
