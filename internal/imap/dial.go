package imap

import (
	"crypto/tls"
	"net"
	"time"

	"github.com/emersion/go-imap/v2/imapclient"
)

const dialTimeout = 120 * time.Second

func (c *Client) dial(address string, opts *imapclient.Options) (*imapclient.Client, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}

	dialer := net.Dialer{
		Timeout: dialTimeout,
	}

	logger := c.logger.With("server", address).With("timeout", dialTimeout)

	if port == "993" || port == "imaps" {
		logger.Debug("connecting to imap server", "tlsmode", "implicit")
		return dialImplicitTLS(&dialer, address, opts)
	}

	logger.Debug("connecting to imap server", "tlsmode", "explicit")
	return dialExplicitTLS(&dialer, address, host, opts)
}

func dialImplicitTLS(dialer *net.Dialer, address string, opts *imapclient.Options) (*imapclient.Client, error) {
	tlsConfig := tls.Config{
		NextProtos: []string{"imap"},
	}

	conn, err := tls.DialWithDialer(dialer, "tcp", address, &tlsConfig)
	if err != nil {
		return nil, err
	}

	return imapclient.New(conn, opts), nil
}

func dialExplicitTLS(dialer *net.Dialer, address, host string, opts *imapclient.Options) (*imapclient.Client, error) {
	con, err := dialer.Dial("tcp", address)
	if err != nil {
		return nil, err
	}

	newOptions := *opts
	newOptions.TLSConfig = &tls.Config{
		ServerName: host,
	}

	return imapclient.NewStartTLS(con, opts)
}
