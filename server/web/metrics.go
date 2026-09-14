package web

import (
	"fmt"
	"net/http"
	"sync/atomic"
	"time"
)

var heartbeatDurationBuckets = [...]time.Duration{
	10 * time.Millisecond, 50 * time.Millisecond, 100 * time.Millisecond, 250 * time.Millisecond,
	500 * time.Millisecond, time.Second, 2 * time.Second, 5 * time.Second,
}

type heartbeatMetrics struct {
	accepted, rejected, rateLimited, serverErrors uint64
	durationCount, durationMicroseconds           uint64
	durationBuckets                               [len(heartbeatDurationBuckets)]uint64
}

func (m *heartbeatMetrics) record(status int, duration time.Duration) {
	switch {
	case status == http.StatusTooManyRequests:
		atomic.AddUint64(&m.rateLimited, 1)
	case status >= 500:
		atomic.AddUint64(&m.serverErrors, 1)
	case status >= 400:
		atomic.AddUint64(&m.rejected, 1)
	default:
		atomic.AddUint64(&m.accepted, 1)
	}
	atomic.AddUint64(&m.durationCount, 1)
	atomic.AddUint64(&m.durationMicroseconds, uint64(duration.Microseconds()))
	for index, upperBound := range heartbeatDurationBuckets {
		if duration <= upperBound {
			atomic.AddUint64(&m.durationBuckets[index], 1)
		}
	}
}

func (a *App) metricsAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = fmt.Fprintf(w, "# TYPE compasso_heartbeats_total counter\n")
	_, _ = fmt.Fprintf(w, "compasso_heartbeats_total{result=\"accepted\"} %d\n", atomic.LoadUint64(&a.metrics.accepted))
	_, _ = fmt.Fprintf(w, "compasso_heartbeats_total{result=\"rejected\"} %d\n", atomic.LoadUint64(&a.metrics.rejected))
	_, _ = fmt.Fprintf(w, "compasso_heartbeats_total{result=\"rate_limited\"} %d\n", atomic.LoadUint64(&a.metrics.rateLimited))
	_, _ = fmt.Fprintf(w, "compasso_heartbeats_total{result=\"server_error\"} %d\n", atomic.LoadUint64(&a.metrics.serverErrors))
	_, _ = fmt.Fprintf(w, "# TYPE compasso_heartbeat_duration_seconds histogram\n")
	for index, upperBound := range heartbeatDurationBuckets {
		_, _ = fmt.Fprintf(w, "compasso_heartbeat_duration_seconds_bucket{le=\"%.3f\"} %d\n", upperBound.Seconds(), atomic.LoadUint64(&a.metrics.durationBuckets[index]))
	}
	count := atomic.LoadUint64(&a.metrics.durationCount)
	_, _ = fmt.Fprintf(w, "compasso_heartbeat_duration_seconds_bucket{le=\"+Inf\"} %d\n", count)
	_, _ = fmt.Fprintf(w, "compasso_heartbeat_duration_seconds_sum %.6f\n", float64(atomic.LoadUint64(&a.metrics.durationMicroseconds))/1_000_000)
	_, _ = fmt.Fprintf(w, "compasso_heartbeat_duration_seconds_count %d\n", count)
}
