package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
)

// TestStartsWithDamagedFiles: settings and the page cache cut short by a
// crash, emptied, or holding what an older or newer gotion wrote. The app
// starts, signed in, on the defaults.
func TestStartsWithDamagedFiles(t *testing.T) {
	saveToken("ntn_test")
	defer deleteToken()
	dir := configDir()
	os.MkdirAll(dir, 0o700)
	defer os.Remove(filepath.Join(dir, "settings.json"))
	defer os.Remove(filepath.Join(dir, "pages.json"))
	for _, c := range []struct{ name, settings, pages string }{
		{"empty", "", ""},
		{"garbage", "\x00\x01not json", "<html>"},
		{"cut short", `{"theme":"dark","tabs":[["abc"`, `[{"object":"page","id":"a"`},
		{"null", "null", "null"},
		{"null items", `{"tabs":[null,null],"open":null,"views":null,"zoom":-3,"sidebarWidth":-1}`, `[null,{"object":"page","id":"a","parent":{"type":"workspace"}}]`},
		{"wrong types", `{"theme":42,"tabs":"x","zoom":"big"}`, `{"results":[]}`},
	} {
		t.Run(c.name, func(t *testing.T) {
			os.WriteFile(filepath.Join(dir, "settings.json"), []byte(c.settings), 0o600)
			os.WriteFile(filepath.Join(dir, "pages.json"), []byte(c.pages), 0o600)
			a := New()
			if a.client == nil {
				t.Fatal("not signed in")
			}
			a.Attach(nil)
			if a.set.Zoom != 1 || a.set.SidebarWidth < 200 {
				t.Errorf("zoom %v sidebar %v", a.set.Zoom, a.set.SidebarWidth)
			}
			a.saveSettings()
			if loadSettings().Theme == "" {
				t.Error("no theme after saving")
			}
		})
	}
}

// TestSettingsAreWrittenWhole: a save goes through a file beside it, so a
// crash mid-write leaves the old settings, not half of the new.
func TestSettingsAreWrittenWhole(t *testing.T) {
	dir := configDir()
	defer os.Remove(filepath.Join(dir, "settings.json"))
	a := &App{set: loadSettings()}
	a.set.Theme = "dark"
	a.saveSettings()
	if _, err := os.Stat(filepath.Join(dir, "settings.json.tmp")); !os.IsNotExist(err) {
		t.Errorf("a temporary file is left: %v", err)
	}
	if loadSettings().Theme != "dark" {
		t.Error("not saved")
	}
}

// TestWallpaperMode: the "wallpaper" mode is dark over a dark wallpaper and
// light over a light one, and follows the system until the wallpaper is
// known.
func TestWallpaperMode(t *testing.T) {
	a := &App{set: loadSettings(), nodes: map[string]*node{}}
	a.set.Theme = "wallpaper"
	tester := ui.NewTester(a.View, 900, 600)
	for _, c := range []struct {
		wall int
		dark bool
	}{{2, true}, {1, false}} {
		a.wallDark = c.wall
		tester.Frame()
		if a.dark != c.dark {
			t.Errorf("wallpaper %d: dark %v", c.wall, a.dark)
		}
	}
	a.wallDark = 0
	tester.Frame()
	sys := a.dark
	a.set.Theme = "system"
	tester.Frame()
	if a.dark != sys {
		t.Error("unknown wallpaper: not the system's mode")
	}
}
