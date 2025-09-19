package iscan

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/fho/rspamd-iscan/internal/log"
	"github.com/fho/rspamd-iscan/internal/rspamc"
	"github.com/fho/rspamd-iscan/internal/testutils/assert"
	"github.com/fho/rspamd-iscan/internal/testutils/imapserver"
	"github.com/fho/rspamd-iscan/internal/testutils/mail"
	"github.com/fho/rspamd-iscan/internal/testutils/mock"
)

func startServerClient(t *testing.T) (*imapserver.Server, *Client) {
	srv := imapserver.StartServer(t)
	return srv, newTestClient(t, srv)
}

func testClientCfg(t *testing.T, srv *imapserver.Server) *Config {
	return &Config{
		ServerAddr:                  srv.ListenAddr,
		User:                        srv.UserName,
		Password:                    srv.UserPasswd,
		AllowInsecureIMAPConnection: true,
		ScanMailbox:                 srv.ScanMailbox,
		InboxMailbox:                srv.InboxMailBox,
		BackupMailbox:               srv.BackupMailbox,
		HamMailbox:                  srv.HamMailbox,
		SpamMailboxName:             srv.SpamMailbox,
		UndetectedMailboxName:       srv.UndetectedMailbox,
		Logger:                      log.SlogTestLogger(t),
		Rspamc:                      mock.NewRspamc(),
		SpamTreshold:                10,
		TempDir:                     t.TempDir(),
	}
}

func newTestClient(t *testing.T, srv *imapserver.Server) *Client {
	var err error
	var clt *Client

	// we retry connecting because the server might not have finished
	// startup
	for range 9 {
		clt, err = NewClient(testClientCfg(t, srv))
		if err != nil {
			t.Logf("establishing connection to imap server failed (server still starting?): %s", err)
			time.Sleep(500 * time.Millisecond)
			continue
		}
		break
	}

	assert.NoError(t, err)

	t.Logf("connection to imap server established successfully")

	t.Cleanup(func() { _ = clt.Stop() })
	return clt
}

func TestProcessScanBox_DownloadAndScanFails(t *testing.T) {
	srv, clt := startServerClient(t)
	clt.rspamc = &mock.Rspamc{
		CheckFn: func(context.Context, io.Reader, *rspamc.MailHeaders) (*rspamc.CheckResult, error) {
			return nil, errors.New("mock err")
		},
	}

	err := clt.clt.Upload(mail.TestHamMailPath(t), srv.ScanMailbox, time.Now())
	assert.NoError(t, err)

	err = clt.clt.Upload(mail.TestHamMailPath(t), srv.ScanMailbox, time.Now())
	assert.NoError(t, err)

	err = clt.ProcessScanBox()
	assert.Error(t, err)

	clt.rspamc = mock.NewRspamc()

	// Ensure client is still usable
	err = clt.ProcessScanBox()
	assert.NoError(t, err)
}

func TestRun(t *testing.T) {
	srv, clt := startServerClient(t)
	clt.learnInterval = 100 * time.Millisecond

	runErrChan := make(chan error, 1)
	go func() {
		runErrChan <- clt.Monitor()
	}()

	clt2 := newTestClient(t, srv)

	err := clt2.clt.Upload(mail.TestHamMailPath(t), srv.ScanMailbox, time.Now())
	assert.NoError(t, err)

	err = clt2.clt.Upload(mail.TestSpamMailPath(t), srv.ScanMailbox, time.Now())
	assert.NoError(t, err)

	err = clt2.clt.Upload(mail.TestHamMailPath(t), srv.HamMailbox, time.Now())
	assert.NoError(t, err)

	err = clt2.clt.Upload(mail.TestSpamMailPath(t), srv.UndetectedMailbox, time.Now())
	assert.NoError(t, err)

	for clt.cntProcessedMails.Load() < 4 {
		time.Sleep(50 * time.Millisecond)
	}

	assert.Equal(t, true, mailboxIsEmpty(t, clt2.clt, srv.ScanMailbox))
	assert.Equal(t, true, mailboxIsEmpty(t, clt2.clt, srv.UndetectedMailbox))

	assert.Equal(t, 1,
		mailboxContainsMailCnt(t, clt2.clt, clt.backupMailbox, mail.HamMailSubject),
	)

	assert.Equal(t, 1,
		mailboxContainsMailCnt(t, clt2.clt, clt.backupMailbox, mail.SpamMailSubject),
	)

	assert.Equal(t, 2,
		mailboxContainsMailCnt(t, clt2.clt, clt.inboxMailbox, mail.HamMailSubject),
	)

	assert.Equal(t, 2,
		mailboxContainsMailCnt(t, clt2.clt, clt.spamMailbox, mail.SpamMailSubject),
	)

	assert.NoError(t, clt.Stop())
	err = <-runErrChan
	assert.NoError(t, err)
}

func mailboxIsEmpty(t *testing.T, clt IMAPClient, mailbox string) bool {
	for _, err := range clt.Messages(mailbox) {
		assert.NoError(t, err)
		return false
	}
	return true
}

func mailboxContainsMailCnt(
	t *testing.T,
	clt IMAPClient,
	mailbox string,
	mailSubject string,
) int {
	cnt := 0
	for msg, err := range clt.Messages(mailbox) {
		assert.NoError(t, err)
		if msg.Envelope.Subject == mailSubject {
			cnt++
		}
	}

	return cnt
}
