package app

import (
	"image/png"
	"os"
	"testing"

	"gotion/internal/notion"
	"time"

	"github.com/egoist/mygo/ui"
)

func TestOnbShot(t *testing.T) {
	dir := os.Getenv("ONBSHOT")
	if dir == "" {
		t.Skip()
	}
	for _, s := range []struct {
		name, theme, wash string
		step2             bool
		w                 int
	}{
		{"light", "light", "hutan", false, 1280},
		{"dark", "dark", "hutan", false, 1280},
		{"step2", "light", "senja", true, 1280},
		{"narrow", "light", "laut", false, 760},
	} {
		a := &App{}
		a.set = settings{Theme: s.theme, Wash: s.wash, SidebarWidth: 270, Toggles: map[string]bool{}}
		if s.step2 {
			a.oauth = &oauthState{}
		}
		tt := ui.NewTester(a.View, s.w, 800)
		for i := 0; i < 90; i++ {
			tt.Advance(16 * time.Millisecond)
		}
		f, _ := os.Create(dir + "/" + s.name + ".png")
		png.Encode(f, tt.Image())
		f.Close()
	}
}

// TestSplitTableShot draws two database panels side by side; ONBSHOT=<dir>
// saves it, to see the toolbar fold on a narrow panel.
func TestSplitTableShot(t *testing.T) {
	dir := os.Getenv("ONBSHOT")
	if dir == "" {
		t.Skip()
	}
	a := tableApp(t)
	a.set.Views = map[string]string{}
	db := func(id string) *panel {
		a.nodes[id] = &node{ID: id, Title: "Tugas Harian", Kind: "database"}
		return &panel{id: id, title: "Tugas Harian", table: &tableView{id: id, dsID: "ds" + id, title: "Tugas Harian",
			columns: []notion.Property{{Name: "Nama", Type: "title"}, {Name: "Status", Type: "status"}}}}
	}
	a.panels[0] = db("db1")
	a.panels[1] = db("db2")
	a.widths = [2]float32{470, 470}
	a.panels[0].width, a.panels[1].width = 470, 470
	tt := ui.NewTester(a.View, 1280, 500)
	for i := 0; i < 40; i++ {
		tt.Advance(16 * time.Millisecond)
	}
	f, _ := os.Create(dir + "/split-table.png")
	png.Encode(f, tt.Image())
	f.Close()
}
