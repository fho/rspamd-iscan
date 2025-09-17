package iscan

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fho/rspamd-iscan/internal/imapclt"
	"github.com/fho/rspamd-iscan/internal/log"
	"github.com/fho/rspamd-iscan/internal/mail"
	"github.com/fho/rspamd-iscan/internal/rspamc"
)

const (
	hdrScanSymbolPrefix = "X-rspamd-iscan-"
)

type RspamdClient interface {
	Check(context.Context, io.Reader, *rspamc.MailHeaders) (*rspamc.CheckResult, error)
	Spam(context.Context, io.Reader, *rspamc.MailHeaders) error
	Ham(context.Context, io.Reader, *rspamc.MailHeaders) error
}

type Client struct {
	clt    *imapclt.Client
	logger *slog.Logger

	stopCh   chan struct{}
	stopOnce sync.Once
	wgRun    sync.WaitGroup

	scanMailbox       string
	inboxMailbox      string
	spamMailbox       string
	hamMailbox        string
	backupMailbox     string
	undetectedMailbox string
	spamTreshold      float32

	tempDir       string
	keepTempFiles bool

	learnInterval time.Duration

	rspamc RspamdClient

	// cntScannedMails is only used in testcases
	cntScannedMails atomic.Uint64
}

type scannedMail struct {
	Path        string
	UID         uint32
	Envelope    *imapclt.Envelope
	CheckResult *rspamc.CheckResult
}

type learnFn func(context.Context, io.Reader, *rspamc.MailHeaders) error

func NewClient(cfg *Config) (*Client, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	c := &Client{
		logger:            log.SloggerWithGroup(cfg.Logger, "iscan"),
		inboxMailbox:      cfg.InboxMailbox,
		scanMailbox:       cfg.ScanMailbox,
		spamMailbox:       cfg.SpamMailboxName,
		hamMailbox:        cfg.HamMailbox,
		undetectedMailbox: cfg.UndetectedMailboxName,
		rspamc:            cfg.Rspamc,
		spamTreshold:      cfg.SpamTreshold,
		learnInterval:     30 * time.Minute,
		backupMailbox:     cfg.BackupMailbox,
		tempDir:           cfg.TempDir,
		keepTempFiles:     cfg.KeepTempFiles,
		stopCh:            make(chan struct{}),
	}

	clt, err := imapclt.Connect(&imapclt.Config{
		Address:       cfg.ServerAddr,
		User:          cfg.User,
		Password:      cfg.Password,
		AllowInsecure: cfg.AllowInsecureIMAPConnection,
		Logger:        c.logger,
	})
	if err != nil {
		return nil, err
	}

	c.clt = clt

	return c, nil
}

func (c *Client) ProcessHam() error {
	if c.hamMailbox == "" {
		return nil
	}

	return c.learn(c.hamMailbox, c.inboxMailbox, c.rspamc.Ham)
}

func (c *Client) ProcessSpam() error {
	if c.undetectedMailbox == "" {
		return nil
	}

	return c.learn(c.undetectedMailbox, c.spamMailbox, c.rspamc.Spam)
}

func (c *Client) learn(srcMailbox, destMailbox string, learnFn learnFn) error {
	//nolint:prealloc // number of mails is unknown before iterating
	var learnedMsgUIDs []uint32

	logger := c.logger.With("mailbox.source", srcMailbox)

	logger.Info("checking mailbox for new messages to learn")

	for msg, err := range c.clt.Messages(srcMailbox) {
		if err != nil {
			return fmt.Errorf("fetching messages from imap mailbox failed: %w", err)
		}

		logger := c.logger.With("mail.subject", msg.Envelope.Subject, "mail.uid", msg.UID)
		logger.Debug("fetched message")

		// TODO: retry Check if it failed with a temporary error
		err = learnFn(
			context.TODO(),
			msg.Message,
			envelopeToRspamcHdrs(&msg.Envelope),
		)
		if err != nil {
			logger.Warn("learning message failed", "error", err,
				"event", "rspamd.msg_learn_failed")
			return nil
		}

		logger.Info("learned message", "event", "rspamd.msg_learned")
		learnedMsgUIDs = append(learnedMsgUIDs, msg.UID)
	}

	if len(learnedMsgUIDs) == 0 {
		return nil
	}

	err := c.clt.Move(learnedMsgUIDs, destMailbox)
	if err != nil {
		return fmt.Errorf("moving messages after learning failed: %w", err)
	}

	return nil
}

func toHdrMap(prefix string, scores map[string]*rspamc.Symbol, skipZeroScore bool) map[string]string {
	result := make(map[string]string, len(scores))

	for _, v := range scores {
		if skipZeroScore && v.Score == 0 {
			continue
		}

		// map key is the same as v.Name
		result[prefix+v.Name] = fmt.Sprint(v.Score)
	}

	return result
}

