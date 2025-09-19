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
	once         bool
}

func parseFlags() *flags {
	var result flags

	flag.StringVar(&result.cfgPath, "cfg-file", "/etc/rspamd-iscan/config.toml",
		"Path to the rspamd-iscan config file")
	flag.BoolVar(&result.printVersion, "version", false,
		"print the version and exit")
	flag.BoolVar(&result.once, "once", false,
		"processes all mails in the ham, spam and scan mailbox once and terminates",
	)

	flag.Parse()

	return &result
}

func configureLogger() *slog.Logger {
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

	return slog.New(h)
}

var handledSignals = []os.Signal{syscall.SIGTERM, syscall.SIGINT}

func removeSigHandler() {
	signal.Reset(handledSignals...)
}

func installSigHandler(logger *slog.Logger, clt *iscan.Client) {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, handledSignals...)

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

func newIscanClient(
	cfg *config.Config,
	logger *slog.Logger,
	rspamc iscan.RspamdClient,
) (*iscan.Client, error) {
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
		logger.Error("creating iscan client failed", "error", err)
	}

	return clt, err
}

func runOnceAndTerminate(
	cfg *config.Config,
	logger *slog.Logger,
	rspamc iscan.RspamdClient,
) {
	clt, err := newIscanClient(cfg, logger, rspamc)
	if err != nil {
		os.Exit(1)
	}

	if err := clt.RunOnce(); err != nil {
		logger.Error(err.Error())
		os.Exit(1)
	}
	os.Exit(0)
}

func mustMonitorUntilFatalError(
	cfg *config.Config,
	logger *slog.Logger,
	rspamc iscan.RspamdClient,
) {
	for {
		err := monitor(cfg, logger, rspamc)
		if err != nil {
			rError := &iscan.ErrRetryable{}
			if !errors.As(err, &rError) {
				logger.Error("non-retryable error occurred, terminating", "error", err)
				os.Exit(1)
			}

			logger.Error("retryable error occurred, restarting iscan monitoring process", "error", err)
			continue
		}

		logger.Info("iscan process terminated normally, shutting down")
		os.Exit(0)
	}
}

func monitor(
	cfg *config.Config,
	logger *slog.Logger,
	rspamc iscan.RspamdClient,
) error {
	clt, err := newIscanClient(cfg, logger, rspamc)
	if err != nil {
		return err
	}

	installSigHandler(logger, clt)
	defer removeSigHandler()

	err = clt.Monitor()
	if err != nil {
		_ = clt.Stop()
		return fmt.Errorf("monitoring imap mailboxes failed: %w", err)
	}

	_ = clt.Stop()
	return nil
}

func main() {
	flags := parseFlags()
	if flags.printVersion {
		fmt.Printf("rspamd-iscan %s (%s)\n", version, commit)
		os.Exit(0)
	}

	logger := configureLogger()

	cfg, err := config.FromFile(flags.cfgPath)
	if err != nil {
		logger.Error("loading config failed", "error", err)
		os.Exit(1)
	}

	cfg.SetDefaults()

	fmt.Println(cfg.String())

	// TODO: allow passing all attrs as single URL to rspamc http client
	rspamc := rspamc.New(logger, cfg.RspamdURL, cfg.RspamdPassword)

	if flags.once {
		runOnceAndTerminate(cfg, logger, rspamc)
	} else {
		mustMonitorUntilFatalError(cfg, logger, rspamc)
	}
}
