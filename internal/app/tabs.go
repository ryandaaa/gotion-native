package app

import (
	"gotion/internal/tr"
	"strings"

	"github.com/egoist/mygo/ui"

	"gotion/internal/emoji"
	"gotion/internal/syncer"
)

// tabIndex is where p is among panel i's tabs, -1 when it is not.
func (a *App) tabIndex(i int, p *panel) int {
	for k, t := range a.tabs[i] {
		if t == p {
			return k
		}
	}
	return -1
}

// activate shows tab t of panel i. A tab kept from the last run loads
// when it first shows.
func (a *App) activate(i int, t *panel) {
	a.panels[i], a.focus = t, i
	if t.id != "" && t.doc == nil && t.table == nil && !t.opening && t.openErr == "" && !isSpecial(a.kindOf(t.id)) {
		if n := a.nodes[t.id]; n != nil {
			a.load(t, n)
		}
	}
	a.saveTabs()
}

// closeTab closes tab k of panel i, sending what it has not sent. The tab
// beside it shows; the second panel goes with its last tab.
func (a *App) closeTab(i, k int) {
	if k < 0 || k >= len(a.tabs[i]) {
		return
	}
	t := a.tabs[i][k]
	a.flushPanel(t)
	if t.session != nil {
		go t.session.Close()
	}
	a.tabs[i] = append(a.tabs[i][:k], a.tabs[i][k+1:]...)
	if a.panels[i] != t {
		a.saveTabs()
		return
	}
	switch {
	case len(a.tabs[i]) > 0:
		a.activate(i, a.tabs[i][min(k, len(a.tabs[i])-1)])
	case i == 1:
		a.closeSecond()
	case a.panels[1] != nil:
		// The first panel is empty: the second takes its place.
		a.panels[0], a.tabs[0] = a.panels[1], a.tabs[1]
		a.panels[1], a.tabs[1] = nil, nil
		a.focus = 0
		a.saveTabs()
	default:
		a.panels[0] = &panel{}
		a.saveTabs()
	}
}

// cycleTab shows the next tab of the focused panel, or the one before.
func (a *App) cycleTab(dir int) {
	i := a.focus
	n := len(a.tabs[i])
	if n < 2 {
		return
	}
	k := (a.tabIndex(i, a.panels[i]) + dir + n) % n
	a.activate(i, a.tabs[i][k])
}

// saveTabs keeps the panels' tabs for the next run.
func (a *App) saveTabs() {
	for i := range 2 {
		a.set.Tabs[i] = nil
		for _, t := range a.tabs[i] {
			if t.id != "" {
				a.set.Tabs[i] = append(a.set.Tabs[i], t.id)
			}
		}
		a.set.Active[i] = max(0, a.tabIndex(i, a.panels[i]))
	}
	if p := a.panels[0]; p != nil {
		a.set.Last = p.id
	}
	a.set.Second = ""
	if p := a.panels[1]; p != nil {
		a.set.Second = p.id
	}
	a.saveSettings()
}

// restoreTabs puts back the last run's tabs, loading only those showing.
func (a *App) restoreTabs() {
	ids := a.set.Tabs
	if len(ids[0]) == 0 && a.set.Last != "" {
		// From before tabs: the page each panel showed.
		ids[0] = []string{a.set.Last}
		if a.set.Second != "" {
			ids[1] = []string{a.set.Second}
		}
	}
	for i := range 2 {
		for _, id := range ids[i] {
			if a.nodes[id] != nil || a.special(id) != nil {
				a.tabs[i] = append(a.tabs[i], &panel{id: id})
			}
		}
		if len(a.tabs[i]) == 0 {
			continue
		}
		k := min(max(a.set.Active[i], 0), len(a.tabs[i])-1)
		a.activate(i, a.tabs[i][k])
	}
	if a.panels[0] == nil || a.panels[0].id == "" && a.panels[1] != nil {
		if a.panels[1] != nil && len(a.tabs[0]) == 0 {
			a.panels[0], a.tabs[0], a.panels[1], a.tabs[1] = a.panels[1], a.tabs[1], nil, nil
		}
	}
	if a.panels[0] == nil {
		a.panels[0] = &panel{}
	}
	if a.panels[1] != nil {
		a.guessSplit()
	}
}

