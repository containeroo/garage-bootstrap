// Package backoff calculates retry intervals.
package backoff

import (
	"math"
	"time"
)

// Mode selects retry interval growth.
type Mode string

const (
	// ModeLinear keeps the base interval.
	ModeLinear Mode = "linear"
	// ModeExponential doubles the interval.
	ModeExponential Mode = "exponential"
)

// NextInterval returns a capped retry interval.
func NextInterval(mode Mode, base time.Duration, attempt int, limit time.Duration) time.Duration {
	if base <= 0 {
		return 0
	}
	if mode != ModeExponential || attempt <= 1 {
		return capInterval(base, limit)
	}

	interval := base
	for range attempt - 1 {
		if interval > time.Duration(math.MaxInt64)/2 {
			return capInterval(time.Duration(math.MaxInt64), limit)
		}
		interval *= 2
		if limit > 0 && interval >= limit {
			return limit
		}
	}
	return capInterval(interval, limit)
}

// capInterval caps interval when limit is positive.
func capInterval(interval, limit time.Duration) time.Duration {
	if limit > 0 && interval > limit {
		return limit
	}
	return interval
}
