package syncer

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo/ui/richtext"

	"gotion/internal/doc"
	"gotion/internal/notion"
)

// slowNotion answers like Notion, latency late: a page of 30 blocks, 10
// of them with 3 children each, and an image.
func slowNotion(latency time.Duration, requests *atomic.Int32) http.Handler {
	block := func(id, typ string, kids bool) string {
		return fmt.Sprintf(`{"object":"block","id":%q,"type":%q,"has_children":%v,%q:{"rich_text":[{"type":"text","plain_text":"teks %s","annotations":{"color":"default"}}]}}`, id, typ, kids, typ, id)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		time.Sleep(latency)
		path := strings.TrimPrefix(r.URL.Path, "/v1")
		switch {
		case path == "/pages/p":
			io.WriteString(w, `{"object":"page","id":"p","last_edited_time":"2026-11-24T10:00:00Z","last_edited_by":{"id":"u"}}`)
		case path == "/blocks/p/children":
			var bs []string
			for i := 0; i < 30; i++ {
				bs = append(bs, block(fmt.Sprintf("b%d", i), "bulleted_list_item", i%3 == 0))
			}
			bs = append(bs, `{"object":"block","id":"img","type":"image","image":{"type":"external","external":{"url":"http://`+r.Host+`/img.png"}}}`)
			io.WriteString(w, `{"results":[`+strings.Join(bs, ",")+`],"has_more":false}`)
		case strings.HasPrefix(path, "/blocks/"):
			id := strings.Split(path, "/")[2]
			var bs []string
			for i := 0; i < 3; i++ {
				bs = append(bs, block(id+fmt.Sprint("-", i), "paragraph", false))
			}
			io.WriteString(w, `{"results":[`+strings.Join(bs, ",")+`],"has_more":false}`)
		case path == "/img.png":
			w.WriteHeader(404)
		}
	})
}

// TestOpenTime reports how long a page not kept yet takes to open over a
// network of 150ms a request.
func TestOpenTime(t *testing.T) {
	if testing.Short() {
		t.Skip("slow")
	}
	var requests atomic.Int32
	srv := httptest.NewServer(slowNotion(150*time.Millisecond, &requests))
	defer srv.Close()
	c := notion.New("t")
	c.Base = srv.URL + "/v1"
	start := time.Now()
	if _, _, _, err := Open(context.Background(), c, "bot", "p", doc.Shape{}); err != nil {
		t.Fatal(err)
	}
	t.Logf("opened in %v with %d requests", time.Since(start).Round(time.Millisecond), requests.Load())
}

// TestNestedBlocksFollow: a page opens with its own blocks, and what is
// nested in them comes right after, without a change on Notion's side.
func TestNestedBlocksFollow(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(slowNotion(0, &requests))
	defer srv.Close()
	c := notion.New("t")
	c.Base = srv.URL + "/v1"
	s, text, _, err := Open(context.Background(), c, "bot", "p", doc.Shape{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(text.PlainText(), "teks b0-0") {
		t.Fatal("the first text already holds nested blocks: nothing to test")
	}
	got := make(chan string, 1)
	s.OnRemote = func(text richtext.Text, _ []doc.Extra) { got <- text.PlainText() }
	s.Start()
	defer s.Close()
	select {
	case full := <-got:
		if !strings.Contains(full, "teks b0-0") || !strings.Contains(full, "teks b29") {
			t.Errorf("the page that followed lacks blocks:\n%s", full)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the nested blocks never came")
	}
}
