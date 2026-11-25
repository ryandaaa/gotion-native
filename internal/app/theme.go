package app

import (
	_ "embed"

	"github.com/egoist/mygo/ui"
)

//go:embed fonts/inter-400-normal.ttf
var inter400 []byte

//go:embed fonts/inter-500-normal.ttf
var inter500 []byte

//go:embed fonts/inter-600-normal.ttf
var inter600 []byte

// RegisterFonts makes Inter available to the app.
func RegisterFonts() {
	for _, f := range [][]byte{inter400, inter500, inter600} {
		ui.RegisterFont(f, "Inter")
	}
}

// palette is the app's colors, from the mockup.
type palette struct {
	Sidebar, Panel, Window, Text, Muted, Faint, Line, Hover, Accent, Warn, OK, EditBg, EditLine ui.Color
}

var light = palette{
	Sidebar: ui.Hex("#F5F5F4"), Panel: ui.Hex("#FFFFFF"), Window: ui.Hex("#F5F5F4"),
	Text: ui.Hex("#1C1917"), Muted: ui.Hex("#57534E"), Faint: ui.Hex("#78716C"),
	Line: ui.Hex("#F0EFEE"), Hover: ui.Hex("#EBEAE8"), Accent: ui.Hex("#2563EB"),
	Warn: ui.Hex("#B45309"), OK: ui.Hex("#15803D"), EditBg: ui.Hex("#F5F8FF"), EditLine: ui.Hex("#BFD3FE"),
}

var dark = palette{
	Sidebar: ui.Hex("#1C1C1E"), Panel: ui.Hex("#252527"), Window: ui.Hex("#1C1C1E"),
	Text: ui.Hex("#EDEDEC"), Muted: ui.Hex("#A8A29E"), Faint: ui.Hex("#8A8580"),
	Line: ui.Hex("#333336"), Hover: ui.Hex("#2C2C2F"), Accent: ui.Hex("#60A5FA"),
	Warn: ui.Hex("#F59E0B"), OK: ui.Hex("#4ADE80"), EditBg: ui.Hex("#1E2A44"), EditLine: ui.Hex("#3B5BA8"),
}

// theme returns the MyGo theme for a palette.
func theme(p palette, isDark bool) *ui.Theme {
	t := ui.LightTheme()
	if isDark {
		t = ui.DarkTheme()
	}
	t.Font = "Inter"
	t.Background = p.Window
	t.Text, t.TextMuted = p.Text, p.Faint
	t.Accent = p.Accent
	// Split draws its divider in Border: a color that lets the window's
	// own through hides it until the pointer is over it, when it shows in
	// the accent.
	t.Border = p.Hover.Alpha(0)
	return t
}

func icon(path string) *ui.SVG {
	return ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">` + path + `</svg>`))
}

