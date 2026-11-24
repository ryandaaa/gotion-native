package app

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
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

func runsText(p richtext.Paragraph) string {
	var b strings.Builder
	for _, r := range p.Runs {
		b.WriteString(r.Text)
	}
	return b.String()
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

// TestDragGrip hovers a block, takes its grip and drops it lower: the
// block moves. The grip is found by its pixels: its dots in the margin.
func TestDragGrip(t *testing.T) {
	a := tableApp(t)
	pn := &panel{id: "ppsi", title: "Urutan", doc: threeBlocks()}
	a.panels[0] = pn
	tester := ui.NewTester(a.View, 1100, 600)
	tester.Frame()
	vis := pn.doc.VisibleParagraphs()
	if len(vis) < 4 {
		t.Fatalf("visible %v", vis)
	}
	// The editor's top in the window: the first line's text, found by
	// moving down the margin until the grip shows for paragraph 0.
	var top float32 = -1
	for y := float32(80); y < 400 && top < 0; y += 4 {
		tester.Move(500, y)
		tester.Frame()
		if pn.handle.hover == 0 {
			top = y // the box's top: the pointer is over it from there
		}
	}
	if top < 0 {
		t.Fatal("no grip showed over the first paragraph")
	}
	snapshot(t, tester, "grip-hover")
	pad := padding(pn.width, false)
	gx := pad - 30 + 11 + 0 // the grip's middle, in the editor
	// The panel's left edge: the sidebar (270) and its edge (10).
	gx += 280
	gy := top + vis[0].Top + min(vis[0].Height, 26)/2
	tester.Press(gx, gy)
	for y := gy; y < top+vis[3].Top+vis[3].Height-2; y += 6 {
		tester.Move(gx, y)
		tester.Frame()
	}
	snapshot(t, tester, "grip-drag")
	tester.Release(gx, top+vis[3].Top+vis[3].Height-2)
	tester.Frame()
	if got := texts(pn.doc); got != "dua|dua lanjut|tiga|satu" {
		t.Errorf("after the drag: %q", got)
	}
}

func TestGripMenuDuplicates(t *testing.T) {
	a := tableApp(t)
	pn := &panel{id: "ppsi", title: "Urutan", doc: threeBlocks()}
	a.panels[0] = pn
	tester := ui.NewTester(a.View, 1100, 600)
	tester.Frame()
	vis := pn.doc.VisibleParagraphs()
	var top float32 = -1
	for y := float32(80); y < 400 && top < 0; y += 4 {
		tester.Move(500, y)
		tester.Frame()
		if pn.handle.hover == 0 {
			top = y
		}
	}
	gx := padding(pn.width, false) - 30 + 11 + 280
	tester.ClickAt(gx, top+vis[0].Top+min(vis[0].Height, 26)/2)
	tester.Advance(time.Second)
	snapshot(t, tester, "grip-menu")
	if err := tester.Click("Duplikat"); err != nil {
		t.Fatalf("no menu: %v", err)
	}
	tester.Frame()
	if got := texts(pn.doc); got != "satu|satu|dua|dua lanjut|tiga" {
		t.Errorf("after duplicate: %q", got)
	}
}
