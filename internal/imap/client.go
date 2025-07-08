package imap

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"slices"
	"strconv"
	"time"

	"github.com/fho/rspamd-iscan/internal/mail"
	"github.com/fho/rspamd-iscan/internal/rspamc"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
)

const hdrScanSymbolPrefix = "X-rspamd-iscan-"

const defChanBufSiz = 32

type eventNewMessages struct {
	NewMsgCount uint32
}

type Client struct {
	clt    *imapclient.Client
	logger *slog.Logger

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

	eventCh chan eventNewMessages

	rspamc *rspamc.Client
}

type Config struct {
	ServerAddr            string
	User                  string
	Passwd                string
	ScanMailbox           string
	InboxMailbox          string
	HamMailbox            string
	SpamMailboxName       string
	BackupMailboxName     string
	TempDir               string
	KeepTempFiles         bool
	UndetectedMailboxName string
	SpamTreshold          float32
	Logger                *slog.Logger
	Rspamc                *rspamc.Client
}

type scannedMail struct {
	Path        string
	UID         imap.UID
	Envelope    *imap.Envelope
	CheckResult *rspamc.CheckResult
}

type learnFn func(context.Context, io.Reader) error

func NewClient(cfg *Config) (*Client, error) {
	logger := cfg.Logger.WithGroup("imap").With("server", cfg.ServerAddr)
	c := &Client{
		logger:            logger,
		inboxMailbox:      cfg.InboxMailbox,
		scanMailbox:       cfg.ScanMailbox,
		spamMailbox:       cfg.SpamMailboxName,
		hamMailbox:        cfg.HamMailbox,
		undetectedMailbox: cfg.UndetectedMailboxName,
		eventCh:           make(chan eventNewMessages, defChanBufSiz),
		rspamc:            cfg.Rspamc,
		spamTreshold:      cfg.SpamTreshold,
		learnInterval:     30 * time.Minute,
		backupMailbox:     cfg.BackupMailboxName,
		tempDir:           cfg.TempDir,
		keepTempFiles:     cfg.KeepTempFiles,
	}

	clt, err := imapclient.DialTLS(cfg.ServerAddr, &imapclient.Options{
		UnilateralDataHandler: &imapclient.UnilateralDataHandler{
			Mailbox: c.mailboxUpdateHandler,
		},
		// DebugWriter: os.Stderr,
	})
	if err != nil {
		return nil, err
	}
	c.clt = clt

	if err := clt.Login(cfg.User, cfg.Passwd).Wait(); err != nil {
		return nil, err
	}

	logger.Debug("connection established")

	return c, nil
}

func (c *Client) mailboxUpdateHandler(d *imapclient.UnilateralDataMailbox) {
	if d.NumMessages == nil {
		c.logger.Debug("ignoring mailbox update with nil NumMessages")
		return
	}

	c.logger.Debug("received mailbox update", "num_messages", *d.NumMessages)
	c.eventCh <- eventNewMessages{NewMsgCount: *d.NumMessages}
}

func (c *Client) Close() error {
	return c.clt.Close()
}

// Monitor monitors mailbox for changes.
// stop must be called before any other imap commands can be processed,
// otherwise the client will hang.
func (c *Client) Monitor(mailbox string) (stop func() error, err error) {
	logger := c.logger.With("mailbox", mailbox)

	logger.Debug("starting to monitor mailbox for changes")
	// "ReadOnly: false" causes that moved messages are shown as read instead
	// of unread
	d, err := c.clt.Select(mailbox, &imap.SelectOptions{ReadOnly: true}).Wait()
	if err != nil {
		return nil, fmt.Errorf("selecting mailbox %q failed: %w", mailbox, err)
	}

	if d.NumMessages != 0 {
		logger.Debug("mailbox has new message, skipping monitoring", "num_messages", d.NumMessages)
		c.eventCh <- eventNewMessages{NewMsgCount: d.NumMessages}
		return func() error { return nil }, nil
	}

	idlecmd, err := c.clt.Idle()
	if err != nil {
		return nil, err
	}

	return func() error {
		logger.Debug("canceling idle")
		err := errors.Join(idlecmd.Close(), idlecmd.Wait())
		logger.Debug("idle canceled")
		return err
	}, nil
}

