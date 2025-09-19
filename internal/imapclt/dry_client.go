package imapclt

import "time"

// DryClient is an IMAP client that simulates operations that do changes on the
// IMAP-Server.
type DryClient struct {
	*Client
}

// NewDryClient creates an new IMAP-Client.
// [*DryClient.Connect] must be called before any other methods.
func NewDryClient(cfg *Config) *DryClient {
	return &DryClient{Client: NewClient(cfg)}
}

// Upload logs a debug message and returns nil
func (c *DryClient) Upload(path, mailbox string, _ time.Time) error {
	c.logger.Debug("dry-client: skipping uploading mail to mailbox",
		lkMailbox, mailbox, "filepath", path)
	return nil
}

// Move logs a debug message and returns nil
func (c *DryClient) Move(uids []uint32, mailbox string) error {
	c.logger.Debug("dry-client: skipping moving messages to mailbox",
		lkMailbox, mailbox,
		"count", len(uids),
	)
	return nil
}
