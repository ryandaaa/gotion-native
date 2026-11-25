package app

import (
	"context"
	"errors"
	"fmt"
	"gotion/internal/tr"
	"sort"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"gotion/internal/emoji"
	"gotion/internal/notion"
)

// tableView is a data source shown read-only, as a table or as cards.
type tableView struct {
	id             string
	dsID           string // the data source, where new rows go
	dbID           string // the database container, when known
	title          string
	titleSent      string
	titleInit      bool
	titleEdit      time.Time
	titleDirty     bool
	edit           cellEdit
	adding         newRow
	query          tableQuery
	related        map[string][]notion.Linked // the pages of relations' data sources
	schema         schemaEdit
	templates      []notion.Template
	templatesAsked bool
	columns        []notion.Property
	rows           []*notion.Page
	boardMoved     map[string]int // frame countdown for smooth card entrance
	cal            calendarState
	selectedID     string
	previewID      string
	previewBlocks  []*notion.Block
	previewLoading bool
	previewErr     string
	loading        bool
	err            string
}

func (a *App) openTable(pn *panel, n *node) {
	initTitle := n.Title
	if initTitle == tr.T("Memuat…") || initTitle == tr.T("Tanpa judul") || initTitle == tr.T("Database tanpa judul") {
		initTitle = ""
	}
	t := &tableView{id: n.ID, title: initTitle, titleSent: initTitle, titleInit: true, loading: true}
	if n.Kind == "database" {
		t.dbID = n.ID
	}
	pn.table = t
	client := a.client
	go func() {
		ctx := context.Background()
		dsID := n.ID
		var err error
		var db *notion.Database
		if n.Kind == "database" {
			// A database block: its table is its first data source.
			if db, err = client.Database(ctx, n.ID); err == nil {
				if len(db.DataSources) == 0 {
					err = errors.New("database ini tidak punya data source")
				} else {
					dsID = db.DataSources[0].ID
				}
			}
		}
		var ds *notion.DataSource
		if err == nil {
			ds, err = client.DataSource(ctx, dsID)
		}
		var rows []*notion.Page
		if err == nil {
			rows, err = client.Query(ctx, dsID, 500)
		}
		a.update(func() {
			t.loading = false
			if err != nil {
				t.err = errText(err)
				return
			}
			for name, p := range ds.Properties {
				if p.Name == "" {
					p.Name = name
				}
				if p.Type == "" {
					continue
				}
				t.columns = append(t.columns, p)
			}
			if len(t.columns) == 0 {
				t.columns = []notion.Property{{Name: "Name", Type: "title"}}
			}
			// The title first, then by name.
			sort.Slice(t.columns, func(i, j int) bool {
				ci, cj := t.columns[i], t.columns[j]
				if (ci.Type == "title") != (cj.Type == "title") {
					return ci.Type == "title"
				}
				return ci.Name < cj.Name
			})
			t.rows, t.dsID = rows, dsID
			if t.dbID == "" && ds.Parent.Type == "database_id" {
				t.dbID = ds.Parent.DatabaseID
			}
			title := notion.PlainText(ds.Title)
			if title == "" && db != nil {
				title = notion.PlainText(db.Title)
			}
			if !t.titleDirty {
				t.title, t.titleSent = title, title
				if n := a.nodes[t.id]; n != nil {
					n.Title = nonEmpty(title, tr.T("Database tanpa judul"))
					a.regroup()
				}
			}
		})
	}()
}

// chipColors are Notion's option colors: background and text, light then dark.
var chipColors = map[string][2][2]string{
	"default": {{"#EEEEEC", "#37352F"}, {"#373737", "#D4D4D4"}},
	"gray":    {{"#E3E2E0", "#32302C"}, {"#454545", "#D4D4D4"}},
	"brown":   {{"#EEE0DA", "#442A1E"}, {"#4C3D35", "#E6D4C9"}},
	"orange":  {{"#FADEC9", "#49290E"}, {"#5C3B23", "#F5D2B5"}},
	"yellow":  {{"#FDECC8", "#402C1B"}, {"#564328", "#F5E4B5"}},
	"green":   {{"#DBEDDB", "#1C3829"}, {"#2B4A3A", "#C8E6D2"}},
	"blue":    {{"#D3E5EF", "#183347"}, {"#28456C", "#C9DDF5"}},
	"purple":  {{"#E8DEEE", "#412454"}, {"#492F64", "#E2D2F2"}},
	"pink":    {{"#F5E0E9", "#4C2337"}, {"#69314C", "#F2CFE0"}},
	"red":     {{"#FFE2DD", "#5D1715"}, {"#6E3630", "#F7CFCA"}},
}