type SeenStatus struct {
	UIDValidity      uint32
	UIDLastProcessed imap.UID
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
	logger := c.logger.With("mailbox.source", srcMailbox)

	logger.Debug("checking for new messages")

	mbox, err := c.clt.Select(srcMailbox, &imap.SelectOptions{}).Wait()
	if err != nil {
		return err
	}

	if mbox.NumMessages == 0 {
		logger.Debug("mailbox is empty, nothing to learn", "event", "imap.mailbox_empty")
		return nil
	}

	logger.Debug("new messages found", "event", "imap.new_messages", "count", mbox.NumMessages)

	n := imap.SeqSet{}
	n.AddRange(1, 0)

	fetchCmd := c.clt.Fetch(n, &imap.FetchOptions{
		Envelope:    true,
		UID:         true,
		BodySection: []*imap.FetchItemBodySection{{Peek: true}},
	})
	defer fetchCmd.Close()

	var learnedSet imap.UIDSet
	for {
		msgData := fetchCmd.Next()
		if msgData == nil {
			break
		}

		msg, err := msgData.Collect()
		if err != nil {
			return err
		}

		if msg.Envelope == nil {
			return errors.New("msg.Envelope is nil")
		}
		if msg.UID == 0 {
			return errors.New("msg.UID is nil")
		}

		logger := c.logger.With("mail.subject", msg.Envelope.Subject, "mail.uid", msg.UID)
		logger.Debug("fetched message")

		if len(msg.BodySection) != 1 {
			return fmt.Errorf("msg has %d body sections, expecting 1", len(msg.BodySection))
		}
		var txt []byte
		for _, b := range msg.BodySection {
			txt = b.Bytes
			break
		}
		if txt == nil {
			return errors.New("body is nil")
		}
		if len(txt) == 0 {
			return errors.New("body is empty")
		}

		// TODO: retry Check if it failed with a temporary error
		err = learnFn(context.TODO(), bytes.NewReader(txt))
		if err != nil {
			logger.Warn("learning message failed", "error", err)
			return nil
		}
		logger.Info("learned message")
		learnedSet.AddNum(msg.UID)
	}

	err = fetchCmd.Close()
	if err != nil {
		// TODO: try to move the learned messages anyways
		return err
	}

	_, err = c.clt.Move(learnedSet, destMailbox).Wait()
	if err != nil {
		return fmt.Errorf("moving messages after learning successfully failed: %w", err)
	}

	logger.Info("moved messages", "mailbox.destination", destMailbox)

	return nil
}

func (c *Client) downloadMsg(msgData *imapclient.FetchMessageData, w io.Writer) (imap.UID, *imap.Envelope, error) {
	var envelope *imap.Envelope
	var bodyWritten bool
	var uid imap.UID

	for i := 0; ; i++ {
		item := msgData.Next()
		if item == nil {
			if envelope == nil {
				return 0, nil, errors.New("envelope is missing")
			}

			if !bodyWritten {
				return 0, nil, errors.New("message data is missing")
			}

			if uid == 0 {
				return 0, nil, errors.New("uid is missing")
			}

			return uid, envelope, nil
		}

		if i > 2 {
			return 0, nil, errors.New("expected 3 message items, got >3")
		}

		switch item := item.(type) {
		case imapclient.FetchItemDataUID:
			uid = item.UID

		case imapclient.FetchItemDataBodySection:
			if item.Literal == nil {
				return 0, nil, errors.New("message data reader is nil")
			}

			if item.Literal.Size() == 0 {
				return 0, nil, errors.New("message data reader is empty")
			}

			_, err := io.Copy(w, item.Literal)
			if err != nil {
				return 0, nil, fmt.Errorf("copying message from server to disk failed: %w", err)
			}

			bodyWritten = true

		case imapclient.FetchItemDataEnvelope:
			if item.Envelope == nil {
				return 0, nil, errors.New("envelope is nil")
			}

			envelope = item.Envelope
		default:
			return 0, nil, fmt.Errorf("message data has unexpected type: %T", item)
		}
	}
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
func (c *Client) replaceWithModifiedMails(mails []*scannedMail) (imap.UIDSet, error) {
	var processed imap.UIDSet
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
		uidOrg := imap.UIDSetNum(mail.UID)
		_, err := c.clt.Move(uidOrg, c.backupMailbox).Wait()
		if err != nil {
			errs = append(errs, fmt.Errorf(
				"moving mail (%d) (%s) to backup mailbox %s failed: %w",
				mail.UID, mail.Envelope.Subject, c.backupMailbox, err,
			))

			continue
		}

		logger.Debug("moved mail to backup mailbox", "mailbox", c.backupMailbox)

		if c.isSpam(mail.CheckResult) {
			mbox = c.spamMailbox
		} else {
			mbox = c.inboxMailbox
		}

		err = c.appendMail(mail.Path, mbox, mail.Envelope.Date)
		if err != nil {
			errs = append(errs, fmt.Errorf(
				"uploading email %q (%s) (%s) to %s failed: %w",
				mail.UID, mail.Envelope.Subject, mail.Path, mbox, err,
			))
			logger.Warn("uploading scanned email to inbox failed, please find the original email in the backup mailbox!")

			continue
		}

		logger.Debug("uploaded modified mail with scan result", "mailbox", mbox)

		processed.AddNum(mail.UID)

		if !c.keepTempFiles {
			if err := os.Remove(mail.Path); err != nil {
				logger.Warn(
					"deleting email file failed",
					"error", err,
				)
			}
		}
	}

	return processed, errors.Join(errs...)
}

