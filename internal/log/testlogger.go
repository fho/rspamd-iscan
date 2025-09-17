package log

import (
	"log/slog"
	"testing"
)

// SlogTestLogger returns a [*slog.Logger] instance that redirects it's output
// to [testing.T.Log]
func SlogTestLogger(t *testing.T) *slog.Logger {
	return slog.New(
		slog.NewTextHandler(t.Output(), &slog.HandlerOptions{
			Level: slog.LevelDebug,
		}),
	)
}