// tabStrip is panel i's tabs, as an editor's: the active one raised to
// the card, a dot for one with edits not sent, × to close.
func (a *App) tabStrip(c *ui.Context, i int) {
	p := a.pal()
	ui.ScrollHorizontal(c).Grow(1).MinWidth(0).Scrollbars(false).Children(func() {
		ui.Row(c).AlignItems(ui.End).Gap(2).Children(func() {
			tabs := a.tabs[i]
			if cur := a.panels[i]; cur != nil && cur.id != "" && a.tabIndex(i, cur) < 0 {
				tabs = append(tabs, cur) // shown, though not among the tabs
			}
			for k, t := range tabs {
				a.tab(c, p, i, k, t)
			}
		})
	})
}

func (a *App) tab(c *ui.Context, p palette, i, k int, t *panel) {
	n := a.nodes[t.id]
	title, emojiText, kind := tr.T("Memuat…"), "", "page"
	if n != nil {
		title, emojiText, kind = n.Title, n.Emoji, n.Kind
	}
	active := a.panels[i] == t
	tab := ui.Row(c.Key("tab"+t.id)).AlignItems(ui.Center).Gap(7).Padding(7, 6, 7, 12).Radius(8).
		MaxWidth(220).Shrink(0).Cursor(ui.CursorPointer).Tooltip(title)
	hovered := tab.Hovered()
	switch {
	case active:
		tab.Background(p.Panel).Shadow(0, 1, 2, 0, ui.RGBA(0, 0, 0, 0.08))
	case hovered:
		tab.Background(p.Hover)
	}
	var closed bool
	tab.Children(func() {
		switch {
		case emojiText != "":
			emoji.View(c, emojiText, 14)
		case kind == "library":
			ui.Icon(c, iconLibrary).Size(14, 14).TextColor(p.Faint)
		case kind == "trash":
			ui.Icon(c, iconTrash).Size(14, 14).TextColor(p.Faint)
		case kind != "page":
			ui.Icon(c, iconTable).Size(14, 14).TextColor(p.Faint)
		default:
			ui.Icon(c, iconPage).Size(14, 14).TextColor(p.Faint)
		}
		label := ui.Text(c, title).FontSize(13).SingleLine().MinWidth(0).Shrink(1)
		if active {
			label.FontWeight(600)
		} else {
			label.TextColor(p.Muted)
		}
		pending := false
		if t.session != nil {
			st, _ := t.session.Status()
			pending = st == syncer.Pending || st == syncer.Syncing
		}
		x := ui.Box(c.Key("x")).Size(18, 18).Radius(4).Center().Label(tr.T("Tutup tab")).Tooltip(tr.T("Tutup (Ctrl+W)"))
		if x.Hovered() {
			x.Background(p.Line)
		}
		x.Children(func() {
			switch {
			case x.Hovered() || (hovered || active) && !pending:
				ui.Icon(c, iconClose).Size(12, 12).TextColor(p.Muted)
			case pending:
				ui.Icon(c, iconDot).Size(10, 10).TextColor(p.Text)
			}
		})
		closed = x.Clicked()
	})
	switch {
	case closed:
		a.closeTab(i, k)
	case tab.Clicked():
		a.activate(i, t)
	}
}

func (a *App) kindOf(id string) string {
	if n := a.nodes[id]; n != nil {
		return n.Kind
	}
	return ""
}

// byTitleThenID orders pages by title, then by ID: never two the same, so
// pages listed from a map come out the same way every frame.
func byTitleThenID(x, y *node) bool {
	tx, ty := strings.ToLower(x.Title), strings.ToLower(y.Title)
	if tx != ty {
		return tx < ty
	}
	return x.ID < y.ID
}

// byEditedThenTitle orders pages by when they were edited, the latest
// first; pages edited at once (Notion keeps the minute) by title and ID.
func byEditedThenTitle(x, y *node) bool {
	if !x.Edited.Equal(y.Edited) {
		return x.Edited.After(y.Edited)
	}
	return byTitleThenID(x, y)
}
