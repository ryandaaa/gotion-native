package app

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func TestSettingsDialog(t *testing.T) {
	for _, sec := range []string{"tampilan", "akun", "tentang"} {
		a := tableApp(t)
		a.openSettings(sec)
		tester := ui.NewTester(a.View, 1100, 640)
		tester.Advance(time.Second)
		snapshot(t, tester, "settings-"+sec)
	}
	// Ctrl+, opens it; Esc closes it.
	a := tableApp(t)
	tester := ui.NewTester(a.View, 1100, 640)
	tester.Frame()
	tester.Key(ui.Cmd, ui.KeyComma)
	tester.Frame()
	if !a.settings.open {
		t.Fatal("Ctrl+, did not open the settings")
	}
	if err := tester.Click("Akun"); err != nil {
		t.Fatal(err)
	}
	// Signing out asks first.
	if err := tester.Click("Keluar"); err != nil {
		t.Fatal(err)
	}
	tester.Frame()
	if a.client == nil || !a.settings.confirm {
		t.Fatal("signed out without asking")
	}
	tester.Key(0, ui.KeyEscape)
	tester.Frame()
	if a.settings.open {
		t.Error("Esc did not close the settings")
	}
}