func (a *App) chip(c *ui.Context, ch notion.Chip) {
	cs, ok := chipColors[ch.Color]
	if !ok {
		cs = chipColors["default"]
	}
	pair := cs[0]
	if a.dark {
		pair = cs[1]
	}
	ui.Text(c, ch.Name).FontSize(12).Padding(1, 7).Radius(4).Background(ui.Hex(pair[0])).TextColor(ui.Hex(pair[1])).SingleLine()
}

// value shows a property of a row: tags, a check box, or text.
func (a *App) value(c *ui.Context, raw []byte, typ string) {
	p := a.pal()
	if chips := notion.Chips(raw); chips != nil {
		ui.Row(c).Gap(4).MinWidth(0).Clip().Children(func() {
			for _, ch := range chips {
				a.chip(c, ch)
			}
		})
		return
	}
	text := notion.FormatProperty(raw)
	if typ == "relation" {
		// Notion gives a relation's pages by ID: name those the app knows.
		var names []string
		for _, l := range notion.Links(raw) {
			if n := a.nodes[l.ID]; n != nil {
				names = append(names, n.Title)
			} else if l.Name != "" {
				names = append(names, l.Name)
			}
		}
		if len(names) > 0 {
			text = strings.Join(names, ", ")
		}
	}
	switch typ {
	case "checkbox":
		box := ui.Box(c).Size(16, 16).Radius(4).Center()
		if text == "☑" {
			box.Background(p.Accent).Children(func() { ui.Icon(c, iconCheck).Size(12, 12).TextColor(p.Panel) })
		} else {
			box.Border(1.5, p.Faint)
		}
		return
	case "url", "email":
		ui.Text(c, text).FontSize(14).TextColor(p.Accent).SingleLine().MinWidth(0)
		return
	}
	ui.Text(c, text).FontSize(14).TextColor(p.Text).SingleLine().MinWidth(0)
}

// colWidth is how wide a column of a property type shows in the table.
func colWidth(p notion.Property) float32 {
	switch p.Type {
	case "title":
		return 300
	case "checkbox":
		return 90
	case "number", "unique_id":
		return 110
	case "date", "created_time", "last_edited_time":
		return 185
	}
	return 190
}

func (a *App) openRow(i int, t *tableView, r *notion.Page) {
	n := a.nodes[r.ID]
	if n == nil {
		n = &node{ID: r.ID, Title: nonEmpty(r.Title(), tr.T("Tanpa judul")), Emoji: r.Emoji(), Kind: "page", ParentID: t.id, Edited: r.LastEditedTime}
		a.nodes[r.ID] = n
	}
	if i < 0 {
		a.openBeside(n)
		return
	}
	a.open(n, i)
}

// hasValue reports whether a property of a row holds anything to show.
func hasValue(raw []byte) bool {
	if chips := notion.Chips(raw); chips != nil {
		return len(chips) > 0
	}
	return notion.FormatProperty(raw) != ""
}

// sendTableTitle sends table t's title to Notion when it changed.
func (a *App) sendTableTitle(t *tableView) {
	if t == nil || !t.titleDirty {
		return
	}
	title := strings.TrimSpace(t.title)
	t.titleDirty = false
	if title == t.titleSent {
		return
	}
	old := t.titleSent
	t.titleSent = title
	shown := nonEmpty(title, tr.T("Database tanpa judul"))
	if n := a.nodes[t.id]; n != nil {
		n.Title = shown
	}
	if t.dbID != "" {
		if n := a.nodes[t.dbID]; n != nil {
			n.Title = shown
		}
	}
	a.regroup()
	if a.client == nil {
		return
	}
	client, dsID, dbID := a.client, nonEmpty(t.dsID, t.id), t.dbID
	go func() {
		if err := client.SetDataSourceTitle(context.Background(), dsID, dbID, title); err != nil {
			a.update(func() {
				t.titleSent = old
				a.toast(tr.T("Judul gagal disimpan: ") + errText(err))
			})
		}
	}()
}

