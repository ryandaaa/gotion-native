// gotion is a native desktop client for Notion, built with MyGo.
package main

import (
	_ "embed"
	"github.com/egoist/mygo/plugins/updater"
	"github.com/egoist/mygo/plugins/updater/native"
	"log"
	"os"
	"path/filepath"
	"runtime"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"gotion/internal/app"
	"gotion/internal/emoji"
	"gotion/internal/syncer"
)

//go:embed resources/icon.png
var trayIcon []byte

func main() {
	app.TrayIcon = trayIcon
	app.RegisterFonts()
	if dir, err := os.UserConfigDir(); err == nil {
		syncer.CacheDir = filepath.Join(dir, "gotion", "cache")
		emoji.Dir = filepath.Join(dir, "gotion", "emoji")
	}
	app.PreferredLanguages = func() (langs []string) {
		defer func() { recover() }() // before the app is ready: none yet
		return mygo.App.PreferredLanguages()
	}
	// Updates: a window offers each new release, signed with the key whose
	// public half is in mygo.json; it stays off in development builds.
	mygo.Use(native.Plugin)
	a := app.New()
	mygo.App.WhenReady(func() {
		a.ApplyLanguage() // the system's languages are known now
		if v := mygo.App.Version(); v != "" {
			app.Version = v
		}
		app.CheckForUpdates = func() bool {
			if !mygo.Updater.Enabled() {
				return false
			}
			updater.CheckForUpdates()
			return true
		}
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
		// On Windows, only a window made with a material can show one:
		// it is made with the glass (Acrylic), which the settings then
		// keep or take away (applyGlass).
		var vibrancy mygo.Vibrancy
		if runtime.GOOS == "windows" {
			vibrancy = mygo.VibrancyGlass
		}
		w := mygo.NewWindow(mygo.WindowOptions{
			Title:         "gotion",
			Width:         1280,
			Height:        820,
			MinWidth:      760,
			MinHeight:     480,
			StateKey:      "main",
			TitleBarStyle: mygo.TitleBarHidden,
			Vibrancy:      vibrancy,
			Content:       ui.View(a.View),
		})
		emoji.OnLoad = w.Invalidate
		a.Attach(w)
	})
	mygo.App.OnNotificationClick(func(id string) { a.OnNotificationClick(id) })
	mygo.App.OnActivate(func(hasVisibleWindows bool) {
		if !hasVisibleWindows {
			a.ShowWindow()
		}
	})
	mygo.App.OnBeforeQuit(func(*mygo.QuitEvent) { a.Close() })
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
