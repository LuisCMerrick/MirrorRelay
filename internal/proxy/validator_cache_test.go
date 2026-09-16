package proxy

import (
	"net/http"
	"testing"
	"time"
)

func TestValidatorFreshnessNeverExceedsOriginLifetime(t *testing.T) {
	now := time.Date(2026, 9, 16, 0, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name      string
		header    http.Header
		remaining time.Duration
		cacheable bool
	}{
		{"default", http.Header{}, 5 * time.Minute, true},
		{"max-age", http.Header{"Cache-Control": {"public, max-age=60"}}, time.Minute, true},
		{"age", http.Header{"Cache-Control": {"max-age=60"}, "Age": {"20"}}, 40 * time.Second, true},
		{"old-date", http.Header{"Cache-Control": {"max-age=60"}, "Date": {now.Add(-70 * time.Second).Format(http.TimeFormat)}}, 0, false},
		{"expires", http.Header{"Expires": {now.Add(30 * time.Second).Format(http.TimeFormat)}}, 30 * time.Second, true},
		{"s-maxage", http.Header{"Cache-Control": {"max-age=60, s-maxage=10"}}, 10 * time.Second, true},
		{"gzip-vary", http.Header{"Vary": {"Accept, Accept-Encoding"}}, 5 * time.Minute, true},
		{"untracked-vary", http.Header{"Vary": {"Accept-Language"}}, 0, false},
		{"malformed-age", http.Header{"Age": {"invalid"}}, 0, false},
		{"malformed-max-age", http.Header{"Cache-Control": {"max-age=invalid"}}, 0, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			remaining, _, ok := validatorResponseFreshness(&http.Response{StatusCode: 200, Header: test.header}, now, 5*time.Minute)
			if ok != test.cacheable || (ok && remaining != test.remaining) {
				t.Fatalf("freshness=%v/%v want=%v/%v", remaining, ok, test.remaining, test.cacheable)
			}
		})
	}
}