func (a *App) tablePanel(c *ui.Context, i int, pn *panel) {
	p := a.pal()
	t := pn.table
	if !t.titleInit {
		t.titleInit = true
		t.titleSent = t.title
	}
	if !t.titleDirty && t.title == "" && t.loading {
		if n := a.nodes[t.id]; n != nil && n.Title != tr.T("Memuat…") && n.Title != tr.T("Tanpa judul") && n.Title != tr.T("Database tanpa judul") {
			t.title, t.titleSent = n.Title, n.Title
		}
	}
	headTitle := nonEmpty(strings.TrimSpace(t.title), tr.T("Database tanpa judul"))
	view := a.set.Views[t.id]
	if view == "" {
		view = "table"
	}
	a.panelHeader(c, i, iconTable, headTitle, "database", func() {
		if t.loading || t.err != "" {
			return
		}
		if t.loading && t.dsID != "" {
			ui.Spinner(c).Size(12, 12)
		}
		views := []struct{ id, label string }{
			{"table", tr.T("Tabel")},
			{"cards", tr.T("Kartu")},
			{"board", tr.T("Board")},
			{"calendar", tr.T("Kalender")},
		}
		pick := func(id string) {
			prev := a.set.Views[t.id]
			a.set.Views[t.id] = id
			a.saveSettings()
			if prev == "calendar" && id != "calendar" && t.cal.queriedKey != "" {
				t.cal.queriedKey = ""
				a.requery(t)
			}
		}
		// A narrow panel (a split) has no room for all of it beside its
		// tabs: the count goes, and the views fold into one menu.
		narrow := pn.width > 0 && pn.width < 820
		if !narrow {
			ui.Text(c, fmt.Sprintf(tr.T("%d baris"), len(t.rows))).FontSize(12).TextColor(p.Faint).Shrink(0)
		}
		a.queryButtons(c, t, narrow)
		if narrow {
			labels := make([]string, len(views))
			current := views[0].label
			for k, v := range views {
				labels[k] = v.label
				if v.id == view {
					current = v.label
				}
			}
			if ui.Select(c.Key("view-menu"), &current, labels).Width(112).Shrink(0).Label(tr.T("Tampilan database")).Changed() {
				for _, v := range views {
					if v.label == current {
						pick(v.id)
					}
				}
			}
			return
		}
		ui.Row(c).Gap(2).Padding(2).Radius(7).Background(p.Sidebar).Shrink(0).Children(func() {
			for _, v := range views {
				b := ui.Text(c.Key("view"+v.id), v.label).FontSize(12).Padding(3, 10).Radius(5).Cursor(ui.CursorPointer)
				if view == v.id {
					b.Background(p.Panel).FontWeight(600).Shadow(0, 1, 2, 0, ui.RGBA(0, 0, 0, 0.08))
				} else {
					b.TextColor(p.Muted)
				}
				if b.Clicked() {
					pick(v.id)
				}
			}
		})
	})
	switch {
	case t.loading && t.dsID == "":
		ui.Column(c).Fill().Center().Gap(10).Children(func() {
			ui.Spinner(c).Size(22, 22)
			ui.Text(c, tr.T("Memuat database…")).FontSize(13).TextColor(p.Faint)
		})
		return
	case t.err != "":
		ui.Column(c).Fill().Center().Children(func() {
			ui.Text(c, tr.T("Gagal memuat database: ")+t.err).FontSize(14).TextColor(c.Theme().Danger)
		})
		return
	}
	ui.Row(c).Padding(16, 24, 0, 24).AlignItems(ui.Center).Gap(10).Children(func() {
		if n := a.nodes[t.id]; n != nil && n.Emoji != "" {
			emoji.View(c, n.Emoji, 28)
		}
		in := ui.TextInput(c.Key("db-title"+t.id), &t.title).Placeholder(tr.T("Database tanpa judul")).
			FontSize(26).FontWeight(700).Grow(1).Background(ui.Color{}).Padding(2, 0).FocusRing(false).
			Label(tr.T("Judul database"))
		if in.Focused() {
			in.BorderWidth(0, 0, 1.5, 0).BorderColor(p.EditLine).Radius(0)
		} else {
			in.Border(0, ui.Color{})
		}
		if in.Changed() {
			t.titleDirty, t.titleEdit = true, time.Now()
			c.After(1600 * time.Millisecond)
		}
		if t.titleDirty && (in.Submitted() || !in.Focused() || time.Since(t.titleEdit) > 1500*time.Millisecond) {
			a.sendTableTitle(t)
		}
	})
	if a.rightPaneOpen && i == a.focus && !t.edit.open && !t.adding.open {
		a.tablePreviewKeys(c, i, t)
	}
	switch view {
	case "cards":
		a.cards(c, i, t)
	case "board":
		a.board(c, i, t)
	case "calendar":
		a.calendar(c, i, t)
	default:
		a.grid(c, i, t)
	}
}

