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
	checkUrl string
	hamURL   string
	logger   *slog.Logger
	password string
}

func New(logger *slog.Logger, url, password string) *Client {
	return &Client{
		checkUrl: url + "/checkv2",
		hamURL:   url + "/learnham",
		logger:   logger.WithGroup("rspamc").With("server", url),
		password: password,
	}
}

func (c *Client) sendRequest(ctx context.Context, url string, msg io.Reader, result any) error {
	logger := c.logger.With("url", url)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, msg)
	if err != nil {
		return nil
	}

	req.Header.Add("password", c.password)

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

	const contentTypeJson = "application/json"
	ctype := resp.Header.Get("Content-Type")
	if ctype != contentTypeJson {
		// TODO: cancel context first
		_, _ = io.Copy(io.Discard, resp.Body)
		return fmt.Errorf("got response with content-type: %q, expecting: %q", ctype, contentTypeJson)
	}

	if result == nil {
		buf, err := io.ReadAll(resp.Body)
		if err != nil {
			logger.Error("rspamc reading http error body failed", "error", err)
		}
		if len(buf) != 0 {
			logger.Warn("expected no response body but got one", "response", string(buf))
		}
		return nil
	}

	err = json.NewDecoder(resp.Body).Decode(result)
	if err != nil {
		return err
	}

	return nil
}

func (c *Client) Check(ctx context.Context, msg io.Reader) (*Result, error) {
	var result Result
	err := c.sendRequest(ctx, c.checkUrl, msg, &result)
	if err != nil {
		return nil, err
	}
	return &result, err
}

func (c *Client) Ham(ctx context.Context, msg io.Reader) error {
	var resp learnHamReponse

	err := c.sendRequest(ctx, c.hamURL, msg, &resp)
	if err != nil {
		return err
	}

	// resp code 208 == already learned, returns an json with an "error"
	// field

	// It is also unsuccessful if the same message has already learned
	// before
	// if !resp.Success {
	// 	return errors.New("unsuccessful")
	// }

	return nil
}

type learnHamReponse struct {
	Success bool `json:"success"`
}

type Result struct {
	Action    string            `json:"action"`
	Score     float32           `json:"score"`
	IsSkipped bool              `json:"is_skipped"`
	Symbols   map[string]Symbol `json:"symbols"`
}

// https://rspamd.com/doc/architecture/protocol.html#protocol-basics
type Symbol struct {
	Name  string  `json:"name"`
	Score float32 `json:"score"`
}
