package app

import (
	"encoding/json"
	"image/png"
	"os"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/richtext"

	"gotion/internal/doc"
	"gotion/internal/notion"
)

const samplePage = `[
 {"object":"block","id":"h","type":"heading_2","heading_2":{"rich_text":[{"type":"text","plain_text":"Agenda","annotations":{"color":"default"}}]}},
 {"object":"block","id":"p","type":"paragraph","paragraph":{"rich_text":[{"type":"text","plain_text":"Bahas progres sprint minggu ini dan rencana rilis ","annotations":{"color":"default"}},{"type":"text","plain_text":"v0.2","annotations":{"bold":true,"color":"default"}},{"type":"text","plain_text":".","annotations":{"color":"default"}}]}},
 {"object":"block","id":"t1","type":"to_do","to_do":{"checked":true,"rich_text":[{"type":"text","plain_text":"Review PR halaman login","annotations":{"color":"default"}}]}},
 {"object":"block","id":"t2","type":"to_do","to_do":{"checked":false,"rich_text":[{"type":"text","plain_text":"Deploy ke staging","annotations":{"color":"default"}}]}},
 {"object":"block","id":"b1","type":"bulleted_list_item","bulleted_list_item":{"rich_text":[{"type":"text","plain_text":"Sync engine sudah bisa push per block","annotations":{"color":"default"}}]}},
 {"object":"block","id":"q","type":"callout","callout":{"rich_text":[{"type":"text","plain_text":"Jangan lupa backup database sebelum migrasi.","annotations":{"color":"default"}}]}},
 {"object":"block","id":"c","type":"code","code":{"language":"bash","rich_text":[{"type":"text","plain_text":"go tool mygo build -platform linux/amd64","annotations":{"color":"default"}}]}},
 {"object":"block","id":"x","type":"column_list","column_list":{}}
]`

// TestShot draws the window with sample data; SHOT=<dir> saves PNGs.
func TestShot(t *testing.T) {
	var blocks []*notion.Block
	if err := json.Unmarshal([]byte(samplePage), &blocks); err != nil {
		t.Fatal(err)
	}
	text, _, extras := doc.Build("page", blocks)
	now := time.Now()
	for _, dark := range []bool{false, true} {
		a := &App{closed: map[string]bool{}, client: notion.New("x"), bot: &notion.User{}, dark: dark}
		a.nodes = map[string]*node{}
		add := func(id, title, parent, kind string, ago time.Duration) {
			a.nodes[id] = &node{ID: id, Title: title, ParentID: parent, Kind: kind, Edited: now.Add(-ago)}
		}
		add("proj", "Proyek", "", "page", time.Hour)
		add("page", "Catatan Rapat", "proj", "page", 0)
		add("road", "Roadmap Q1", "proj", "page", 2*time.Minute)
		add("me", "Pribadi", "", "page", 3*time.Hour)
		add("jur", "Jurnal Harian", "me", "page", 5*time.Minute)
		add("ide", "Ide App", "me", "page", 26*time.Hour)
		add("tasks", "Tasks", "", "data_source", time.Hour)
		var pages []*notion.Page
		_ = pages
		// Build the groups the way setPages does.
		for _, n := range a.nodes {
			if n.Kind == "data_source" {
				a.databases = append(a.databases, n)
			} else if p := a.nodes[n.ParentID]; p != nil {
				p.Children = append(p.Children, n)
			} else {
				a.groups = append(a.groups, n)
			}
		}
		a.groups = []*node{a.nodes["proj"], a.nodes["me"]}
		a.nodes["proj"].Children = []*node{a.nodes["page"], a.nodes["road"]}
		a.nodes["me"].Children = []*node{a.nodes["ide"], a.nodes["jur"]}
		a.selected = "page"
		a.doc = richtext.NewDocument(text)
		a.extras = extras
		RegisterFonts()
		tester := ui.NewTester(a.View, 1280, 800)
		tester.Frame()
		tester.Frame()
		if dir := os.Getenv("SHOT"); dir != "" {
			name := dir + "/light.png"
			if dark {
				name = dir + "/dark.png"
			}
			f, err := os.Create(name)
			if err != nil {
				t.Fatal(err)
			}
			png.Encode(f, tester.Image())
			f.Close()
		}
	}
}
