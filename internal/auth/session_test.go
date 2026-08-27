package auth

import (
	"fmt"
	"testing"
	"time"
)

func TestLoginLimiterStrictlyBoundsCapacity(t *testing.T) {
	limiter := NewLoginLimiter(time.Minute, 5)
	limiter.maxItems = 100

	// Fill the map with unexpired failure records.
	for i := 0; i < limiter.maxItems; i++ {
		key := fmt.Sprintf("user-%d", i)
		release, allowed := limiter.Acquire(key)
		if !allowed {
			t.Fatalf("key %s should be allowed", key)
		}
		release(false) // record failure
	}
	if release, allowed := limiter.Acquire("new-key-at-capacity"); allowed || release != nil {
		t.Fatal("a new key was admitted by evicting an unexpired limiter record")
	}

	limiter.mu.Lock()
	count := len(limiter.items)
	limiter.mu.Unlock()

	if count > limiter.maxItems {
		t.Fatalf("LoginLimiter items count %d exceeded maxItems %d", count, limiter.maxItems)
	}

	// The legacy direct Failure path must also remain bounded.
	for i := 100; i < 500; i++ {
		key := fmt.Sprintf("user-%d", i)
		limiter.Failure(key)
	}

	limiter.mu.Lock()
	count = len(limiter.items)
	limiter.mu.Unlock()

	if count > limiter.maxItems {
		t.Fatalf("LoginLimiter items count %d after Failure exceeded maxItems %d", count, limiter.maxItems)
	}
}
