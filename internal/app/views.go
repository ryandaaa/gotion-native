package app

import (
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/richtext"

	"gotion/internal/syncer"
)

// readWidth is the width of the text of a page.
const readWidth = 760

// View builds the window.
func (a *App) View(c *ui.Context) {
	// Before SetTheme, the theme is the system's.
	sysDark := c.Theme().Dark
	a.dark = a.set.Theme == "dark" || a.set.Theme != "light" && sysDark
	p := a.pal()
	c.SetTheme(theme(p, a.dark))
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
	ui.Column(c).Fill().Background(p.Sidebar).Children(func() {
		a.titleBar(c, true)
		ui.Row(c).Grow(1).MinHeight(0).AlignItems(ui.Stretch).Children(func() {
			if a.set.SidebarOff {
				a.main(c)
				return
			}
			s := ui.Split(c, &a.set.SidebarWidth, func() { a.sidebar(c) }, func() { a.main(c) }).Fill()
			if s.Changed() {
				a.saveSettings()
			}
		})
	})
	if a.switcher.open {
		a.switcherView(c)
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
		a.closeSecond()
	case c.Shortcut(ui.Cmd|ui.Shift, ui.KeyL):
		a.cycleTheme()
	case c.Shortcut(ui.Cmd, ui.KeyR):
		a.refresh()
	}
}

func (a *App) cycleTheme() {
	next := map[string]string{"system": "light", "light": "dark", "dark": "system"}[a.set.Theme]
	if next == "" {
		next = "system"
	}
	a.set.Theme = next
	a.saveSettings()
	a.toast(map[string]string{"system": "Tema: ikut sistem", "light": "Tema: terang", "dark": "Tema: gelap"}[next])
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
		if iconButton(c, p, iconSidebar, "Sidebar (Ctrl+\\)") {
			a.set.SidebarOff = !a.set.SidebarOff
			a.saveSettings()
		}
		if navButton(c, p, iconBack, "Kembali (Ctrl+[)", a.hpos > 0) {
			a.back()
		}
		if navButton(c, p, iconForward, "Maju (Ctrl+])", a.hpos < len(a.history)-1) {
			a.forward()
		}
		a.breadcrumbs(c)
		ui.Box(c).Grow(1)
		search := ui.Row(c.Key("search")).AlignItems(ui.Center).Gap(8).Padding(5, 12).Radius(8).Background(p.Hover).
			Cursor(ui.CursorPointer).Width(260).Children(func() {
			ui.Icon(c, iconSearch).Size(14, 14).TextColor(p.Faint)
			ui.Text(c, "Cari halaman…").FontSize(13).TextColor(p.Faint).Grow(1)
			ui.Text(c, "Ctrl K").FontSize(11).TextColor(p.Faint)
		})
		if search.Clicked() {
			a.openSwitcher()
		}
		ui.Box(c).Grow(1)
		if a.loading {
			ui.Spinner(c).Size(14, 14)
		}
		if iconButton(c, p, iconRefresh, "Muat ulang (Ctrl+R)") {
			a.refresh()
		}
		themeIcon := map[string]*ui.SVG{"system": iconAuto, "light": iconSun, "dark": iconMoon}[a.set.Theme]
		if themeIcon == nil {
			themeIcon = iconAuto
		}
		if iconButton(c, p, themeIcon, "Ganti tema (Ctrl+Shift+L)") {
			a.cycleTheme()
		}
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
					ui.Text(c, n.Emoji).FontSize(13)
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

// onboarding asks for the integration token.
func (a *App) onboarding(c *ui.Context) {
	p := a.pal()
	step := func(n string, body func()) {
		ui.Row(c).Gap(14).AlignItems(ui.Start).Children(func() {
			ui.Box(c).Size(24, 24).Shrink(0).Radius(12).Background(p.Text).Center().Children(func() {
				ui.Text(c, n).FontSize(12).FontWeight(600).TextColor(p.Panel)
			})
			ui.Column(c).Grow(1).Gap(8).Children(body)
		})
	}
	ui.Column(c).Grow(1).Center().Background(p.Window).Children(func() {
		ui.Column(c).Width(520).Padding(32, 36).Gap(22).Background(p.Panel).Radius(14).
			Shadow(0, 8, 30, 0, ui.RGBA(0, 0, 0, 0.10)).Children(func() {
			ui.Column(c).Gap(6).Children(func() {
				ui.Text(c, "Hubungkan ke Notion").FontSize(22).FontWeight(700)
				ui.Text(c, "Setup sekali, sekitar 2 menit. Token disimpan di keychain sistem.").FontSize(14).TextColor(p.Muted)
			})
			step("1", func() {
				ui.Text(c, "Buat integrasi baru di Notion").FontSize(14)
				if ui.Link(c, "Buka halaman integrasi ↗", "").FontWeight(500).Clicked() {
					go mygo.Shell.OpenExternal("https://www.notion.so/profile/integrations")
				}
			})
			step("2", func() {
				ui.Text(c, "Tempel Internal Integration Secret").FontSize(14)
				ui.Row(c).Gap(8).Children(func() {
					in := ui.TextInput(c, &a.tokenInput).Password().Placeholder("ntn_…").Grow(1).Label("Token").AutoFocus()
					if (in.Submitted() || ui.Button(c, "Cek").Disabled(a.checking).Clicked()) && !a.checking {
						a.checkToken()
					}
				})
				switch {
				case a.checking:
					ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
						ui.Spinner(c).Size(14, 14)
						ui.Text(c, "Mengecek…").FontSize(13).TextColor(p.Muted)
					})
				case a.tokenErr != "":
					ui.Text(c, a.tokenErr).FontSize(13).TextColor(c.Theme().Danger)
				}
			})
			step("3", func() {
				ui.Text(c, "Di Notion, buka page yang mau dipakai → ••• → Connections → pilih integrasi ini. Sub-page ikut otomatis.").
					FontSize(14).TextColor(p.Muted)
			})
		})
	})
}