func (c *Client) appendMail(path, mailbox string, ts time.Time) error {
	fi, err := os.Stat(path)
	if err != nil {
		return err
	}

	appendCmd := c.clt.Append(mailbox, fi.Size(), &imap.AppendOptions{Time: ts})

	fd, err := os.Open(path)
	if err != nil {
		return err
	}
	defer fd.Close()

	_, err = io.Copy(appendCmd, fd)
	if err != nil {
		return fmt.Errorf("uploading mail to imap mailbox failed: %w", err)
	}

	err = appendCmd.Close()
	if err != nil {
		return fmt.Errorf("uploading mail to imap mailbox failed: %w", err)
	}

	return nil
}

func (c *Client) downloadAndScan(msgData *imapclient.FetchMessageData) (*scannedMail, error) {
	tmpFile, err := os.CreateTemp(
		c.tempDir,
		"rspamd-iscan-mail-"+strconv.Itoa(int(msgData.SeqNum)),
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
				"error", err, "path", tmpFile.Name())
		}
	}

	uid, env, err := c.downloadMsg(msgData, tmpFile)
	if err != nil {
		errCleanupfn()
		return nil, fmt.Errorf("downloading imap message to disk failed: %w", err)
	}

	logger := c.logger.With("mail.subject", env.Subject, "mail.uid", uid)
	logger.Debug("downloaded imap message",
		"path", tmpFile.Name(),
		"mail.Envelope.MessageID", env.MessageID,
		"mail.Envelope.From", env.From,
		"mail.Envelope.To", env.To,
	)

	_, err = tmpFile.Seek(0, 0)
	if err != nil {
		errCleanupfn()
		return nil, fmt.Errorf("setting %q file position to beginning failed: %w", tmpFile.Name(), err)
	}

	// TODO: retry Check if it failed with a temporary error
	scanResult, err := c.rspamc.Check(context.Background(), tmpFile)
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

	logger.Debug("message scanned",
		"scan.score", scanResult.Score, "scan.IsSpam", c.isSpam(scanResult),
	)

	return &scannedMail{
		Path:        tmpFile.Name(),
		UID:         uid,
		Envelope:    env,
		CheckResult: scanResult,
	}, nil
}

