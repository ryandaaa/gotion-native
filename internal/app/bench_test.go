package app

import (
	"fmt"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/richtext"

	"gotion/internal/doc"
	"gotion/internal/notion"
)

// bigBlocks is a long page: n blocks of the kinds a page holds.
func bigBlocks(n int) []*notion.Block {
	var bs []*notion.Block
	kinds := []string{"paragraph", "bulleted_list_item", "heading_2", "to_do", "paragraph", "numbered_list_item"}
	for i := 0; i < n; i++ {
		k := kinds[i%len(kinds)]
		text := fmt.Sprintf("Baris %d: %s", i, strings.Repeat("kata yang cukup panjang ", 6))
		payload := fmt.Sprintf(`{"rich_text":[{"type":"text","plain_text":%q,"text":{"content":%q},"annotations":{"bold":%v,"color":"default"}}]}`, text, text, i%7 == 0)
		bs = append(bs, &notion.Block{Object: "block", ID: fmt.Sprintf("b%05d", i), Type: k, Payload: []byte(payload)})
	}
	return bs
}

func BenchmarkBuild2000(b *testing.B) {
	bs := bigBlocks(2000)
	for b.Loop() {
		doc.Build("p", bs, doc.Shape{})
	}
}

func BenchmarkItems2000(b *testing.B) {
	text, known, _ := doc.Build("p", bigBlocks(2000), doc.Shape{})
	for b.Loop() {
		doc.Items(text, known)
	}
}

// BenchmarkKeystroke2000 is what the app does on its side for one key
// typed in a 2000-block page, besides the editor's own work: check the
// read-only lines and look for the slash menu.
func BenchmarkKeystroke2000(b *testing.B) {
	text, _, _ := doc.Build("p", bigBlocks(2000), doc.Shape{})
	d := richtext.NewDocument(text)
	ro := map[string]string{"b01000": "x"}
	d.SetSelection(text.Len()/2, text.Len()/2)
	pn := &panel{doc: d}
	a := &App{}
	for b.Loop() {
		brokeReadOnly(d, ro)
		a.slashUpdate(pn)
	}
}

// BenchmarkFrame2000 is one frame of the window on a 2000-block page.
func BenchmarkFrame2000(b *testing.B) {
	text, _, _ := doc.Build("p", bigBlocks(2000), doc.Shape{})
	a := &App{client: notion.New("x"), bot: &notion.User{}, panels: [2]*panel{{}, nil}}
	a.set = settings{Theme: "light", SidebarWidth: 270, Zoom: 1, Open: map[string]bool{}, Closed: map[string]bool{}, Toggles: map[string]bool{}}
	a.nodes = map[string]*node{"p": {ID: "p", Title: "Panjang", Kind: "page"}}
	for i := 0; i < 300; i++ {
		id := fmt.Sprintf("n%d", i)
		a.nodes[id] = &node{ID: id, Title: "Halaman " + id, Kind: "page", ParentID: "p"}
	}
	a.set.Open["p"] = true
	a.regroup()
	a.panels[0] = &panel{id: "p", doc: richtext.NewDocument(text), title: "Panjang"}
	RegisterFonts()
	tester := ui.NewTester(a.View, 1280, 800)
	tester.Frame()
	for b.Loop() {
		a.panels[0].doc.SetSelection(10, 10)
		a.panels[0].doc.InsertText("x")
		tester.Frame()
	}
}
