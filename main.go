package main

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/fho/rspamd-iscan/internal/config"
	"github.com/fho/rspamd-iscan/internal/imapclt"
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
	dryRun       bool
}

func mustParseFlags() *flags {
	var result flags

	flag.StringVar(&result.cfgPath, "cfg-file", "/etc/rspamd-iscan/config.toml",
		"Path to the rspamd-iscan config file")
	flag.BoolVar(&result.printVersion, "version", false,
		"print the version and exit")
	flag.BoolVar(&result.once, "once", false,
		"processes all mails in the ham, spam and scan mailbox once and terminates",
	)
	flag.BoolVarP(&result.dryRun, "dry-run", "n", false,
		"simulates modifying operations on the IMAP server, also enables --once",
	)

	flag.Parse()

	if result.dryRun {
		result.once = true
	}

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

func newIMAPClient(cfg *config.Config, flags *flags, logger *slog.Logger) (iscan.IMAPClient, error) {
	var clt iscan.IMAPClient

	imapCfg := imapclt.Config{
		Address:       cfg.ImapAddr,
		User:          cfg.ImapUser,
		Password:      cfg.ImapPassword,
		AllowInsecure: false,
		Logger:        logger,
		LogIMAPData:   cfg.LogIMAPData,
	}

	if flags.dryRun {
		fmt.Println("--dry-run enabled, IMAP mailboxes are not modified")
		clt = imapclt.NewDryClient(&imapCfg)
	} else {
		clt = imapclt.NewClient(&imapCfg)
	}

	if err := clt.Connect(); err != nil {
		return nil, err
	}

	return clt, nil
}

func newIscanClient(
	cfg *config.Config,
	logger *slog.Logger,
	rspamc iscan.RspamdClient,
	imapClt iscan.IMAPClient,
) (*iscan.Client, error) {
	iscanCfg := iscan.Config{
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
		IMAPClient:            imapClt,
	}

	return iscan.NewClient(&iscanCfg)
}

func runOnceAndTerminate(
	cfg *config.Config,
	flags *flags,
	logger *slog.Logger,
	rspamc iscan.RspamdClient,
) {
	imapClt, err := newIMAPClient(cfg, flags, logger)
	if err != nil {
		logger.Error("creating iscan client failed", "error", err)
		os.Exit(1)
	}

	clt, err := newIscanClient(cfg, logger, rspamc, imapClt)
	if err != nil {
		logger.Error("creating iscan client failed", "error", err)
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
	flags *flags,
	logger *slog.Logger,
	rspamc iscan.RspamdClient,
) {
	retryTimeout := 60 * time.Second
	for {
		err := monitor(cfg, flags, logger, rspamc)
		if err != nil {
			rError := &iscan.ErrRetryable{}
			if !errors.As(err, &rError) {
				logger.Error("non-retryable error occurred, terminating", "error", err)
				os.Exit(1)
			}

			logger.Error("retryable error occurred, restarting iscan monitoring process after pause", "error", err, "pause", retryTimeout)
			time.Sleep(retryTimeout)
			continue
		}

		logger.Info("iscan process terminated normally, shutting down")
		os.Exit(0)
	}
}

func monitor(
	cfg *config.Config,
	flags *flags,
	logger *slog.Logger,
	rspamc iscan.RspamdClient,
) error {
	imapClt, err := newIMAPClient(cfg, flags, logger)
	if err != nil {
		return fmt.Errorf("creating imap client failed: %w", err)
	}

	clt, err := newIscanClient(cfg, logger, rspamc, imapClt)
	if err != nil {
		return err
	}

	installSigHandler(logger, clt)
	defer removeSigHandler()

	err = clt.Monitor()
	_ = clt.Stop()
	if err != nil {
		return fmt.Errorf("monitoring imap mailboxes failed: %w", err)
	}

	return nil
}

func main() {
	flags := mustParseFlags()
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
	fmt.Print(cfg.String())

	// TODO: allow passing all attrs as single URL to rspamc http client
	rspamc := rspamc.New(logger, cfg.RspamdURL, cfg.RspamdPassword)

	if flags.once {
		fmt.Printf("Running 1x and terminating (--once).\n\n")
		runOnceAndTerminate(cfg, flags, logger, rspamc)
	} else {
		fmt.Printf("Monitoring IMAP mailboxes continuously.\n\n")
		mustMonitorUntilFatalError(cfg, flags, logger, rspamc)
	}
}
