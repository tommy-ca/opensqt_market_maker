package retry

import (
	"context"
	"math/rand"
	"time"
)

// RetryPolicy defines how to retry an operation
type RetryPolicy struct {
	MaxAttempts    int
	InitialBackoff time.Duration
	MaxBackoff     time.Duration
}

// DefaultPolicy is a sensible default retry policy
var DefaultPolicy = RetryPolicy{
	MaxAttempts:    3,
	InitialBackoff: 100 * time.Millisecond,
	MaxBackoff:     2 * time.Second,
}

// IsTransientFunc defines if an error is transient and should be retried
type IsTransientFunc func(error) bool

// Do executes a function with retries according to the policy
func Do(ctx context.Context, policy RetryPolicy, isTransient IsTransientFunc, fn func() error) error {
	var err error
	backoff := policy.InitialBackoff

	for attempt := 0; attempt < policy.MaxAttempts; attempt++ {
		err = fn()
		if err == nil {
			return nil
		}

		if !isTransient(err) {
			return err
		}

		if attempt == policy.MaxAttempts-1 {
			break
		}

		// Calculate jittered backoff: backoff + random(0, 50% of backoff)
		jitter := time.Duration(rand.Int63n(int64(backoff / 2)))
		sleepTime := backoff + jitter

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(sleepTime):
			backoff = minDuration(backoff*2, policy.MaxBackoff)
		}
	}

	return err
}

func minDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}
