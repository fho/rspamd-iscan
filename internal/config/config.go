package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

type Config struct {
	RspamdURL         string
	RspamdPassword    string
	ImapAddr          string
	ImapUser          string
	ImapPassword      string
	InboxMailbox      string
	SpamMailbox       string
	ScanMailbox       string
	HamMailbox        string
	BackupMailbox     string
	UndetectedMailbox string
	SpamThreshold     float32
	TempDir           string
	KeepTempFiles     bool
	LogIMAPData       bool
}

func (c *Config) String() string {
	const unset = "UNSET"
	const hiddenPasswd = "***"
	var sb strings.Builder

	printKv := func(k string, v any) {
		fmt.Fprintf(&sb, "%-30v%-50v\n", k+":", v)
	}

	sb.WriteString("Configuration:\n")
	printKv("Rspamd URL", c.RspamdURL)

	if c.RspamdPassword == "" {
		printKv("Rspamd Password", unset)
	} else {
		printKv("Rspamd Password", hiddenPasswd)
	}

	printKv("IMAP Server Address", c.ImapAddr)
	printKv("IMAP User", c.ImapUser)

	if c.ImapPassword == "" {
		printKv("IMAP Password", unset)
	} else {
		printKv("IMAP Password", hiddenPasswd)
	}

	printKv("Spam Treshold", c.SpamThreshold)
	printKv("Scan Mailbox", c.ScanMailbox)
	printKv("Inbox Mailbox", c.InboxMailbox)
	printKv("Spam Mailbox", c.SpamMailbox)
	printKv("Undetected Mailbox", c.UndetectedMailbox)
	printKv("Backup Mailbox", c.BackupMailbox)
	printKv("Temporary Directory", c.TempDir)
	printKv("Keep Temporary Files", c.KeepTempFiles)
	printKv("Log IMAP Data", c.LogIMAPData)

	sb.WriteRune('\n')
	fmt.Fprintf(&sb, "Mails in %q are scanned and backuped to %q.\n", c.ScanMailbox, c.BackupMailbox)
	fmt.Fprintf(&sb, "Mails with a spam score of >=%f are moved to %q,\n", c.SpamThreshold, c.SpamMailbox)
	fmt.Fprintf(&sb, "others are moved to %q.\n", c.InboxMailbox)
	if c.UndetectedMailbox != "" {
		fmt.Fprintf(&sb, "Mails in %q are learned as Spam and moved to %q.\n", c.UndetectedMailbox, c.SpamMailbox)
	}
	fmt.Fprintf(&sb, "Mails in %q are learned as Ham and moved to %q.\n", c.HamMailbox, c.InboxMailbox)

	return sb.String()
}

func FromFile(path string) (*Config, error) {
	var result Config
	buf, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	err = toml.Unmarshal(buf, &result)
	if err != nil {
		return nil, err
	}

	return &result, nil
}

func (c *Config) SetDefaults() {
	if c.TempDir == "" {
		c.TempDir = os.TempDir()
	}
}
