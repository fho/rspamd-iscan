package main

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/fho/rspamd-iscan/internal/config"
	"github.com/fho/rspamd-iscan/internal/iscan"
	"github.com/fho/rspamd-iscan/internal/rspamc"

	flag "github.com/spf13/pflag"
)

var (
	version = "version-undefined"
	commit  = "commit-undefined"
)

type flags struct {
	cfgPath      string
	printVersion bool
}

func parseFlags() *flags {
	var result flags

	flag.StringVar(&result.cfgPath, "cfg-file", "/etc/rspamd-iscan/config.toml", "Path to the rspamd-iscan config file")
	flag.BoolVar(&result.printVersion, "version", false, "print the version and exit")
	flag.Parse()

	return &result
}

func installSigHandler(logger *slog.Logger, clt *iscan.Client) {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)

	go func() {
		var strSig string

		sig := <-sigCh

		switch ssig, ok := sig.(syscall.Signal); ok {
		case true:
			strSig = fmt.Sprintf("%d, %s", ssig, ssig)
		default:
			strSig = sig.String()
		}

		logger.Info(fmt.Sprintf("received signal (%s), terminating iscan process", strSig))
		_ = clt.Stop()
	}()
}

func main() {
	flags := parseFlags()
	if flags.printVersion {
		fmt.Printf("rspamd-iscan %s (%s)\n", version, commit)
		os.Exit(0)
	}

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

	cfg, err := config.FromFile(flags.cfgPath)
	if err != nil {
		logger.Error("loading config failed", "error", err)
		os.Exit(1)
	}

	cfg.SetDefaults()

	fmt.Println(cfg.String())

	// TODO: allow passing all attrs as single URL to rspamc http client
	rspamc := rspamc.New(logger, cfg.RspamdURL, cfg.RspamdPassword)

	for {
		clt, err := iscan.NewClient(&iscan.Config{
			ServerAddr:            cfg.ImapAddr,
			User:                  cfg.ImapUser,
			Password:              cfg.ImapPassword,
			ScanMailbox:           cfg.ScanMailbox,
			InboxMailbox:          cfg.InboxMailbox,
			HamMailbox:            cfg.HamMailbox,
			SpamMailboxName:       cfg.SpamMailbox,
			UndetectedMailboxName: cfg.UndetectedMailbox,
			BackupMailbox:         cfg.BackupMailbox,
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

		installSigHandler(logger, clt)

		err = clt.Start()
		if err != nil {
			_ = clt.Stop()
			rError := &iscan.ErrRetryable{}
			if !errors.As(err, &rError) {
				logger.Error("non-retryable error occurred, terminating", "error", err)
				os.Exit(1)
			}

			logger.Error("retryable error occurred, restarting monitoring iscan process", "error", err)
			continue
		}
		_ = clt.Stop()
		logger.Info("iscan process terminated normally, shutting down")
		return
	}
}
