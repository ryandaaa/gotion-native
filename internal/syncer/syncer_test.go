package syncer

import (
	"context"
	"encoding/json"
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

// fake is a Notion that records the writes it gets.
type fake struct {
	mu     sync.Mutex
	writes []string
}

func (f *fake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	path := strings.TrimPrefix(r.URL.Path, "/v1")
	switch {
	case r.Method == "GET" && path == "/pages/p":
		io.WriteString(w, `{"object":"page","id":"p","last_edited_time":"2026-11-24T10:00:00Z","last_edited_by":{"id":"user"}}`)
	case r.Method == "GET" && path == "/blocks/p/children":
		io.WriteString(w, `{"results":[
		 {"object":"block","id":"a","type":"paragraph","paragraph":{"rich_text":[{"type":"text","plain_text":"one","text":{"content":"one"},"annotations":{"color":"default"}}]}},
		 {"object":"block","id":"b","type":"paragraph","paragraph":{"rich_text":[{"type":"text","plain_text":"two","text":{"content":"two"},"annotations":{"color":"default"}}]}}
		],"has_more":false}`)
	default:
		f.mu.Lock()
		f.writes = append(f.writes, r.Method+" "+path+" "+string(body))
		f.mu.Unlock()
		if strings.HasSuffix(path, "/children") {
			io.WriteString(w, `{"results":[{"object":"block","id":"new1","type":"paragraph","paragraph":{"rich_text":[]}}]}`)
			return
		}
		io.WriteString(w, `{"object":"block","id":"x"}`)
	}
}

func TestPushSendsOnlyChanges(t *testing.T) {
	f := &fake{}
	srv := httptest.NewServer(f)
	defer srv.Close()
	c := notion.New("t")
	c.Base = srv.URL + "/v1"
	s, text, _, err := Open(context.Background(), c, "bot", "p")
	if err != nil {
		t.Fatal(err)
	}
	d := richtext.NewDocument(text)
	d.IdentifyParagraphs(doc.NewID)
	// Edit "one", add a paragraph after it, and delete "two".
	d.SetSelection(3, 3)
	d.InsertText("!\nthree")
	end := d.Text().Len()
	d.SetSelection(len("one!\nthree"), end)
	d.InsertText("")
	s.Edited(d.Text())
	s.push(context.Background())
	if st, err := s.Status(); st != Synced {
		t.Fatalf("status %v %v; writes %v", st, err, f.writes)
	}
	got := strings.Join(f.writes, "\n")
	for _, want := range []string{`DELETE /blocks/b`, `PATCH /blocks/a {"paragraph":`, `"content":"one!"`, `PATCH /blocks/p/children`, `"after_block":{"id":"a"}`, `"content":"three"`} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %s in\n%s", want, got)
		}
	}
	// Nothing changed: nothing sent.
	f.writes = nil
	s.Edited(d.Text())
	s.push(context.Background())
	if len(f.writes) != 0 {
		t.Errorf("sent %v without a change", f.writes)
	}
	_ = json.Valid
	_ = time.Second
}