// grid is the table view: a header that stays, rows that open their page.
func (a *App) grid(c *ui.Context, i int, t *tableView) {
	p := a.pal()
	if len(t.columns) == 0 {
		t.columns = []notion.Property{{Name: "Name", Type: "title"}}
	}
	var total float32
	for _, col := range t.columns {
		total += colWidth(col)
	}
	total += 44 // the + that adds a column
	cellOf := func(key string, col notion.Property, last bool) ui.Element {
		e := ui.Row(c.Key(key)).Width(colWidth(col)).Shrink(0).Height(38).AlignItems(ui.Center).Padding(0, 10).Clip()
		if !last {
			e.BorderWidth(0, 1, 0, 0).BorderColor(p.Line)
		}
		return e
	}
	ui.ScrollBoth(c).Grow(1).MinHeight(0).Margin(12, 24, 16, 24).Children(func() {
		ui.Column(c).Gap(10).AlignItems(ui.Start).Children(func() {
			ui.Column(c).Width(total).Border(1, p.Line).Radius(8).Clip().Children(func() {
				// The header stays at the top as the rows scroll.
				ui.Row(c).Sticky().Top(0).ZIndex(2).Background(p.Sidebar).BorderWidth(0, 0, 1, 0).BorderColor(p.Line).Children(func() {
					for _, col := range t.columns {
						a.headerCell(c, t, col, cellOf("h"+col.Name, col, false))
					}
					a.addColumn(c, t)
				})
				for k, r := range t.rows {
					row := ui.Row(c.Key("row"+r.ID)).BorderWidth(0, 0, 1, 0).BorderColor(p.Line)
					row.Children(func() {
						for j, col := range t.columns {
							e := cellOf("c"+col.Name, col, j == len(t.columns)-1)
							if col.Type == "title" {
								// The title opens the row as a page (or selects it when the right pane is open).
								e.Cursor(ui.CursorPointer)
								selected := a.rightPaneOpen && t.selectedID == r.ID
								if selected {
									accentBg, _ := tintColors(p.Accent, a.dark)
									e.Background(accentBg.Alpha(0.55))
								} else if e.Hovered() {
									e.Background(p.Hover)
								}
								e.Children(func() {
									if em := r.Emoji(); em != "" {
										emoji.View(c, em, 16).MarginEnd(6)
									} else {
										ui.Icon(c, iconPage).Size(14, 14).TextColor(p.Faint).MarginEnd(6)
									}
									ui.Text(c, nonEmpty(r.Title(), tr.T("Tanpa judul"))).FontSize(14).FontWeight(500).SingleLine().MinWidth(0)
								})
								if a.rightPaneOpen {
									if e.DoubleClicked() {
										a.openRow(i, t, r)
									} else if e.Clicked() {
										t.selectedID = r.ID
									}
								} else if e.Clicked() {
									a.openRow(i, t, r)
								}
								e.ContextMenu(func(m *ui.Menu) {
									if m.Item(tr.T("Buka")).Chosen() {
										a.openRow(i, t, r)
									}
									if m.Item(tr.T("Buka di samping")).Chosen() {
										a.openRow(-1, t, r)
									}
									m.Separator()
									if m.Item(tr.T("Buang ke sampah")).Chosen() {
										a.trashRow(t, r)
									}
								})
								continue
							}
							a.editable(c, t, r, col, e, func() { a.value(c, r.Properties[col.Name], col.Type) })
						}
					})
					_ = k
				}
				a.addRow(c, i, t)
			})
			if len(t.rows) == 0 && len(t.query.conds) == 0 {
				ui.Text(c, tr.T("Database ini masih kosong")).FontSize(13).TextColor(p.Faint).Padding(0, 4)
			}
		})
	})
}

