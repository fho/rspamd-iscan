package rspamc

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
)

type Client struct {
	checkURL string
	hamURL   string
	spamURL  string
	logger   *slog.Logger
	password string
}

func New(logger *slog.Logger, url, password string) *Client {
	return &Client{
		checkURL: url + "/checkv2",
		hamURL:   url + "/learnham",
		spamURL:  url + "/learnspam",
		logger:   logger.WithGroup("rspamc").With("server", url),
		password: password,
	}
}

func (c *Client) sendRequest(ctx context.Context, url string, hdrs http.Header, msg io.Reader, result any) error {
	logger := c.logger.With("url", url)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, msg)
	if err != nil {
		return nil
	}

	req.Header = hdrs.Clone()
	req.Header.Add("password", c.password)

	// TODO: use custom client with configured timeouts
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil
	}

	if resp.StatusCode != http.StatusOK {
		// TODO: check content length, set max. size of body to read
		buf, err := io.ReadAll(resp.Body)
		if err != nil {
			logger.Error("rspamc reading http error body failed", "error", err)
		}
		logger.Debug("rspamc http response", "body", string(buf), "status", resp.Status)
		if resp.StatusCode >= 200 && resp.StatusCode <= 300 {
			return nil
		}
		return fmt.Errorf("request failed with status: %s", resp.Status)
	}

	const contentTypeJSON = "application/json"
	ctype := resp.Header.Get("Content-Type")
	if ctype != contentTypeJSON {
		return fmt.Errorf("got response with content-type: %q, expecting: %q", ctype, contentTypeJSON)
	}

	if result == nil {
		buf, err := io.ReadAll(resp.Body)
		if err != nil {
			logger.Error("rspamc reading http error body failed", "error", err)
		}
		if len(buf) != 0 {
			logger.Debug("response body is not processed", "body", string(buf))
		}

		return nil
	}

	err = json.NewDecoder(resp.Body).Decode(result)
	if err != nil {
		return err
	}

	return nil
}

func (c *Client) Check(ctx context.Context, msg io.Reader, hdrs *MailHeaders) (*CheckResult, error) {
	var result CheckResult
	// wrap in NopCloser to prevent that http.NewRequest closes the reader,
	// it is not responsible for closing it, the caller is
	err := c.sendRequest(ctx, c.checkURL, hdrs.asHeader(), io.NopCloser(msg), &result)
	if err != nil {
		return nil, err
	}
	return &result, err
}

func (c *Client) Ham(ctx context.Context, msg io.Reader, hdrs *MailHeaders) error {
	// resp code 208 == already learned, returns a json with an "error"
	// field
	return c.sendRequest(ctx, c.hamURL, hdrs.asHeader(), msg, nil)
}

func (c *Client) Spam(ctx context.Context, msg io.Reader, hdrs *MailHeaders) error {
	return c.sendRequest(ctx, c.spamURL, hdrs.asHeader(), msg, nil)
}

type CheckResult struct {
	Action    string             `json:"action"`
	Score     float32            `json:"score"`
	IsSkipped bool               `json:"is_skipped"`
	Symbols   map[string]*Symbol `json:"symbols"`
}

// https://rspamd.com/doc/architecture/protocol.html#protocol-basics
type Symbol struct {
	Name  string  `json:"name"`
	Score float32 `json:"score"`
}
