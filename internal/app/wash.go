package app

import (
	"gotion/internal/tr"
	"runtime"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// glassSupported reports whether the system draws a glass behind windows:
// macOS and Windows 11 do, Linux does not.
func glassSupported() bool { return runtime.GOOS == "darwin" || runtime.GOOS == "windows" }

// applyGlass shows or hides the system's glass behind the window, as the
// settings ask: Liquid Glass on macOS 26 (the popover material before),
// Acrylic on Windows 11.
func (a *App) applyGlass() {
	if a.win == nil || !glassSupported() {
		return
	}
	if a.set.Glass {
		a.win.SetVibrancy(glassMaterial(a.set.GlassKind))
	} else {
		a.win.SetVibrancy(mygo.VibrancyNone)
	}
}

// glassMaterial is the system's material for the glass: on Windows, Mica
// unless Acrylic was chosen; Liquid Glass (or the popover's) on macOS.
func glassMaterial(kind string) mygo.Vibrancy {
	if runtime.GOOS != "windows" {
		return mygo.VibrancyGlass
	}
	if kind == "acrylic" {
		return mygo.VibrancyAcrylic
	}
	return mygo.VibrancyMica
}

// veil is how much of the sidebar's color, or of the wash, lies over the
// glass so its text reads: little over Mica, already calm, more over
// Acrylic, which shows whatever is behind the window.
func (a *App) veil() float32 {
	if runtime.GOOS == "windows" && a.set.GlassKind == "acrylic" {
		return 0.8
	}
	return 0.6
}

// alphaGradient is g made see-through by alpha.
func alphaGradient(g ui.LinearGradient, alpha float32) ui.LinearGradient {
	stops := make([]ui.GradientStop, len(g.Stops))
	for i, st := range g.Stops {
		stops[i] = ui.GradientStop{Color: st.Color.Alpha(alpha), At: st.At}
	}
	g.Stops = stops
	return g
}

// A wash is a faint color behind the cards: a gradient down the window and
// a glow from its top right, light and dark, with an accent of its hue.
// The cards themselves stay white or gray, so the text reads the same.
type wash struct {
	id, label    string
	light, dark  []string
	glowL, glowD ui.Color
	accL, accD   ui.Color
}

var washes = []wash{
	{id: "polos", label: tr.N("Polos")},
	{id: "lavender", label: "Lavender",
		light: []string{"#ece6f6", "#f2eaf3", "#f6efea", "#f5f0dc"}, dark: []string{"#1d1426", "#211320", "#22170f"},
		glowL: ui.RGBA(235, 215, 248, 0.7), glowD: ui.RGBA(120, 70, 160, 0.28),
		accL: ui.Hex("#7c5cd6"), accD: ui.Hex("#a993ff")},
	{id: "kabut", label: tr.N("Kabut"),
		light: []string{"#e4ebf6", "#ebf0f6", "#f1f3f4", "#f3f2ed"}, dark: []string{"#0e1729", "#0f1a28", "#121c24"},
		glowL: ui.RGBA(205, 222, 250, 0.7), glowD: ui.RGBA(50, 95, 170, 0.28),
		accL: ui.Hex("#3b6cf6"), accD: ui.Hex("#6b93ff")},
	{id: "hutan", label: tr.N("Hutan"),
		light: []string{"#e4efe6", "#ecf2e8", "#f3f2e4", "#f4f0d9"}, dark: []string{"#0b1c10", "#121d0e", "#1b1c0d"},
		glowL: ui.RGBA(212, 238, 220, 0.7), glowD: ui.RGBA(20, 110, 60, 0.26),
		accL: ui.Hex("#2f8a57"), accD: ui.Hex("#5cc98a")},
	{id: "senja", label: tr.N("Senja"),
		light: []string{"#e8dcf2", "#f0dfec", "#f6e5de", "#f6e8d2"}, dark: []string{"#1b112c", "#261125", "#2b160d"},
		glowL: ui.RGBA(245, 208, 225, 0.75), glowD: ui.RGBA(130, 65, 140, 0.28),
		accL: ui.Hex("#c2457a"), accD: ui.Hex("#ec7aa8")},
	{id: "laut", label: tr.N("Laut"),
		light: []string{"#dcecef", "#e4eff1", "#ecf3ef", "#f1f2e9"}, dark: []string{"#021c22", "#031e20", "#0c1e1b"},
		glowL: ui.RGBA(196, 232, 238, 0.7), glowD: ui.RGBA(0, 110, 128, 0.28),
		accL: ui.Hex("#0e8a9a"), accD: ui.Hex("#4fc8d6")},
	{id: "aprikot", label: tr.N("Aprikot"),
		light: []string{"#f4e2d7", "#f7e8dd", "#f7eee0", "#f5f0dc"}, dark: []string{"#29130a", "#28160a", "#24190c"},
		glowL: ui.RGBA(250, 214, 196, 0.75), glowD: ui.RGBA(150, 64, 24, 0.28),
		accL: ui.Hex("#d0642a"), accD: ui.Hex("#f29a63")},
}

func washByID(id string) wash {
	for _, w := range washes {
		if w.id == id {
			return w
		}
	}
	return washes[0]
}

// plain reports whether the wash is no color at all.
func (w wash) plain() bool { return len(w.light) == 0 }

// base is the gradient down the window.
func (w wash) base(dark bool) ui.LinearGradient {
	hexes := w.light
	if dark {
		hexes = w.dark
	}
	stops := make([]ui.GradientStop, len(hexes))
	for i, h := range hexes {
		stops[i] = ui.GradientStop{Color: ui.Hex(h), At: float32(i) / float32(len(hexes)-1)}
	}
	return ui.LinearGradient{Angle: 180, Oklab: true, Stops: stops}
}

// glow is the light from the top right.
func (w wash) glow(dark bool) ui.LinearGradient {
	c := w.glowL
	if dark {
		c = w.glowD
	}
	return ui.LinearGradient{Angle: 225, Oklab: true, Stops: []ui.GradientStop{{Color: c, At: 0}, {Color: c.Alpha(0), At: 0.5}}}
}

// tint changes a palette for a wash: hovers that let the color through,
// and the wash's accent.
func (w wash) tint(p palette, dark bool) palette {
	if w.plain() {
		return p
	}
	if dark {
		p.Hover, p.Accent = ui.RGBA(255, 255, 255, 0.07), w.accD
	} else {
		p.Hover, p.Accent = ui.RGBA(0, 0, 0, 0.05), w.accL
	}
	p.EditBg = p.Accent.Alpha(0.12)
	p.EditLine = p.Accent.Alpha(0.45)
	return p
}

// swatch draws a wash as a dot to pick it by.
func (a *App) swatch(c *ui.Context, w wash, selected bool) ui.Element {
	p := a.pal()
	dot := ui.Box(c.Key("wash"+w.id)).Size(28, 28).Radius(14).Cursor(ui.CursorPointer).Tooltip(tr.T(w.label))
	if w.plain() {
		dot.Background(p.Sidebar)
	} else {
		dot.LinearGradient(w.base(a.dark))
	}
	if selected {
		dot.Border(2, p.Accent)
	} else {
		dot.Border(1, p.Faint.Alpha(0.4))
	}
	return dot
}
