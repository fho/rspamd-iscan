package main

/* TODO:
- add command to create example config
- Support learning ham
- Support learning spam
- Modify E-mail headers after spam, add spam-score etc hdrs from rspamd
- add --verbose flag, to enable logging debug messages
*/

import (
	"errors"
	"fmt"
	"log/slog"
	"os"

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
	RspamdURL      string
	RspamdPassword string
	ImapAddr       string
	ImapUser       string
	ImapPassword   string
	InboxMailbox   string
	SpamMailbox    string
	ScanMailbox    string
	HamMailbox     string
	SpamThreshold  float32
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

	// TODO: allow passing all attrs as single URL to rspamc http client
	rspamc := rspamc.New(logger, cfg.RspamdURL, cfg.RspamdPassword)

	for {
		clt, err := imap.NewClient(
			cfg.ImapAddr,
			cfg.ImapUser,
			cfg.ImapPassword,
			cfg.ScanMailbox,
			cfg.InboxMailbox,
			cfg.HamMailbox,
			cfg.SpamMailbox,
			*stateFilePath,
			cfg.SpamThreshold,
			logger,
			rspamc,
		)
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
