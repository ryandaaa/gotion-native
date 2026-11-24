package app

import (
	"sync"
	"time"

	"gotion/internal/wallpaper"

	"github.com/egoist/mygo"
)

// wallState is what the wallpaper watcher reads of the settings, set by
// each frame: the watcher runs off the UI's goroutine.
type wallState struct {
	mu         sync.Mutex
	on         bool
	sidebar    float32
	sidebarOff bool
}

func (w *wallState) set(on bool, sidebar float32, off bool) {
	w.mu.Lock()
	w.on, w.sidebar, w.sidebarOff = on, sidebar, off
	w.mu.Unlock()
}

func (w *wallState) get() (bool, float32, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.on, w.sidebar, w.sidebarOff
}

// watchWall looks, every two seconds while the mode follows the wallpaper,
// whether the wallpaper behind the sidebar is light or dark: the window
// moves, the wallpaper changes.
func (a *App) watchWall(ws *wallState) {
	win := a.win
	last := 0
	for range time.Tick(2 * time.Second) {
		on, sidebar, off := ws.get()
		if !on {
			last = 0
			continue
		}
		b := win.Bounds()
		if b.Width <= 0 || b.Height <= 0 {
			continue
		}
		d := mygo.Screen.DisplayNearestPoint(mygo.Point{X: b.X + b.Width/2, Y: b.Y + b.Height/2})
		width := int(sidebar)
		if off || width <= 0 {
			width = b.Width / 4 // no sidebar: the left of the window, under the title
		}
		dark, ok := wallpaper.IsDark(
			wallpaper.Rect{X: d.Bounds.X, Y: d.Bounds.Y, W: d.Bounds.Width, H: d.Bounds.Height},
			wallpaper.Rect{X: b.X, Y: b.Y, W: min(width, b.Width), H: b.Height})
		v := 0
		if ok {
			v = 1
			if dark {
				v = 2
			}
		}
		if v != last {
			last = v
			a.update(func() { a.wallDark = v })
		}
	}
}
