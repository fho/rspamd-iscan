package main

import (
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/fho/rspamd-iscan/internal/config"
	"github.com/fho/rspamd-iscan/internal/imapclt"
	"github.com/fho/rspamd-iscan/internal/iscan"
	"github.com/fho/rspamd-iscan/internal/neterr"
	"github.com/fho/rspamd-iscan/internal/retry"
	"github.com/fho/rspamd-iscan/internal/rspamc"

	flag "github.com/spf13/pflag"
)

const (
	maxRetriesSameError = 10
)

var (
	version = "version-undefined"
	commit  = "commit-undefined"
)

type flags struct {
	cfgPath              string
	credentialsDirectory string
	printVersion         bool
	once                 bool
	dryRun               bool
}

func mustParseFlags() *flags {
	var result flags

	flag.StringVar(&result.cfgPath, "cfg-file", "/etc/rspamd-iscan/config.toml",
		"Path to the rspamd-iscan config file")
	flag.StringVar(&result.credentialsDirectory, "credentials-directory", os.Getenv("CREDENTIALS_DIRECTORY"),
		"Directory containing credential files (defaults to $CREDENTIALS_DIRECTORY)")
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
) error {
	imapClt, err := newIMAPClient(cfg, flags, logger)
	if err != nil {
		return fmt.Errorf("creating imap client failed: %w", err)
	}

	clt, err := newIscanClient(cfg, logger, rspamc, imapClt)
	if err != nil {
		return fmt.Errorf("creating iscan client failed %w", err)
	}

	return clt.RunOnce()
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

func run() error {
	flags := mustParseFlags()
	if flags.printVersion {
		fmt.Printf("rspamd-iscan %s (%s)\n", version, commit)
		return nil
	}

	logger := configureLogger()

	cfg, err := config.FromFile(flags.cfgPath)
	if err != nil {
		return fmt.Errorf("loading config failed: %w", err)
	}

	if flags.credentialsDirectory != "" {
		if err := cfg.LoadCredentialsFromDirectory(flags.credentialsDirectory); err != nil {
			logger.Error("loading credentials from directory failed", "error", err)
			os.Exit(1)
		}
	}

	cfg.SetDefaults()
	fmt.Print(cfg.String())

	// TODO: allow passing all attrs as single URL to rspamc http client
	rspamc := rspamc.New(logger, cfg.RspamdURL, cfg.RspamdPassword)

	if flags.once {
		logger.Info("running once and terminating (--once)")
		return runOnceAndTerminate(cfg, flags, logger, rspamc)
	}

	logger.Info("monitoring IMAP mailboxes continuously, retrying on retryable errors",
		"max_retries_same_error", maxRetriesSameError)

	retryRunner := retry.Runner{
		Fn:                  func() error { return monitor(cfg, flags, logger, rspamc) },
		IsRetryable:         neterr.IsRetryableError,
		MaxRetriesSameError: maxRetriesSameError,
		RetryIntervals: []time.Duration{
			3 * time.Second,
			30 * time.Second,
			time.Minute,
			3 * time.Minute,
		},
		Logger: logger,
	}

	return retryRunner.Run()
}

func main() {
	if err := run(); err != nil {
		slog.Error(err.Error())
	}
}
