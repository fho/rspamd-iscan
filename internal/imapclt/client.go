package imapclt

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"sync"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"

	"github.com/fho/rspamd-iscan/internal/log"
)

const (
	defChanBufSiz = 1
	dialTimeout   = 120 * time.Second
)

type Client struct {
	clt    *imapclient.Client
	logger *slog.Logger

	newMessagesCh chan<- *EventNewMessages
	mu            sync.Mutex
}

type Config struct {
	// Address is the address of the IMAP server. If the port is "993" or
	// "imaps" an implicit TLS (SSL) is established.
	// Otherwise a explicitl TLS (STARTTLS) connection is established.
	Address  string
	User     string
	Password string
	// AllowInsecure enables falling back to establishing the
	// connection without encryption when the server does not support TLS
	AllowInsecure bool
	Logger        *slog.Logger
}

type EventNewMessages struct {
	NewMsgCount uint32
}

// Connect establishes a connection with the IMAP server and returns a new
// Client.
func Connect(cfg *Config) (*Client, error) {
	result := newClient(cfg)

	clt, err := result.dial(cfg.Address, cfg.AllowInsecure, &imapclient.Options{
		UnilateralDataHandler: &imapclient.UnilateralDataHandler{
			Mailbox: result.mailboxUpdateHandler,
		},
		Dialer: &net.Dialer{Timeout: dialTimeout},
	})
	if err != nil {
		return nil, fmt.Errorf("establishing imap server connection failed: %w", err)
	}
	result.clt = clt

	if err := clt.Login(cfg.User, cfg.Password).Wait(); err != nil {
		return nil, fmt.Errorf("login at imap server failed: %w", err)
	}

	result.logger.Info("connection established, authentication succeeded",
		"event", "imap.connection_established")

	return result, nil
}

func newClient(cfg *Config) *Client {
	result := Client{}
	result.logger = log.SloggerWithGroup(cfg.Logger, "imapclt")

	return &result
}

func (c *Client) Close() error {
	return c.clt.Close()
}

func (c *Client) dial(address string, allowInsecure bool, opts *imapclient.Options) (*imapclient.Client, error) {
	_, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}

	logger := c.logger.With("server", address).With("timeout", dialTimeout)

	if port == "993" || port == "imaps" {
		logger.Debug("connecting to imap server", "tlsmode", "implicit")
		return imapclient.DialTLS(address, opts)
	}

	logger.Debug("connecting to imap server", "tlsmode", "explicit")
	clt, err := imapclient.DialStartTLS(address, opts)
	if err != nil && allowInsecure && isStartTLSNotSupportedErr(err) {
		logger.Warn("establishing secure connection failed, connecting without encryption", "tlsmode", "none", "error", err)
		return imapclient.DialInsecure(address, opts)
	}

	return clt, err
}

func isStartTLSNotSupportedErr(err error) bool {
	var imapErr *imap.Error

	if errors.As(err, &imapErr) {
		return imapErr.Text == "STARTTLS not supported"
	}

	return false
}

func (c *Client) mailboxUpdateHandler(d *imapclient.UnilateralDataMailbox) {
	if d.NumMessages == nil {
		c.logger.Debug("ignoring mailbox update with nil NumMessages")
		return
	}

	c.logger.Debug("received mailbox update", "num_messages", *d.NumMessages)

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.newMessagesCh == nil {
		c.logger.Warn("ignoring mailbox update message, event channel is nil", "num_messages", *d.NumMessages)
		return
	}

	sendEventNewMessages(c.newMessagesCh, *d.NumMessages)
}

// Upload reads a message (mail) from file and appends it to an imap mailbox.
// The internal date of the message is set to ts.
func (c *Client) Upload(path, mailbox string, ts time.Time) error {
	fi, err := os.Stat(path)
	if err != nil {
		return err
	}

	fd, err := os.Open(path)
	if err != nil {
		return err
	}
	defer fd.Close()

	appendCmd := c.clt.Append(mailbox, fi.Size(), &imap.AppendOptions{Time: ts})

	_, err = io.Copy(appendCmd, fd)
	if err != nil {
		_ = appendCmd.Close()
		return fmt.Errorf("uploading mail to imap mailbox failed: %w", err)
	}

	err = appendCmd.Close()
	if err != nil {
		return fmt.Errorf("closing append command failed: %w", err)
	}

	_, err = appendCmd.Wait()
	if err != nil {
		return fmt.Errorf("waiting for append to finish failed: %w", err)
	}

	c.logger.Debug(
		"uploaded messages to imap mailbox",
		lkMailbox, mailbox,
		"event", "imap.messages_uploaded",
		"filepath", path,
	)

	return nil
}

// Monitor starts to monitor mailbox for new messages.
// When new messages are found an event is sent to ch.
// Message delivery to ch must not block. If delievery would block the
// message is discarded.
//
// While Monitor is running, running other IMAP operations will block forever!
// To issue other IMAP operations, the returned stop function must be called
// before!
func (c *Client) Monitor(mailbox string) (
	_ <-chan *EventNewMessages, stop func() error, _ error,
) {
	logger := c.logger.With("mailbox", mailbox)
	logger.Debug("starting to monitor mailbox for changes")

	ch := make(chan *EventNewMessages, defChanBufSiz)

	d, err := c.clt.Select(mailbox, &imap.SelectOptions{ReadOnly: true}).Wait()
	if err != nil {
		return nil, nil, fmt.Errorf("selecting mailbox %q failed: %w", mailbox, err)
	}

	if d.NumMessages != 0 {
		logger.Debug("mailbox has new message, skipping monitoring",
			"count", d.NumMessages,
		)
		sendEventNewMessages(ch, d.NumMessages)
		close(ch)
		return ch, func() error { return nil }, nil
	}

	c.setNewMessagesCH(ch)

	idlecmd, err := c.clt.Idle()
	if err != nil {
		c.setNewMessagesCH(nil)
		close(ch)
		return nil, nil, err
	}

	return ch, func() error {
		logger.Debug("stopping idle command")
		err := errors.Join(idlecmd.Close(), idlecmd.Wait())
		c.setNewMessagesCH(nil)
		close(ch)
		return err
	}, nil
}

func sendEventNewMessages(ch chan<- *EventNewMessages, newMessages uint32) {
	select {
	case ch <- &EventNewMessages{NewMsgCount: newMessages}:
	default:
	}
}

func asUIDSet(uids []uint32) imap.UIDSet {
	var result imap.UIDSet

	for _, uid := range uids {
		result.AddNum(imap.UID(uid))
	}
	return result
}

// Move moves the messages with the given uids to mailbox.
func (c *Client) Move(uids []uint32, mailbox string) error {
	if len(uids) == 0 {
		return errors.New("no uids were given")
	}

	_, err := c.clt.Move(asUIDSet(uids), mailbox).Wait()
	if err != nil {
		return err
	}

	c.logger.Debug(
		"moved imap messages",
		lkMailbox, mailbox,
		"count", len(uids),
		"event", "imap.messages_moved",
	)
	return err
}

func (c *Client) setNewMessagesCH(ch chan<- *EventNewMessages) {
	c.mu.Lock()
	c.newMessagesCh = ch
	c.mu.Unlock()
}
