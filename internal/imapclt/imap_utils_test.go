package imapclt

import (
	"testing"
	"time"

	"github.com/fho/rspamd-iscan/internal/log"
	"github.com/fho/rspamd-iscan/internal/testutils/assert"
	"github.com/fho/rspamd-iscan/internal/testutils/imapserver"
)

const (
	testMailSubject   = "An RFC 822 formatted message"
	testMailRecipient = "someone_else@example.com"
	testMailSender    = "someone@example.com"
)

func testClientCfg(t *testing.T, srv *imapserver.Server) *Config {
	return &Config{
		Address:       srv.ListenAddr,
		User:          srv.UserName,
		Password:      srv.UserPasswd,
		AllowInsecure: true,
		Logger:        log.SlogTestLogger(t),
	}
}

func startServerClient(t *testing.T) (*imapserver.Server, *Client) {
	srv := imapserver.StartServer(t)
	return srv, newTestClient(t, srv)
}

func newTestClient(t *testing.T, srv *imapserver.Server) *Client {
	var err error

	clt := NewClient(testClientCfg(t, srv))

	// we retry connecting because the server might not have finished
	// startup

	for range 9 {
		err = clt.Connect()
		if err != nil {
			t.Logf("establishing connection to imap server failed (server still starting?): %s", err)
			time.Sleep(500 * time.Millisecond)
			continue
		}
		break
	}

	assert.NoError(t, err)

	t.Logf("connection to imap server established successfully")

	t.Cleanup(func() { clt.Close() })

	return clt
}
