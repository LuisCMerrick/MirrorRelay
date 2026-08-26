package auth

import (
	"fmt"
	"testing"
	"time"
)

func TestLoginLimiterStrictlyBoundsCapacity(t *testing.T) {
	limiter := NewLoginLimiter(time.Minute, 5)
	limiter.maxItems = 100 // Test with small maxItems

	// Insert 250 distinct keys
	for i := 0; i < 250; i++ {
		key := fmt.Sprintf("user-%d", i)
		release, allowed := limiter.Acquire(key)
		if !allowed {
			t.Fatalf("key %s should be allowed", key)
		}
		release(false) // record failure
	}

	limiter.mu.Lock()
	count := len(limiter.items)
	limiter.mu.Unlock()

	if count > limiter.maxItems {
		t.Fatalf("LoginLimiter items count %d exceeded maxItems %d", count, limiter.maxItems)
	}

	// Also test Failure directly
	for i := 250; i < 500; i++ {
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
