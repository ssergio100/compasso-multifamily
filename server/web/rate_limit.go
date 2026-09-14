package web

import (
	"sync"
	"time"
)

type rateEntry struct {
	attempts     []time.Time
	blockedUntil time.Time
}

type rateLimiter struct {
	mu      sync.Mutex
	entries map[string]rateEntry
	buckets map[string]tokenBucket
}

type tokenBucket struct {
	tokens float64
	last   time.Time
}

func newRateLimiter() *rateLimiter {
	return &rateLimiter{entries: make(map[string]rateEntry), buckets: make(map[string]tokenBucket)}
}

func (l *rateLimiter) takeBucket(key string, ratePerSecond, burst float64, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	bucket, exists := l.buckets[key]
	if !exists || now.Before(bucket.last) {
		bucket = tokenBucket{tokens: burst, last: now}
	} else {
		bucket.tokens += now.Sub(bucket.last).Seconds() * ratePerSecond
		if bucket.tokens > burst {
			bucket.tokens = burst
		}
		bucket.last = now
	}
	if bucket.tokens < 1 {
		l.buckets[key] = bucket
		return false
	}
	bucket.tokens--
	l.buckets[key] = bucket
	if len(l.buckets) >= 1000 {
		for existingKey, existing := range l.buckets {
			if now.Sub(existing.last) > time.Hour {
				delete(l.buckets, existingKey)
			}
		}
	}
	return true
}

func (l *rateLimiter) take(key string, maximum int, window time.Duration, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.pruneLocked(now)
	entry := l.entries[key]
	entry.attempts = recentAttempts(entry.attempts, now.Add(-window))
	if len(entry.attempts) >= maximum {
		l.entries[key] = entry
		return false
	}
	entry.attempts = append(entry.attempts, now)
	l.entries[key] = entry
	return true
}

func (l *rateLimiter) loginAllowed(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.pruneLocked(now)
	entry := l.entries[key]
	if entry.blockedUntil.After(now) {
		return false
	}
	entry.attempts = recentAttempts(entry.attempts, now.Add(-15*time.Minute))
	l.entries[key] = entry
	return true
}

func (l *rateLimiter) loginFailure(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	entry := l.entries[key]
	entry.attempts = append(recentAttempts(entry.attempts, now.Add(-15*time.Minute)), now)
	if len(entry.attempts) >= 10 {
		entry.blockedUntil = now.Add(15 * time.Minute)
		entry.attempts = nil
	}
	l.entries[key] = entry
	return entry.blockedUntil.After(now)
}

func (l *rateLimiter) loginSuccess(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.entries, key)
}

func recentAttempts(values []time.Time, cutoff time.Time) []time.Time {
	first := 0
	for first < len(values) && values[first].Before(cutoff) {
		first++
	}
	return append([]time.Time(nil), values[first:]...)
}

func (l *rateLimiter) pruneLocked(now time.Time) {
	if len(l.entries) < 1000 {
		return
	}
	cutoff := now.Add(-15 * time.Minute)
	for key, entry := range l.entries {
		entry.attempts = recentAttempts(entry.attempts, cutoff)
		if len(entry.attempts) == 0 && !entry.blockedUntil.After(now) {
			delete(l.entries, key)
			continue
		}
		l.entries[key] = entry
	}
}
