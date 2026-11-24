package app

import (
	"fmt"
	"gotion/internal/tr"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/richtext"

	"gotion/internal/emoji"
	"gotion/internal/syncer"
)

// readWidth is the width of the text of a page.
const readWidth = 760

// View builds the window.
func (a *App) View(c *ui.Context) {
	// Before SetTheme, the theme is the system's.
	sysDark := c.Theme().Dark
	switch {
	case a.set.Theme == "wallpaper" && a.wallDark != 0:
		a.dark = a.wallDark == 2
	default:
		a.dark = a.set.Theme == "dark" || a.set.Theme != "light" && sysDark
	}
	if a.wall != nil {
		a.wall.set(a.set.Theme == "wallpaper", a.set.SidebarWidth, a.set.SidebarOff)
	}
	if a.win != nil && (a.buttonsSet == 0 || (a.buttonsSet == 2) != a.dark) {
		// Windows draws its window buttons for the system's theme, not the
		// app's: light glyphs on a light page when only the system is dark.
		a.buttonsSet = 1
		theme := mygo.ThemeLight
		if a.dark {
			a.buttonsSet, theme = 2, mygo.ThemeDark
		}
		a.win.SetWindowButtonsTheme(theme)
	}
	p := a.pal()
	c.SetTheme(theme(p, a.dark))
	if a.set.Zoom <= 0 {
		a.set.Zoom = 1
	}
	c.SetZoom(a.set.Zoom)
	for _, m := range a.toasts {
		c.Toast(m)
	}
	a.toasts = nil
	if a.client == nil || a.bot == nil && a.checking {
		ui.Column(c).Fill().Children(func() {
			a.titleBar(c, false)
			a.onboarding(c)
		})
		return
	}
	a.shortcuts(c)
	root := ui.Column(c).Fill()
	w := washByID(a.set.Wash)
	switch {
	case c.Vibrancy():
		// The system's glass shows through: only a veil of the sidebar's
		// color, or of the wash, keeps the text on it legible.
		if w.plain() {
			c.Root().Background(p.Sidebar.Alpha(a.veil() - 0.05))
		} else {
			c.Root().LinearGradient(alphaGradient(w.base(a.dark), a.veil()))
			root.LinearGradient(w.glow(a.dark))
		}
	case w.plain():
		root.Background(p.Sidebar)
	default:
		// The wash under everything, and its glow on top of it.
		c.Root().LinearGradient(w.base(a.dark))
		root.LinearGradient(w.glow(a.dark))
	}
	root.Children(func() {
		a.titleBar(c, true)
		row := ui.Row(c).Grow(1).MinHeight(0).AlignItems(ui.Stretch)
		// The sidebar slides open and shut: its width springs to its target,
		// its content keeping its own width, clipped, as it goes.
		target := a.set.SidebarWidth
		if a.set.SidebarOff {
			target = 0
		}
		w := row.AnimateTo("sidebar", target, ui.Smooth)
		open := min(1, max(0, w/a.set.SidebarWidth))
		row.Children(func() {
			if w > 0.5 {
				ui.Box(c).Width(w).Shrink(0).Clip().Children(func() {
					ui.Column(c).Width(a.set.SidebarWidth).FillHeight().Children(func() { a.sidebar(c) })
				})
			}
			if !a.set.SidebarOff {
				a.sidebarHandle(c)
			}
			// Room on the left of the panels while the sidebar is shut, so
			// they keep looking like cards.
			a.main(c, 10*(1-open))
		})
	})
	if a.switcher.open {
		a.switcherView(c)
	}
	if a.viewer.open {
		a.viewerView(c)
	}
	if a.settings.open {
		a.settingsDialog(c)
	}
}

// shortcuts are the window's keys.
func (a *App) shortcuts(c *ui.Context) {
	switch {
	case c.Shortcut(ui.Cmd, ui.KeyK), c.Shortcut(ui.Cmd, ui.KeyP):
		a.openSwitcher()
	case c.Shortcut(ui.Cmd, ui.KeyN):
		if p := a.panels[a.focus]; p != nil {
			a.newPage(p.id)
		}
	case c.Shortcut(ui.Cmd, ui.KeyBracketLeft), c.Shortcut(ui.Alt, ui.KeyLeft):
		a.back()
	case c.Shortcut(ui.Cmd, ui.KeyBracketRight), c.Shortcut(ui.Alt, ui.KeyRight):
		a.forward()
	case c.Shortcut(ui.Cmd, ui.KeyBackslash):
		a.set.SidebarOff = !a.set.SidebarOff
		a.saveSettings()
	case c.Shortcut(ui.Cmd, ui.KeyW):
		if p := a.panels[a.focus]; p != nil {
			a.closeTab(a.focus, a.tabIndex(a.focus, p))
		}
	case c.Shortcut(ui.Ctrl, ui.KeyTab):
		a.cycleTab(1)
	case c.Shortcut(ui.Ctrl|ui.Shift, ui.KeyTab):
		a.cycleTab(-1)
	case c.Shortcut(ui.Cmd|ui.Shift, ui.KeyL):
		a.cycleTheme()
	case c.Shortcut(ui.Cmd, ui.KeyR):
		a.refresh()
	case c.Shortcut(ui.Cmd, ui.KeyComma):
		a.openSettings("")
	case c.Shortcut(ui.Cmd, ui.KeyEqual), c.Shortcut(ui.Cmd|ui.Shift, ui.KeyEqual):
		a.zoom(+1)
	case c.Shortcut(ui.Cmd, ui.KeyMinus):
		a.zoom(-1)
	case c.Shortcut(ui.Cmd, ui.Key0):
		a.zoom(0)
	}
}

