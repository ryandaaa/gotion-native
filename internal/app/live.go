package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/websocket"
)

// liveEvent is an event of the workspace, as the Worker relays Notion's
// webhooks.
type liveEvent struct {
	ID        string `json:"id"`
	Type      string `json:"type"`
	Timestamp string `json:"timestamp"`
	Entity    struct {
		ID   string `json:"id"`
		Type string `json:"type"`
	} `json:"entity"`
	Data struct {
		PageID string `json:"page_id"`
		Parent struct {
			ID   string `json:"id"`
			Type string `json:"type"`
		} `json:"parent"`
		UpdatedBlocks []struct {
			ID string `json:"id"`
		} `json:"updated_blocks"`
	} `json:"data"`
	Authors []struct {
		ID   string `json:"id"`
		Type string `json:"type"`
	} `json:"authors"`
}

var (
	// livePoll is how often the app asks the Worker for events while it has
	// no socket.
	livePoll = 8 * time.Second
	// liveRetry is how long after a socket fails the app tries one again,
	// at most; it doubles from a second up to this.
	liveRetry = 30 * time.Second
	// livePing keeps the socket open through proxies; the Worker answers
	// without waking.
	livePing = 30 * time.Second
	// liveNoSocketRetry is how often an app polling because the Worker has
	// no /live (an older deploy) looks for one again.
	liveNoSocketRetry = 5 * time.Minute
)

// errNoLive is the Worker without /live: the app polls instead.
var errNoLive = errors.New("no live socket here")

// startLive follows the workspace's events while signed in with Notion's
// login, whose integration has the webhook: pages made, moved or deleted
// show in the sidebar at once, and a page open goes fresh when it changes
// elsewhere. The Worker pushes them down a WebSocket as they come; while it
// can't, the app asks it every few seconds, and after a socket comes back it
// asks once for what it missed.
func (a *App) startLive() {
	if a.liveCancel != nil || a.client == nil || a.client.Refresh == nil || oauthURL() == "" {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.liveCancel = cancel
	client, base := a.client, oauthURL()
	l := &liveFeed{
		poll: livePoll, retry: liveRetry, ping: livePing, noSocketRetry: liveNoSocketRetry,
		base:  base,
		token: client.CurrentToken,
		since: time.Now().UTC().Format(time.RFC3339),
		seen:  map[string]bool{},
		deliver: func(events []liveEvent) {
			a.update(func() { a.onEvents(events) })
		},
	}
	go l.run(ctx)
}

func (a *App) stopLive() {
	if a.liveCancel != nil {
		a.liveCancel()
		a.liveCancel = nil
	}
}

// liveFeed is the events' way in: a socket, or polling while there is none.
type liveFeed struct {
	// The intervals, copied from the package's when the feed starts.
	poll, retry, ping, noSocketRetry time.Duration

	base    string
	token   func() string
	deliver func([]liveEvent)

	mu    sync.Mutex
	since string          // the newest event's time, for /events
	seen  map[string]bool // events delivered, by ID: each once
	order []string
}

func (l *liveFeed) run(ctx context.Context) {
	backoff := time.Second
	noSocketUntil := time.Time{}
	for ctx.Err() == nil {
		if time.Now().After(noSocketUntil) {
			// Whatever came while there was no socket, then the socket.
			l.fetch(ctx)
			err := l.socket(ctx)
			if ctx.Err() != nil {
				return
			}
			switch {
			case errors.Is(err, errNoLive):
				noSocketUntil = time.Now().Add(l.noSocketRetry)
			case err == nil:
				backoff = time.Second // it was up a while: start over
			default:
				backoff = min(backoff*2, l.retry)
			}
		}
		// No socket now: poll until the next try.
		wait := backoff
		if time.Now().Before(noSocketUntil) {
			wait = l.poll
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		if time.Now().Before(noSocketUntil) {
			l.fetch(ctx)
		}
	}
}

// socket follows the Worker's socket until it closes: nil when it was up and
// closed, errNoLive when the Worker has none, another error when it failed.
func (l *liveFeed) socket(ctx context.Context) error {
	u := strings.Replace(strings.Replace(l.base, "https://", "wss://", 1), "http://", "ws://", 1) + "/live"
	cfg, err := websocket.NewConfig(u, l.base)
	if err != nil {
		return err
	}
	cfg.Header = http.Header{"Authorization": {"Bearer " + l.token()}}
	dctx, dcancel := context.WithTimeout(ctx, 15*time.Second)
	ws, err := cfg.DialContext(dctx)
	dcancel()
	if err != nil {
		var de *websocket.DialError
		if errors.As(err, &de) && errors.Is(de.Err, websocket.ErrBadStatus) {
			if l.status(ctx) == http.StatusNotFound {
				return errNoLive
			}
		}
		return err
	}
	defer ws.Close()
	done := make(chan struct{})
	defer close(done)
	go func() {
		t := time.NewTicker(l.ping)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				ws.Close()
				return
			case <-done:
				return
			case <-t.C:
				if websocket.Message.Send(ws, "ping") != nil {
					ws.Close()
					return
				}
			}
		}
	}()
	for {
		var msg string
		if err := websocket.Message.Receive(ws, &msg); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return nil // closed after being up: reconnect soon
		}
		if msg == "pong" {
			continue
		}
		var e liveEvent
		if json.Unmarshal([]byte(msg), &e) != nil || e.ID == "" {
			continue // the hello, or something not an event
		}
		l.take([]liveEvent{e})
	}
}

