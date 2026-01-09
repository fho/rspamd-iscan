package retry

import (
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/fho/rspamd-iscan/internal/log"
	"github.com/fho/rspamd-iscan/internal/testutils/assert"
)

func TestRun_SuccessOnFirstTry(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		calls := 0
		r := &Runner{
			Fn: func() error {
				calls++
				return nil
			},
			IsRetryable:         func(error) bool { return true },
			MaxRetriesSameError: 3,
			RetryIntervals:      []time.Duration{time.Second},
			Logger:              log.SlogTestLogger(t),
		}

		err := r.Run()
		assert.NoError(t, err)
		assert.Equal(t, 1, calls)
	})
}

func TestRun_SuccessAfterRetries(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		calls := 0
		runErr := errors.New("error")
		r := &Runner{
			Fn: func() error {
				calls++
				if calls < 3 {
					return runErr
				}
				return nil
			},
			IsRetryable:         func(error) bool { return true },
			MaxRetriesSameError: 4,
			RetryIntervals:      []time.Duration{time.Second},
			Logger:              log.SlogTestLogger(t),
		}

		err := r.Run()
		assert.NoError(t, err)
		assert.Equal(t, 3, calls)
	})
}

func TestRun_NonRetryableError(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		calls := 0
		permanentErr := errors.New("permanent error")
		r := &Runner{
			Fn: func() error {
				calls++
				return permanentErr
			},
			IsRetryable:         func(error) bool { return false },
			MaxRetriesSameError: 5,
			RetryIntervals:      []time.Duration{time.Second},
			Logger:              log.SlogTestLogger(t),
		}

		err := r.Run()
		assert.Error(t, err)
		assert.Equal(t, true, errors.Is(err, permanentErr))
		assert.Equal(t, 1, calls)
	})
}

func TestRun_MaxRetriesExceeded(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		calls := 0
		sameErr := errors.New("same error")
		r := &Runner{
			Fn: func() error {
				calls++
				return &retryableError{err: sameErr}
			},
			IsRetryable:         func(error) bool { return true },
			MaxRetriesSameError: 3,
			RetryIntervals:      []time.Duration{time.Millisecond},
			Logger:              log.SlogTestLogger(t),
		}

		err := r.Run()
		assert.Error(t, err)
		assert.Equal(t, true, errors.Is(err, sameErr))
		assert.Equal(t, 3, calls)
	})
}

func TestRun_DifferentErrorsResetCounter(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		calls := 0
		r := &Runner{
			Fn: func() error {
				calls++
				// Return different errors, then succeed
				if calls <= 6 {
					return &retryableError{err: errors.New("error " + string(rune('a'+calls-1)))}
				}
				return nil
			},
			IsRetryable:         func(error) bool { return true },
			MaxRetriesSameError: 3,
			RetryIntervals:      []time.Duration{time.Millisecond},
			Logger:              log.SlogTestLogger(t),
		}

		err := r.Run()
		assert.NoError(t, err)
		assert.Equal(t, 7, calls)
	})
}

func TestRun_PauseTimes(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		intervals := []time.Duration{
			1 * time.Millisecond,
			2 * time.Millisecond,
			3 * time.Millisecond,
		}
		var sleepTimes []time.Duration
		calls := 0
		lastTime := time.Now()
		sameErr := errors.New("error")

		r := &Runner{
			Fn: func() error {
				calls++
				now := time.Now()
				if calls > 1 {
					sleepTimes = append(sleepTimes, now.Sub(lastTime))
				}
				lastTime = now
				if calls < 6 {
					return &retryableError{err: sameErr}
				}
				return nil
			},
			IsRetryable:         func(error) bool { return true },
			MaxRetriesSameError: 10,
			RetryIntervals:      intervals,
			Logger:              log.SlogTestLogger(t),
		}

		err := r.Run()
		assert.NoError(t, err)

		expected := []time.Duration{
			1 * time.Millisecond,
			2 * time.Millisecond,
			3 * time.Millisecond,
			3 * time.Millisecond,
			3 * time.Millisecond,
		}
		assert.Equal(t, len(expected), len(sleepTimes))
		for i, exp := range expected {
			assert.Equal(t, exp, sleepTimes[i])
		}
	})
}

type retryableError struct {
	err error
}

func (e *retryableError) Error() string {
	return "retryable: " + e.err.Error()
}

func (e *retryableError) Unwrap() error {
	return e.err
}
