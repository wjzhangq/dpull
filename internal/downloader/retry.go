package downloader

import (
	"context"
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

type permanentError interface {
	Permanent() bool
}

// isRetryableError reports whether an error should trigger a retry.
// Canceled work and errors that mark themselves permanent stop immediately.
// Everything else still retries: a dropped connection is the common case.
func isRetryableError(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) {
		return false
	}
	var permanent permanentError
	if errors.As(err, &permanent) && permanent.Permanent() {
		return false
	}

	if errors.Is(err, ErrURLExpired) || errors.Is(err, ErrRateLimited) {
		return true
	}

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
