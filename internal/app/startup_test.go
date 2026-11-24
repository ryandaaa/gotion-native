package app

import (
	"os"
	"path/filepath"
	"testing"
)

// TestStartsWithoutPageCache: signed in, with a Pustaka tab kept from the
// last run but no cache of pages on disk, the app starts instead of
// panicking on a nil map, and the tab comes back.
func TestStartsWithoutPageCache(t *testing.T) {
	saveToken("ntn_test")
	defer deleteToken()
	os.Remove(filepath.Join(configDir(), "pages.json"))
	set := loadSettings()
	set.Tabs = [2][]string{{libraryID, trashID}, nil}
	set.Last = libraryID
	b := &App{set: set}
	b.saveSettings()

	a := New()
	if a.client == nil {
		t.Fatal("not signed in")
	}
	a.Attach(nil)
	if len(a.tabs[0]) != 2 {
		t.Errorf("tabs back: %d", len(a.tabs[0]))
	}
	a.signOut()
	if a.special(libraryID) == nil {
		t.Error("no library after signing out")
	}
}
