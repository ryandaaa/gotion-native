package app

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/richtext"

	"gotion/internal/doc"
	"gotion/internal/notion"
)

// scrollApp is a window on a long page whose cover, title and properties
// sit above its text.
func scrollApp(t *testing.T) (*App, *ui.Tester) {
	text, _, _ := doc.Build("p", bigBlocks(150), doc.Shape{})
	a := &App{client: notion.New("x"), bot: &notion.User{}, panels: [2]*panel{{}, nil}}
	a.set = settings{Theme: "light", SidebarWidth: 270, Zoom: 1, Open: map[string]bool{}, Closed: map[string]bool{}, Toggles: map[string]bool{}}
	a.nodes = map[string]*node{"p": {ID: "p", Title: "Tab", Kind: "page"}}
	a.panels[0] = &panel{id: "p", doc: richtext.NewDocument(text), title: "Judul Halaman",
		cover: coverState{url: "https://contoh.id/cover.png"},
		// A property under the title, which the tester can find on screen.
		props: &tableView{columns: []notion.Property{{Name: "Tenggat Waktu", Type: "date"}},
			rows: []*notion.Page{{ID: "p", Properties: map[string]json.RawMessage{}}}}}
	RegisterFonts()
	tester := ui.NewTester(a.View, 1200, 700)
	tester.Frame()
	tester.Frame()
	return a, tester
}

// titleShows tells that the top of the page (its title and properties) is
// visible on screen, below the panel's tab strip.
func titleShows(tester *ui.Tester) bool {
	r, ok := tester.Find("Tenggat Waktu")
	return ok && r.W > 0 && r.H > 0 && r.Y > 80 && r.Y < 700
}

// The cover, title and properties scroll away with the text, as in Notion,
// rather than staying over it.
func TestTitleScrollsWithText(t *testing.T) {
	_, tester := scrollApp(t)
	snapshot(t, tester, "scroll-top")
	if !titleShows(tester) {
		r, ok := tester.Find("Tenggat Waktu")
		t.Fatalf("the title does not show at first: %v %v", r, ok)
	}
	tester.Wheel(700, 450, 0, 1500)
	tester.Advance(time.Second)
	if titleShows(tester) {
		t.Error("the title stayed on screen as the page scrolled")
	}
	snapshot(t, tester, "scroll-down")
	tester.Wheel(700, 450, 0, -100000)
	tester.Advance(time.Second)
	if !titleShows(tester) {
		t.Error("scrolled back up, the title does not show")
	}
}

// Typing at the end of a long page brings the caret into view, while still
// letting the user scroll away with the wheel afterwards.
func TestTypingAtTheEndShowsTheCaret(t *testing.T) {
	a, tester := scrollApp(t)
	pn := a.panels[0]
	end := pn.doc.Text().Len()
	pn.doc.SetSelection(end, end)
	pn.focusEditor = true
	tester.Frame()
	tester.Type("akhir")
	tester.Advance(time.Second)
	if titleShows(tester) {
		t.Error("typing at the end left the page at its top")
	}
	snapshot(t, tester, "scroll-typing")

	// While the editor keeps focus, scrolling back up with the wheel works.
	tester.Wheel(700, 450, 0, -100000)
	tester.Advance(time.Second)
	if !titleShows(tester) {
		t.Error("could not scroll back to the top while the editor stayed focused")
	}

	// Typing again brings the offscreen caret back into view.
	tester.Type(" lagi")
	tester.Advance(time.Second)
	if titleShows(tester) {
		t.Error("typing while focused did not scroll the caret back into view")
	}
}