func (c *Client) ProcessScanBox(startStatus *SeenStatus) (*SeenStatus, error) {
	var scannedMails []*scannedMail
	var errs []error

	status := *startStatus

	logger := c.logger.With("mailbox.source", c.scanMailbox)

	mbox, err := c.clt.Select(c.scanMailbox, &imap.SelectOptions{}).Wait()
	if err != nil {
		return startStatus, err
	}

	if mbox.UIDValidity != startStatus.UIDValidity {
		logger.Info("uidValidity of mailbox changed, resetting last seen UID, scanning all messages",
			"uid_validity_last", startStatus.UIDValidity, "uid_validity_new", mbox.UIDValidity,
			"event", "imap.uidvalidity_change",
		)
		status.UIDValidity = mbox.UIDValidity
		status.UIDLastProcessed = 0
	}

	if mbox.NumMessages == 0 {
		logger.Info("scan mailbox is empty, nothing to do", "event", "imap.mailbox_empty")
		return &status, nil
	}

	if mbox.UIDNext == startStatus.UIDLastProcessed+1 {
		logger.Debug("all messages have already been processed, nothing to do",
			"event", "imap.mailbox_all_scanned",
			"last_seen.uid_validity", startStatus.UIDValidity,
			"last_seen.processed", startStatus.UIDLastProcessed,
			"mailbox_update.uid_validity", mbox.UIDValidity,
			"mailbox_update.uid_next", mbox.UIDNext,
		)
		return &status, nil
	}

	numSet := imap.UIDSet{}
	numSet.AddRange(status.UIDLastProcessed+1, 0)

	fetchCmd := c.clt.Fetch(numSet, &imap.FetchOptions{
		Envelope:    true,
		BodySection: []*imap.FetchItemBodySection{{Peek: true}},
		UID:         true,
	})
	defer fetchCmd.Close()

	for {
		msgData := fetchCmd.Next()
		if msgData == nil {
			break
		}

		sm, err := c.downloadAndScan(msgData)
		if err != nil {
			// TODO: abort on local tmpfile errors immediately,
			// unlikely that the following mail won't encounter the
			// same issue
			errs = append(errs, err)
			continue
		}
		scannedMails = append(scannedMails, sm)
	}

	err = fetchCmd.Close()
	if err != nil {
		return startStatus, errors.Join(append(errs, err)...)
	}

	processedMails, err := c.replaceWithModifiedMails(scannedMails)
	if err != nil {
		errs = append(errs, err)
	}

	if len(processedMails) == 0 {
		return &status, errors.Join(errs...)
	}

	lastProcessed, err := maxUID(processedMails)
	if err != nil {
		return &status, errors.Join(append(errs, fmt.Errorf("evaluating uid of last successfully processed mail failed: %w", err))...)
	}

	if lastProcessed > status.UIDLastProcessed {
		status.UIDLastProcessed = lastProcessed
	}

	return &status, errors.Join(errs...)
}

func maxUID(s imap.UIDSet) (imap.UID, error) {
	uids, ok := s.Nums()
	if !ok {
		return 0, errors.New("getting all uids from set failed")
	}

	return slices.Max(uids), nil
}

func (c *Client) Run() error {
	lastSeen := &SeenStatus{}

	err := c.ProcessHam()
	if err != nil {
		return fmt.Errorf("learning ham failed: %w", WrapRetryableError(err))
	}

	err = c.ProcessSpam()
	if err != nil {
		return fmt.Errorf("learning spam failed: %w", WrapRetryableError(err))
	}

	seen, err := c.ProcessScanBox(lastSeen)
	if err != nil {
		return WrapRetryableError(err)
	}
	lastSeen = seen

	monitorCancelFn, err := c.Monitor(c.scanMailbox)
	if err != nil {
		return WrapRetryableError(err)
	}

	c.logger.Debug("waiting for mailbox update events")

	var lastLearn time.Time
	for {
		select {
		// sometimes monitoring stops working and no updates are
		// send anymore, despite new imap messages,
		// therefore we additionally check every 30min for new
		// mails, to workaround it.
		case <-time.After(30 * time.Minute):
			c.logger.Debug("timer expired, checking mailbox for new messages")

			if err := monitorCancelFn(); err != nil {
				return WrapRetryableError(err)
			}

			seen, err := c.ProcessScanBox(lastSeen)
			if err != nil {
				return WrapRetryableError(err)
			}
			lastSeen = seen

			if time.Since(lastLearn) >= c.learnInterval {
				if err := c.ProcessHam(); err != nil {
					return WrapRetryableError(err)
				}

				if err := c.ProcessSpam(); err != nil {
					return WrapRetryableError(err)
				}

				lastLearn = time.Now()
			}

			monitorCancelFn, err = c.Monitor(c.scanMailbox)
			if err != nil {
				return WrapRetryableError(err)
			}

		case evA, ok := <-c.eventCh:
			if !ok {
				c.logger.Debug("event channel was closed")
				_ = monitorCancelFn()
				return nil
			}

			if err := monitorCancelFn(); err != nil {
				return WrapRetryableError(err)
			}

			if time.Since(lastLearn) >= c.learnInterval {
				if err := c.ProcessHam(); err != nil {
					return WrapRetryableError(err)
				}
				lastLearn = time.Now()
			}

			// TODO: we might receive multiple events at once,
			// instead of processing all sequentially, fetch
			// all and only call ProcessScanBox 1x
			if evA.NewMsgCount == 0 {
				c.logger.Info("ignoring MailboxUpdate, no new messages")
				continue
			}

			seen, err := c.ProcessScanBox(lastSeen)
			if err != nil {
				return WrapRetryableError(err)
			}
			lastSeen = seen

			monitorCancelFn, err = c.Monitor(c.scanMailbox)
			if err != nil {
				return WrapRetryableError(err)
			}
		}
	}
}
