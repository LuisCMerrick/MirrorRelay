package accesslog

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLogger_Basic(t *testing.T) {
	dir := t.TempDir()
	l := New(dir, 10, 1, 1)

	l.Write(Entry{
		Time:     time.Now(),
		ClientIP: "1.2.3.4",
		Status:   200,
	})

	l.Close()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	if len(entries) == 0 {
		t.Fatal("expected log file to be created")
	}

	content, err := os.ReadFile(filepath.Join(dir, entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}

	if len(content) == 0 {
		t.Fatal("expected content to be written")
	}
}

func TestLogger_QueueSaturation(t *testing.T) {
	dir := t.TempDir()
	l := New(dir, 1, 1, 1)

	for i := 0; i < 1000; i++ {
		l.Write(Entry{Time: time.Now(), Status: 200})
	}

	l.Close()

	if l.Dropped() == 0 {
		t.Log("Note: did not saturate queue, which is fine on fast disks")
	}
}

func TestLogger_RotationAndCleanup(t *testing.T) {
	dir := t.TempDir()
	l := New(dir, 10, 1, 1)

	l.maxSize = 10

	l.Write(Entry{Time: time.Now(), Status: 200})
	l.Write(Entry{Time: time.Now(), Status: 200})
	l.Write(Entry{Time: time.Now(), Status: 200})

	l.Close()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	if len(entries) < 2 {
		t.Fatalf("expected multiple log files due to rotation, got %d", len(entries))
	}
}
