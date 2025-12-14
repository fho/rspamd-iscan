package iscan

import (
	"errors"
	"fmt"
	"iter"
	"log/slog"
	"os"
	"time"

	"github.com/fho/rspamd-iscan/internal/imapclt"
)

type IMAPClient interface {
	Close() error
	Connect() error
	Messages(mailbox string) iter.Seq2[*imapclt.Message, error]
	Monitor(mailbox string) (<-chan *imapclt.EventNewMessages, func() error, error)
	Move(uids []uint32, mailbox string) error
	Upload(path, mailbox string, ts time.Time) error
}

type Config struct {
	BackupMailbox         string
	HamMailbox            string
	InboxMailbox          string
	ScanMailbox           string
	SpamMailboxName       string
	UndetectedMailboxName string

	TempDir       string
	KeepTempFiles bool

	SpamTreshold float32

	Logger     *slog.Logger
	IMAPClient IMAPClient
	Rspamc     RspamdClient
}

func (c *Config) validate() error {
	if c.SpamTreshold <= 0 {
		return errors.New("SpamTreshold must be >0")
	}

	if c.ScanMailbox == c.InboxMailbox {
		return errors.New("ScanMailbox and InboxMailbox must differ")
	}

	if c.ScanMailbox == c.UndetectedMailboxName {
		return errors.New("ScanMailbox and UndetectedMailbox must differ")
	}

	if c.ScanMailbox == c.HamMailbox {
		return errors.New("ScanMailbox and HamMailbox must differ")
	}

	if c.BackupMailbox == "" {
		return errors.New("BackupMailbox can not be empty")
	}

	if c.BackupMailbox == c.InboxMailbox {
		return errors.New("BackupMailbox and InboxMailbox must differ")
	}

	// Using the same mailbox for Spam, Ham and/or Backup would be weird but
	// should work fine!
	if c.SpamTreshold == 0 {
		return errors.New("SpamThreshold must be >0")
	}

	fd, err := os.Stat(c.TempDir)
	if err != nil {
		return fmt.Errorf("invalid TempDir (%s): %w", c.TempDir, err)
	}

	if !fd.IsDir() {
		return fmt.Errorf("specified TempDir (%s) is not a directory", c.TempDir)
	}

	if c.Rspamc == nil {
		return errors.New("rspamc can not be nil")
	}

	return nil
}
