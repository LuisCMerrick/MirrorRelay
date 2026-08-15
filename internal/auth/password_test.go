package auth

import "testing"

func TestPasswordRoundTrip(t *testing.T) {
	hash, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if !VerifyPassword(hash, "correct horse battery staple") {
		t.Fatal("password did not verify")
	}
	if VerifyPassword(hash, "wrong password") {
		t.Fatal("wrong password verified")
	}
}