func addScanResultHeaders(mailFilepath string, result *rspamc.CheckResult) error {
	var hdrsData []byte

	hdrs := toHdrMap(hdrScanSymbolPrefix+"Symbol-", result.Symbols, true)
	hdrs[hdrScanSymbolPrefix+"Score"] = fmt.Sprint(result.Score)

	// TODO: instead of adding a header line per symbol, add a multiline
	// header with all symbols
	hdrsData, err := mail.AsHeaders(hdrs)
	if err != nil {
		return err
	}

	return mail.AddHeaders(mailFilepath, hdrsData)
}

func (c *Client) isSpam(r *rspamc.CheckResult) bool {
	return r.Score >= c.spamTreshold
}

// replaceWithModifiedMails uploads mails to the spam or inbox mailbox, depending on their
// spam score.
// The original email is moved to the backup mailbox.
// It returns an UIDSet of all successfully uploaded mails.
// When errors happen, an error **and** a non-empty UIDSet can be returned.
func (c *Client) replaceWithModifiedMails(mails []*scannedMail) error {
	var errs []error

	for _, mail := range mails {
		var mbox string

		logger := c.logger.With(
			"mail.subject", mail.Envelope.Subject,
			"mail.uid", mail.UID,
			"filepath", mail.Path,
		)

		// TODO: support deleting emails from the mailbox, when backupMailbox is
		// empty instead of keeping a copy of the original, deleting
		// must happen after appendMail!
		err := c.clt.Move([]uint32{mail.UID}, c.backupMailbox)
		if err != nil {
			errs = append(errs, fmt.Errorf(
				"moving mail (%d) (%s) to backup mailbox %s failed: %w",
				mail.UID, mail.Envelope.Subject, c.backupMailbox, err,
			))

			continue
		}

		if c.isSpam(mail.CheckResult) {
			mbox = c.spamMailbox
		} else {
			mbox = c.inboxMailbox
		}

		err = c.clt.Upload(mail.Path, mbox, mail.Envelope.Date)
		if err != nil {
			errs = append(errs, fmt.Errorf(
				"uploading email %q (%s) (%s) to %s failed: %w",
				mail.UID, mail.Envelope.Subject, mail.Path, mbox, err,
			))
			logger.Warn(
				"uploading scanned email to inbox failed, please find the original email in the backup mailbox!",
				"event", "imap.msg_append_failed",
			)

			continue
		}

		logger.Info(
			"moved original messages to backup mailbox and uploaded modified message with scan result",
			"imap.mailbox", mbox,
			"imap.backupMailbox", c.backupMailbox,
		)

		if c.keepTempFiles {
			continue
		}

		if err := os.Remove(mail.Path); err != nil {
			logger.Warn(
				"deleting email file failed",
				"error", err,
				"event", "imap.msg_delete_failed",
			)
		}
	}

	return errors.Join(errs...)
}

func (c *Client) downloadAndScan(msg *imapclt.Message) (*scannedMail, error) {
	tmpFile, err := os.CreateTemp(
		c.tempDir,
		"rspamd-iscan-mail-"+strconv.Itoa(int(msg.UID)),
	)
	if err != nil {
		return nil, fmt.Errorf("creating temporary file failed: %w", err)
	}

	errCleanupfn := func() {
		_ = tmpFile.Close()

		if c.keepTempFiles {
			return
		}

		if err := os.Remove(tmpFile.Name()); err != nil {
			c.logger.Error("deleting temporary file failed",
				"error", err, "path", tmpFile.Name(),
				"event", "file.deletion_failed")
		}
	}

	_, err = io.Copy(tmpFile, msg.Message)
	if err != nil {
		errCleanupfn()
		return nil, fmt.Errorf("downloading imap message to disk failed: %w", err)
	}

	env := &msg.Envelope
	logger := c.logger.With("mail.subject", env.Subject, "mail.uid", msg.UID)
	logger.Debug("downloaded imap message",
		"path", tmpFile.Name(),
		"mail.envelope.messageID", env.MessageID,
		"mail.envelope.from", env.From,
		"mail.envelope.recipients", env.Recipients,
	)

	_, err = tmpFile.Seek(0, 0)
	if err != nil {
		errCleanupfn()
		return nil, fmt.Errorf("setting %q file position to beginning failed: %w", tmpFile.Name(), err)
	}
	// TODO: retry Check if it failed with a temporary error
	scanResult, err := c.rspamc.Check(context.Background(), tmpFile, envelopeToRspamcHdrs(env))
	if err != nil {
		errCleanupfn()
		return nil, err
	}

	if err := tmpFile.Close(); err != nil {
		errCleanupfn()
		return nil, fmt.Errorf("closing file of downloaded mail failed: %w", err)
	}

	err = addScanResultHeaders(tmpFile.Name(), scanResult)
	if err != nil {
		return nil, fmt.Errorf("adding scan result headers to local mail copy failed: %w", err)
	}

	logger.Info("message scanned",
		"scan.score", scanResult.Score, "scan.IsSpam", c.isSpam(scanResult),
	)

	return &scannedMail{
		Path:        tmpFile.Name(),
		UID:         msg.UID,
		Envelope:    env,
		CheckResult: scanResult,
	}, nil
}

