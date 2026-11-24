// Package wallpaper tells whether the desktop wallpaper is dark where a
// window sits, so that a window with glass can pick light or dark text for
// what shows through it. Only the wallpaper is known: windows of other apps
// behind it are not.
package wallpaper

import (
	"image"
	_ "image/jpeg" // the wallpaper's formats
	_ "image/png"
	"os"
	"sync"
	"time"

	_ "golang.org/x/image/bmp"
)

// Rect is an area in screen coordinates.
type Rect struct{ X, Y, W, H int }

// grid is the wallpaper shrunk to a few cells of luminance, 0 to 1: enough
// to tell light from dark, cheap to look at again as the window moves.
type grid struct {
	w, h int
	lum  []float64
}

const cells = 48

func shrink(img image.Image) grid {
	b := img.Bounds()
	gw, gh := cells, max(1, cells*b.Dy()/max(1, b.Dx()))
	g := grid{w: gw, h: gh, lum: make([]float64, gw*gh)}
	n := make([]int, gw*gh)
	// Every few pixels is plenty for an average.
	step := max(1, min(b.Dx(), b.Dy())/400)
	for y := b.Min.Y; y < b.Max.Y; y += step {
		cy := (y - b.Min.Y) * gh / b.Dy()
		for x := b.Min.X; x < b.Max.X; x += step {
			r, gr, bl, _ := img.At(x, y).RGBA()
			cx := (x - b.Min.X) * gw / b.Dx()
			i := cy*gw + cx
			// Relative luminance, of the 16-bit channels.
			g.lum[i] += (0.2126*float64(r) + 0.7152*float64(gr) + 0.0722*float64(bl)) / 65535
			n[i]++
		}
	}
	for i := range g.lum {
		if n[i] > 0 {
			g.lum[i] /= float64(n[i])
		}
	}
	return g
}

// luminance is the average luminance of the part of the wallpaper behind
// area, on a display of the given bounds, the wallpaper filling the display
// as Windows' default "Fill" does: scaled to cover it, centered, cropped.
func (g grid) luminance(display, area Rect) float64 {
	if display.W <= 0 || display.H <= 0 || g.w == 0 {
		return 0.5
	}
	// The scale that covers the display, in grid cells per screen unit.
	s := max(float64(g.w)/float64(display.W), float64(g.h)/float64(display.H))
	s = min(s, float64(g.w)/float64(display.W)*1e9) // never zero
	ox := (float64(g.w) - float64(display.W)*s) / 2
	oy := (float64(g.h) - float64(display.H)*s) / 2
	cell := func(px, py int) (int, int) {
		x := int(ox + float64(px-display.X)*s)
		y := int(oy + float64(py-display.Y)*s)
		return min(max(x, 0), g.w-1), min(max(y, 0), g.h-1)
	}
	x0, y0 := cell(area.X, area.Y)
	x1, y1 := cell(area.X+area.W-1, area.Y+area.H-1)
	sum, n := 0.0, 0
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			sum += g.lum[y*g.w+x]
			n++
		}
	}
	if n == 0 {
		return 0.5
	}
	return sum / float64(n)
}

// Dark is where light text reads better than dark: the luminance at which
// white and near-black text have the same contrast is about 0.18; a little
// above it, as glass lightens what is behind it.
const Dark = 0.3

var cache struct {
	sync.Mutex
	path string
	mod  time.Time
	g    grid
	ok   bool
}

// IsDark reports whether the wallpaper is dark behind area of the display,
// and false for ok when the wallpaper can't be read: none on this system,
// a slideshow between pictures, a format not known.
func IsDark(display, area Rect) (dark, ok bool) {
	path := current()
	if path == "" {
		return false, false
	}
	st, err := os.Stat(path)
	if err != nil {
		return false, false
	}
	cache.Lock()
	defer cache.Unlock()
	if cache.path != path || !cache.mod.Equal(st.ModTime()) {
		cache.path, cache.mod, cache.ok = path, st.ModTime(), false
		if f, err := os.Open(path); err == nil {
			img, _, err := image.Decode(f)
			f.Close()
			if err == nil {
				cache.g, cache.ok = shrink(img), true
			}
		}
	}
	if !cache.ok {
		return false, false
	}
	return cache.g.luminance(display, area) < Dark, true
}

// Supported reports whether this system tells where its wallpaper is.
func Supported() bool { return supported }
