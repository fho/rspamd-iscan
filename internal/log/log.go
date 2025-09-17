package log

import "log/slog"

// EnsureLoggerInstance returns logger if it not nil.
// Otherwise a new logger that discards all output is returned.
func EnsureLoggerInstance(logger *slog.Logger) *slog.Logger {
	if logger == nil {
		return slog.New(slog.DiscardHandler)
	}

	return logger
}
