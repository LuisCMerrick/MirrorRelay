package auth

import (
	"testing"
	"time"
)

func TestPasswordRoundTrip(t *testing.T) {
	hash, err := HashPassword("SuperSecret123!")
	if err != nil {
		t.Fatal(err)
	}
	if !VerifyPassword(hash, "SuperSecret123!") {
		t.Fatal("password verification failed")
	}
	if VerifyPassword(hash, "wrong-password") {
		t.Fatal("wrong password was accepted")
	}
}

func TestLoginLimiterAtomicAcquire(t *testing.T) {
	limiter := NewLoginLimiter(time.Minute, 2)
	rel1, ok1 := limiter.Acquire("1.2.3.4")
	if !ok1 {
		t.Fatal("first acquire should succeed")
	}
	rel2, ok2 := limiter.Acquire("1.2.3.4")
	if !ok2 {
		t.Fatal("second acquire should succeed")
	}
	// Max inFlight is 2, third concurrent acquire should fail
	_, ok3 := limiter.Acquire("1.2.3.4")
	if ok3 {
		t.Fatal("third concurrent acquire should fail")
	}

	// Release with failure
	rel1(false)
	rel2(false)

	// Now 2 failures recorded, next acquire should fail even with 0 inFlight
	_, ok4 := limiter.Acquire("1.2.3.4")
	if ok4 {
		t.Fatal("acquire after max failures should fail")
	}

	// Success clears
	limiter.Success("1.2.3.4")
	rel5, ok5 := limiter.Acquire("1.2.3.4")
	if !ok5 {
		t.Fatal("acquire after success should succeed")
	}
	rel5(true)
}
