package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image/png"
	"os"
	"testing"
	"time"

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

// tablePanel is a panel showing the table page, its tables drawn across
// its own text width.
func tablePanel(t *testing.T, id, title string) *panel {
	var blocks []*notion.Block
	if err := json.Unmarshal([]byte(tablePage), &blocks); err != nil {
		t.Fatal(err)
	}
	p := &panel{id: id, title: title}
	text, _, _ := doc.Build(id, blocks, doc.Shape{Width: &p.textWidth})
	p.doc = richtext.NewDocument(text)
	return p
}

func tableApp(t *testing.T) *App {
	a := &App{client: notion.New("x"), bot: &notion.User{}, panels: [2]*panel{{}, nil}}
	a.set = settings{Theme: "light", SidebarWidth: 270, Open: map[string]bool{}, Closed: map[string]bool{}, Toggles: map[string]bool{}}
	a.nodes = map[string]*node{"ppsi": {ID: "ppsi", Title: "PPSI", Kind: "page"}, "two": {ID: "two", Title: "Dua", Kind: "page"}}
	a.regroup()
	a.panels[0] = tablePanel(t, "ppsi", "PPSI")
	RegisterFonts()
	return a
}

func snapshot(t *testing.T, tester *ui.Tester, name string) []byte {
	var b bytes.Buffer
	png.Encode(&b, tester.Image())
	if dir := os.Getenv("SHOT"); dir != "" {
		os.WriteFile(dir+"/"+name+".png", b.Bytes(), 0o644)
	}
	return b.Bytes()
}

// TestSplitSettlesAtOnce: after opening or closing a second panel, at
// most one frame is drawn with the old widths; the app asks for the next
// one itself, and from then on every frame draws the same. Before, a
// stale frame stayed on screen until the next mouse move.
func TestSplitSettlesAtOnce(t *testing.T) {
	a := tableApp(t)
	tester := ui.NewTester(a.View, 1600, 900)
	tester.Frame()
	if a.widths[0] == 0 {
		t.Fatal("the panel's width was not measured")
	}
	settles := func(what string) {
		t.Helper()
		tester.Frame() // may draw with the widths of before
		tester.Frame() // the frame it asked for
		settled := snapshot(t, tester, what)
		for j := 0; j < 3; j++ {
			tester.Frame()
			if !bytes.Equal(settled, snapshot(t, tester, what+"-again")) {
				t.Fatalf("%s: frame %d drew differently: it never settles", what, j)
			}
		}
		for i, p := range a.panels {
			if p != nil && p.width > 0 && p.textWidth < min(minText, p.width-32) {
				t.Errorf("%s: panel %d text %v wide", what, i, p.textWidth)
			}
		}
	}
	a.panels[1] = tablePanel(t, "two", "Dua")
	a.guessSplit()
	settles("split")
	a.closeSecond()
	settles("split-closed")
}

// TestScrolledTableStaysInEditor renders a table in a narrow, scrolled
// editor; SHOT=<dir> saves it to look at.
func TestScrolledTableStaysInEditor(t *testing.T) {
	a := tableApp(t)
	a.panels[1] = tablePanel(t, "two", "Dua")
	tester := ui.NewTester(a.View, 1280, 380)
	tester.Frame()
	a.panels[0].doc.ScrollBy(120)
	a.panels[1].doc.ScrollBy(120)
	tester.Frame()
	snapshot(t, tester, "table-scrolled")
}

// TestTableFitsANarrowPanel: in a panel narrower than a table asks for, the
// table is drawn across the panel's text, not shrunk.
func TestTableFitsANarrowPanel(t *testing.T) {
	a := tableApp(t)
	a.set.SplitSize = 900
	a.panels[1] = tablePanel(t, "two", "Dua")
	tester := ui.NewTester(a.View, 1280, 520)
	tester.Frame()
	tester.Frame()
	snapshot(t, tester, "table-narrow")
	for i, p := range a.panels {
		if p.textWidth <= 0 || p.textWidth > p.width {
			t.Errorf("panel %d: text %v wide in a panel %v wide", i, p.textWidth, p.width)
		}
	}
}

