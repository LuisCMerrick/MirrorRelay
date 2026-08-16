// Package applog configures and initializes structured application logging.
package applog

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type Logger struct {
	dir     string
	queue   chan []byte
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
	logger := &Logger{dir: dir, queue: make(chan []byte, queueSize), maxSize: maxSizeMB << 20,
		keep: time.Duration(keepDays) * 24 * time.Hour, done: make(chan struct{})}
	logger.wg.Add(1)
	go logger.run()
	return logger
}

func (l *Logger) Write(value []byte) (int, error) {
	copyValue := append([]byte(nil), value...)
	select {
	case l.queue <- copyValue:
	default:
		l.dropped.Add(1)
	}
	return len(value), nil
}

func (l *Logger) Dropped() uint64 { return l.dropped.Load() }

func (l *Logger) Close() {
	close(l.done)
	l.wg.Wait()
}

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
			name := "application-" + date + ".jsonl"
			if sequence > 0 {
				name = fmt.Sprintf("application-%s.%d.jsonl", date, sequence)
			}
			opened, err := os.OpenFile(filepath.Join(l.dir, name), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o640)
			if err != nil {
				l.dropped.Add(1)
				return false
			}
			currentSize = 0
			if info, infoErr := opened.Stat(); infoErr == nil {
				currentSize = info.Size()
			}
			if currentSize < l.maxSize {
				file = opened
				return true
			}
			_ = opened.Close()
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
			if entry.IsDir() || !strings.HasPrefix(entry.Name(), "application-") || !strings.HasSuffix(entry.Name(), ".jsonl") {
				continue
			}
			if info, infoErr := entry.Info(); infoErr == nil && info.ModTime().Before(cutoff) {
				_ = os.Remove(filepath.Join(l.dir, entry.Name()))
			}
		}
	}
	write := func(value []byte) {
		date := time.Now().Format("2006-01-02")
		if date != currentDate {
			closeFile()
			currentDate, sequence = date, 0
			cleanup(time.Now())
			if !openFile(date) {
				return
			}
		}
		if currentSize+int64(len(value)) > l.maxSize {
			closeFile()
			sequence++
			if !openFile(date) {
				return
			}
		}
		written, err := file.Write(value)
		if err != nil {
			l.dropped.Add(1)
			return
		}
		currentSize += int64(written)
	}
	for {
		select {
		case value := <-l.queue:
			write(value)
		case <-l.done:
			for {
				select {
				case value := <-l.queue:
					write(value)
				default:
					return
				}
			}
		}
	}
}
