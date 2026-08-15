package accesslog

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type Entry struct {
	Time               time.Time `json:"time"`
	RequestID          string    `json:"request_id"`
	ClientIP           string    `json:"client_ip"`
	Mirror             string    `json:"mirror"`
	Method             string    `json:"method"`
	URI                string    `json:"uri"`
	Status             int       `json:"status"`
	Bytes              int64     `json:"bytes"`
	DurationMS         int64     `json:"duration_ms"`
	Upstream           string    `json:"upstream,omitempty"`
	UpstreamDurationMS int64     `json:"upstream_duration_ms,omitempty"`
	CacheStatus        string    `json:"cache_status"`
}

type Logger struct {
	dir     string
	queue   chan Entry
	maxSize int64
	keep    time.Duration
	dropped atomic.Uint64
	done    chan struct{}
	wg      sync.WaitGroup
}

func New(dir string, queueSize int, maxSizeMB int64, keepDays int) *Logger {
	if queueSize <= 0 {
		queueSize = 8192
	}
	if maxSizeMB <= 0 {
		maxSizeMB = 1024
	}
	if keepDays <= 0 {
		keepDays = 30
	}
	l := &Logger{dir: dir, queue: make(chan Entry, queueSize), maxSize: maxSizeMB << 20,
		keep: time.Duration(keepDays) * 24 * time.Hour, done: make(chan struct{})}
	l.wg.Add(1)
	go l.run()
	return l
}

func (l *Logger) Write(e Entry) {
	select {
	case l.queue <- e:
	default:
		l.dropped.Add(1)
	}
}

func (l *Logger) Dropped() uint64 { return l.dropped.Load() }

func (l *Logger) Close() { close(l.done); l.wg.Wait() }

func (l *Logger) run() {
	defer l.wg.Done()
	var file *os.File
	var currentDate string
	var currentSize int64
	var sequence int
	closeFile := func() {
		if file != nil {
			_ = file.Sync()
			_ = file.Close()
			file = nil
		}
	}
	defer closeFile()
	openFile := func(date string) bool {
		for {
			name := "access-" + date + ".jsonl"
			if sequence > 0 {
				name = fmt.Sprintf("access-%s.%d.jsonl", date, sequence)
			}
			path := filepath.Join(l.dir, name)
			f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o640)
			if err != nil {
				l.dropped.Add(1)
				return false
			}
			info, _ := f.Stat()
			currentSize = 0
			if info != nil {
				currentSize = info.Size()
			}
			if currentSize < l.maxSize {
				file = f
				return true
			}
			_ = f.Close()
			sequence++
		}
	}
	cleanup := func(now time.Time) {
		entries, err := os.ReadDir(l.dir)
		if err != nil {
			return
		}
		cutoff := now.Add(-l.keep)
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasPrefix(entry.Name(), "access-") || !strings.HasSuffix(entry.Name(), ".jsonl") {
				continue
			}
			if info, infoErr := entry.Info(); infoErr == nil && info.ModTime().Before(cutoff) {
				_ = os.Remove(filepath.Join(l.dir, entry.Name()))
			}
		}
	}
	write := func(e Entry) {
		date := e.Time.Format("2006-01-02")
		if date != currentDate {
			closeFile()
			currentDate, sequence = date, 0
			cleanup(e.Time)
			if !openFile(date) {
				return
			}
		}
		b, err := json.Marshal(e)
		if err != nil {
			l.dropped.Add(1)
			return
		}
		if currentSize+int64(len(b))+1 > l.maxSize {
			closeFile()
			sequence++
			if !openFile(date) {
				return
			}
		}
		written, err := fmt.Fprintf(file, "%s\n", b)
		if err != nil {
			l.dropped.Add(1)
			return
		}
		currentSize += int64(written)
	}
	for {
		select {
		case e := <-l.queue:
			write(e)
		case <-l.done:
			for {
				select {
				case e := <-l.queue:
					write(e)
				default:
					return
				}
			}
		}
	}
}
