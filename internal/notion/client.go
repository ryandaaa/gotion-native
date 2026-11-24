// Package notion is a small client for the Notion REST API, pinned to one
// API version, with a shared rate limit and retries for 429 and 529.
package notion

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"
)

const (
	BaseURL = "https://api.notion.com/v1"
	Version = "2026-03-11"
)

// Client talks to Notion with one integration token.
type Client struct {
	Token string
	HTTP  *http.Client
	Base  string

	mu   sync.Mutex
	next time.Time // the earliest the next request may start
	gap  time.Duration
}

func New(token string) *Client {
	return &Client{Token: token, HTTP: &http.Client{Timeout: 30 * time.Second}, Base: BaseURL, gap: 350 * time.Millisecond}
}

// Error is an error response from Notion.
type Error struct {
	Status  int    `json:"status"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string { return fmt.Sprintf("notion %d %s: %s", e.Status, e.Code, e.Message) }

// IsNotFound reports whether err is a 404 from Notion.
func IsNotFound(err error) bool {
	var e *Error
	return errors.As(err, &e) && e.Status == 404
}

// wait blocks until the client may send another request, keeping under
// Notion's average of three requests a second.
func (c *Client) wait(ctx context.Context) error {
	c.mu.Lock()
	now := time.Now()
	start := c.next
	if start.Before(now) {
		start = now
	}
	c.next = start.Add(c.gap)
	c.mu.Unlock()
	if d := time.Until(start); d > 0 {
		select {
		case <-time.After(d):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

// pause delays every request for d, after a 429.
func (c *Client) pause(d time.Duration) {
	c.mu.Lock()
	if t := time.Now().Add(d); t.After(c.next) {
		c.next = t
	}
	c.mu.Unlock()
}

// Do sends a request with body encoded as JSON, and decodes the response
// into out when it is not nil.
func (c *Client) Do(ctx context.Context, method, path string, body, out any) error {
	var payload []byte
	if body != nil {
		var err error
		if payload, err = json.Marshal(body); err != nil {
			return err
		}
	}
	for attempt := 0; ; attempt++ {
		if err := c.wait(ctx); err != nil {
			return err
		}
		req, err := http.NewRequestWithContext(ctx, method, c.Base+path, bytes.NewReader(payload))
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+c.Token)
		req.Header.Set("Notion-Version", Version)
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := c.HTTP.Do(req)
		if err != nil {
			if attempt < 3 && (method == http.MethodGet || method == http.MethodDelete) {
				time.Sleep(time.Duration(attempt+1) * time.Second)
				continue
			}
			return err
		}
		data, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return err
		}
		if resp.StatusCode == 429 || resp.StatusCode == 529 ||
			(resp.StatusCode >= 500 && (method == http.MethodGet || method == http.MethodDelete)) {
			if attempt < 5 {
				d := time.Duration(attempt+1) * time.Second
				if s, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil {
					d = time.Duration(s) * time.Second
				}
				c.pause(d)
				continue
			}
		}
		if resp.StatusCode >= 300 {
			e := &Error{Status: resp.StatusCode}
			json.Unmarshal(data, e)
			e.Status = resp.StatusCode
			return e
		}
		if out != nil {
			return json.Unmarshal(data, out)
		}
		return nil
	}
}
