package log

import "log/slog"

// SloggerWithGroup returns the logger with the given group, if logger is not
// nil.
// Otherwise it returns a new logger that discards all output.
func SloggerWithGroup(logger *slog.Logger, group string) *slog.Logger {
	if logger == nil {
		return slog.New(slog.DiscardHandler)
	}

	return logger.WithGroup(group)
}
