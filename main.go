package main

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/fho/rspamd-scan/internal/imap"
	"github.com/fho/rspamd-scan/internal/rspamc"

	"github.com/pelletier/go-toml/v2"
	flag "github.com/spf13/pflag"
)

var (
	version = "version-undefined"
	commit  = "commit-undefined"
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
	UndetectedMailbox string
	SpamThreshold     float32
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

	sb.WriteRune('\n')
	fmt.Fprintf(&sb, "Mails in %q are scanned.\n", c.ScanMailbox)
	fmt.Fprintf(&sb, "Mails with a spam score of >=%f are moved to %q,\n", c.SpamThreshold, c.SpamMailbox)
	fmt.Fprintf(&sb, "others are moved to %q.\n", c.InboxMailbox)
	if c.UndetectedMailbox != "" {
		fmt.Fprintf(&sb, "Mails in %q are learned as Spam and moved to %q.\n", c.UndetectedMailbox, c.SpamMailbox)
	}
	fmt.Fprintf(&sb, "Mails in %q are learned as Ham and moved to %q.\n", c.HamMailbox, c.InboxMailbox)

	return sb.String()
}

func LoadConfig(path string) (*Config, error) {
	var result Config
	buf, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	err = toml.Unmarshal(buf, &result)
	if err != nil {
		return nil, err
	}

	if result.SpamThreshold == 0 {
		return nil, errors.New("spam thresold must be >0")
	}
	return &result, nil
}

func main() {
	cfgPath := flag.String("cfg-file", "rspamd-iscan.toml", "Path to the rspamd-iscan config file")
	stateFilePath := flag.String("state-file", ".rspamd-iscan.state", "Path to a file that stores the scan state")
	printVersion := flag.Bool("version", false, "print the version and exit")
	flag.Parse()

	if *printVersion {
		fmt.Printf("rspamd-iscan %s (%s)\n", version, commit)
		os.Exit(0)
	}

	// TODO: implement signal handler for clean shutdown
	h := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelDebug,
		ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			// do not log timestamp, rspamd-iscan is normally run as
			// daemon, journald/syslog already adds timestamps
			if a.Key == slog.TimeKey {
				return slog.Attr{}
			}
			return a
		},
	})
	logger := slog.New(h)

	cfg, err := LoadConfig(*cfgPath)
	if err != nil {
		logger.Error("loading config failed", "error", err)
		os.Exit(1)
	}

	fmt.Println(cfg.String())

	// TODO: allow passing all attrs as single URL to rspamc http client
	rspamc := rspamc.New(logger, cfg.RspamdURL, cfg.RspamdPassword)

	for {
		clt, err := imap.NewClient(&imap.Config{
			ServerAddr:            cfg.ImapAddr,
			User:                  cfg.ImapUser,
			Passwd:                cfg.ImapPassword,
			ScanMailbox:           cfg.ScanMailbox,
			InboxMailbox:          cfg.InboxMailbox,
			HamMailbox:            cfg.HamMailbox,
			SpamMailboxName:       cfg.SpamMailbox,
			UndetectedMailboxName: cfg.UndetectedMailbox,
			StateFilePath:         *stateFilePath,
			SpamTreshold:          cfg.SpamThreshold,
			Logger:                logger,
			Rspamc:                rspamc,
		})
		if err != nil {
			logger.Error("creating imap client failed", "error", err)
			os.Exit(1)
		}

		err = clt.Run()
		if err != nil {
			clt.Close()
			rError := &imap.ErrRetryable{}
			if !errors.As(err, &rError) {
				logger.Error("run failed with fatal error, terminating", "error", err)
				os.Exit(1)
			}
		}
		logger.Error("run failed with temporary error, restarting imap client", "error", err)
	}
}
