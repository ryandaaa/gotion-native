package app

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui/richtext"

	"gotion/internal/doc"
	"gotion/internal/notion"
	"gotion/internal/syncer"
)

func texts(d *richtext.Document) string {
	var out []string
	for _, p := range paragraphsOf(d) {
		out = append(out, paragraphText(d.Text(), 0)[:0]+runsText(p))
	}
	return strings.Join(out, "|")
}

func threeBlocks() *richtext.Document {
	para := func(id, text string) richtext.Paragraph {
		return richtext.Paragraph{Style: richtext.ParagraphStyle{ID: id}, Runs: []richtext.Run{{Text: text}}}
	}
	return richtext.NewDocument(richtext.TextOf(para("a", "satu"), para("b", "dua"), para("cont:b:1", "dua lanjut"), para("c", "tiga")))
}

func TestMoveBlockCarriesItsLines(t *testing.T) {
	d := threeBlocks()
	// Move "dua" (with its later line) to the end.
	if !moveBlock(d, 1, 4) {
		t.Fatal("not moved")
	}
	if got := texts(d); got != "satu|tiga|dua|dua lanjut" {
		t.Errorf("order %q", got)
	}
	ps := paragraphsOf(d)
	if ps[2].Style.ID == "b" || !doc.IsLocal(ps[2].Style.ID) || ps[3].Style.ID == "cont:b:1" {
		t.Errorf("a moved block keeps its old IDs: %q %q", ps[2].Style.ID, ps[3].Style.ID)
	}
	// Moving within itself is no move.
	d = threeBlocks()
	if moveBlock(d, 1, 2) {
		t.Error("moved onto itself")
	}
}

func TestDuplicateDeleteRestyle(t *testing.T) {
	d := threeBlocks()
	duplicateBlock(d, 0)
	if got := texts(d); got != "satu|satu|dua|dua lanjut|tiga" {
		t.Errorf("duplicate %q", got)
	}
	if ps := paragraphsOf(d); ps[0].Style.ID != "a" || ps[1].Style.ID == "a" {
		t.Error("the copy must be a new block, the original kept")
	}
	d = threeBlocks()
	deleteBlock(d, 2) // a later line of "dua": the whole block goes
	if got := texts(d); got != "satu|tiga" {
		t.Errorf("delete %q", got)
	}
	d = threeBlocks()
	restyleBlock(d, 1, richtext.ParagraphStyle{Heading: 2})
	ps := paragraphsOf(d)
	if ps[1].Style.Heading != 2 || ps[2].Style.Heading != 2 || ps[1].Style.ID != "b" {
		t.Errorf("restyle %+v %+v", ps[1].Style, ps[2].Style)
	}
}

// TestMoveSyncs: a moved block reaches Notion as the old one deleted and a
// new one after the block it now follows; a block with children refuses.
func TestMoveSyncs(t *testing.T) {
	rec := &recorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		path := strings.TrimPrefix(r.URL.Path, "/v1")
		rec.mu.Lock()
		rec.sent = append(rec.sent, r.Method+" "+path+" "+string(b))
		rec.mu.Unlock()
		switch {
		case path == "/pages/p":
			io.WriteString(w, `{"object":"page","id":"p","last_edited_by":{"id":"u"}}`)
		case path == "/blocks/p/children" && r.Method == "GET":
			io.WriteString(w, `{"results":[
			 {"object":"block","id":"a","type":"paragraph","paragraph":{"rich_text":[{"type":"text","plain_text":"satu","annotations":{"color":"default"}}]}},
			 {"object":"block","id":"b","type":"paragraph","paragraph":{"rich_text":[{"type":"text","plain_text":"dua","annotations":{"color":"default"}}]}},
			 {"object":"block","id":"c","type":"toggle","has_children":true,"toggle":{"rich_text":[{"type":"text","plain_text":"tiga","annotations":{"color":"default"}}]}}],"has_more":false}`)
		default:
			io.WriteString(w, `{"results":[{"object":"block","id":"new","type":"paragraph","paragraph":{"rich_text":[]}}]}`)
		}
	}))
	defer srv.Close()
	c := notion.New("x")
	c.Base = srv.URL + "/v1"
	s, text, _, err := syncer.Open(context.Background(), c, "bot", "p", doc.Shape{})
	if err != nil {
		t.Fatal(err)
	}
	a := &App{}
	pn := &panel{session: s, doc: richtext.NewDocument(text)}
	// The toggle has children: refused, nothing changes.
	a.dropBlock(pn, 2, 0)
	if got := texts(pn.doc); !strings.HasPrefix(got, "satu|dua|") {
		t.Errorf("a block with children moved: %q", got)
	}
	if len(a.toasts) == 0 {
		t.Error("no word why")
	}
	// Move "satu" after "dua", then sync.
	a.dropBlock(pn, 0, 2)
	s.Start()
	defer s.Close()
	if !waitSent(rec, "DELETE /blocks/a") || !waitSent(rec, `"after_block":{"id":"b"}`) {
		t.Fatalf("sent:\n%s", rec.all())
	}
	if !strings.Contains(rec.all(), `"content":"satu"`) {
		t.Errorf("the block was not made again:\n%s", rec.all())
	}
}

func TestAddBelow(t *testing.T) {
	d := threeBlocks()
	addBelow(d, 1) // after "dua" and its later line
	if got := texts(d); got != "satu|dua|dua lanjut||tiga" {
		t.Errorf("%q", got)
	}
	if _, caret := d.Selection(); caret != len("satu\ndua\ndua lanjut\n") {
		t.Errorf("caret %d", caret)
	}
}