// zooms are the steps Ctrl+= and Ctrl+- go through, as a browser's.
var zooms = []float32{0.67, 0.75, 0.8, 0.9, 1, 1.1, 1.25, 1.5, 1.75, 2, 2.5}

// zoom steps the zoom in (dir 1) or out (-1), or back to 100% (0).
func (a *App) zoom(dir int) {
	z := float32(1)
	if dir != 0 {
		i := 0
		for i < len(zooms) && zooms[i] < a.set.Zoom-0.001 {
			i++
		}
		// i is the step at or just above the zoom now.
		if dir > 0 {
			if i < len(zooms) && zooms[i] <= a.set.Zoom+0.001 {
				i++
			}
			i = min(i, len(zooms)-1)
		} else {
			i = max(i-1, 0)
		}
		z = zooms[i]
	}
	a.set.Zoom = z
	a.saveSettings()
	a.toast(fmt.Sprintf("Zoom %d%%", int(z*100+0.5)))
}

func (a *App) cycleTheme() {
	next := map[string]string{"system": "light", "light": "dark", "dark": "system"}[a.set.Theme]
	if next == "" {
		next = "system"
	}
	a.set.Theme = next
	a.saveSettings()
	a.toast(map[string]string{"system": tr.T("Tema: ikut sistem"), "light": tr.T("Tema: terang"), "dark": tr.T("Tema: gelap")}[next])
}

// titleBar is the bar the window controls sit in: it drags the window.
func (a *App) titleBar(c *ui.Context, full bool) {
	p := a.pal()
	bar := c.TitleBar()
	ui.Row(c).Height(max(bar.Height, 44)).Shrink(0).AlignItems(ui.Center).Gap(4).
		Padding(0, bar.Right+8, 0, bar.Left+12).DragWindow().Children(func() {
		if !full {
			ui.Text(c, "gotion").FontSize(14).FontWeight(600)
			return
		}
		if iconButton(c, p, iconSidebar, tr.T("Sidebar (Ctrl+\\)")) {
			a.set.SidebarOff = !a.set.SidebarOff
			a.saveSettings()
		}
		if navButton(c, p, iconBack, tr.T("Kembali (Ctrl+[)"), a.hpos > 0) {
			a.back()
		}
		if navButton(c, p, iconForward, tr.T("Maju (Ctrl+])"), a.hpos < len(a.history)-1) {
			a.forward()
		}
		a.breadcrumbs(c)
		ui.Box(c).Grow(1)
		search := ui.Row(c.Key("search")).AlignItems(ui.Center).Gap(8).Padding(5, 12).Radius(8).Background(p.Hover).
			Cursor(ui.CursorPointer).Width(260).Children(func() {
			ui.Icon(c, iconSearch).Size(14, 14).TextColor(p.Faint)
			ui.Text(c, tr.T("Cari halaman…")).FontSize(13).TextColor(p.Faint).Grow(1)
			ui.Text(c, "Ctrl K").FontSize(11).TextColor(p.Faint)
		})
		if search.Clicked() {
			a.openSwitcher()
		}
		ui.Box(c).Grow(1)
		if a.loading {
			ui.Spinner(c).Size(14, 14)
		}
		if z := a.set.Zoom; z < 0.99 || z > 1.01 {
			b := ui.Text(c.Key("zoom"), fmt.Sprintf("%d%%", int(z*100+0.5))).FontSize(12).TextColor(p.Muted).
				Padding(3, 8).Radius(6).Cursor(ui.CursorPointer).Tooltip(tr.T("Kembali ke 100% (Ctrl+0)"))
			if b.Hovered() {
				b.Background(p.Hover)
			}
			if b.Clicked() {
				a.zoom(0)
			}
		}
		if iconButton(c, p, iconRefresh, tr.T("Muat ulang (Ctrl+R)")) {
			a.refresh()
		}
		a.themeButton(c)
	})
}

// breadcrumbs shows where the focused page is; each step opens.
func (a *App) breadcrumbs(c *ui.Context) {
	p := a.pal()
	pn := a.panels[a.focus]
	if pn == nil || a.nodes[pn.id] == nil {
		return
	}
	steps := append(a.path(pn.id), a.nodes[pn.id])
	if len(steps) > 4 {
		steps = append(steps[:1], steps[len(steps)-3:]...)
	}
	ui.Row(c).AlignItems(ui.Center).Gap(2).MarginStart(6).MinWidth(0).Shrink(1).Children(func() {
		for i, n := range steps {
			if i > 0 {
				ui.Text(c, "/").FontSize(13).TextColor(p.Faint)
			}
			last := i == len(steps)-1
			b := ui.Row(c.Key("crumb"+n.ID)).AlignItems(ui.Center).Gap(4).Padding(3, 6).Radius(6).Cursor(ui.CursorPointer).MinWidth(0)
			if b.Hovered() {
				b.Background(p.Hover)
			}
			b.Children(func() {
				if n.Emoji != "" {
					emoji.View(c, n.Emoji, 15)
				}
				t := ui.Text(c, n.Title).FontSize(13).SingleLine().MaxWidth(180)
				if last {
					t.FontWeight(500)
				} else {
					t.TextColor(p.Muted)
				}
			})
			if b.Clicked() && !last {
				a.open(n, a.focus)
			}
		}
	})
}

