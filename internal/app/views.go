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
	ui.Column(c).Fill().Center().Background(p.Window).Children(func() {
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
	ui.Column(c).Width(260).Shrink(0).Padding(12, 10, 10, 10).Gap(4).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(4).Padding(2, 6, 10, 8).Children(func() {
			ui.Text(c, "gotion").FontSize(15).FontWeight(600).Grow(1)
			if a.loading {
				ui.Spinner(c).Size(14, 14)
			}
			if iconButton(c, p, iconRefresh, "Muat ulang daftar halaman") {
				a.refresh()
			}
		})
		ui.Row(c).AlignItems(ui.Center).Gap(6).Padding(6, 10).Margin(0, 0, 8, 0).Background(p.Hover).Radius(8).Children(func() {
			ui.Icon(c, iconSearch).Size(14, 14).TextColor(p.Faint)
			ui.TextInput(c, &a.query).Placeholder("Cari halaman").Grow(1).Label("Cari").
				Background(ui.Color{}).Border(0, ui.Color{}).Padding(0).FontSize(13)
		})
		ui.Scroll(c).Grow(1).Children(func() {
			ui.Column(c).Gap(2).Children(func() {
				if a.loadErr != "" {
					ui.Text(c, "Gagal memuat: "+a.loadErr).FontSize(12).TextColor(c.Theme().Danger).Padding(4, 10)
				}
				q := strings.ToLower(strings.TrimSpace(a.query))
				for _, g := range a.groups {
					a.group(c, g, q)
				}
				if len(a.databases) > 0 {
					a.groupHeader(c, "Database", len(a.databases), "db")
					if !a.closed["db"] {
						for _, n := range a.databases {
							if q == "" || strings.Contains(strings.ToLower(n.Title), q) {
								a.item(c, n, 0)
							}
						}
					}
				}
			})
		})
		ui.Row(c).AlignItems(ui.Center).Gap(2).Padding(6, 4, 0, 4).Children(func() {
			if iconButton(c, p, iconMoon, "Mode gelap") {
				a.dark = !a.dark
			}
			if iconButton(c, p, iconLogout, "Keluar (hapus token)") {
				a.signOut()
			}
			ui.Box(c).Grow(1)
			if a.bot != nil && a.bot.Bot != nil {
				ui.Text(c, a.bot.Bot.WorkspaceName).FontSize(12).TextColor(p.Faint).SingleLine().MaxWidth(150)
			}
		})
	})
}

func (a *App) groupHeader(c *ui.Context, title string, count int, key string) {
	p := a.pal()
	h := ui.Row(c.Key("g"+key)).AlignItems(ui.Center).Gap(6).Padding(12, 10, 4, 10).Cursor(ui.CursorPointer).Children(func() {
		ui.Text(c, title).FontSize(13).FontWeight(600).TextColor(p.Muted).SingleLine()
		if count > 0 {
			ui.Text(c, fmt.Sprint(count)).FontSize(13).FontWeight(500).TextColor(p.Faint)
		}
		ui.Box(c).Grow(1)
		if key == a.groupOf(a.selected) && a.session != nil {
			if st, _ := a.session.Status(); st == syncer.Failed {
				ui.Text(c, "1 gagal").FontSize(12).FontWeight(600).TextColor(p.Warn)
			}
		}
	})
	if h.Clicked() {
		a.closed[key] = !a.closed[key]
	}
}

// groupOf returns the root page of a page.
func (a *App) groupOf(id string) string {
	for n := a.nodes[id]; n != nil; n = a.nodes[n.ParentID] {
		if a.nodes[n.ParentID] == nil || a.nodes[n.ParentID].Kind != "page" {
			return n.ID
		}
	}
	return ""
}

