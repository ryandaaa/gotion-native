package app

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"gotion/internal/notion"
)

// tabsApp is an app over a Notion that has every page, empty.
func tabsApp(t *testing.T) *App {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/children") {
			io.WriteString(w, `{"results":[],"has_more":false}`)
			return
		}
		id := strings.TrimPrefix(r.URL.Path, "/v1/pages/")
		icon := ""
		if id == "dua" {
			icon = `,"icon":{"type":"emoji","emoji":"📝"}`
		}
		io.WriteString(w, `{"object":"page","id":"`+id+`","last_edited_by":{"id":"u"},"properties":{"title":{"type":"title","title":[{"plain_text":"Halaman `+id+`"}]}}`+icon+`}`)
	}))
	t.Cleanup(srv.Close)
	a := tableApp(t)
	a.client = notion.New("x")
	a.client.Base = srv.URL + "/v1"
	a.panels[0] = &panel{}
	a.nodes = map[string]*node{}
	for _, id := range []string{"satu", "dua", "tiga"} {
		a.nodes[id] = &node{ID: id, Title: "Halaman " + id, Kind: "page"}
	}
	a.regroup()
	return a
}

func ids(a *App, i int) string {
	var out []string
	for _, t := range a.tabs[i] {
		mark := ""
		if a.panels[i] == t {
			mark = "*"
		}
		out = append(out, t.id+mark)
	}
	return strings.Join(out, " ")
}

func TestTabs(t *testing.T) {
	a := tabsApp(t)
	a.open(a.nodes["satu"], 0) // into the empty panel
	a.open(a.nodes["dua"], 0)  // a new tab after it
	a.open(a.nodes["tiga"], 0)
	if got := ids(a, 0); got != "satu dua tiga*" {
		t.Fatalf("tabs %q", got)
	}
	a.open(a.nodes["satu"], 0) // already open: its tab shows
	if got := ids(a, 0); got != "satu* dua tiga" {
		t.Errorf("reopen %q", got)
	}
	a.navigate(a.nodes["tiga"], 0) // a link to an open page goes to its tab
	if got := ids(a, 0); got != "satu dua tiga*" {
		t.Errorf("navigate to open %q", got)
	}
	a.cycleTab(1)
	if got := ids(a, 0); got != "satu* dua tiga" {
		t.Errorf("cycle %q", got)
	}
	a.closeTab(0, 0)
	if got := ids(a, 0); got != "dua* tiga" {
		t.Errorf("close %q", got)
	}
	// Kept for the next run, then put back with the same tab showing.
	if strings.Join(a.set.Tabs[0], ",") != "dua,tiga" || a.set.Active[0] != 0 {
		t.Errorf("saved %v %v", a.set.Tabs, a.set.Active)
	}
	b := tabsApp(t)
	b.set.Tabs, b.set.Active = a.set.Tabs, [2]int{1, 0}
	b.panels[0] = nil
	b.restoreTabs()
	if got := ids(b, 0); got != "dua tiga*" {
		t.Errorf("restored %q", got)
	}
	if b.tabs[0][0].opening || b.tabs[0][0].doc != nil {
		t.Error("a tab not showing loaded at start")
	}
	// The last tab closed leaves an empty panel.
	a.closeTab(0, 0)
	a.closeTab(0, 0)
	if a.panels[0] == nil || a.panels[0].id != "" || len(a.tabs[0]) != 0 {
		t.Errorf("after the last: %+v %d", a.panels[0], len(a.tabs[0]))
	}
}

func TestTabsShot(t *testing.T) {
	a := tabsApp(t)
	a.nodes["dua"].Emoji = "📝"
	for _, id := range []string{"satu", "dua", "tiga"} {
		a.open(a.nodes[id], 0)
	}
	tester := ui.NewTester(a.View, 1280, 500)
	for i := 0; i < 20; i++ {
		tester.Advance(100 * time.Millisecond)
	}
	snapshot(t, tester, "tabs")
	// The first tab, at the strip's left (the sidebar's name comes first in
	// the window, so the tab is clicked where it is).
	tester.ClickAt(360, 66)
	tester.Frame()
	if a.panels[0].id != "satu" {
		t.Errorf("a click on a tab shows %q", a.panels[0].id)
	}
}

// TestSwitcherOrderIsStable: pages edited in the same minute list in the
// same order every time, not as the map happens to give them.
func TestSwitcherOrderIsStable(t *testing.T) {
	a := tabsApp(t)
	when := time.Date(2026, 9, 14, 10, 0, 0, 0, time.UTC)
	for i := 0; i < 40; i++ {
		id := "lit" + string(rune('a'+i%26)) + string(rune('a'+i/26))
		a.nodes[id] = &node{ID: id, Title: "Literature " + id, Kind: "page", Edited: when}
	}
	first := a.switcherMatches()
	for k := 0; k < 50; k++ {
		got := a.switcherMatches()
		for j := range got {
			if got[j].ID != first[j].ID {
				t.Fatalf("run %d: item %d is %s, was %s", k, j, got[j].ID, first[j].ID)
			}
		}
	}
}