// sidebar is the page tree.
func (a *App) sidebar(c *ui.Context) {
	p := a.pal()
	ui.Column(c).Fill().Padding(4, 8, 10, 10).Gap(2).Children(func() {
		for _, id := range []string{libraryID, trashID} {
			n := a.special(id)
			s := map[string]*ui.SVG{libraryID: iconLibrary, trashID: iconTrash}[id]
			current := a.panels[a.focus] != nil && a.panels[a.focus].id == id
			r := ui.Row(c.Key("sp"+id)).AlignItems(ui.Center).Gap(10).Padding(7, 10).Radius(8).Cursor(ui.CursorPointer)
			if current {
				r.Background(p.Panel).Shadow(0, 1, 2, 0, ui.RGBA(0, 0, 0, 0.08))
			} else if r.Hovered() {
				r.Background(p.Hover)
			}
			r.Children(func() {
				ui.Icon(c, s).Size(16, 16).TextColor(p.Muted)
				ui.Text(c, n.Title).FontSize(14).Grow(1)
				if id == trashID && len(a.set.Trashed) > 0 {
					ui.Text(c, itoa(len(a.set.Trashed))).FontSize(12).TextColor(p.Faint)
				}
			})
			if r.Clicked() {
				a.open(n, a.focus)
			}
		}
		ui.Row(c).AlignItems(ui.Center).Gap(6).Padding(10, 8, 6, 10).Children(func() {
			ui.Text(c, tr.T("Halaman")).FontSize(12).FontWeight(600).TextColor(p.Faint).Grow(1)
			if iconButton(c, p, iconPlus, tr.T("Halaman baru di halaman ini (Ctrl+N)")) {
				if pn := a.panels[a.focus]; pn != nil {
					a.newPage(pn.id)
				}
			}
		})
		// Only the rows in view are built: a workspace of hundreds of pages
		// costs a frame what a screenful does.
		rows := a.sideRows()
		a.sideList.Key = func(i int) any { return rows[i].key }
		ui.List(c, &a.sideList, len(rows), func(i int) {
			r := rows[i]
			switch r.kind {
			case "error":
				ui.Text(c, tr.T("Gagal memuat: ")+a.loadErr).FontSize(12).TextColor(c.Theme().Danger).Padding(4, 10)
			case "empty":
				ui.Text(c, tr.T("Belum ada halaman yang terhubung. Di Notion: ••• → Connections → pilih integrasi kamu.")).
					FontSize(12).TextColor(p.Faint).Padding(4, 10)
			case "db":
				a.sectionHeader(c, "Database", len(a.databases), "db")
			default:
				a.item(c, r.n, r.depth)
			}
		}).Grow(1).Label(tr.T("Halaman")).ScrollbarStyle(a.thinScrollbar()).Padding(0, 10, 0, 0)
		ui.Row(c).AlignItems(ui.Center).Gap(2).Padding(6, 4, 0, 4).Children(func() {
			if iconButton(c, p, iconSettings, tr.T("Pengaturan (Ctrl+,)")) {
				a.openSettings("")
			}
			ui.Box(c).Grow(1)
			if a.bot != nil && a.bot.Bot != nil {
				ui.Text(c, a.bot.Bot.WorkspaceName).FontSize(12).TextColor(p.Faint).SingleLine().MaxWidth(180)
			}
		})
	})
}

func (a *App) sectionHeader(c *ui.Context, title string, count int, key string) {
	p := a.pal()
	h := ui.Row(c.Key("s"+key)).AlignItems(ui.Center).Gap(6).Padding(14, 10, 4, 10).Cursor(ui.CursorPointer).Children(func() {
		ui.Text(c, title).FontSize(12).FontWeight(600).TextColor(p.Faint).SingleLine()
		ui.Text(c, fmt.Sprint(count)).FontSize(12).TextColor(p.Faint)
	})
	if h.Clicked() {
		a.set.Closed[key] = !a.set.Closed[key]
		a.saveSettings()
	}
}

// sideRow is a row of the sidebar: a page, a group's or a section's
// header, or a note.
type sideRow struct {
	kind  string // "item", "db", "error", "empty"
	n     *node
	depth int
	key   string
}

