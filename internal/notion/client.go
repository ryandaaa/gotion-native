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

	// A token bucket: up to burst requests at once, then rate a second,
	// as Notion averages three a second and lets bursts through.
	mu          sync.Mutex
	tokens      float64
	last        time.Time
	pausedUntil time.Time // after a 429, nothing goes before this
	rate, burst float64
}

func New(token string) *Client {
	return &Client{Token: token, HTTP: &http.Client{Timeout: 30 * time.Second}, Base: BaseURL, rate: 2.8, burst: 8, tokens: 8}
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

// wait blocks until the client may send another request: at once while
// the bucket holds a token, else when one comes, keeping under Notion's
// average of three requests a second.
func (c *Client) wait(ctx context.Context) error {
	for {
		c.mu.Lock()
		now := time.Now()
		if c.last.IsZero() {
			c.last = now
		}
		c.tokens = min(c.burst, c.tokens+now.Sub(c.last).Seconds()*c.rate)
		c.last = now
		var d time.Duration
		switch {
		case now.Before(c.pausedUntil):
			d = c.pausedUntil.Sub(now)
		case c.tokens >= 1:
			c.tokens--
			c.mu.Unlock()
			return nil
		default:
			d = time.Duration((1 - c.tokens) / c.rate * float64(time.Second))
		}
		c.mu.Unlock()
		select {
		case <-time.After(d):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// pause holds every request for d, after a 429, and empties the bucket.
func (c *Client) pause(d time.Duration) {
	c.mu.Lock()
	if t := time.Now().Add(d); t.After(c.pausedUntil) {
		c.pausedUntil = t
	}
	c.tokens = 0
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