// sidebar is the page tree.
func (a *App) sidebar(c *ui.Context) {
	p := a.pal()
	ui.Column(c).Fill().Padding(4, 8, 10, 10).Gap(2).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(6).Padding(6, 8, 6, 10).Children(func() {
			ui.Text(c, "Halaman").FontSize(12).FontWeight(600).TextColor(p.Faint).Grow(1)
			if iconButton(c, p, iconPlus, "Halaman baru di halaman ini (Ctrl+N)") {
				if pn := a.panels[a.focus]; pn != nil {
					a.newPage(pn.id)
				}
			}
		})
		ui.Scroll(c).Grow(1).Children(func() {
			ui.Column(c).Gap(1).Children(func() {
				if a.loadErr != "" {
					ui.Text(c, "Gagal memuat: "+a.loadErr).FontSize(12).TextColor(c.Theme().Danger).Padding(4, 10)
				}
				if len(a.nodes) == 0 && !a.loading {
					ui.Text(c, "Belum ada halaman yang terhubung. Di Notion: ••• → Connections → pilih integrasi kamu.").
						FontSize(12).TextColor(p.Faint).Padding(4, 10)
				}
				for _, g := range a.groups {
					a.groupHeader(c, g)
					if !a.set.Closed[g.ID] {
						a.tree(c, g, 0)
					}
				}
				if len(a.databases) > 0 {
					a.sectionHeader(c, "Database", len(a.databases), "db")
					if !a.set.Closed["db"] {
						for _, n := range a.databases {
							a.tree(c, n, 0)
						}
					}
				}
			})
		})
		ui.Row(c).AlignItems(ui.Center).Gap(2).Padding(6, 4, 0, 4).Children(func() {
			if iconButton(c, p, iconLogout, "Keluar (hapus token)") {
				a.signOut()
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

// groupHeader heads the pages under a top page, with how many there are.
func (a *App) groupHeader(c *ui.Context, g *node) {
	p := a.pal()
	count := a.count(g)
	h := ui.Row(c.Key("g"+g.ID)).AlignItems(ui.Center).Gap(6).Padding(14, 10, 4, 10).Cursor(ui.CursorPointer).Children(func() {
		ui.Text(c, g.Title).FontSize(12).FontWeight(600).TextColor(p.Faint).SingleLine().MinWidth(0)
		ui.Text(c, fmt.Sprint(count)).FontSize(12).TextColor(p.Faint)
		ui.Box(c).Grow(1)
		for _, pn := range a.panels {
			if pn != nil && pn.session != nil && a.rootOf(pn.id) == g.ID {
				if st, _ := pn.session.Status(); st == syncer.Failed || st == syncer.Offline {
					ui.Text(c, st.String()).FontSize(11).FontWeight(600).TextColor(p.Warn)
				}
			}
		}
	})
	if h.Clicked() {
		a.set.Closed[g.ID] = !a.set.Closed[g.ID]
		a.saveSettings()
	}
}

func (a *App) count(n *node) int {
	total := 1
	for _, ch := range n.Children {
		total += a.count(ch)
	}
	return total
}

func (a *App) rootOf(id string) string {
	path := a.path(id)
	if len(path) > 0 {
		return path[0].ID
	}
	return id
}

// tree shows n and, when expanded, the pages under it.
func (a *App) tree(c *ui.Context, n *node, depth int) {
	a.item(c, n, depth)
	if a.set.Open[n.ID] {
		for _, ch := range n.Children {
			a.tree(c, ch, depth+1)
		}
	}
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
				ui.Icon(c, s).Size(12, 12).TextColor(p.Faint)
			})
			if arrow.Hovered() {
				arrow.Background(p.Line)
			}
			toggled = arrow.Clicked()
		}
		switch {
		case n.Emoji != "":
			ui.Text(c, n.Emoji).FontSize(13)
		case n.Kind != "page":
			ui.Icon(c, iconTable).Size(14, 14).TextColor(p.Faint)
		default:
			ui.Icon(c, iconPage).Size(14, 14).TextColor(p.Faint)
		}
		ui.Text(c, n.Title).FontSize(14).SingleLine().Grow(1).MinWidth(0)
		if hovered {
			if n.Kind == "page" {
				added = smallButton(c, p, iconPlus, "Halaman baru di dalamnya")
			}
			beside = smallButton(c, p, iconSplit, "Buka di samping")
			return
		}
		if n.Kind != "page" {
			ui.Text(c, "db").FontSize(11).FontWeight(600).TextColor(p.Muted).Padding(1, 6).Background(p.Hover).Radius(5)
			return
		}
		ui.Text(c, ago(n.Edited)).FontSize(12).TextColor(p.Faint)
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
	color := p.Line
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
func (a *App) main(c *ui.Context) {
	ui.Row(c).Grow(1).MinWidth(0).AlignItems(ui.Stretch).Padding(0, 10, 10, 0).Children(func() {
		if a.panels[1] == nil {
			a.panelView(c, 0)
			return
		}
		if a.set.SplitSize < 300 {
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
	if a.panels[1] != nil && a.focus == i {
		card.Border(1, p.EditLine)
	}
	if card.PressedInside() {
		a.focus = i
	}
	card.Children(func() {
		switch {
		case pn == nil || pn.id == "":
			a.empty(c)
		case pn.table != nil:
			a.tablePanel(c, i, pn)
		default:
			a.pagePanel(c, i, pn)
		}
	})
}

func (a *App) empty(c *ui.Context) {
	p := a.pal()
	ui.Column(c).Fill().Center().Gap(10).Children(func() {
		ui.Icon(c, iconPage).Size(28, 28).TextColor(p.Faint)
		ui.Text(c, "Pilih halaman di sidebar").FontSize(15).TextColor(p.Muted)
		ui.Text(c, "Ctrl+K cari · Ctrl+N halaman baru · Ctrl+\\ sidebar · Ctrl+klik buka di samping").FontSize(12).TextColor(p.Faint)
	})
}

// panelHeader is the bar over a panel.
func (a *App) panelHeader(c *ui.Context, i int, s *ui.SVG, title, badge string, right func()) {
	p := a.pal()
	ui.Row(c).AlignItems(ui.Center).Gap(8).Padding(10, 12, 10, 16).BorderWidth(0, 0, 1, 0).BorderColor(p.Line).Children(func() {
		ui.Icon(c, s).Size(16, 16).TextColor(p.Muted)
		ui.Text(c, title).FontSize(14).FontWeight(600).SingleLine().MinWidth(0).Shrink(1)
		ui.Box(c).Grow(1)
		if right != nil {
			right()
		}
		ui.Text(c, badge).FontSize(11).FontWeight(600).TextColor(p.Muted).Padding(2, 8).Background(p.Sidebar).Radius(6)
		if a.panels[1] == nil {
			if iconButton(c, p, iconSplit, "Belah panel") {
				if pn := a.panels[i]; pn != nil {
					if n := a.nodes[pn.id]; n != nil {
						a.openBeside(n)
					}
				}
			}
		} else if iconButton(c, p, iconClose, "Tutup panel (Ctrl+W)") {
			if i == 1 {
				a.closeSecond()
			} else {
				a.panels[0], a.panels[1] = a.panels[1], nil
				a.set.Last, a.set.Second = a.panels[0].id, ""
				a.focus = 0
				a.saveSettings()
			}
		}
	})
}

// pagePanel is an open page and its editor.
func (a *App) pagePanel(c *ui.Context, i int, pn *panel) {
	p := a.pal()
	n := a.nodes[pn.id]
	title := "Halaman"
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
				color, label = p.OK, "✓ Tersimpan"
			case syncer.Failed, syncer.Offline:
				color = p.Warn
				if err != nil {
					label += ": " + errText(err)
				}
			}
			ui.Text(c, label).FontSize(12).FontWeight(500).TextColor(color).SingleLine().MaxWidth(280)
		}
		if iconButton(c, p, iconWide, "Lebar penuh") {
			a.set.FullWidth = !a.set.FullWidth
			a.saveSettings()
		}
		if iconButton(c, p, iconExt, "Buka di Notion") {
			go mygo.Shell.OpenExternal("https://www.notion.so/" + strings.ReplaceAll(pn.id, "-", ""))
		}
	})
	if pn.opening {
		ui.Column(c).Fill().Center().Gap(10).Children(func() {
			ui.Spinner(c).Size(22, 22)
			ui.Text(c, "Memuat halaman…").FontSize(13).TextColor(p.Faint)
		})
		return
	}
	if pn.openErr != "" {
		ui.Column(c).Fill().Center().Gap(10).Children(func() {
			ui.Text(c, "Gagal membuka: "+pn.openErr).FontSize(14).TextColor(c.Theme().Danger)
			if ui.Button(c, "Coba lagi").Clicked() {
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
	col := ui.Column(c).Grow(1).MinHeight(0).Padding(24, 0, 0, 0).Gap(10)
	if b := col.LayoutBounds(); b.W > 0 {
		pn.width, a.widths[i] = b.W, b.W
		if padding(b.W, a.set.FullWidth) != pad {
			// Drawn with a stale width: draw again now, not at the next
			// mouse move.
			c.After(0)
		}
	}
	col.Children(func() {
		ui.Row(c).PaddingX(pad).AlignItems(ui.Center).Gap(10).Children(func() {
			if n != nil && n.Emoji != "" {
				ui.Text(c, n.Emoji).FontSize(30)
			}
			in := ui.TextInput(c.Key("title"+pn.id), &pn.title).Placeholder("Tanpa judul").FontSize(30).FontWeight(700).
				Grow(1).Background(ui.Color{}).Border(0, ui.Color{}).Padding(0).FocusRing(false).Label("Judul halaman")
			if in.Changed() {
				pn.titleDirty, pn.titleEdit = true, time.Now()
				c.After(1600 * time.Millisecond)
			}
			if pn.titleDirty && (in.Submitted() || !in.Focused() || time.Since(pn.titleEdit) > 1500*time.Millisecond) {
				a.sendTitle(pn)
			}
		})
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
			ui.Text(c, "Isi bertingkat di dalam “"+x.Title+"” terlalu dalam untuk diedit di sini").FontSize(13).TextColor(p.Muted).Grow(1)
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
	// Clip: what the editor draws itself (tables, images) must not show
	// over the title when scrolled up past it.
	ui.Box(c).Grow(1).MinHeight(0).Clip().Children(func() {
		e := richtext.Editor(c.Key("editor-"+pn.id), pn.doc, richtext.EditorOptions{
			Theme:       &richtext.Theme{Padding: [2]float32{pad, 12}},
			Label:       "Isi halaman",
			Placeholder: "Ketik / untuk perintah…",
			OnLink:      a.followLink(i),
			OnKey:       func(k ui.Key, m ui.Modifiers) bool { return a.slashKey(pn, k, m) },
			Font:        ui.Font{Family: "Inter", Size: 15},
			Space:       func(int) float32 { return 6 },
		}).LineHeight(1.5).Fill().Background(p.Panel).Border(0, p.Panel).Radius(0)
		if e.Focused() {
			a.focus = i
		}
		if e.Changed() {
			if pn.session != nil {
				pn.session.Edited(pn.doc.Text())
			}
			a.slashUpdate(pn)
		}
		a.slashView(c, pn)
		if e.Focused() && !pn.slash.open {
			a.formatBar(c, pn)
		}
	})
}

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