// cards is the gallery view: a card per row with its title and its first
// few properties that have a value.
func (a *App) cards(c *ui.Context, i int, t *tableView) {
	p := a.pal()
	ui.Scroll(c).Grow(1).MinHeight(0).Children(func() {
		ui.Row(c).Wrap().Gap(14).Padding(12, 24, 16, 24).AlignItems(ui.Start).Children(func() {
			for _, r := range t.rows {
				a.rowCard(c, i, t, r, 250)
			}
			a.addCard(c, i, t)
		})
		if len(t.rows) == 0 && len(t.query.conds) == 0 {
			ui.Text(c, tr.T("Database ini masih kosong")).FontSize(13).TextColor(p.Faint).Padding(0, 24, 16, 24)
		}
	})
}

// rowCard draws a card for row r (used by both cards and board views) and
// returns its element so callers can attach drag/pose behavior.
func (a *App) rowCard(c *ui.Context, i int, t *tableView, r *notion.Page, width float32) ui.Element {
	p := a.pal()
	selected := a.rightPaneOpen && t.selectedID == r.ID
	card := ui.Column(c.Key("card"+r.ID)).Width(width).Gap(8).Padding(14, 16).Radius(10).
		Background(p.Panel).Cursor(ui.CursorPointer)
	switch {
	case selected:
		accentBg, _ := tintColors(p.Accent, a.dark)
		card.Background(accentBg.Alpha(0.28)).Border(1.5, p.Accent).Shadow(0, 1, 2, 0, ui.RGBA(0, 0, 0, 0.05))
	case card.Hovered():
		card.Border(1, p.EditLine).Shadow(0, 6, 18, 0, ui.RGBA(0, 0, 0, 0.12))
	default:
		card.Border(1, p.Line).Shadow(0, 1, 2, 0, ui.RGBA(0, 0, 0, 0.05))
	}
	own := false // a click on a property edits it, not opens the page
	card.Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			if e := r.Emoji(); e != "" {
				emoji.View(c, e, 18)
			}
			ui.Text(c, nonEmpty(r.Title(), tr.T("Tanpa judul"))).FontSize(15).FontWeight(600).MaxLines(2).MinWidth(0)
		})
		shown := 0
		ui.Column(c).FillWidth().Gap(3).Children(func() {
			for _, col := range t.columns {
				raw := r.Properties[col.Name]
				if col.Type == "title" || shown >= 4 || !hasValue(raw) {
					continue
				}
				shown++
				box := ui.Row(c.Key("v"+col.Name)).FillWidth().Padding(3, 6).Margin(0, -6).Radius(5).
					MinHeight(24).Gap(8).AlignItems(ui.Center)
				if a.editable(c, t, r, col, box, func() {
					if col.Type == "checkbox" {
						a.value(c, raw, col.Type)
						ui.Text(c, col.Name).FontSize(12).TextColor(p.Muted).SingleLine().Grow(1).MinWidth(0)
						return
					}
					ui.Text(c, col.Name).FontSize(12).TextColor(p.Faint).SingleLine().Shrink(0).MaxWidth(90)
					ui.Box(c).Grow(1)
					a.value(c, raw, col.Type)
				}) {
					own = true
				}
			}
		})
		ui.Text(c, tr.T("Diubah ")+ago(r.LastEditedTime)).FontSize(11).TextColor(p.Faint)
	})
	if !own && !t.edit.open {
		if a.rightPaneOpen {
			if card.DoubleClicked() {
				a.openRow(i, t, r)
			} else if card.Clicked() {
				t.selectedID = r.ID
			}
		} else if card.Clicked() {
			a.openRow(i, t, r)
		}
	}
	return card
}

// propIcon is the icon of a property type, as Notion heads its columns.
func propIcon(typ string) *ui.SVG {
	switch typ {
	case "title", "rich_text":
		return iconText
	case "number", "unique_id":
		return iconHash
	case "select", "multi_select", "status":
		return iconList
	case "date", "created_time", "last_edited_time":
		return iconCalendar
	case "checkbox":
		return iconCheck
	case "people", "created_by", "last_edited_by":
		return iconUser
	case "url", "email", "phone_number":
		return iconLink
	}
	return iconDot
}
