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
 {"object":"block","id":"dv","type":"divider","divider":{}},
 {"object":"block","id":"sp","type":"child_page","child_page":{"title":"01. INISIASI PROYEK"}},
 {"object":"block","id":"db","type":"child_database","child_database":{"title":"Deliverables"}},
 {"object":"block","id":"x","type":"column_list","column_list":{}}
]`

// TestShot draws the window with sample data; SHOT=<dir> saves PNGs.
func TestShot(t *testing.T) {
	var blocks []*notion.Block
	if err := json.Unmarshal([]byte(samplePage), &blocks); err != nil {
		t.Fatal(err)
	}
	text, _, extras := doc.Build("page", blocks, doc.Shape{OpenToggles: map[string]bool{}})
	now := time.Now()
	shots := []struct {
		name  string
		theme string
		setup func(a *App)
	}{
		{"light", "light", nil},
		{"dark", "dark", nil},
		{"switcher", "light", func(a *App) { a.openSwitcher(); a.switcher.query = "a" }},
		{"split", "light", func(a *App) {
			a.panels[1] = &panel{id: "road", doc: richtext.NewDocument(text), title: "Roadmap Q1"}
		}},
	}
	for _, shot := range shots {
		a := &App{client: notion.New("x"), bot: &notion.User{}, panels: [2]*panel{{}, nil}}
		a.set = settings{Theme: shot.theme, SidebarWidth: 270, Open: map[string]bool{"proj": true, "me": true}, Closed: map[string]bool{}, Toggles: map[string]bool{}}
		a.nodes = map[string]*node{}
		add := func(id, title, emoji, parent, kind string, ago time.Duration) {
			a.nodes[id] = &node{ID: id, Title: title, Emoji: emoji, ParentID: parent, Kind: kind, Edited: now.Add(-ago)}
		}
		add("proj", "Proyek", "🚀", "", "page", time.Hour)
		add("page", "Catatan Rapat", "📝", "proj", "page", 0)
		add("road", "Roadmap Q1", "", "proj", "page", 2*time.Minute)
		add("sub", "Detail teknis", "", "road", "page", 2*time.Minute)
		add("me", "Pribadi", "", "", "page", 3*time.Hour)
		add("jur", "Jurnal Harian", "", "me", "page", 5*time.Minute)
		add("ide", "Ide App", "💡", "me", "page", 26*time.Hour)
		add("tasks", "Tasks", "", "", "data_source", time.Hour)
		a.regroup()
		a.panels[0] = &panel{id: "page", doc: richtext.NewDocument(text), extras: extras, title: "Catatan Rapat"}
		a.history, a.hpos = []string{"road", "page"}, 1
		if shot.setup != nil {
			shot.setup(a)
		}
		RegisterFonts()
		tester := ui.NewTester(a.View, 1280, 800)
		tester.Frame()
		tester.Frame()
		if dir := os.Getenv("SHOT"); dir != "" {
			f, err := os.Create(dir + "/" + shot.name + ".png")
			if err != nil {
				t.Fatal(err)
			}
			png.Encode(f, tester.Image())
			f.Close()
		}
	}
}
