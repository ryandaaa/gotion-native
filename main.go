// gotion is a native desktop client for Notion, built with MyGo.
package main

import (
	"log"
	"os"
	"path/filepath"
	"runtime"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"gotion/internal/app"
	"gotion/internal/syncer"
)

func main() {
	app.RegisterFonts()
	if dir, err := os.UserConfigDir(); err == nil {
		syncer.CacheDir = filepath.Join(dir, "gotion", "cache")
	}
	a := app.New()
	mygo.App.WhenReady(func() {
		if runtime.GOOS == "darwin" {
			// The Mac's menu bar; elsewhere the window draws its own bar.
			mygo.App.SetMenu(mygo.NewMenu([]*mygo.MenuItem{
				{Role: mygo.RoleAppMenu},
				{Role: mygo.RoleFileMenu},
				{Role: mygo.RoleEditMenu},
				{Role: mygo.RoleFormatMenu},
				{Role: mygo.RoleWindowMenu},
			}))
		}
		w := mygo.NewWindow(mygo.WindowOptions{
			Title:         "gotion",
			Width:         1280,
			Height:        820,
			MinWidth:      760,
			MinHeight:     480,
			StateKey:      "main",
			TitleBarStyle: mygo.TitleBarHidden,
			Content:       ui.View(a.View),
		})
		a.Attach(w)
	})
	mygo.App.OnBeforeQuit(func(*mygo.QuitEvent) { a.Close() })
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
