package imapserver

import (
	"errors"
	"testing"

	"github.com/emersion/go-imap/v2/imapserver"
	"github.com/emersion/go-imap/v2/imapserver/imapmemserver"
)

type Server struct {
	UserName   string
	UserPasswd string
	ListenAddr string

	BackupMailbox     string
	HamMailbox        string
	InboxMailBox      string
	ScanMailbox       string
	SpamMailbox       string
	UndetectedMailbox string

	srv *imapserver.Server
	ch  chan error
}

func StartServer(t *testing.T) *Server {
	srv := Server{
		UserName:          "user",
		UserPasswd:        "none",
		ListenAddr:        "localhost:10143",
		ch:                make(chan error, 2),
		InboxMailBox:      "INBOX",
		ScanMailbox:       "unscanned",
		BackupMailbox:     "backup",
		HamMailbox:        "ham",
		SpamMailbox:       "spam",
		UndetectedMailbox: "undetected",
	}

	user := imapmemserver.NewUser(srv.UserName, srv.UserPasswd)
	createMailbox(t, user, srv.BackupMailbox)
	createMailbox(t, user, srv.HamMailbox)
	createMailbox(t, user, srv.InboxMailBox)
	createMailbox(t, user, srv.ScanMailbox)
	createMailbox(t, user, srv.SpamMailbox)
	createMailbox(t, user, srv.UndetectedMailbox)

	msrv := imapmemserver.New()
	msrv.AddUser(user)

	isrv := imapserver.New(&imapserver.Options{
		NewSession: func(*imapserver.Conn) (imapserver.Session, *imapserver.GreetingData, error) {
			return msrv.NewSession(), nil, nil
		},
		Logger:       testLoggerAsImapServerLogger(t),
		InsecureAuth: true,
	})

	t.Cleanup(func() { _ = isrv.Close() })
	go func() {
		err := isrv.ListenAndServe(srv.ListenAddr)
		srv.ch <- err
		close(srv.ch)
	}()

	return &srv
}

func createMailbox(t *testing.T, user *imapmemserver.User, mailboxName string) {
	if err := user.Create(mailboxName, nil); err != nil {
		t.Fatalf("creating %s mailbox failed: %s", mailboxName, err)
	}
}

func (s *Server) Close() error {
	err := s.srv.Close()

	for chErr := range s.ch {
		err = errors.Join(err, chErr)
	}
	return err
}
