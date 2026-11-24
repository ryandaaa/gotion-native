package app

import (
	"context"
	"encoding/json"
	"image/png"
	"os"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/richtext"

	"gotion/internal/doc"
	"gotion/internal/emoji"
	"gotion/internal/notion"
)

// TestHero renders the landing page's screenshots when HERO=<dir>.
func TestHero(t *testing.T) {
	dir := os.Getenv("HERO")
	if dir == "" {
		t.Skip("HERO not set")
	}
	var blocks []*notion.Block
	if err := json.Unmarshal([]byte(heroPage), &blocks); err != nil {
		t.Fatal(err)
	}
	for _, v := range []struct{ name, theme, wash string }{{"hero-light", "light", "polos"}, {"hero-dark", "dark", "polos"}} {
		a := tableApp(t)
		a.set.Theme, a.set.Wash = v.theme, v.wash
		a.nodes = map[string]*node{}
		add := func(id, title, e, parent string) {
			a.nodes[id] = &node{ID: id, Title: title, Emoji: e, ParentID: parent, Kind: "page"}
		}
		add("ppsi", "Rencana Liburan", "🏝️", "")
		add("f1", "Itinerary", "🗺️", "ppsi")
		add("f2", "Daftar Bawaan", "🎒", "ppsi")
		add("f3", "Kuliner", "🍜", "ppsi")
		add("jur", "Jurnal Harian", "📝", "")
		add("ide", "Ide Proyek", "💡", "")
		a.nodes["db"] = &node{ID: "db", Title: "Bacaan", Kind: "data_source"}
		a.set.Open["ppsi"] = true
		a.regroup()
		for _, n := range a.nodes {
			if n.Emoji != "" {
				emoji.Load(context.Background(), n.Emoji)
			}
		}
		p := &panel{id: "ppsi", title: "Rencana Liburan"}
		text, _, _ := doc.Build("ppsi", blocks, doc.Shape{Width: &p.textWidth, Emoji: func(e string) ui.ImageSource {
			if s := emoji.Load(context.Background(), e); s != nil {
				return s
			}
			return nil
		}})
		p.doc = richtext.NewDocument(text)
		a.panels[0] = p
		a.history, a.hpos = []string{"ppsi"}, 0
		tester := ui.NewTester(a.View, 1280, 800)
		tester.Frame()
		tester.Frame()
		f, err := os.Create(dir + "/" + v.name + ".png")
		if err != nil {
			t.Fatal(err)
		}
		png.Encode(f, tester.Image())
		f.Close()
	}
}

// heroPage is a made-up page for the landing page's screenshots: nobody's
// real notes.
var heroPage = `[
 {"object":"block","id":"h1","type":"heading_2","heading_2":{"rich_text":[` + txt("Minggu ini", "") + `]}},
 {"object":"block","id":"p1","type":"paragraph","paragraph":{"rich_text":[` + txt("Tiga hari di pantai, satu hari di kota. Semua catatan tetap di Notion, ", "") + `,` + txt("diedit dari aplikasi desktop yang ringan", `,"bold":true`) + `,` + txt(".", "") + `]}},
 {"object":"block","id":"t1","type":"to_do","to_do":{"checked":true,"rich_text":[` + txt("Pesan penginapan", "") + `]}},
 {"object":"block","id":"t2","type":"to_do","to_do":{"checked":true,"rich_text":[` + txt("Tukar uang", "") + `]}},
 {"object":"block","id":"t3","type":"to_do","to_do":{"checked":false,"rich_text":[` + txt("Sewa motor di hari kedua", "") + `]}},
 {"object":"block","id":"co","type":"callout","callout":{"icon":{"type":"emoji","emoji":"☀️"},"rich_text":[` + txt("Bawa tabir surya dan topi, siang hari terik sekali.", "") + `]}},
 {"object":"block","id":"h2","type":"heading_2","heading_2":{"rich_text":[` + txt("Rute", "") + `]}},
 {"object":"block","id":"n1","type":"numbered_list_item","numbered_list_item":{"rich_text":[` + txt("Pagi: pantai timur, lihat matahari terbit", "") + `]}},
 {"object":"block","id":"n2","type":"numbered_list_item","numbered_list_item":{"rich_text":[` + txt("Siang: pasar ikan dan makan siang", "") + `]}},
 {"object":"block","id":"n3","type":"numbered_list_item","numbered_list_item":{"rich_text":[` + txt("Sore: kembali ke penginapan", "") + `]}},
 {"object":"block","id":"dv","type":"divider","divider":{}},
 {"object":"block","id":"s1","type":"child_page","child_page":{"title":"Itinerary"}},
 {"object":"block","id":"s2","type":"child_page","child_page":{"title":"Daftar Bawaan"}},
 {"object":"block","id":"db1","type":"child_database","child_database":{"title":"Bacaan"}}
]`
