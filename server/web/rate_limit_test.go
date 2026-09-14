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

func TestTokenBucketAllowsBurstRefillsAndPrunesInactiveKeys(t *testing.T) {
	limiter := newRateLimiter()
	now := time.Date(2026, time.August, 10, 12, 0, 0, 0, time.UTC)
	if !limiter.takeBucket("device", 1, 2, now) || !limiter.takeBucket("device", 1, 2, now) {
		t.Fatal("initial burst was blocked")
	}
	if limiter.takeBucket("device", 1, 2, now) {
		t.Fatal("request beyond burst was accepted")
	}
	if !limiter.takeBucket("device", 1, 2, now.Add(time.Second)) {
		t.Fatal("one token was not refilled after one second")
	}
	if limiter.takeBucket("device", 1, 2, now.Add(time.Second)) {
		t.Fatal("bucket refilled more than its configured rate")
	}
	for index := 0; index < 1000; index++ {
		limiter.buckets[fmt.Sprintf("old:%d", index)] = tokenBucket{last: now}
	}
	limiter.takeBucket("new", 1, 1, now.Add(2*time.Hour))
	if len(limiter.buckets) > 2 {
		t.Fatalf("inactive token buckets were not pruned: %d", len(limiter.buckets))
	}
}
