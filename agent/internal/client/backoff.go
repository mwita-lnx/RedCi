package client

import (
	"math/rand"
	"time"
)

// Backoff produces exponential delays with jitter, from min to max, for
// retrying after network errors (spec: 1s..30s with jitter).
type Backoff struct {
	Min, Max time.Duration
	cur      time.Duration
}

// NewBackoff returns a Backoff over [min, max].
func NewBackoff(min, max time.Duration) *Backoff {
	return &Backoff{Min: min, Max: max}
}

// Next returns the next delay and advances the state.
func (b *Backoff) Next() time.Duration {
	if b.cur == 0 {
		b.cur = b.Min
	} else {
		b.cur *= 2
		if b.cur > b.Max {
			b.cur = b.Max
		}
	}
	// Full jitter in [cur/2, cur].
	half := b.cur / 2
	return half + time.Duration(rand.Int63n(int64(half)+1))
}

// Reset returns to the minimum delay after a success.
func (b *Backoff) Reset() { b.cur = 0 }
