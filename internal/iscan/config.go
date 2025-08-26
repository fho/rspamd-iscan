package iscan

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
)

type Config struct {
	ServerAddr                  string
	AllowInsecureIMAPConnection bool
	User                        string
	Password                    string

	BackupMailbox         string
	HamMailbox            string
	InboxMailbox          string
	ScanMailbox           string
	SpamMailboxName       string
	UndetectedMailboxName string

	TempDir       string
	KeepTempFiles bool

	SpamTreshold float32
	Logger       *slog.Logger
	Rspamc       RspamdClient
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

	return nil
}
