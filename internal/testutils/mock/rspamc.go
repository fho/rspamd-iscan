package mock

import (
	"bytes"
	"context"
	"fmt"
	"io"

	"github.com/fho/rspamd-iscan/internal/rspamc"
)

type Rspamc struct {
	CheckFn func(context.Context, io.Reader) (*rspamc.CheckResult, error)
}

func NewRspamc() *Rspamc {
	return &Rspamc{
		CheckFn: CheckFnDefault,
	}
}

var SpamCheckResult = rspamc.CheckResult{
	Score: 100,
}

var SubjectRewriteResult = rspamc.CheckResult{
	Score:   6,
	Subject: "[SPAM] Claim your FREE reward NOW!!!",
}

func CheckFnDefault(_ context.Context, r io.Reader) (
	*rspamc.CheckResult, error,
) {
	buf, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("reading email in mock failed: %w", err)
	}
	switch {
	case bytes.Contains(buf, []byte("Test spam mail (GTUBE)")):
		return &SpamCheckResult, nil
	case bytes.Contains(buf, []byte("Claim your FREE reward NOW!!!")):
		return &SubjectRewriteResult, nil
	default:
		return &rspamc.CheckResult{}, nil
	}
}

func (c *Rspamc) Check(ctx context.Context, r io.Reader) (
	*rspamc.CheckResult, error,
) {
	return c.CheckFn(ctx, r)
}

func (*Rspamc) Spam(context.Context, io.Reader) error {
	return nil
}

func (*Rspamc) Ham(context.Context, io.Reader) error {
	return nil
}
