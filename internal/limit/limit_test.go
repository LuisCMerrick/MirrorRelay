package limit

import (
	"sync"
	"testing"
)

func TestLimiter_Acquire(t *testing.T) {
	l := New(2, 1)

	// Acquire first
	release1, ok1 := l.Acquire("192.168.1.1", 1, 2)
	if !ok1 {
		t.Fatal("expected acquire to succeed")
	}

	// Per-IP limit is 1, so this should fail
	_, ok2 := l.Acquire("192.168.1.1", 1, 2)
	if ok2 {
		t.Fatal("expected acquire to fail due to per-IP limit")
	}

	// Acquire second from different IP
	release3, ok3 := l.Acquire("192.168.1.2", 1, 2)
	if !ok3 {
		t.Fatal("expected acquire to succeed")
	}

	// Total limit is 2, so this should fail
	_, ok4 := l.Acquire("192.168.1.3", 1, 2)
	if ok4 {
		t.Fatal("expected acquire to fail due to total limit")
	}

	// Release first
	release1()

	// Ensure cleanup happened
	l.mu.Lock()
	if l.byIP["192.168.1.1"] != 0 {
		t.Errorf("expected IP counter to be cleaned up")
	}
	l.mu.Unlock()

	// Try again
	_, ok5 := l.Acquire("192.168.1.1", 1, 2)
	if !ok5 {
		t.Fatal("expected acquire to succeed after release")
	}

	release3()
}

func TestLimiter_ConcurrentAccess(t *testing.T) {
	l := New(100, 100)
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			release, ok := l.Acquire("127.0.0.1", 1, 100)
			if ok {
				release()
			}
		}()
	}

	wg.Wait()

	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.byIP) != 0 || len(l.byMirror) != 0 || l.total != 0 {
		t.Errorf("expected all counters to be zero, got total=%d ip=%d mirror=%d", l.total, len(l.byIP), len(l.byMirror))
	}
}