var (
	iconCheck       = icon(`<path d="M5 12l5 5L20 7"/>`)
	iconSync        = icon(`<path d="M21 12a9 9 0 1 1-3-6.7"/><path d="M21 4v5h-5"/>`)
	iconWarn        = icon(`<circle cx="12" cy="12" r="8"/><path d="M12 8v5M12 16h.01"/>`)
	iconDot         = icon(`<circle cx="12" cy="12" r="4" fill="currentColor"/>`)
	iconOffline     = icon(`<path d="M2 2l20 20M8.5 16.5a5 5 0 0 1 7 0M5 13a10 10 0 0 1 5-2.8M19 13a10 10 0 0 0-2.2-1.7M12 20h.01"/>`)
	iconPage        = icon(`<path d="M14 3H7a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V8z"/><path d="M14 3v5h5"/>`)
	iconTable       = icon(`<rect x="3" y="4" width="18" height="16" rx="2"/><path d="M3 10h18M9 10v10"/>`)
	iconSearch      = icon(`<circle cx="11" cy="11" r="7"/><path d="M20 20l-3.5-3.5"/>`)
	iconRefresh     = icon(`<path d="M3 12a9 9 0 0 1 15-6.7L21 8M21 3v5h-5M21 12a9 9 0 0 1-15 6.7L3 16M3 21v-5h5"/>`)
	iconLogout      = icon(`<path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4M16 17l5-5-5-5M21 12H9"/>`)
	iconMoon        = icon(`<path d="M21 12.8A9 9 0 1 1 11.2 3a7 7 0 0 0 9.8 9.8z"/>`)
	iconExt         = icon(`<path d="M14 4h6v6M20 4l-9 9M18 14v5a1 1 0 0 1-1 1H5a1 1 0 0 1-1-1V7a1 1 0 0 1 1-1h5"/>`)
	iconChevron     = icon(`<path d="M9 6l6 6-6 6"/>`)
	iconChevronDown = icon(`<path d="M6 9l6 6 6-6"/>`)
	iconDown        = icon(`<path d="M6 9l6 6 6-6"/>`)
	iconSidebar     = icon(`<rect x="3" y="4" width="18" height="16" rx="2"/><path d="M9 4v16"/>`)
	iconRightPane   = icon(`<rect x="3" y="4" width="18" height="16" rx="2"/><path d="M15 4v16"/>`)
	iconBack        = icon(`<path d="M15 18l-6-6 6-6"/>`)
	iconPlus        = icon(`<path d="M12 5v14M5 12h14"/>`)
	iconSplit       = icon(`<rect x="3" y="4" width="18" height="16" rx="2"/><path d="M12 4v16"/>`)
	iconClose       = icon(`<path d="M6 6l12 12M18 6L6 18"/>`)
	iconWide        = icon(`<path d="M3 12h18M7 8l-4 4 4 4M17 8l4 4-4 4"/>`)
	iconSun         = icon(`<circle cx="12" cy="12" r="4"/><path d="M12 2v2M12 20v2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M2 12h2M20 12h2M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4"/>`)
	iconText        = icon(`<path d="M4 7V5h16v2M9 19h6M12 5v14"/>`)
	iconHash        = icon(`<path d="M5 9h14M5 15h14M10 4L8 20M16 4l-2 16"/>`)
	iconList        = icon(`<circle cx="12" cy="12" r="9"/><path d="M8 12l3 3 5-6"/>`)
	iconCalendar    = icon(`<rect x="3" y="5" width="18" height="16" rx="2"/><path d="M3 10h18M8 3v4M16 3v4"/>`)
	iconClock       = icon(`<circle cx="12" cy="12" r="9"/><path d="M12 7v5l3 2"/>`)
	iconUser        = icon(`<circle cx="12" cy="8" r="4"/><path d="M4 21a8 8 0 0 1 16 0"/>`)
	iconLink        = icon(`<path d="M10 14a4 4 0 0 0 5.7 0l3-3a4 4 0 0 0-5.7-5.7l-1 1M14 10a4 4 0 0 0-5.7 0l-3 3a4 4 0 0 0 5.7 5.7l1-1"/>`)
	iconSettings    = icon(`<circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.7 1.7 0 0 0 .3 1.8l.1.1a2 2 0 1 1-2.8 2.8l-.1-.1a1.7 1.7 0 0 0-1.8-.3 1.7 1.7 0 0 0-1 1.5V21a2 2 0 1 1-4 0v-.1a1.7 1.7 0 0 0-1.1-1.5 1.7 1.7 0 0 0-1.8.3l-.1.1a2 2 0 1 1-2.8-2.8l.1-.1a1.7 1.7 0 0 0 .3-1.8 1.7 1.7 0 0 0-1.5-1H3a2 2 0 1 1 0-4h.1a1.7 1.7 0 0 0 1.5-1.1 1.7 1.7 0 0 0-.3-1.8l-.1-.1a2 2 0 1 1 2.8-2.8l.1.1a1.7 1.7 0 0 0 1.8.3H9a1.7 1.7 0 0 0 1-1.5V3a2 2 0 1 1 4 0v.1a1.7 1.7 0 0 0 1 1.5 1.7 1.7 0 0 0 1.8-.3l.1-.1a2 2 0 1 1 2.8 2.8l-.1.1a1.7 1.7 0 0 0-.3 1.8V9a1.7 1.7 0 0 0 1.5 1H21a2 2 0 1 1 0 4h-.1a1.7 1.7 0 0 0-1.5 1z"/>`)
	iconLibrary     = icon(`<path d="M4 19V5a1 1 0 0 1 1-1h3v16H5a1 1 0 0 1-1-1z"/><path d="M8 4h4v16H8"/><path d="M14.5 5.5l3.4-.9 3.1 14.5-3.4.8z"/>`)
	iconTrash       = icon(`<path d="M4 7h16M10 11v6M14 11v6M6 7l1 13h10l1-13M9 7V4h6v3"/>`)
	iconMore        = icon(`<circle cx="5" cy="12" r="1.4" fill="currentColor" stroke="none"/><circle cx="12" cy="12" r="1.4" fill="currentColor" stroke="none"/><circle cx="19" cy="12" r="1.4" fill="currentColor" stroke="none"/>`)
	iconComment     = icon(`<path d="M21 12a8 8 0 0 1-11.6 7.1L4 20l1-4.6A8 8 0 1 1 21 12z"/>`)
	iconFilter      = icon(`<path d="M4 5h16l-6 8v6l-4-2v-4z"/>`)
	iconSort        = icon(`<path d="M7 4v16M3 16l4 4 4-4M17 20V4M13 8l4-4 4 4"/>`)
	iconImport      = icon(`<path d="M12 3v12M7 10l5 5 5-5M5 21h14"/>`)
	iconRestore     = icon(`<path d="M3 12a9 9 0 1 0 3-6.7L3 8M3 3v5h5"/>`)
	iconUpload      = icon(`<path d="M12 21V9M7 14l5-5 5 5M5 3h14"/>`)
	iconAuto        = icon(`<circle cx="12" cy="12" r="9"/><path d="M12 3a9 9 0 0 1 0 18z" fill="currentColor"/>`)
)
