package app

import (
	"encoding/json"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/richtext"

	"gotion/internal/doc"
	"gotion/internal/notion"
)

func buildTextFromJSON(t *testing.T, raw string) richtext.Text {
	t.Helper()
	var blocks []*notion.Block
	if err := json.Unmarshal([]byte(raw), &blocks); err != nil {
		t.Fatal(err)
	}
	txt, _, _ := doc.Build("p1", blocks, doc.Shape{})
	return txt
}

func TestRightPaneFollowsFocusAndAutoHides(t *testing.T) {
	a := New()
	a.client = notion.New("x")
	a.bot = &notion.User{ID: "u1", Name: "Ryanda"}
	a.set.RightPane = true
	a.set.SidebarWidth = 270
	a.nodes["p1"] = &node{ID: "p1", Title: "Page One", Kind: "page", Edited: time.Now()}
	a.nodes["p2"] = &node{ID: "p2", Title: "Today", Kind: "today"}
	txt := buildTextFromJSON(t, `[{"object":"block","id":"h1","type":"heading_1","heading_1":{"rich_text":[{"type":"text","plain_text":"Section A"}]}}]`)
	a.panels[0] = &panel{
		id:    "p1",
		title: "Page One",
		doc:   richtext.NewDocument(txt),
	}
	a.panels[1] = &panel{id: todayID}

	// Wide enough for split + right pane (270 + 2*560 + 300 = 1690).
	ttWide := ui.NewTester(a.View, 1720, 800)
	ttWide.Frame()
	ttWide.Frame()
	if !a.rightPaneOpen {
		t.Fatal("expected right pane open on wide window")
	}
	if !strings.Contains(strings.Join(ttWide.Texts(), "|"), "Section A") {
		t.Fatalf("expected panel 0 outline in right pane, got %v", ttWide.Texts())
	}

	// Switch focus to panel 1 (Today) -> right pane content switches to Today context.
	a.focus = 1
	ttWide.Frame()
	if !strings.Contains(strings.Join(ttWide.Texts(), "|"), "Besok · 0") {
		t.Fatalf("expected Today context in right pane when panel 1 focused, got %v", ttWide.Texts())
	}

	// Narrow window (< 2*560 + 300) -> auto-hides right pane, preserves a.set.RightPane!
	ttNarrow := ui.NewTester(a.View, 1200, 800)
	ttNarrow.Frame()
	ttNarrow.Frame()
	if a.rightPaneOpen {
		t.Fatal("expected right pane auto-hidden when split width < 2*560+300")
	}
	if !a.set.RightPane {
		t.Fatal("expected user preference a.set.RightPane to remain true")
	}
}

func TestRightPaneOutlineAndScroll(t *testing.T) {
	txt := buildTextFromJSON(t, `[
		{"object":"block","id":"h1","type":"heading_1","heading_1":{"rich_text":[{"type":"text","plain_text":"Intro"}]}},
		{"object":"block","id":"b1","type":"paragraph","paragraph":{"rich_text":[{"type":"text","plain_text":"Body text"}]}},
		{"object":"block","id":"h2","type":"heading_2","heading_2":{"rich_text":[{"type":"text","plain_text":"Details"}]}},
		{"object":"block","id":"h3","type":"heading_3","heading_3":{"rich_text":[{"type":"text","plain_text":"Sub"}]}}
	]`)
	items := extractOutline(txt)
	if len(items) != 3 {
		t.Fatalf("expected 3 outline items, got %d", len(items))
	}
	if items[0].Level != 1 || items[0].Index != 0 || items[1].Level != 2 || items[1].Index != 2 || items[2].Level != 3 || items[2].Index != 3 {
		t.Fatalf("unexpected outline items: %+v", items)
	}

	a := New()
	a.client = notion.New("x")
	a.bot = &notion.User{ID: "u1"}
	a.set.RightPane = true
	a.nodes["p1"] = &node{ID: "p1", Title: "Doc", Kind: "page"}
	a.panels[0] = &panel{id: "p1", title: "Doc", doc: richtext.NewDocument(txt)}

	tt := ui.NewTester(a.View, 1400, 800)
	tt.Frame()
	tt.Frame()
	tt.Click("Details")
	if a.panels[0].flashPara != 3 || a.panels[0].flashAt.IsZero() {
		t.Fatalf("expected flashPara=3 and non-zero flashAt after clicking 'Details', got flash=%d at=%v", a.panels[0].flashPara, a.panels[0].flashAt)
	}
}

