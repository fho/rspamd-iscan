package imapclt

import (
	"context"
	"log/slog"
	"runtime"
	"time"
)

type DebugWriter struct {
	l *slog.Logger
}

func NewDebugWriter(l *slog.Logger) *DebugWriter {
	return &DebugWriter{l: l}
}

func (w *DebugWriter) Write(p []byte) (n int, err error) {
	var pcs [1]uintptr

	runtime.Callers(2, pcs[:])

	r := slog.NewRecord(time.Now(), slog.LevelDebug, string(p), pcs[0])
	err = w.l.Handler().Handle(context.Background(), r)
	if err != nil {
		return 0, err
	}

	return len(p), nil
}