func (a *App) group(c *ui.Context, g *node, q string) {
	var rows []struct {
		n     *node
		depth int
	}
	var walk func(n *node, depth int)
	walk = func(n *node, depth int) {
		if q == "" || strings.Contains(strings.ToLower(n.Title), q) {
			rows = append(rows, struct {
				n     *node
				depth int
			}{n, depth})
		}
		for _, ch := range n.Children {
			walk(ch, depth+1)
		}
	}
	walk(g, 0)
	if len(rows) == 0 {
		return
	}
	a.groupHeader(c, g.Title, len(rows), g.ID)
	if a.closed[g.ID] {
		return
	}
	for _, r := range rows {
		a.item(c, r.n, min(r.depth, 4))
	}
}

func (a *App) item(c *ui.Context, n *node, depth int) {
	p := a.pal()
	selected := a.selected == n.ID
	row := ui.Row(c.Key(n.ID)).AlignItems(ui.Center).Gap(8).Padding(7, 10, 7, 10+float32(depth)*14).Radius(8).Cursor(ui.CursorPointer)
	if selected {
		row.Background(p.Panel).Shadow(0, 1, 2, 0, ui.RGBA(0, 0, 0, 0.08))
	} else if row.Hovered() {
		row.Background(p.Hover)
	}
	row.Children(func() {
		if n.Emoji != "" {
			ui.Text(c, n.Emoji).FontSize(13)
		}
		ui.Text(c, n.Title).FontSize(14).SingleLine().Grow(1).MinWidth(0)
		if n.Kind != "page" {
			ui.Text(c, "db").FontSize(11).FontWeight(600).TextColor(p.Muted).Padding(1, 6).Background(p.Hover).Radius(5)
			return
		}
		ui.Text(c, ago(n.Edited)).FontSize(12).TextColor(p.Faint)
		if selected && a.session != nil {
			st, _ := a.session.Status()
			statusIcon(c, p, st).Size(14, 14)
		}
	})
	if row.Clicked() {
		a.open(n)
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

// panelHeader is the bar over a panel.
func (a *App) panelHeader(c *ui.Context, s *ui.SVG, title, crumb, badge string, right func()) {
	p := a.pal()
	ui.Row(c).AlignItems(ui.Center).Gap(8).Padding(12, 16).BorderWidth(0, 0, 1, 0).BorderColor(p.Line).Children(func() {
		ui.Icon(c, s).Size(16, 16).TextColor(p.Muted)
		ui.Text(c, title).FontSize(14).FontWeight(600).SingleLine()
		if crumb != "" {
			ui.Text(c, crumb).FontSize(13).TextColor(p.Faint).SingleLine().MinWidth(0)
		}
		ui.Box(c).Grow(1)
		if right != nil {
			right()
		}
		ui.Text(c, badge).FontSize(11).FontWeight(600).TextColor(p.Muted).Padding(2, 8).Background(p.Sidebar).Radius(6)
	})
}

func (a *App) crumb(id string) string {
	var parts []string
	for n := a.nodes[a.nodes[id].parentOrEmpty()]; n != nil; n = a.nodes[n.ParentID] {
		parts = append([]string{n.Title}, parts...)
	}
	return strings.Join(parts, " / ")
}

func (n *node) parentOrEmpty() string {
	if n == nil {
		return ""
	}
	return n.ParentID
}

// pagePanel is the open page and its editor.
func (a *App) pagePanel(c *ui.Context) {
	p := a.pal()
	n := a.nodes[a.selected]
	title := "Halaman"
	if n != nil {
		title = n.Title
	}
	a.panelHeader(c, iconPage, title, a.crumb(a.selected), "page", func() {
		if a.session != nil {
			st, err := a.session.Status()
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
			ui.Text(c, label).FontSize(12).FontWeight(500).TextColor(color).SingleLine().MaxWidth(320)
		}
		if n != nil && iconButton(c, p, iconExt, "Buka di Notion") {
			go mygo.Shell.OpenExternal("https://www.notion.so/" + strings.ReplaceAll(n.ID, "-", ""))
		}
	})
	if a.opening != "" {
		ui.Column(c).Fill().Center().Gap(10).Children(func() {
			ui.Spinner(c).Size(22, 22)
			ui.Text(c, "Memuat halaman…").FontSize(13).TextColor(p.Faint)
		})
		return
	}
	if a.openErr != "" {
		ui.Column(c).Fill().Center().Children(func() {
			ui.Text(c, "Gagal membuka: "+a.openErr).FontSize(14).TextColor(c.Theme().Danger)
		})
		return
	}
	// The editor scrolls itself, so it fills the rest of the panel.
	// The text keeps a reading width, centered; the editor itself spans the
	// panel so its scroll bar sits at the panel's edge.
	pad := max(56, (a.panelWidth-readWidth)/2)
	col := ui.Column(c).Grow(1).MinHeight(0).Padding(28, 0, 0, 0).Gap(18)
	if b := col.LayoutBounds(); b.W > 0 {
		a.panelWidth = b.W
	}
	col.Children(func() {
		ui.Column(c).PaddingX(pad).Gap(18).Children(func() {
			ui.Text(c, title).FontSize(30).FontWeight(700).Selectable()
			a.extrasView(c)
		})
		e := richtext.Editor(c.Key("editor-"+a.selected), a.doc, richtext.EditorOptions{
			Theme:       &richtext.Theme{Padding: [2]float32{pad, 12}},
			Label:       "Isi halaman",
			Placeholder: "Mulai menulis…",
			OnLink:      a.followLink,
			Font:        ui.Font{Family: "Inter", Size: 15},
			Space:       func(int) float32 { return 6 },
		}).LineHeight(1.5).Grow(1).MinHeight(0).Background(p.Panel).Border(0, p.Panel).Radius(0)
		if e.Changed() && a.session != nil {
			a.session.Edited(a.doc.Text())
		}
	})
}

// extrasView lists what the editor does not edit.
func (a *App) extrasView(c *ui.Context) {
	if len(a.extras) == 0 {
		return
	}
	p := a.pal()
	ui.Column(c).Gap(6).Children(func() {
		for _, x := range a.extras {
			if x.Type != "nested" {
				continue // shown in place in the editor
			}
			row := ui.Row(c.Key("x"+x.ID)).AlignItems(ui.Center).Gap(10).Padding(10, 14).Radius(8).
				Border(1, p.Line).Cursor(ui.CursorPointer)
			row.Children(func() {
				switch x.Type {
				case "child_page":
					ui.Icon(c, iconPage).Size(16, 16).TextColor(p.Muted)
					ui.Text(c, nonEmpty(x.Title, "Tanpa judul")).FontSize(14).Underline().Grow(1)
				case "child_database":
					ui.Icon(c, iconTable).Size(16, 16).TextColor(p.Muted)
					ui.Text(c, nonEmpty(x.Title, "Database")).FontSize(14).Underline().Grow(1)
				case "nested":
					ui.Text(c, "Isi bertingkat di dalam “"+x.Title+"” terlalu dalam untuk diedit di sini").FontSize(13).TextColor(p.Muted).Grow(1)
				default:
					ui.Text(c, "Block "+strings.ReplaceAll(x.Type, "_", " ")+" belum didukung").FontSize(13).TextColor(p.Muted).Grow(1)
				}
				ui.Icon(c, iconExt).Size(14, 14).TextColor(p.Faint)
			})
			if row.Clicked() {
				if x.Type == "child_page" {
					if n := a.nodes[x.ID]; n != nil {
						a.open(n)
						continue
					}
					a.selected = x.ID
					a.nodes[x.ID] = &node{ID: x.ID, Title: nonEmpty(x.Title, "Tanpa judul"), Kind: "page", ParentID: a.selectedParent()}
					a.open(a.nodes[x.ID])
					continue
				}
				go mygo.Shell.OpenExternal("https://www.notion.so/" + strings.ReplaceAll(x.ID, "-", ""))
			}
		}
	})
}

func (a *App) selectedParent() string {
	if a.session != nil {
		return a.session.PageID
	}
	return ""
}

func nonEmpty(s, alt string) string {
	if strings.TrimSpace(s) == "" {
		return alt
	}
	return s
}

// readWidth is the width of the text of a page.
const readWidth = 760
