package retry

import (
	"errors"
	"fmt"
	"log/slog"
	"time"
)

type Runner struct {
	Fn                  func() error
	IsRetryable         func(error) bool
	MaxRetriesSameError int
	RetryIntervals      []time.Duration
	Logger              *slog.Logger

	lastError error
	failures  int
}

func (r *Runner) Run() error {
	for {
		err := r.Fn()
		if err == nil {
			return nil
		}

		r.failures++

		if !r.IsRetryable(err) {
			return fmt.Errorf("non-retryable error: %w", err)
		}

		if errors.Is(err, r.lastError) {
			if r.failures >= r.MaxRetriesSameError {
				return fmt.Errorf("max. number of retries (%d) exceeded: %w", r.failures, err)
			}
		} else {
			r.failures = 1
		}

		r.lastError = errors.Unwrap(err)

		sleepTime := r.sleepTime()

		r.Logger.Warn(
			"retryable error occurred, retrying after pause",
			"error", err,
			"failures", r.failures,
			"max_retries", r.MaxRetriesSameError,
			"pause", sleepTime,
		)

		time.Sleep(sleepTime)
	}
}

func (r *Runner) sleepTime() time.Duration {
	if r.failures-1 < len(r.RetryIntervals) {
		return r.RetryIntervals[r.failures-1]
	}

	return r.RetryIntervals[len(r.RetryIntervals)-1]
}