func TestZoomSteps(t *testing.T) {
	a := &App{set: settings{Zoom: 1}}
	for _, want := range []float32{1.1, 1.25, 1.5} {
		a.zoom(+1)
		if a.set.Zoom != want {
			t.Fatalf("zoom in: %v, want %v", a.set.Zoom, want)
		}
	}
	a.zoom(-1)
	if a.set.Zoom != 1.25 {
		t.Errorf("zoom out: %v", a.set.Zoom)
	}
	a.set.Zoom = 1.2 // between steps
	if a.zoom(+1); a.set.Zoom != 1.25 {
		t.Errorf("from 120%%: %v", a.set.Zoom)
	}
	a.zoom(0)
	if a.set.Zoom != 1 {
		t.Errorf("reset: %v", a.set.Zoom)
	}
	for i := 0; i < 20; i++ {
		a.zoom(+1)
	}
	if a.set.Zoom != zooms[len(zooms)-1] {
		t.Errorf("max: %v", a.set.Zoom)
	}
}

func TestZoomedWindow(t *testing.T) {
	a := tableApp(t)
	a.set.Zoom = 1.25
	tester := ui.NewTester(a.View, 1366, 768)
	tester.Frame()
	tester.Frame()
	snapshot(t, tester, "zoom-125")
}

// TestSidebarSlides: shutting the sidebar slides it away over a few
// frames, and leaves room on the left of the panels.
func TestSidebarSlides(t *testing.T) {
	a := tableApp(t)
	tester := ui.NewTester(a.View, 1280, 600)
	tester.Frame()
	open := snapshot(t, tester, "sidebar-open")
	a.set.SidebarOff = true
	tester.Frame() // as the click on the button builds one
	tester.Advance(80 * time.Millisecond)
	mid := snapshot(t, tester, "sidebar-closing")
	tester.Advance(2 * time.Second)
	shut := snapshot(t, tester, "sidebar-off")
	if bytes.Equal(open, mid) || bytes.Equal(mid, shut) {
		t.Error("the sidebar does not slide: no frame between open and shut")
	}
	// It comes back the same way.
	a.set.SidebarOff = false
	tester.Frame()
	tester.Advance(2 * time.Second)
	if !bytes.Equal(open, snapshot(t, tester, "sidebar-back")) {
		t.Error("the sidebar came back different")
	}
}

// TestSwitcherFadesIn: the quick switcher comes in over a few frames.
func TestSwitcherFadesIn(t *testing.T) {
	a := tableApp(t)
	tester := ui.NewTester(a.View, 1280, 600)
	tester.Frame()
	a.openSwitcher()
	tester.Frame()
	tester.Advance(60 * time.Millisecond)
	mid := snapshot(t, tester, "switcher-opening")
	tester.Advance(time.Second)
	if bytes.Equal(mid, snapshot(t, tester, "switcher-open")) {
		t.Error("the switcher shows at once, with no frame between")
	}
}

// TestWashes renders each wash, light and dark, with the picker open on
// the first; SHOT=<dir> saves them to look at.
func TestWashes(t *testing.T) {
	for i, w := range washes {
		for _, theme := range []string{"light", "dark"} {
			a := tableApp(t)
			a.set.Theme, a.set.Wash = theme, w.id
			a.themeOpen = i == 1 && theme == "light"
			tester := ui.NewTester(a.View, 1100, 560)
			tester.Frame()
			tester.Advance(time.Second)
			snapshot(t, tester, "wash-"+w.id+"-"+theme)
		}
	}
}

func TestSidebarScrollShot(t *testing.T) {
	a := tableApp(t)
	for i := 0; i < 60; i++ {
		id := fmt.Sprintf("p%02d", i)
		a.nodes[id] = &node{ID: id, Title: "Halaman " + id, Kind: "page", ParentID: "ppsi"}
	}
	a.set.Open["ppsi"] = true
	a.regroup()
	tester := ui.NewTester(a.View, 1000, 500)
	tester.Frame()
	tester.Move(150, 300)
	tester.Frame()
	snapshot(t, tester, "sidebar-scroll")
}
