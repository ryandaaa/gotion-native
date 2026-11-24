//go:build ignore

// Draws gotion's icon, a note being typed: a heading, two lines and a
// caret on a white tile. Run from the repository root:
//
//	go run tools/icon/main.go
//
// It writes resources/icon.png (1024 px, which mygo build turns into the
// .ico, .icns and Linux icons) and worker/public/favicon.svg.
package main

import (
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
)

type rect struct {
	x, y, w, h, r float64
	c             color.NRGBA
}

func hex(s string) color.NRGBA {
	var v [3]uint8
	for i := range v {
		var n uint8
		for _, ch := range s[1+2*i : 3+2*i] {
			n <<= 4
			switch {
			case ch >= '0' && ch <= '9':
				n |= uint8(ch - '0')
			default:
				n |= uint8(ch-'a') + 10
			}
		}
		v[i] = n
	}
	return color.NRGBA{v[0], v[1], v[2], 255}
}

// The design, on a 64-unit grid, back to front.
var shapes = []rect{
	{0, 0, 64, 64, 15, hex("#e4e2df")}, // the tile's edge
	{2, 2, 60, 60, 13, hex("#ffffff")}, // the tile
	{14, 16, 30, 7, 3.5, hex("#1d1d1f")},
	{14, 29, 36, 5, 2.5, hex("#b5b2ad")},
	{14, 40, 26, 5, 2.5, hex("#b5b2ad")},
	{43, 38, 2.5, 9, 1.25, hex("#2563eb")},
}

func inside(r rect, px, py float64) bool {
	cx, cy := r.x+r.w/2, r.y+r.h/2
	qx := math.Abs(px-cx) - (r.w/2 - r.r)
	qy := math.Abs(py-cy) - (r.h/2 - r.r)
	d := math.Hypot(math.Max(qx, 0), math.Max(qy, 0)) + math.Min(math.Max(qx, qy), 0) - r.r
	return d <= 0
}

func main() {
	const size, ss = 1024, 4
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	unit := 64.0 / size
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			var r, g, b, a float64
			for sy := 0; sy < ss; sy++ {
				for sx := 0; sx < ss; sx++ {
					px := (float64(x) + (float64(sx)+0.5)/ss) * unit
					py := (float64(y) + (float64(sy)+0.5)/ss) * unit
					for i := len(shapes) - 1; i >= 0; i-- {
						if s := shapes[i]; inside(s, px, py) {
							r, g, b, a = r+float64(s.c.R), g+float64(s.c.G), b+float64(s.c.B), a+1
							break
						}
					}
				}
			}
			if a > 0 {
				img.SetNRGBA(x, y, color.NRGBA{uint8(r / a), uint8(g / a), uint8(b / a), uint8(a / (ss * ss) * 255)})
			}
		}
	}
	f, err := os.Create("resources/icon.png")
	if err != nil {
		panic(err)
	}
	if err := png.Encode(f, img); err != nil {
		panic(err)
	}
	f.Close()
	svg := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64"><rect x="1" y="1" width="62" height="62" rx="14" fill="#fff" stroke="#e4e2df" stroke-width="2"/><rect x="14" y="16" width="30" height="7" rx="3.5" fill="#1d1d1f"/><rect x="14" y="29" width="36" height="5" rx="2.5" fill="#b5b2ad"/><rect x="14" y="40" width="26" height="5" rx="2.5" fill="#b5b2ad"/><rect x="43" y="38" width="2.5" height="9" rx="1.25" fill="#2563eb"/></svg>` + "\n"
	if err := os.WriteFile("worker/public/favicon.svg", []byte(svg), 0o644); err != nil {
		panic(err)
	}
}