// status asks /live without upgrading, to tell a Worker without it (404)
// from one that refused the socket for now.
func (l *liveFeed) status(ctx context.Context) int {
	req, err := http.NewRequestWithContext(ctx, "GET", l.base+"/live", nil)
	if err != nil {
		return 0
	}
	req.Header.Set("Authorization", "Bearer "+l.token())
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return 0
	}
	resp.Body.Close()
	return resp.StatusCode
}

// fetch asks the Worker for the events since the newest delivered.
func (l *liveFeed) fetch(ctx context.Context) {
	l.mu.Lock()
	since := l.since
	l.mu.Unlock()
	req, err := http.NewRequestWithContext(ctx, "GET", l.base+"/events?since="+since, nil)
	if err != nil {
		return
	}
	req.Header.Set("Authorization", "Bearer "+l.token())
	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	var out struct {
		Events []liveEvent `json:"events"`
	}
	if json.NewDecoder(resp.Body).Decode(&out) != nil {
		return
	}
	l.take(out.Events)
}

// take delivers the events not delivered yet, each once whether it came by
// the socket or the polling, and moves since on.
func (l *liveFeed) take(events []liveEvent) {
	l.mu.Lock()
	var fresh []liveEvent
	for _, e := range events {
		if e.Timestamp > l.since {
			l.since = e.Timestamp
		}
		if e.ID == "" || l.seen[e.ID] {
			continue
		}
		l.seen[e.ID] = true
		l.order = append(l.order, e.ID)
		fresh = append(fresh, e)
	}
	// Remember the last thousand: enough for any overlap of the two ways.
	if n := len(l.order); n > 1000 {
		for _, id := range l.order[:n-1000] {
			delete(l.seen, id)
		}
		l.order = append([]string(nil), l.order[n-1000:]...)
	}
	l.mu.Unlock()
	if len(fresh) > 0 {
		l.deliver(fresh)
	}
}

// ownEdit tells an event made by gotion's own edits only: its authors are
// all the integration, so the page open already shows it.
func ownEdit(e liveEvent, botID string) bool {
	if botID == "" || len(e.Authors) == 0 {
		return false
	}
	for _, au := range e.Authors {
		if au.ID != botID {
			return false
		}
	}
	return true
}

// onEvents does what the workspace's events ask: the sidebar again for
// pages made, moved or gone; a page open, fresh; its comments, again.
func (a *App) onEvents(events []liveEvent) {
	tree := false
	today := false
	botID := ""
	if a.bot != nil {
		botID = a.bot.ID
	}
	for _, e := range events {
		switch e.Type {
		case "page.created", "page.deleted", "page.undeleted", "page.moved", "page.properties_updated",
			"database.created", "database.deleted", "data_source.created", "data_source.deleted":
			tree = true
			today = true
		}
		if len(e.Type) > 8 && e.Type[:8] == "comment." {
			a.handleCommentEvent(e)
		}
		for _, pn := range a.panels {
			if pn == nil || pn.id == "" {
				continue
			}
			switch {
			case e.Type == "page.content_updated" && e.Entity.ID == pn.id && pn.session != nil:
				// What gotion itself sent is on screen already: no reload, no
				// flicker under the caret.
				if !ownEdit(e, botID) {
					// Only the blocks it names, where it names them.
					var ids []string
					for _, b := range e.Data.UpdatedBlocks {
						ids = append(ids, b.ID)
					}
					pn.session.RefreshBlocks(ids)
				}
			case len(e.Type) > 8 && e.Type[:8] == "comment." && pn.comments.loaded:
				a.loadComments(pn)
			}
		}
	}
	if tree {
		a.refresh()
	} else if today {
		a.loadToday()
	}
}
