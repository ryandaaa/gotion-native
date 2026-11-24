package syncer

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui/richtext"

	"gotion/internal/doc"
	"gotion/internal/notion"
)

// broken is the fake Notion of the tests, with a hook that answers a write
// before the fake does: a block deleted elsewhere, a page that refuses.
type broken struct {
	fake
	mu2   sync.Mutex
	write func(w http.ResponseWriter, method, path string) bool
}

func (b *broken) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/v1")
	if r.Method != "GET" {
		b.mu2.Lock()
		hook := b.write
		b.mu2.Unlock()
		if hook != nil && hook(w, r.Method, path) {
			io.Copy(io.Discard, r.Body)
			return
		}
	}
	b.fake.ServeHTTP(w, r)
}

func openEdited(t *testing.T, base string) (*Session, *richtext.Document) {
	t.Helper()
	c := notion.New("t")
	c.Base = base + "/v1"
	s, text, _, err := Open(context.Background(), c, "bot", "p", doc.Shape{})
	if err != nil {
		t.Fatal(err)
	}
	d := richtext.NewDocument(text)
	d.SetSelection(3, 3)
	d.InsertText("!")
	s.Edited(d.Text())
	return s, d
}

// A block someone deleted in Notion, then edited here: the edit makes the
// block again instead of failing for good.
func TestEditOfABlockDeletedElsewhere(t *testing.T) {
	for _, reply := range []struct {
		name   string
		status int
		body   string
	}{
		{"404", 404, `{"object":"error","status":404,"code":"object_not_found","message":"Could not find block with ID: a."}`},
		{"archived", 400, `{"object":"error","status":400,"code":"validation_error","message":"Can't edit block that is archived. You must unarchive the block before editing."}`},
	} {
		t.Run(reply.name, func(t *testing.T) {
			f := &broken{}
			f.write = func(w http.ResponseWriter, method, path string) bool {
				if method == "PATCH" && path == "/blocks/a" {
					w.WriteHeader(reply.status)
					io.WriteString(w, reply.body)
					return true
				}
				return false
			}
			srv := httptest.NewServer(f)
			defer srv.Close()
			s, _ := openEdited(t, srv.URL)
			s.push(context.Background())
			if st, err := s.Status(); st != Synced {
				t.Fatalf("status %v %v; writes %v", st, err, f.writes)
			}
			if got := strings.Join(f.writes, "\n"); !strings.Contains(got, `PATCH /blocks/p/children`) || !strings.Contains(got, `"content":"one!"`) {
				t.Errorf("the block was not made again: %v", got)
			}
		})
	}
}

// Notion refusing the same edit over and over: one message, not one every
// second and a half, and the tries slow down.
func TestRefusedEditSaysSoOnce(t *testing.T) {
	f := &broken{}
	f.write = func(w http.ResponseWriter, method, path string) bool {
		w.WriteHeader(400)
		io.WriteString(w, `{"object":"error","status":400,"code":"validation_error","message":"body failed validation"}`)
		return true
	}
	srv := httptest.NewServer(f)
	defer srv.Close()
	s, _ := openEdited(t, srv.URL)
	var said []string
	s.OnError = func(m string) { said = append(said, m) }
	for range 4 {
		s.push(context.Background())
	}
	if st, _ := s.Status(); st != Failed {
		t.Fatalf("status %v", st)
	}
	if len(said) != 1 {
		t.Errorf("said %d times: %q", len(said), said)
	}
	s.mu.Lock()
	wait := time.Until(s.retryAt)
	s.mu.Unlock()
	if wait < 5*time.Second {
		t.Errorf("after four refusals the next try is in %v", wait)
	}
}

// Quitting while offline, the moment after typing: the edit is on disk
// for the next run, though the run loop never got to write it.
func TestQuitOfflineKeepsTheLastEdit(t *testing.T) {
	f := &broken{}
	srv := httptest.NewServer(f)
	CacheDir = t.TempDir()
	defer func() { CacheDir = "" }()
	s, _ := openEdited(t, srv.URL)
	srv.Close() // offline
	start := time.Now()
	s.Close()
	if d := time.Since(start); d > 10*time.Second {
		t.Errorf("closing took %v", d)
	}
	if _, err := os.Stat(cacheFile("p", "unsent")); err != nil {
		t.Fatalf("the edit was not kept: %v", err)
	}
	c := notion.New("t")
	c.Base = srv.URL + "/v1"
	s2, text, _, err := Open(context.Background(), c, "bot", "p", doc.Shape{})
	if err != nil {
		t.Fatal(err)
	}
	if st, _ := s2.Status(); st != Pending || !strings.Contains(text.PlainText(), "one!") {
		t.Errorf("status %v text %q", st, text.PlainText())
	}
}

// A connection that hangs: closing the page gives up after a while
// instead of holding the window for the client's whole timeout.
func TestCloseDoesNotHangOnAStalledNetwork(t *testing.T) {
	f := &broken{}
	release := make(chan struct{})
	f.write = func(w http.ResponseWriter, method, path string) bool {
		<-release
		return false
	}
	srv := httptest.NewServer(f)
	defer srv.Close()
	defer close(release)
	CacheDir = t.TempDir()
	defer func() { CacheDir = "" }()
	s, _ := openEdited(t, srv.URL)
	start := time.Now()
	s.Close()
	if d := time.Since(start); d > closeWait+2*time.Second {
		t.Errorf("closing took %v", d)
	}
	if _, err := os.Stat(cacheFile("p", "unsent")); err != nil {
		t.Errorf("the edit was not kept: %v", err)
	}
}

// The words the user reads for what failed: no raw URLs or Go errors.
func TestExplain(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.Close()
	c := notion.New("t")
	c.Base = srv.URL + "/v1"
	_, offline := c.Me(context.Background())
	if got := Explain(offline); strings.Contains(got, "127.0.0.1") || strings.Contains(got, "dial") {
		t.Errorf("offline: %q", got)
	}
	if got := Explain(&notion.Error{Status: 401, Code: "unauthorized", Message: "API token is invalid."}); strings.Contains(got, "401") || strings.Contains(got, "invalid") {
		t.Errorf("401: %q", got)
	}
	if got := Explain(&notion.Error{Status: 400, Message: "body failed validation"}); got != "body failed validation" {
		t.Errorf("400: %q", got)
	}
}
