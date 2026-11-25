package app

import (
	"image/png"
	"os"
	"testing"
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