// sideRows lists the sidebar's rows: the top pages, with what is inside
// them where the user opened them, then the databases.
func (a *App) sideRows() []sideRow {
	var rows []sideRow
	if a.loadErr != "" {
		rows = append(rows, sideRow{kind: "error", key: "!error"})
	}
	if len(a.nodes) == 0 && !a.loading {
		rows = append(rows, sideRow{kind: "empty", key: "!empty"})
	}
	var tree func(n *node, depth int)
	tree = func(n *node, depth int) {
		rows = append(rows, sideRow{kind: "item", n: n, depth: depth, key: n.ID})
		if a.set.Open[n.ID] {
			for _, ch := range n.Children {
				tree(ch, depth+1)
			}
		}
	}
	for _, g := range a.groups {
		tree(g, 0)
	}
	if len(a.databases) > 0 {
		rows = append(rows, sideRow{kind: "db", key: "!db"})
		if !a.set.Closed["db"] {
			for _, n := range a.databases {
				tree(n, 0)
			}
		}
	}
	return rows
}

func (a *App) item(c *ui.Context, n *node, depth int) {
	p := a.pal()
	var current *panel
	for _, pn := range a.panels {
		if pn != nil && pn.id == n.ID {
			current = pn
		}
	}
	row := ui.Row(c.Key(n.ID)).AlignItems(ui.Center).Gap(6).Padding(6, 8, 6, 4+float32(min(depth, 6))*14).Radius(8).Cursor(ui.CursorPointer)
	hovered := row.Hovered()
	if current != nil {
		row.Background(p.Panel).Shadow(0, 1, 2, 0, ui.RGBA(0, 0, 0, 0.08))
	} else if hovered {
		row.Background(p.Hover)
	}
	var toggled, added, beside bool
	row.Children(func() {
		// The arrow, or room for it.
		arrow := ui.Box(c.Key("arrow")).Size(18, 18).Center().Radius(4)
		if len(n.Children) > 0 {
			arrow.Children(func() {
				s := iconChevron
				if a.set.Open[n.ID] {
					s = iconDown
				}
				ui.Icon(c, s).Size(14, 14).TextColor(p.Muted)
			})
			if arrow.Hovered() {
				arrow.Background(p.Line)
			}
			toggled = arrow.Clicked()
		}
		switch {
		case n.Emoji != "":
			emoji.View(c, n.Emoji, 15)
		case n.Kind != "page":
			ui.Icon(c, iconTable).Size(14, 14).TextColor(p.Muted)
		default:
			ui.Icon(c, iconPage).Size(14, 14).TextColor(p.Muted)
		}
		ui.Text(c, n.Title).FontSize(14).SingleLine().Grow(1).MinWidth(0)
		if hovered {
			if n.Kind == "page" {
				added = smallButton(c, p, iconPlus, tr.T("Halaman baru di dalamnya"))
			}
			beside = smallButton(c, p, iconSplit, tr.T("Buka di samping"))
			return
		}
		if n.Kind != "page" {
			ui.Text(c, "db").FontSize(11).FontWeight(600).TextColor(p.Muted).Padding(1, 6).Background(p.Hover).Radius(5)
			return
		}
		if current != nil && current.session != nil {
			st, _ := current.session.Status()
			statusIcon(c, p, st).Size(14, 14)
		}
	})
	switch {
	case toggled:
		a.set.Open[n.ID] = !a.set.Open[n.ID]
		a.saveSettings()
	case added:
		a.newPage(n.ID)
	case beside:
		a.openBeside(n)
	case row.Clicked():
		if row.ClickModifiers()&ui.Ctrl != 0 {
			a.openBeside(n)
		} else {
			a.open(n, a.focus)
		}
	}
}

func statusIcon(c *ui.Context, p palette, st syncer.Status) ui.Element {
	switch st {
	case syncer.Pending:
		return ui.Icon(c, iconDot).TextColor(p.Text).Tooltip(st.String())
	case syncer.Syncing:
		return ui.Icon(c, iconSync).TextColor(p.Faint).Tooltip(st.String())
	case syncer.Failed:
		return ui.Icon(c, iconWarn).TextColor(p.Warn).Tooltip(st.String())
	case syncer.Offline:
		return ui.Icon(c, iconOffline).TextColor(p.Warn).Tooltip(st.String())
	}
	return ui.Icon(c, iconCheck).TextColor(p.Faint).Tooltip(st.String())
}

func iconButton(c *ui.Context, p palette, s *ui.SVG, label string) bool {
	b := ui.Box(c.Key(label)).Padding(5).Radius(6).Cursor(ui.CursorPointer).Label(label).Tooltip(label).Focusable()
	if b.Hovered() {
		b.Background(p.Hover)
	}
	b.Children(func() { ui.Icon(c, s).Size(16, 16).TextColor(p.Muted) })
	return b.Clicked()
}

func navButton(c *ui.Context, p palette, s *ui.SVG, label string, on bool) bool {
	b := ui.Box(c.Key(label)).Padding(5).Radius(6).Label(label).Tooltip(label)
	// Off, it stays a clear gray, as a control that can't be used now
	// does: the line's color was lost over the glass.
	color := p.Faint.Alpha(0.5)
	if on {
		color = p.Muted
		b.Cursor(ui.CursorPointer)
		if b.Hovered() {
			b.Background(p.Hover)
		}
	}
	b.Children(func() { ui.Icon(c, s).Size(16, 16).TextColor(color) })
	return on && b.Clicked()
}

