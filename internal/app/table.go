package app

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/egoist/mygo/ui"

	"gotion/internal/emoji"
	"gotion/internal/notion"
)

// tableView is a data source shown read-only, as a table or as cards.
type tableView struct {
	id             string
	dsID           string // the data source, where new rows go
	title          string
	edit           cellEdit
	adding         newRow
	query          tableQuery
	related        map[string][]notion.Linked // the pages of relations' data sources
	schema         schemaEdit
	templates      []notion.Template
	templatesAsked bool
	columns        []notion.Property
	rows           []*notion.Page
	loading        bool
	err            string
}

func (a *App) openTable(pn *panel, n *node) {
	t := &tableView{id: n.ID, title: n.Title, loading: true}
	pn.table = t
	client := a.client
	go func() {
		ctx := context.Background()
		dsID := n.ID
		var err error
		if n.Kind == "database" {
			// A database block: its table is its first data source.
			var db *notion.Database
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
			for _, p := range ds.Properties {
				t.columns = append(t.columns, p)
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
			if title := notion.PlainText(ds.Title); title != "" && t.title == "Memuat…" {
				t.title = title
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
		return 150
	}
	return 190
}

func (a *App) openRow(i int, t *tableView, r *notion.Page) {
	n := a.nodes[r.ID]
	if n == nil {
		n = &node{ID: r.ID, Title: nonEmpty(r.Title(), "Tanpa judul"), Emoji: r.Emoji(), Kind: "page", ParentID: t.id, Edited: r.LastEditedTime}
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

func (a *App) tablePanel(c *ui.Context, i int, pn *panel) {
	p := a.pal()
	t := pn.table
	if n := a.nodes[t.id]; n != nil && n.Title != "Memuat…" {
		t.title = n.Title
	}
	view := a.set.Views[t.id]
	if view == "" {
		view = "table"
	}
	a.panelHeader(c, i, iconTable, t.title, "database", func() {
		if t.loading || t.err != "" {
			return
		}
		if t.loading && t.dsID != "" {
			ui.Spinner(c).Size(12, 12)
		}
		ui.Text(c, fmt.Sprintf("%d baris", len(t.rows))).FontSize(12).TextColor(p.Faint)
		a.queryButtons(c, t)
		ui.Row(c).Gap(2).Padding(2).Radius(7).Background(p.Sidebar).Children(func() {
			for _, v := range []struct{ id, label string }{{"table", "Tabel"}, {"cards", "Kartu"}} {
				b := ui.Text(c.Key("view"+v.id), v.label).FontSize(12).Padding(3, 10).Radius(5).Cursor(ui.CursorPointer)
				if view == v.id {
					b.Background(p.Panel).FontWeight(600).Shadow(0, 1, 2, 0, ui.RGBA(0, 0, 0, 0.08))
				} else {
					b.TextColor(p.Muted)
				}
				if b.Clicked() {
					a.set.Views[t.id] = v.id
					a.saveSettings()
				}
			}
		})
	})
	switch {
	case t.loading && t.dsID == "":
		ui.Column(c).Fill().Center().Gap(10).Children(func() {
			ui.Spinner(c).Size(22, 22)
			ui.Text(c, "Memuat database…").FontSize(13).TextColor(p.Faint)
		})
		return
	case t.err != "":
		ui.Column(c).Fill().Center().Children(func() {
			ui.Text(c, "Gagal memuat database: "+t.err).FontSize(14).TextColor(c.Theme().Danger)
		})
		return
	case len(t.rows) == 0 && len(t.query.conds) == 0:
		ui.Column(c).Fill().Center().Children(func() {
			ui.Text(c, "Database ini masih kosong").FontSize(14).TextColor(p.Faint)
		})
		return
	}
	ui.Column(c).Padding(16, 24, 0, 24).Children(func() {
		ui.Text(c, t.title).FontSize(26).FontWeight(700)
	})
	if view == "cards" {
		a.cards(c, i, t)
		return
	}
	a.grid(c, i, t)
}

// grid is the table view: a header that stays, rows that open their page.
func (a *App) grid(c *ui.Context, i int, t *tableView) {
	p := a.pal()
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
							// The title opens the row as a page.
							e.Cursor(ui.CursorPointer)
							if e.Hovered() {
								e.Background(p.Hover)
							}
							e.Children(func() {
								if em := r.Emoji(); em != "" {
									emoji.View(c, em, 16).MarginEnd(6)
								} else {
									ui.Icon(c, iconPage).Size(14, 14).TextColor(p.Faint).MarginEnd(6)
								}
								ui.Text(c, nonEmpty(r.Title(), "Tanpa judul")).FontSize(14).FontWeight(500).SingleLine().MinWidth(0)
							})
							if e.Clicked() {
								a.openRow(i, t, r)
							}
							e.ContextMenu(func(m *ui.Menu) {
								if m.Item("Buka").Chosen() {
									a.openRow(i, t, r)
								}
								if m.Item("Buka di samping").Chosen() {
									a.openRow(-1, t, r)
								}
								m.Separator()
								if m.Item("Buang ke sampah").Chosen() {
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
	})
}

// cards is the gallery view: a card per row with its title and its first
// few properties that have a value.
func (a *App) cards(c *ui.Context, i int, t *tableView) {
	p := a.pal()
	ui.Scroll(c).Grow(1).MinHeight(0).Children(func() {
		ui.Row(c).Wrap().Gap(14).Padding(12, 24, 24, 24).AlignItems(ui.Start).Children(func() {
			for _, r := range t.rows {
				card := ui.Column(c.Key("card"+r.ID)).Width(250).Gap(10).Padding(14, 16).Radius(10).
					Background(p.Panel).Cursor(ui.CursorPointer)
				if card.Hovered() {
					card.Border(1, p.EditLine).Shadow(0, 6, 18, 0, ui.RGBA(0, 0, 0, 0.12))
				} else {
					card.Border(1, p.Line).Shadow(0, 1, 2, 0, ui.RGBA(0, 0, 0, 0.05))
				}
				own := false // a click on a property edits it, not opens the page
				card.Children(func() {
					ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
						if e := r.Emoji(); e != "" {
							emoji.View(c, e, 18)
						}
						ui.Text(c, nonEmpty(r.Title(), "Tanpa judul")).FontSize(15).FontWeight(600).MaxLines(2).MinWidth(0)
					})
					shown := 0
					for _, col := range t.columns {
						raw := r.Properties[col.Name]
						if col.Type == "title" || shown >= 4 || !hasValue(raw) {
							continue
						}
						shown++
						ui.Text(c, col.Name).FontSize(11).TextColor(p.Faint).SingleLine()
						box := ui.Row(c.Key("v"+col.Name)).Padding(2, 4).Margin(-2, -4, 0, -4).Radius(5).MinHeight(22).AlignItems(ui.Center)
						if a.editable(c, t, r, col, box, func() { a.value(c, raw, col.Type) }) {
							own = true
						}
					}
					ui.Text(c, "Diubah "+ago(r.LastEditedTime)).FontSize(11).TextColor(p.Faint)
				})
				if card.Clicked() && !own && !t.edit.open {
					a.openRow(i, t, r)
				}
			}
		})
	})
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
