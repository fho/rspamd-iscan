package main

import (
	"errors"
	"fmt"
	"log/slog"
	"os"

	"github.com/fho/rspamd-iscan/internal/config"
	"github.com/fho/rspamd-iscan/internal/imap"
	"github.com/fho/rspamd-iscan/internal/rspamc"

	flag "github.com/spf13/pflag"
)

var (
	version = "version-undefined"
	commit  = "commit-undefined"
)

func main() {
	cfgPath := flag.String("cfg-file", "/etc/rspamd-iscan/config.toml", "Path to the rspamd-iscan config file")
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

	cfg, err := config.FromFile(*cfgPath)
	if err != nil {
		logger.Error("loading config failed", "error", err)
		os.Exit(1)
	}

	cfg.SetDefaults()

	fmt.Println(cfg.String())

	if err := cfg.Validate(); err != nil {
		logger.Error("validating config failed", "error", err)
		os.Exit(1)
	}

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
			BackupMailboxName:     cfg.BackupMailbox,
			SpamTreshold:          cfg.SpamThreshold,
			TempDir:               cfg.TempDir,
			KeepTempFiles:         cfg.KeepTempFiles,
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