func smallButton(c *ui.Context, p palette, s *ui.SVG, label string) bool {
	b := ui.Box(c.Key(label)).Padding(2).Radius(4).Cursor(ui.CursorPointer).Label(label).Tooltip(label)
	if b.Hovered() {
		b.Background(p.Line)
	}
	b.Children(func() { ui.Icon(c, s).Size(14, 14).TextColor(p.Muted) })
	return b.Clicked()
}

func ago(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
	return t.Format("2 Jan")
}

// main is the panels.
func (a *App) main(c *ui.Context, left float32) {
	ui.Row(c).Grow(1).MinWidth(0).AlignItems(ui.Stretch).Padding(0, 10, 10, left).Children(func() {
		if a.panels[1] == nil {
			a.panelView(c, 0)
			return
		}
		// Neither panel narrower than minPanel, however the divider moves.
		if total := a.widths[0] + a.widths[1]; total >= 2*minPanel {
			a.set.SplitSize = min(max(a.set.SplitSize, minPanel), total-minPanel)
		} else if a.set.SplitSize < minPanel {
			a.set.SplitSize = 640
		}
		s := ui.Split(c, &a.set.SplitSize, func() { a.panelView(c, 0) }, func() { a.panelView(c, 1) }).Fill()
		if s.Changed() {
			a.saveSettings()
		}
	})
}

// panelView is the card of panel i.
func (a *App) panelView(c *ui.Context, i int) {
	p := a.pal()
	pn := a.panels[i]
	card := ui.Column(c.Key(fmt.Sprint("panel", i))).Fill().MinWidth(0).Background(p.Panel).Radius(12).Clip().
		Shadow(0, 1, 3, 0, ui.RGBA(0, 0, 0, 0.08))
	if a.panels[1] != nil {
		// A gutter between the two cards, the divider in its middle.
		if i == 0 {
			card.Margin(0, 5, 0, 0)
		} else {
			card.Margin(0, 0, 0, 5)
		}
	}
	if card.PressedInside() {
		a.focus = i
	}
	card.Children(func() {
		switch {
		case pn == nil || pn.id == "":
			a.empty(c)
		case a.kindOf(pn.id) == "library":
			a.libraryPanel(c, i)
		case a.kindOf(pn.id) == "trash":
			a.trashPanel(c, i)
		case pn.table != nil:
			a.tablePanel(c, i, pn)
		default:
			a.pagePanel(c, i, pn)
		}
	})
}

func (a *App) empty(c *ui.Context) {
	p := a.pal()
	if !a.hasPages() {
		a.noPages(c)
		return
	}
	ui.Column(c).Fill().Center().Gap(10).Children(func() {
		ui.Icon(c, iconPage).Size(28, 28).TextColor(p.Faint)
		ui.Text(c, tr.T("Pilih halaman di sidebar")).FontSize(15).TextColor(p.Muted)
		ui.Text(c, tr.T("Ctrl+K cari · Ctrl+N halaman baru · Ctrl+\\ sidebar · Ctrl+klik buka di samping")).FontSize(12).TextColor(p.Faint)
	})
}

// panelHeader is the bar over a panel.
func (a *App) panelHeader(c *ui.Context, i int, s *ui.SVG, title, badge string, right func()) {
	p := a.pal()
	// The tabs, on the panel's own strip, then the page's controls.
	ui.Row(c).AlignItems(ui.Center).Gap(8).Padding(6, 10, 6, 6).Background(p.Sidebar.Alpha(0.6)).
		BorderWidth(0, 0, 1, 0).BorderColor(p.Line).Children(func() {
		a.tabStrip(c, i)
		if right != nil {
			right()
		}
		ui.Text(c, badge).FontSize(11).FontWeight(600).TextColor(p.Muted).Padding(2, 8).Background(p.Sidebar).Radius(6)
		if a.panels[1] == nil {
			if iconButton(c, p, iconSplit, tr.T("Belah panel")) {
				if pn := a.panels[i]; pn != nil {
					if n := a.nodes[pn.id]; n != nil {
						a.openBeside(n)
					}
				}
			}
		} else if iconButton(c, p, iconSidebar, tr.T("Tutup panel ini")) {
			if i == 1 {
				a.closeSecond()
			} else {
				for _, t := range a.tabs[0] {
					a.flushPanel(t)
					if t.session != nil {
						go t.session.Close()
					}
				}
				a.panels[0], a.tabs[0] = a.panels[1], a.tabs[1]
				a.panels[1], a.tabs[1] = nil, nil
				a.focus = 0
				a.saveTabs()
			}
		}
	})
}

