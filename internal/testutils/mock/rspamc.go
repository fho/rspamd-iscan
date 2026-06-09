package mock

import (
	"context"
	"io"

	"github.com/fho/rspamd-iscan/internal/rspamc"
)

type Rspamc struct {
	CheckFn func(context.Context, io.Reader, *rspamc.MailHeaders) (*rspamc.CheckResult, error)
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
	Score: 6,
	Subject: "[SPAM] Claim your FREE reward NOW!!!",
}

// func CheckFnAlwaysSpam(context.Context, io.Reader, *rspamc.MailHeaders) (
// 	*rspamc.CheckResult, error,
// ) {
// 	return &SpamCheckResult, nil
// }

func CheckFnDefault(_ context.Context, _ io.Reader, hdr *rspamc.MailHeaders) (
	*rspamc.CheckResult, error,
) {
	switch hdr.Subject {
	case "Test spam mail (GTUBE)":
		return &SpamCheckResult, nil
	case "Claim your FREE reward NOW!!!":
		return &SubjectRewriteResult, nil
	default:
		return &rspamc.CheckResult{}, nil
	}
}

func (c *Rspamc) Check(ctx context.Context, r io.Reader, hdr *rspamc.MailHeaders) (
	*rspamc.CheckResult, error,
) {
	return c.CheckFn(ctx, r, hdr)
}

func (*Rspamc) Spam(context.Context, io.Reader, *rspamc.MailHeaders) error {
	return nil
}

func (*Rspamc) Ham(context.Context, io.Reader, *rspamc.MailHeaders) error {
	return nil
}
