package imapserver

import "testing"

type imapServerLogger struct {
	t *testing.T
}

func (l *imapServerLogger) Printf(format string, args ...any) {
	l.t.Logf(format, args...)
}

func testLoggerAsImapServerLogger(t *testing.T) *imapServerLogger {
	return &imapServerLogger{t: t}
}
