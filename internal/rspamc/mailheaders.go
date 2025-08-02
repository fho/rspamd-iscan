package rspamc

import (
	"net/http"
)

// MailHeaders contains optional pre-processed email data, to prevent redundant
// processing of mail headers in rspamd
type MailHeaders struct {
	DeliverTo  string
	From       []string
	Recipients []string
	Subject    string
}

func (h *MailHeaders) asHeader() http.Header {
	result := http.Header{}

	if h.DeliverTo != "" {
		result.Add("Deliver-To", h.DeliverTo)
	}

	if h.Subject != "" {
		result.Add("Subject", h.Subject)
	}

	for _, rcpt := range h.Recipients {
		result.Add("Rcpt", rcpt)
	}

	for _, rcpt := range h.From {
		result.Add("From", rcpt)
	}

	return result
}
