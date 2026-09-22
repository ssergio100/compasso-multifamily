package companion

import (
	"testing"
	"time"
)

func TestRestartDelayUsesBoundedExponentialBackoff(t *testing.T) {
	tests := []struct {
		failures int
		want     time.Duration
	}{
		{failures: 0, want: 0},
		{failures: 1, want: 250 * time.Millisecond},
		{failures: 2, want: 500 * time.Millisecond},
		{failures: 5, want: 4 * time.Second},
		{failures: 6, want: 5 * time.Second},
		{failures: 20, want: 5 * time.Second},
	}
	for _, test := range tests {
		if got := restartDelay(test.failures); got != test.want {
			t.Fatalf("restartDelay(%d)=%s, want %s", test.failures, got, test.want)
		}
	}
}