func envelopeToRspamcHdrs(env *imapclt.Envelope) *rspamc.MailHeaders {
	return &rspamc.MailHeaders{
		Subject:    env.Subject,
		From:       env.From,
		Recipients: env.Recipients,
	}
}

func (c *Client) ProcessScanBox() error {
	//nolint:prealloc // number of mails is unknown before iterating
	var scannedMails []*scannedMail
	var errs []error

	logger := c.logger.With("mailbox.source", c.scanMailbox)
	logger.Info("processing scan box")

	for msg, err := range c.clt.Messages(c.scanMailbox) {
		if err != nil {
			return fmt.Errorf("fetching messages from scanbox failed: %w", err)
		}

		sm, err := c.downloadAndScan(msg)
		if err != nil {
			// TODO: abort on local tmpfile errors immediately,
			// unlikely that the following mail won't encounter the
			// same issue
			errs = append(errs, err)
			break
		}

		scannedMails = append(scannedMails, sm)
	}

	err := c.replaceWithModifiedMails(scannedMails)
	if err != nil {
		errs = append(errs, err)
	}

	c.cntScannedMails.Add(1)

	return errors.Join(errs...)
}

// Start monitors the Unscanned mailbox for new messages and processes them
// continuously,
// It also checks periodically the Ham and Undetected Mailbox for new messages.
// sents them to rspamd for leanring and moves them to their target inbox.
//
// The method blocks until an error occurred or [*Client.Stop] is called.
// When an error happens [*Client.Stop] should still be called to ensure that
// the IMAP connection is closed.
func (c *Client) Start() error {
	c.wgRun.Add(1)
	defer c.wgRun.Done()
	err := c.ProcessHam()
	if err != nil {
		return fmt.Errorf("learning ham failed: %w", WrapRetryableError(err))
	}

	err = c.ProcessSpam()
	if err != nil {
		return fmt.Errorf("learning spam failed: %w", WrapRetryableError(err))
	}

	lastLearnAt := time.Now()

	err = c.ProcessScanBox()
	if err != nil {
		return WrapRetryableError(err)
	}

	for {
		eventCh, monitorCancelFn, err := c.clt.Monitor(c.scanMailbox)
		if err != nil {
			return WrapRetryableError(err)
		}

		c.logger.Debug("waiting for mailbox update events")
		select {
		case <-time.After(c.learnInterval - time.Since(lastLearnAt)):
			c.logger.Debug("learn timer expired, checking mailboxes for new messages")

			if err := monitorCancelFn(); err != nil {
				return WrapRetryableError(err)
			}

			// sometimes monitoring stopped working and no updates
			// were send anymore, despite new imap messages, as
			// workaround we additionally check the Scanbox. //
			// TODO: verify if that really is still an issue or
			// could be removed
			if err := c.ProcessScanBox(); err != nil {
				return WrapRetryableError(err)
			}

			if err := c.ProcessHam(); err != nil {
				return WrapRetryableError(err)
			}

			if err := c.ProcessSpam(); err != nil {
				return WrapRetryableError(err)
			}

			lastLearnAt = time.Now()

		case evA, ok := <-eventCh:
			if !ok {
				c.logger.Debug("event channel was closed")
				_ = monitorCancelFn()
				return nil
			}

			if err := monitorCancelFn(); err != nil {
				return WrapRetryableError(err)
			}

			if evA.NewMsgCount == 0 {
				c.logger.Debug("ignoring MailboxUpdate, no new messages")
				continue
			}

			err = c.ProcessScanBox()
			if err != nil {
				return WrapRetryableError(err)
			}

		case <-c.stopCh:
			if err := monitorCancelFn(); err != nil {
				return WrapRetryableError(err)
			}

			return nil
		}
	}
}

// Stop gracefully terminates a running [Client.Start] routine and closes the
// connection to the IMAP-server.
func (c *Client) Stop() error {
	var err error

	c.stopOnce.Do(func() {
		close(c.stopCh)
		c.wgRun.Wait()
		err = c.clt.Close()
	})

	return err
}
