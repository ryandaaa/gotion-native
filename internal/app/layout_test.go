package app

import (
	"bytes"
	"encoding/json"
	"image/png"
	"os"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/richtext"

	"gotion/internal/doc"
	"gotion/internal/notion"
)

func TestPaddingNeverSqueezesText(t *testing.T) {
	for _, w := range []float32{0, 200, 360, 640, 900, 1600} {
		for _, full := range []bool{false, true} {
			pad := padding(w, full)
			if w > 0 && w-2*pad < min(minText, w-32) {
				t.Errorf("width %v full %v: text %v wide", w, full, w-2*pad)
			}
		}
	}
	if pad := padding(1600, false); 1600-2*pad != readWidth {
		t.Errorf("wide panel: text %v wide, want %v", 1600-2*pad, readWidth)
	}
}

const tablePage = `[
 {"object":"block","id":"h","type":"heading_2","heading_2":{"rich_text":[{"type":"text","plain_text":"Pembagian Peran","annotations":{"color":"default"}}]}},
 {"object":"block","id":"p","type":"paragraph","paragraph":{"rich_text":[{"type":"text","plain_text":"Setiap anggota punya peran.","annotations":{"color":"default"}}]}},
 {"object":"block","id":"tb","type":"table","has_children":true,"table":{"has_column_header":true},"children":[
  {"object":"block","id":"r0","type":"table_row","table_row":{"cells":[[{"type":"text","plain_text":"No"}],[{"type":"text","plain_text":"Nama"}],[{"type":"text","plain_text":"Peran"}]]}},
  {"object":"block","id":"r1","type":"table_row","table_row":{"cells":[[{"type":"text","plain_text":"1."}],[{"type":"text","plain_text":"Ryanda"}],[{"type":"text","plain_text":"PM / DevOps"}]]}},
  {"object":"block","id":"r2","type":"table_row","table_row":{"cells":[[{"type":"text","plain_text":"2."}],[{"type":"text","plain_text":"Naila"}],[{"type":"text","plain_text":"SA"}]]}},
  {"object":"block","id":"r3","type":"table_row","table_row":{"cells":[[{"type":"text","plain_text":"3."}],[{"type":"text","plain_text":"Dwi"}],[{"type":"text","plain_text":"SA"}]]}},
  {"object":"block","id":"r4","type":"table_row","table_row":{"cells":[[{"type":"text","plain_text":"4."}],[{"type":"text","plain_text":"Nuraini"}],[{"type":"text","plain_text":"UI/UX"}]]}}]},
 {"object":"block","id":"dv","type":"divider","divider":{}},
 {"object":"block","id":"h2","type":"heading_2","heading_2":{"rich_text":[{"type":"text","plain_text":"Fase Proyek","annotations":{"color":"default"}}]}},
 {"object":"block","id":"p2","type":"paragraph","paragraph":{"rich_text":[{"type":"text","plain_text":"Isi setelah tabel.","annotations":{"color":"default"}}]}}
]`

func tableApp(t *testing.T) (*App, richtext.Text) {
	var blocks []*notion.Block
	if err := json.Unmarshal([]byte(tablePage), &blocks); err != nil {
		t.Fatal(err)
	}
	text, _, _ := doc.Build("ppsi", blocks, doc.Shape{})
	a := &App{client: notion.New("x"), bot: &notion.User{}, panels: [2]*panel{{}, nil}}
	a.set = settings{Theme: "light", SidebarWidth: 270, Open: map[string]bool{}, Closed: map[string]bool{}, Toggles: map[string]bool{}}
	a.nodes = map[string]*node{"ppsi": {ID: "ppsi", Title: "PPSI", Kind: "page"}, "two": {ID: "two", Title: "Dua", Kind: "page"}}
	a.regroup()
	a.panels[0] = &panel{id: "ppsi", doc: richtext.NewDocument(text), title: "PPSI"}
	RegisterFonts()
	return a, text
}

func snapshot(t *testing.T, tester *ui.Tester, name string) []byte {
	var b bytes.Buffer
	png.Encode(&b, tester.Image())
	if dir := os.Getenv("SHOT"); dir != "" {
		os.WriteFile(dir+"/"+name+".png", b.Bytes(), 0o644)
	}
	return b.Bytes()
}

// TestSplitSettlesAtOnce: after opening or closing a second panel the
// layout is stable and no panel's text is squeezed. The Tester settles a
// frame fully, so it cannot see the one stale frame a real window showed
// until the next event; that is checked by hand.
func TestSplitSettlesAtOnce(t *testing.T) {
	a, text := tableApp(t)
	tester := ui.NewTester(a.View, 1600, 900)
	tester.Frame()
	if a.widths[0] == 0 {
		t.Fatal("the panel's width was not measured")
	}
	a.panels[1] = &panel{id: "two", doc: richtext.NewDocument(text), title: "Dua"}
	a.guessSplit()
	tester.Frame()
	first := snapshot(t, tester, "split-first")
	if tester.FramePending() {
		t.Error("a frame is still pending after the split settled")
	}
	tester.Frame()
	if !bytes.Equal(first, snapshot(t, tester, "split-second")) {
		t.Error("the split drew differently on the next frame: it flickers")
	}
	for i, w := range a.widths {
		if w-2*padding(w, false) < minText {
			t.Errorf("panel %d: text %v wide", i, w-2*padding(w, false))
		}
	}
	// Closing it settles the same way.
	a.closeSecond()
	tester.Frame()
	closed := snapshot(t, tester, "split-closed")
	tester.Frame()
	if !bytes.Equal(closed, snapshot(t, tester, "split-closed-2")) {
		t.Error("closing the split flickers")
	}
}

// TestScrolledTableStaysInEditor renders a table in a narrow, scrolled
// editor; SHOT=<dir> saves it to look at.
func TestScrolledTableStaysInEditor(t *testing.T) {
	a, text := tableApp(t)
	a.panels[1] = &panel{id: "two", doc: richtext.NewDocument(text), title: "Dua"}
	tester := ui.NewTester(a.View, 1280, 380)
	tester.Frame()
	a.panels[0].doc.ScrollBy(120)
	a.panels[1].doc.ScrollBy(120)
	tester.Frame()
	snapshot(t, tester, "table-scrolled")
}
