package web

import (
	"fmt"
	"testing"
	"time"
)

func TestRateLimiterAppliesWindowAndPrunesInactiveKeys(t *testing.T) {
	limiter := newRateLimiter()
	now := time.Date(2026, time.August, 10, 12, 0, 0, 0, time.UTC)
	for attempt := 0; attempt < 5; attempt++ {
		if !limiter.take("account:email", 5, 15*time.Minute, now) {
			t.Fatalf("attempt %d was blocked early", attempt+1)
		}
	}
	if limiter.take("account:email", 5, 15*time.Minute, now) {
		t.Fatal("sixth attempt was accepted")
	}
	if !limiter.take("account:email", 5, 15*time.Minute, now.Add(16*time.Minute)) {
		t.Fatal("expired window remained blocked")
	}
	for index := 0; index < 1000; index++ {
		limiter.entries[fmt.Sprintf("old:%d", index)] = rateEntry{attempts: []time.Time{now}}
	}
	limiter.take("new", 1, 15*time.Minute, now.Add(time.Hour))
	if len(limiter.entries) > 2 {
		t.Fatalf("inactive rate-limit keys were not pruned: %d", len(limiter.entries))
	}
}
