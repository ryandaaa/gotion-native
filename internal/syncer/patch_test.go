package syncer

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui/richtext"

	"gotion/internal/doc"
	"gotion/internal/notion"
)

// counting is a Notion whose block b reads "edited", that tells each GET it
// got, and that can say a block is gone.
type counting struct {
	mu    sync.Mutex
	gets  []string
	b     string
	trash bool
}

func (f *counting) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/v1")
	f.mu.Lock()
	f.gets = append(f.gets, r.Method+" "+path)
	text, trash := f.b, f.trash
	f.mu.Unlock()
	para := func(id, s string) string {
		return `{"object":"block","id":"` + id + `","type":"paragraph","paragraph":{"rich_text":[{"type":"text","plain_text":"` + s + `","text":{"content":"` + s + `"},"annotations":{"color":"default"}}]}}`
	}
	switch {
	case path == "/pages/p":
		io.WriteString(w, `{"object":"page","id":"p","last_edited_time":"2026-11-24T10:00:00Z","last_edited_by":{"id":"user"}}`)
	case path == "/blocks/p/children":
		io.WriteString(w, `{"results":[`+para("a", "one")+`,`+para("b", text)+`],"has_more":false}`)
	case path == "/blocks/b" && trash:
		w.WriteHeader(404)
		io.WriteString(w, `{"object":"error","status":404,"code":"object_not_found","message":"gone"}`)
	case path == "/blocks/b":
		io.WriteString(w, para("b", text))
	default:
		w.WriteHeader(404)
		io.WriteString(w, `{"object":"error","status":404,"code":"object_not_found","message":"no"}`)
	}
}

func (f *counting) took() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	g := f.gets
	f.gets = nil
	return g
}

// openKept opens page p once (caching it in full), then again from the
// cache, as an app run after the first.
func openKept(t *testing.T, f *counting) (*Session, chan string) {
	t.Helper()
	CacheDir = t.TempDir()
	t.Cleanup(func() { CacheDir = "" })
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	c := notion.New("t")
	c.Base = srv.URL + "/v1"
	first, _, _, err := Open(context.Background(), c, "bot", "p", doc.Shape{})
	if err != nil {
		t.Fatal(err)
	}
	first.pull(context.Background(), true) // the whole page, into the cache
	s, _, _, err := Open(context.Background(), c, "bot", "p", doc.Shape{})
	if err != nil {
		t.Fatal(err)
	}
	if s.tree == nil {
		t.Fatal("the cache gave no tree")
	}
	got := make(chan string, 8)
	s.OnRemote = func(text richtext.Text, _ []doc.Extra) {
		var b strings.Builder
		for _, p := range text.Paragraphs() {
			for _, r := range p.Runs {
				b.WriteString(r.Text)
			}
			b.WriteString("|")
		}
		got <- b.String()
	}
	s.force = false
	f.took()
	return s, got
}

func waitText(t *testing.T, got chan string) string {
	t.Helper()
	select {
	case s := <-got:
		return s
	case <-time.After(3 * time.Second):
		t.Fatal("the page was not shown again")
	}
	return ""
}

// A change to one block fetches that block, not the page's tree.
func TestRefreshBlocksFetchesOnlyThem(t *testing.T) {
	f := &counting{b: "two"}
	s, got := openKept(t, f)
	f.mu.Lock()
	f.b = "edited"
	f.mu.Unlock()
	s.RefreshBlocks([]string{"b"})
	s.pull(context.Background(), true)
	if txt := waitText(t, got); !strings.Contains(txt, "edited") || !strings.Contains(txt, "one") {
		t.Fatalf("shown %q", txt)
	}
	for _, g := range f.took() {
		if strings.Contains(g, "/children") {
			t.Fatalf("fetched the tree: %s", g)
		}
	}
}

// A block deleted elsewhere leaves the page without a full fetch.
func TestRefreshBlocksDeleted(t *testing.T) {
	f := &counting{b: "two"}
	s, got := openKept(t, f)
	f.mu.Lock()
	f.trash = true
	f.mu.Unlock()
	s.RefreshBlocks([]string{"b"})
	s.pull(context.Background(), true)
	if txt := waitText(t, got); strings.Contains(txt, "two") {
		t.Fatalf("deleted block still shown: %q", txt)
	}
}

// A block the page didn't have (a new one): the whole page, to place it.
func TestRefreshBlocksNewFallsBack(t *testing.T) {
	f := &counting{b: "two"}
	s, got := openKept(t, f)
	s.RefreshBlocks([]string{"zzz"})
	s.pull(context.Background(), true)
	waitText(t, got)
	tree := false
	for _, g := range f.took() {
		if strings.Contains(g, "/blocks/p/children") {
			tree = true
		}
	}
	if !tree {
		t.Fatal("a new block did not bring the whole page")
	}
}

// Opening or shutting a toggle asks Notion nothing.
func TestReshapeNeedsNoFetch(t *testing.T) {
	f := &counting{b: "two"}
	s, got := openKept(t, f)
	s.Reshape(doc.Shape{OpenToggles: map[string]bool{"a": true}})
	if !s.reshape || s.force {
		t.Fatalf("reshape %v force %v", s.reshape, s.force)
	}
	s.rebuild()
	waitText(t, got)
	if g := f.took(); len(g) != 0 {
		t.Fatalf("a toggle asked Notion: %v", g)
	}
}

// While the user types, a change elsewhere does not replace their text.
func TestRefreshBlocksWhileTyping(t *testing.T) {
	f := &counting{b: "two"}
	s, got := openKept(t, f)
	pending := richtext.Text{}
	s.pending = &pending
	s.RefreshBlocks([]string{"b"})
	s.pull(context.Background(), true)
	select {
	case txt := <-got:
		t.Fatalf("replaced the text being typed with %q", txt)
	case <-time.After(200 * time.Millisecond):
	}
}
