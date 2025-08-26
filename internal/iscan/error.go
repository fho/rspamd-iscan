package iscan

import (
	"errors"
	"log/slog"
	"net"
	"strings"
)

type ErrRetryable struct {
	err error
}

func (e *ErrRetryable) Error() string {
	return "ErrRetryable: " + e.err.Error()
}

func (e *ErrRetryable) UnwrapError() error {
	return e
}

func WrapRetryableError(err error) error {
	var ne *net.OpError

	if strings.Contains(err.Error(), "use of closed network connection") {
		return &ErrRetryable{err: err}
	}

	if strings.Contains(err.Error(), "read: permission denied") {
		return &ErrRetryable{err: err}
	}

	if errors.As(err, &ne) {
		if ne.Temporary() {
			return &ErrRetryable{err: err}
		}
		slog.Debug("errRetryable: not a temporary error: ", "error", ne.Error())
	}

	return err
}
