package downloader

import (
	"errors"
	"fmt"
	"time"

	"github.com/cenkalti/backoff/v4"
	"github.com/wjzhangq/dpull/internal/registry"
)

var (
	// ErrURLExpired indicates that a presigned URL has expired
	ErrURLExpired = errors.New("URL expired")

	// ErrRateLimited indicates a 429 response
	ErrRateLimited = errors.New("rate limited")
)

// retryConfig returns a backoff configuration for piece downloads
func retryConfig(maxRetries int) backoff.BackOff {
	b := backoff.NewExponentialBackOff()
	b.InitialInterval = 1 * time.Second
	b.MaxInterval = 30 * time.Second
	b.MaxElapsedTime = 0 // No time limit, only retry count matters

	return backoff.WithMaxRetries(b, uint64(maxRetries))
}

// isRetryableError reports whether an error should trigger a retry
func isRetryableError(err error) bool {
	if err == nil {
		return false
	}

	// Always retry these
	if errors.Is(err, ErrURLExpired) || errors.Is(err, ErrRateLimited) {
		return true
	}

	// Network errors are generally retryable
	return true
}

// RetryWithBackoff retries an operation with exponential backoff
func RetryWithBackoff(op func() error, maxRetries int) error {
	if maxRetries == 0 {
		return op()
	}

	b := retryConfig(maxRetries)
	var lastErr error
	dialFails := 0

	for {
		lastErr = op()
		if lastErr == nil {
			return nil
		}

		if !isRetryableError(lastErr) {
			return lastErr
		}
		// A host that never answers should fail over instead of backing off
		// through the full retry budget.
		if registry.IsDialError(lastErr) {
			dialFails++
			if dialFails >= 2 {
				return fmt.Errorf("host unreachable: %w", lastErr)
			}
		}

		wait := b.NextBackOff()
		if wait == backoff.Stop {
			return fmt.Errorf("max retries exceeded: %w", lastErr)
		}

		time.Sleep(wait)
	}
}
