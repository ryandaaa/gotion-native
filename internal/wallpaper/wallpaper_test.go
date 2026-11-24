package wallpaper

import (
	"image"
	"image/color"
	"testing"
)

// A wallpaper dark on its left half and light on its right: a window on
// the left is over dark, one on the right over light.
func TestLuminanceUnderAWindow(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 1920, 1080))
	for y := range 1080 {
		for x := range 1920 {
			c := color.RGBA{20, 20, 30, 255}
			if x >= 960 {
				c = color.RGBA{240, 235, 245, 255}
			}
			img.Set(x, y, c)
		}
	}
	g := shrink(img)
	display := Rect{0, 0, 1920, 1080}
	if l := g.luminance(display, Rect{100, 100, 300, 800}); l >= Dark {
		t.Errorf("left: %.2f, want dark", l)
	}
	if l := g.luminance(display, Rect{1200, 100, 300, 800}); l < Dark {
		t.Errorf("right: %.2f, want light", l)
	}
	// A display of another shape: the picture fills it, cropped at the
	// sides, and its halves stay where they were.
	tall := Rect{0, 0, 1080, 1080}
	if l := g.luminance(tall, Rect{0, 0, 200, 1080}); l >= Dark {
		t.Errorf("square display, left: %.2f", l)
	}
	// A second display to the right: coordinates are the screen's.
	second := Rect{1920, 0, 1920, 1080}
	if l := g.luminance(second, Rect{1920 + 1200, 0, 300, 600}); l < Dark {
		t.Errorf("second display, right: %.2f", l)
	}
	// A window half off the display reads what is on it.
	if l := g.luminance(display, Rect{-200, 0, 400, 400}); l >= Dark {
		t.Errorf("off the edge: %.2f", l)
	}
}

func TestNoWallpaperHere(t *testing.T) {
	if supported {
		t.Skip("this system has one")
	}
	if _, ok := IsDark(Rect{0, 0, 100, 100}, Rect{0, 0, 10, 10}); ok {
		t.Error("ok without a wallpaper")
	}
}