func TestRightPaneDatabaseClickSelectVsOpen(t *testing.T) {
	a := New()
	a.client = notion.New("x")
	a.bot = &notion.User{ID: "u1"}
	a.nodes["db1"] = &node{ID: "db1", Title: "Tasks", Kind: "data_source"}
	colTitle := notion.Property{Name: "Name", Type: "title"}
	row := &notion.Page{
		ID: "r1",
		Properties: map[string]json.RawMessage{
			"Name": json.RawMessage(`{"type":"title","title":[{"plain_text":"Row One"}]}`),
		},
	}
	tv := &tableView{id: "db1", dsID: "db1", title: "Tasks", columns: []notion.Property{colTitle}, rows: []*notion.Page{row}}
	a.panels[0] = &panel{id: "db1", table: tv}

	// When right pane is open: clicking selects row without opening it.
	a.set.RightPane = true
	tt := ui.NewTester(a.View, 1400, 800)
	tt.Frame()
	tt.Frame()
	tt.Click("Row One")
	if tv.selectedID != "r1" {
		t.Fatalf("expected selectedID 'r1', got %q", tv.selectedID)
	}
	if a.panels[0].id != "db1" {
		t.Fatalf("expected panel to remain on db1 when selecting, got %q", a.panels[0].id)
	}

	// Pressing Enter opens the selected row.
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if a.panels[0].id != "r1" {
		t.Fatalf("expected Enter to open selected row 'r1', got %q", a.panels[0].id)
	}

	// When right pane is closed: clicking opens the row directly.
	a.panels[0] = &panel{id: "db1", table: tv}
	a.set.RightPane = false
	ttClosed := ui.NewTester(a.View, 1400, 800)
	ttClosed.Frame()
	ttClosed.Frame()
	ttClosed.Click("Row One")
	if a.panels[0].id != "r1" {
		t.Fatalf("expected single click to open row when right pane closed, got %q", a.panels[0].id)
	}
}

func TestRightPaneAddCommentCallsAPI(t *testing.T) {
	var mu sync.Mutex
	var posted string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/comments"):
			w.Write([]byte(`{"results":[]}`))
		case r.Method == "POST" && r.URL.Path == "/comments":
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			b, _ := json.Marshal(body)
			mu.Lock()
			posted = string(b)
			mu.Unlock()
			w.Write([]byte(`{"id":"cm1","rich_text":[{"plain_text":"Halo dari pane"}]}`))
		default:
			w.Write([]byte(`{"results":[]}`))
		}
	}))
	defer srv.Close()

	a := New()
	a.client = notion.New("tok")
	a.client.Base = srv.URL
	a.bot = &notion.User{ID: "u1", Name: "Ryanda"}
	a.set.RightPane = true
	a.nodes["p1"] = &node{ID: "p1", Title: "Doc", Kind: "page"}
	a.panels[0] = &panel{
		id:    "p1",
		title: "Doc",
		doc:   richtext.NewDocument(buildTextFromJSON(t, `[{"object":"block","id":"b1","type":"paragraph","paragraph":{"rich_text":[{"type":"text","plain_text":"Hi"}]}}]`)),
		comments: commentsState{
			loaded: true,
			draft:  "Halo dari pane",
		},
	}

	tt := ui.NewTester(a.View, 1400, 800)
	tt.Frame()
	tt.Frame()
	tt.Click("Kirim")

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		p := posted
		mu.Unlock()
		if p != "" {
			if !strings.Contains(p, `"Halo dari pane"`) || !strings.Contains(p, `"p1"`) {
				t.Fatalf("unexpected comment payload: %s", p)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out waiting for POST /comments")
}

// TestRightPaneShots renders light/dark shots on hutan/polos with and without split when SHOT_RP=<dir>.
func TestRightPaneShots(t *testing.T) {
	dir := os.Getenv("SHOT_RP")
	if dir == "" {
		return
	}
	var blocks []*notion.Block
	if err := json.Unmarshal([]byte(samplePage), &blocks); err != nil {
		t.Fatal(err)
	}
	text, _, extras := doc.Build("page", blocks, doc.Shape{OpenToggles: map[string]bool{}})
	now := time.Now()
	cases := []struct {
		name  string
		theme string
		wash  string
		split bool
	}{
		{"hutan-light", "light", "hutan", false},
		{"hutan-dark", "dark", "hutan", false},
		{"polos-light-split", "light", "polos", true},
		{"polos-dark-split", "dark", "polos", true},
	}
	RegisterFonts()
	for _, tc := range cases {
		a := New()
		a.client = notion.New("x")
		a.bot = &notion.User{ID: "u1", Name: "ryanda"}
		a.set.Theme = tc.theme
		a.set.Wash = tc.wash
		a.set.RightPane = true
		a.nodes["page"] = &node{ID: "page", Title: "ryan's vault", Kind: "page", Edited: now.Add(-5 * time.Minute)}
		a.nodes["road"] = &node{ID: "road", Title: "Roadmap Q1", Kind: "page", Edited: now.Add(-2 * time.Minute)}
		a.regroup()
		a.panels[0] = &panel{
			id:     "page",
			title:  "ryan's vault",
			doc:    richtext.NewDocument(text),
			extras: extras,
			comments: commentsState{
				loaded: true,
				list: []notion.Comment{
					{ID: "c1", CreatedTime: now.Add(-10 * time.Minute), CreatedBy: notion.User{ID: "u1", Name: "ryanda"}, RichText: []notion.RichText{{PlainText: "Cek bagian Fokus hari ini ya."}}},
				},
			},
		}
		w := 1440
		if tc.split {
			w = 1760
			a.panels[1] = &panel{id: "road", title: "Roadmap Q1", doc: richtext.NewDocument(text)}
		}
		tt := ui.NewTester(a.View, w, 820)
		tt.Frame()
		tt.Frame()
		f, err := os.Create(dir + "/" + tc.name + ".png")
		if err != nil {
			t.Fatal(err)
		}
		png.Encode(f, tt.Image())
		f.Close()
	}
}