// pagePanel is an open page and its editor.
func (a *App) pagePanel(c *ui.Context, i int, pn *panel) {
	p := a.pal()
	n := a.nodes[pn.id]
	title := tr.T("Halaman")
	if n != nil {
		title = n.Title
	}
	a.panelHeader(c, i, iconPage, title, "page", func() {
		if pn.session != nil {
			st, err := pn.session.Status()
			color := p.Faint
			label := st.String()
			switch st {
			case syncer.Synced:
				color, label = p.OK, tr.T("✓ Tersimpan")
			case syncer.Failed, syncer.Offline:
				color = p.Warn
				if err != nil {
					label += ": " + errText(err)
				}
			}
			ui.Text(c, label).FontSize(12).FontWeight(500).TextColor(color).SingleLine().MaxWidth(280)
		}
		if b := iconButton(c, p, iconComment, tr.T("Komentar")); b {
			a.toggleComments(pn)
		}
		a.pageActions(c, i, pn)
		if iconButton(c, p, iconWide, tr.T("Lebar penuh")) {
			a.set.FullWidth = !a.set.FullWidth
			a.saveSettings()
		}
		if iconButton(c, p, iconExt, tr.T("Buka di Notion")) {
			go mygo.Shell.OpenExternal("https://www.notion.so/" + strings.ReplaceAll(pn.id, "-", ""))
		}
	})
	if pn.opening {
		ui.Column(c).Fill().Center().Gap(10).Children(func() {
			ui.Spinner(c).Size(22, 22)
			ui.Text(c, tr.T("Memuat halaman…")).FontSize(13).TextColor(p.Faint)
		})
		return
	}
	if pn.openErr != "" {
		ui.Column(c).Fill().Center().Gap(10).Children(func() {
			ui.Text(c, tr.T("Gagal membuka: ")+pn.openErr).FontSize(14).TextColor(c.Theme().Danger)
			if a.btn(c, tr.T("Coba lagi"), iconRefresh, false, false) {
				if n != nil {
					a.show(n, i)
				}
			}
		})
		return
	}
	if pn.doc == nil {
		return
	}
	// The text keeps a reading width, centered; the editor itself spans the
	// panel so its scroll bar sits at the panel's edge.
	//
	// The width is known only after layout, so this frame uses the last
	// one's: the panel's, else the last width of any page in this place.
	width := pn.width
	if width == 0 {
		width = a.widths[i]
	}
	pad := padding(width, a.set.FullWidth)
	pn.textWidth = width - 2*pad
	// The page, and its comments beside it when open. The cover, the title,
	// the properties and the text scroll as one, as in Notion: the editor is
	// as tall as its text, in the page's scroll.
	ui.Row(c).Grow(1).MinHeight(0).AlignItems(ui.Stretch).Children(func() {
		ui.Scroll(c.Key("page-scroll" + pn.id)).Grow(1).MinWidth(0).MinHeight(0).ScrollbarStyle(a.thinScrollbar()).Children(func() {
			a.coverView(c, pn)
			a.pageBody(c, i, pn, n, width, pad)
		})
		if pn.comments.open {
			a.commentsDrawer(c, pn)
		}
	})
}

// pageBody is the page's title and text.
func (a *App) pageBody(c *ui.Context, i int, pn *panel, n *node, width, pad float32) {
	col := ui.Column(c).FillWidth().Shrink(0).Padding(24, 0, 0, 0).Gap(10)
	if b := col.LayoutBounds(); b.W > 0 {
		pn.width, a.widths[i] = b.W, b.W
		// Tables draw with the width this frame laid the text out for
		// (textWidth above); a different one is for the next frame.
		if d := b.W - width; d > 0.5 || d < -0.5 {
			// Drawn with a stale width: draw again now, not at the next
			// mouse move.
			c.AnimationFrame()
		}
	}
	col.Children(func() {
		// The page's emoji stands above its title, as Notion shows it,
		// not in the line of the title.
		if n != nil && n.Emoji != "" {
			ui.Row(c).PaddingX(pad).Children(func() { emoji.View(c, n.Emoji, 52) })
		}
		ui.Row(c).PaddingX(pad).AlignItems(ui.Center).Gap(10).Children(func() {
			// The title is larger than any heading in the text: 40 against
			// the first level's 30.
			in := ui.TextInput(c.Key("title"+pn.id), &pn.title).Placeholder(tr.T("Tanpa judul")).FontSize(40).FontWeight(700).
				Grow(1).Background(ui.Color{}).Border(0, ui.Color{}).Padding(0).FocusRing(false).Label(tr.T("Judul halaman"))
			if in.Changed() {
				pn.titleDirty, pn.titleEdit = true, time.Now()
				c.After(1600 * time.Millisecond)
			}
			if pn.titleDirty && (in.Submitted() || !in.Focused() || time.Since(pn.titleEdit) > 1500*time.Millisecond) {
				a.sendTitle(pn)
			}
		})
		a.propsView(c, pn, pad)
		a.extrasView(c, i, pn, pad)
		a.editor(c, i, pn, pad)
	})
}

// extrasView notes the blocks nested too deep to show.
func (a *App) extrasView(c *ui.Context, i int, pn *panel, pad float32) {
	p := a.pal()
	for _, x := range pn.extras {
		if x.Type != "nested" {
			continue
		}
		row := ui.Row(c.Key("x"+x.ID)).AlignItems(ui.Center).Gap(10).Padding(8, 14).Margin(0, pad, 0, pad).Radius(8).
			Border(1, p.Line).Cursor(ui.CursorPointer)
		row.Children(func() {
			ui.Text(c, tr.T("Isi bertingkat di dalam “")+x.Title+tr.T("” terlalu dalam untuk diedit di sini")).FontSize(13).TextColor(p.Muted).Grow(1)
			ui.Icon(c, iconExt).Size(14, 14).TextColor(p.Faint)
		})
		if row.Clicked() {
			go mygo.Shell.OpenExternal("https://www.notion.so/" + strings.ReplaceAll(x.ID, "-", ""))
		}
	}
}

