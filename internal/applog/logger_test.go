package applog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoggerDrainsQueuedApplicationRecords(t *testing.T) {
	directory := t.TempDir()
	logger := New(directory, 4, 1, 1)
	record := []byte("{\"msg\":\"started\"}\n")
	if written, err := logger.Write(record); err != nil || written != len(record) {
		t.Fatalf("write = %d, %v", written, err)
	}
	logger.Close()
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 1 {
		t.Fatalf("application log files = %v, %v", entries, err)
	}
	content, err := os.ReadFile(filepath.Join(directory, entries[0].Name()))
	if err != nil || !strings.Contains(string(content), `"msg":"started"`) {
		t.Fatalf("application log content = %q, %v", content, err)
	}
}
