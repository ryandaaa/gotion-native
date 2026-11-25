package app

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/net/websocket"
)

// fakeWorker is the Worker's /events and /live, the socket pushing what
// push sends.
type fakeWorker struct {
	srv      *httptest.Server
	push     chan string
	conns    atomic.Int32
	polls    atomic.Int32
	noLive   bool
	auth     atomic.Value
	mu       sync.Mutex
	events   string // /events' answer
	dropNext atomic.Bool
}

func newFakeWorker(t *testing.T, noLive bool) *fakeWorker {
	f := &fakeWorker{push: make(chan string, 16), noLive: noLive, events: `{"events":[]}`}
	ws := websocket.Server{Handler: func(c *websocket.Conn) {
		f.conns.Add(1)
		websocket.Message.Send(c, `{"type":"hello"}`)
		if f.dropNext.Swap(false) {
			return // closed at once: the app must come back
		}
		for msg := range f.push {
			if websocket.Message.Send(c, msg) != nil {
				return
			}
		}
	}}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.auth.Store(r.Header.Get("Authorization"))
		switch {
		case r.URL.Path == "/events":
			f.polls.Add(1)
			f.mu.Lock()
			io.WriteString(w, f.events)
			f.mu.Unlock()
		case r.URL.Path == "/live" && !f.noLive && strings.EqualFold(r.Header.Get("Upgrade"), "websocket"):
			ws.ServeHTTP(w, r)
		case r.URL.Path == "/live" && !f.noLive:
			w.WriteHeader(426)
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(func() { f.srv.Close() })
	return f
}

func collect() (func([]liveEvent), func() []string) {
	var mu sync.Mutex
	var got []string
	return func(es []liveEvent) {
			mu.Lock()
			for _, e := range es {
				got = append(got, e.ID)
			}
			mu.Unlock()
		}, func() []string {
			mu.Lock()
			defer mu.Unlock()
			return append([]string(nil), got...)
		}
}

// runFeed runs l until the test ends, and waits for it to stop before the
// test's cleanups put the intervals back.
func runFeed(t *testing.T, l *liveFeed) context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { l.run(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	return ctx
}

// feed is a liveFeed on f with short intervals.
func feed(base string, deliver func([]liveEvent)) *liveFeed {
	return &liveFeed{poll: 30 * time.Millisecond, retry: 100 * time.Millisecond, ping: time.Second, noSocketRetry: time.Hour,
		base: base, token: func() string { return "tok" }, seen: map[string]bool{}, deliver: deliver}
}

// An event pushed down the socket arrives at once, with the token, and an
// event that comes both ways arrives once.
func TestLiveSocketPushes(t *testing.T) {
	f := newFakeWorker(t, false)
	deliver, got := collect()
	l := feed(f.srv.URL, deliver)
	ctx := runFeed(t, l)
	if !waitFor2(func() bool { return f.conns.Load() == 1 }) {
		t.Fatal("no socket")
	}
	start := time.Now()
	f.push <- `{"id":"e1","type":"page.content_updated","timestamp":"2099-01-01T00:00:01Z","entity":{"id":"p","type":"page"}}`
	if !waitFor2(func() bool { return len(got()) == 1 }) {
		t.Fatal("pushed event never arrived")
	}
	if d := time.Since(start); d > time.Second {
		t.Fatalf("pushed event took %v", d)
	}
	// The same event, polled after a reconnect: not again.
	f.mu.Lock()
	f.events = `{"events":[{"id":"e1","type":"page.content_updated","timestamp":"2099-01-01T00:00:01Z"},{"id":"e2","type":"page.created","timestamp":"2099-01-01T00:00:02Z"}]}`
	f.mu.Unlock()
	l.fetch(ctx)
	if g := got(); len(g) != 2 || g[1] != "e2" {
		t.Fatalf("delivered %v, want e1 then e2 once each", g)
	}
	if a, _ := f.auth.Load().(string); a != "Bearer tok" {
		t.Fatalf("auth %q", a)
	}
}

// A socket that closes is opened again, and what came meanwhile is asked
// for.
func TestLiveSocketReconnects(t *testing.T) {
	f := newFakeWorker(t, false)
	f.dropNext.Store(true)
	deliver, _ := collect()
	l := feed(f.srv.URL, deliver)
	runFeed(t, l)
	if !waitFor2(func() bool { return f.conns.Load() >= 2 }) {
		t.Fatalf("socket not opened again: %d", f.conns.Load())
	}
	if f.polls.Load() < 2 {
		t.Fatalf("missed events not asked for after reconnecting: %d polls", f.polls.Load())
	}
}

// A Worker without /live (an older deploy): the app polls, as before.
func TestLiveNoSocketPolls(t *testing.T) {
	f := newFakeWorker(t, true)
	f.mu.Lock()
	f.events = `{"events":[{"id":"x","type":"page.created","timestamp":"2099-01-01T00:00:00Z"}]}`
	f.mu.Unlock()
	deliver, got := collect()
	l := feed(f.srv.URL, deliver)
	runFeed(t, l)
	if !waitFor2(func() bool { return f.polls.Load() >= 4 }) {
		t.Fatalf("not polling: %d", f.polls.Load())
	}
	if g := got(); len(g) != 1 {
		t.Fatalf("delivered %v, want x once", g)
	}
	if f.conns.Load() != 0 {
		t.Fatal("a socket on a Worker without one")
	}
}

// Worker down altogether: no crash, no busy loop, and it comes back.
func TestLiveWorkerDown(t *testing.T) {
	f := newFakeWorker(t, false)
	base := f.srv.URL
	f.srv.Close()
	deliver, _ := collect()
	l := feed(base, deliver)
	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()
	done := make(chan struct{})
	go func() { l.run(ctx); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("feed did not stop with its context")
	}
}

// gotion's own edits come back as events: those the page already shows.
func TestOwnEdit(t *testing.T) {
	var e liveEvent
	e.Authors = append(e.Authors, struct {
		ID   string `json:"id"`
		Type string `json:"type"`
	}{ID: "bot", Type: "bot"})
	if !ownEdit(e, "bot") {
		t.Fatal("own edit not seen")
	}
	e.Authors = append(e.Authors, struct {
		ID   string `json:"id"`
		Type string `json:"type"`
	}{ID: "person", Type: "person"})
	if ownEdit(e, "bot") {
		t.Fatal("someone else's edit taken for gotion's")
	}
	if ownEdit(liveEvent{}, "bot") {
		t.Fatal("an event without authors taken for gotion's")
	}
}