func nonEmpty(s, alt string) string {
	if strings.TrimSpace(s) == "" {
		return alt
	}
	return s
}

// editor is the page's text, with the slash menu and the format bar over it.
func (a *App) editor(c *ui.Context, i int, pn *panel, pad float32) {
	p := a.pal()
	box := ui.Box(c).FillWidth().Shrink(0)
	if files := box.DroppedFiles(); len(files) > 0 {
		// Markdown comes in as sub-pages; anything else as attachments.
		var md, other []string
		for _, f := range files {
			if ext := strings.ToLower(filepath.Ext(f)); ext == ".md" || ext == ".markdown" {
				md = append(md, f)
			} else {
				other = append(other, f)
			}
		}
		if len(md) > 0 {
			a.importMarkdown(pn.id, md)
		}
		if len(other) > 0 {
			a.dropFiles(pn, other)
		}
	}
	box.Children(func() {
		e := richtext.Editor(c.Key("editor-"+pn.id), pn.doc, richtext.EditorOptions{
			Theme:       a.prose(pad),
			Label:       tr.T("Isi halaman"),
			Placeholder: tr.T("Ketik / untuk perintah…"),
			OnLink:      a.followLink(i),
			OnKey: func(k ui.Key, m ui.Modifiers) bool {
				// Ctrl+V with a picture on the clipboard uploads it; with
				// text, the editor pastes as ever.
				if k == ui.KeyV && m == ui.Cmd && a.pasteImage(pn) {
					return true
				}
				return a.mentionKey(pn, k, m) || a.slashKey(pn, k, m)
			},
			Menu: func(m *ui.Menu) {
				m.EditItems()
				m.Separator()
				if m.Item(tr.T("Tempel gambar")).Disabled(len(mygo.Clipboard.ReadImage()) == 0).Chosen() {
					a.pasteImage(pn)
				}
			},
			// A picture on the clipboard is uploaded; text pastes as ever.
			OnCommand: func(name string) bool { return name == "paste" && a.pasteImage(pn) },
			Font:      ui.Font{Family: "Inter", Size: 16, LineHeight: 1.7},
			Space:     func(int) float32 { return 3 },
			// The page scrolls around it, with its own bar.
			NoScrollbar: true,
		}).FillWidth().Height(editorHeight(pn.doc)).Background(p.Panel).Border(0, p.Panel).Radius(0)
		if pn.focusEditor {
			pn.focusEditor = false
			e.Focus()
			pn.revealCaret = true
		}
		if e.Focused() {
			a.focus = i
		}
		if e.Changed() {
			if id, emptied := emptiedReadOnly(pn.doc, pn.readOnly); emptied && pn.session != nil && pn.session.Deletable(id) {
				// Backspace or Delete on a picture, a file or a divider
				// takes its block out of the page.
				removeParagraph(pn.doc, id)
				a.edited(pn)
				a.toast(tr.T("Dihapus dari halaman"))
			} else if brokeReadOnly(pn.doc, pn.readOnly) {
				// Sub-pages, images and tables are not edited here: take
				// the edit back.
				pn.doc.Undo()
				if !pn.warned {
					pn.warned = true
					a.toast(tr.T("Baris ini tidak bisa diedit di gotion. Tekan Enter di ujungnya untuk menulis di bawahnya."))
				}
			} else {
				if pn.session != nil {
					pn.session.Edited(pn.doc.Text())
				}
				a.slashUpdate(pn)
				a.mentionUpdate(pn)
			}
		}
		a.handles(c, pn, box, pad)
		a.slashView(c, pn)
		a.mentionView(c, pn)
		if e.Focused() && !pn.slash.open {
			a.formatBar(c, pn)
		}
		a0, c0 := pn.doc.Selection()
		ver := pn.doc.Version()
		if !pn.caretInit {
			pn.caretInit = true
			pn.caretSeen, pn.verSeen = [2]int{a0, c0}, ver
		} else if pn.caretSeen != [2]int{a0, c0} || pn.verSeen != ver {
			pn.caretSeen, pn.verSeen = [2]int{a0, c0}, ver
			if e.Focused() {
				pn.revealCaret = true
			}
		}
		if pn.revealCaret {
			if !e.Focused() {
				pn.revealCaret = false
			} else if _, ok := pn.doc.CaretRect(); ok {
				pn.revealCaret = false
				e.ScrollIntoView()
			} else {
				c.AnimationFrame()
			}
		}
	})
}

// minPanel is the narrowest a panel of a split gets.
const minPanel = 360

// minText is the narrowest the text of a page gets, however stale a width.
const minText = 320

