package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func TestLibraryAndTrash(t *testing.T) {
	a := tabsApp(t)
	a.nodes["db"] = &node{ID: "db", Title: "Tugas", Kind: "data_source"}
	a.regroup()
	a.open(a.special(libraryID), 0)
	if a.panels[0].id != libraryID || a.panels[0].opening {
		t.Fatalf("library tab: %+v", a.panels[0])
	}
	for _, g := range a.groups {
		if isSpecial(g.Kind) {
			t.Error("the library shows in the page tree")
		}
	}
	tester := ui.NewTester(a.View, 1200, 600)
	tester.Advance(time.Second)
	snapshot(t, tester, "library")
	if _, ok := tester.Find("Halaman dua"); !ok {
		t.Error("the library lists no page")
	}
	// Database only: the segmented choice, clicked where it is (the
	// sidebar has a "Database" heading too).
	before := strings.Count(strings.Join(tester.Texts(), "|"), "Halaman dua")
	tester.ClickAt(822, 174)
	tester.Frame()
	if a.library.kind != "db" {
		t.Fatalf("kind %q", a.library.kind)
	}
	if after := strings.Count(strings.Join(tester.Texts(), "|"), "Halaman dua"); after != before-1 {
		t.Errorf("the database filter kept pages: %d -> %d", before, after)
	}
	a.set.Trashed = []trashed{{ID: "x", Title: "Lama", When: time.Now()}}
	a.open(a.special(trashID), 0)
	tester.Frame()
	tester.Advance(time.Second)
	snapshot(t, tester, "trash")
	if _, ok := tester.Find("Pulihkan"); !ok {
		t.Error("no way to restore")
	}
}

func TestPageMenuShot(t *testing.T) {
	a := tabsApp(t)
	a.open(a.nodes["satu"], 0)
	tester := ui.NewTester(a.View, 1200, 600)
	for i := 0; i < 10; i++ {
		tester.Advance(100 * time.Millisecond)
	}
	a.panels[0].menu.open = true
	tester.Frame()
	tester.Advance(time.Second)
	snapshot(t, tester, "page-menu")
	if _, ok := tester.Find("Pindahkan ke…"); !ok {
		t.Error("no page menu")
	}
}

func TestImportMarkdownNeedsAPage(t *testing.T) {
	a := tabsApp(t)
	md := filepath.Join(t.TempDir(), "catatan.md")
	os.WriteFile(md, []byte("# Catatan\n\nisi"), 0o600)
	a.importMarkdown("", []string{md})
	if len(a.toasts) == 0 || !strings.Contains(a.toasts[0], "Buka halaman tujuan") {
		t.Errorf("toasts %v", a.toasts)
	}
}

func waitFor2(cond func() bool) bool {
	for i := 0; i < 60; i++ {
		if cond() {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false
}

func TestTrashRestoreMoveIcon(t *testing.T) {
	a := tabsApp(t)
	a.open(a.nodes["dua"], 0)
	a.trashPage("dua")
	if !waitFor2(func() bool { return a.nodes["dua"] == nil }) {
		t.Fatal("not trashed")
	}
	if len(a.set.Trashed) != 1 || len(a.tabs[0]) != 0 {
		t.Errorf("trashed %v tabs %d", a.set.Trashed, len(a.tabs[0]))
	}
	a.restorePage(a.set.Trashed[0])
	if !waitFor2(func() bool { return a.nodes["dua"] != nil }) || len(a.set.Trashed) != 0 {
		t.Fatal("not restored")
	}
	a.movePage("dua", "satu")
	if !waitFor2(func() bool { return a.nodes["dua"].ParentID == "satu" }) {
		t.Error("not moved")
	}
	a.setIcon("tiga", "🔥")
	if a.nodes["tiga"].Emoji != "🔥" {
		t.Error("icon not shown at once")
	}
}