// padding is the room left and right of a page's text in a panel width
// wide: centered at the reading width, never squeezing the text.
func padding(width float32, full bool) float32 {
	pad := max(48, (width-readWidth)/2)
	if full {
		pad = 56
	}
	if width > 0 && width-2*pad < minText {
		pad = max(16, (width-minText)/2)
	}
	return pad
}

// prose is how page text looks: room to breathe between paragraphs and
// around headings, links in the text's color, quiet markers and code.
func (a *App) prose(pad float32) *richtext.Theme {
	p := a.pal()
	return &richtext.Theme{
		Padding:             [2]float32{pad, 16},
		Link:                p.Text,
		Muted:               p.Faint,
		Marker:              p.Faint,
		Checkbox:            p.Accent,
		QuoteBar:            p.Faint.Alpha(0.6),
		QuoteText:           p.Text,
		QuoteBarWidth:       2.5,
		ParagraphSpacing:    0.55,
		Indent:              1.6,
		CodeBackground:      p.Hover,
		Code:                p.Warn,
		CodeBlockBackground: p.Sidebar,
		CodeBlockRadius:     0.4,
		CodeBlockPadding:    [2]float32{1, 0.8},
		Headings: [6]richtext.HeadingStyle{
			{Size: 1.875, Weight: 650, Top: 1.0, Bottom: 0.45, LineHeight: 1.25},
			{Size: 1.5, Weight: 650, Top: 1.05, Bottom: 0.4, LineHeight: 1.3},
			{Size: 1.25, Weight: 600, Top: 0.95, Bottom: 0.35, LineHeight: 1.35},
		},
	}
}

// sidebarHandle is the edge between the sidebar and the panels: dragging it
// sets the sidebar's width, a double click puts it back. It shows a line
// only under the pointer.
func (a *App) sidebarHandle(c *ui.Context) {
	p := a.pal()
	h := ui.Box(c.Key("sidebar-edge")).Width(10).Shrink(0).FillHeight().Cursor(ui.CursorResizeEW).Label(tr.T("Lebar sidebar"))
	active := h.Hovered() || h.Pressed()
	h.Draw(func(pt *ui.Painter, r ui.Rect) {
		if active {
			pt.Fill(ui.Rect{X: r.X + r.W/2 - 1, Y: r.Y + 8, W: 2, H: r.H - 16}, p.Accent.Alpha(0.7), 1)
		}
	})
	if dx, _, ok := h.Dragged(); ok {
		a.set.SidebarWidth = min(max(a.set.SidebarWidth+dx, 200), 480)
	}
	if _, _, ended := h.DragEnded(); ended {
		a.saveSettings()
	}
	if h.DoubleClicked() {
		a.set.SidebarWidth = 270
		a.saveSettings()
	}
}

// themeButton opens the theme picker: light, dark or the system's, and
// the color behind the cards.
func (a *App) themeButton(c *ui.Context) {
	p := a.pal()
	s := map[string]*ui.SVG{"system": iconAuto, "light": iconSun, "dark": iconMoon}[a.set.Theme]
	if s == nil {
		s = iconAuto
	}
	b := ui.Box(c.Key("theme")).Padding(5).Radius(6).Cursor(ui.CursorPointer).Label(tr.T("Tema")).Tooltip(tr.T("Tema (Ctrl+Shift+L ganti mode)"))
	if b.Hovered() || a.themeOpen {
		b.Background(p.Hover)
	}
	b.Children(func() { ui.Icon(c, s).Size(16, 16).TextColor(p.Muted) })
	if b.Clicked() {
		a.themeOpen = !a.themeOpen
	}
	ui.Popover(c, b, &a.themeOpen, func() {
		ui.Column(c).Width(268).Padding(6).Gap(14).Children(func() {
			a.appearance(c)
			more := ui.Text(c.Key("more-settings"), tr.T("Pengaturan lainnya…")).FontSize(12).TextColor(p.Muted).Cursor(ui.CursorPointer)
			if more.Clicked() {
				a.themeOpen = false
				a.openSettings("tampilan")
			}
		})
	})
}

// thinScrollbar is a quiet scroll bar: a hairline thumb, no track, that
// thickens a little under the pointer.
func (a *App) thinScrollbar() ui.ScrollbarStyle {
	p := a.pal()
	return ui.ScrollbarStyle{TrackWidth: 8, ThumbWidth: 3, Radius: 2, MinLength: 28,
		Thumb: p.Faint.Alpha(0.28), Hover: p.Faint.Alpha(0.5), Active: p.Faint.Alpha(0.65)}
}

// hasPages tells that the workspace shows a page or a database, not
// counting the library and the trash.
func (a *App) hasPages() bool {
	for id := range a.nodes {
		if id != libraryID && id != trashID {
			return true
		}
	}
	return len(a.databases) > 0
}

// editorHeight is the height of a page's text plus room below it to click
// into; the extra room also keeps the editor taller than its text so wheel
// events always scroll the page around it rather than the editor itself.
func editorHeight(d *richtext.Document) float32 {
	h, ok := d.ContentHeight()
	if !ok {
		h = float32(d.Text().ParagraphCount()) * 100
	}
	return max(h+96, 240)
}
