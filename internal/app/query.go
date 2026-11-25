package app

import (
	"context"
	"gotion/internal/tr"
	"strconv"
	"time"

	"github.com/egoist/mygo/ui"

	"gotion/internal/notion"
)

// condition is one filter of a table: a property and what it must hold.
type condition struct {
	prop  string
	value string // an option's name, "true"/"false", or text
}

// tableQuery is how a table is filtered and sorted, and its popovers.
type tableQuery struct {
	conds      []condition
	sortProp   string
	sortDesc   bool
	filterOpen bool
	sortOpen   bool
	newProp    string
	newValue   string
}

func (t *tableView) column(name string) (notion.Property, bool) {
	for _, c := range t.columns {
		if c.Name == name {
			return c, true
		}
	}
	return notion.Property{}, false
}

// filterBody is the query's filter as Notion takes it, nil for none.
func (t *tableView) filterBody() any {
	var all []any
	for _, c := range t.query.conds {
		if col, ok := t.column(c.prop); ok {
			all = append(all, notion.Condition(col, "", c.value))
		}
	}
	switch len(all) {
	case 0:
		return nil
	case 1:
		return all[0]
	}
	return map[string]any{"and": all}
}

func (t *tableView) sortsBody() []any {
	if t.query.sortProp == "" {
		return nil
	}
	dir := "ascending"
	if t.query.sortDesc {
		dir = "descending"
	}
	return []any{map[string]any{"property": t.query.sortProp, "direction": dir}}
}

// requery fetches the table's rows again, as its filter and sort say.
func (a *App) requery(t *tableView) {
	t.loading = true
	client, ds, filter, sorts := a.client, t.dsID, t.filterBody(), t.sortsBody()
	go func() {
		rows, err := client.QueryWith(context.Background(), ds, filter, sorts, 500)
		a.update(func() {
			t.loading = false
			if err != nil {
				a.toast(tr.T("Gagal memfilter: ") + errText(err))
				return
			}
			t.rows = rows
		})
	}()
}

// filterable reports whether a column can be filtered from the popover.
func filterable(p notion.Property) bool {
	switch p.Type {
	case "title", "rich_text", "select", "status", "multi_select", "checkbox", "number", "url", "email", "phone_number":
		return true
	}
	return false
}

// queryButtons are the table header's Filter and Urutkan, with popovers.
// queryButtons are the filter and sort buttons; compact, on a narrow panel,
// shows their icons alone, their labels as tooltips.
func (a *App) queryButtons(c *ui.Context, t *tableView, compact bool) {
	p := a.pal()
	q := &t.query
	button := func(key, label string, s *ui.SVG, on bool) ui.Element {
		b := ui.Row(c.Key(key)).AlignItems(ui.Center).Gap(5).Padding(4, 8).Radius(6).Cursor(ui.CursorPointer).Shrink(0)
		if compact {
			b.Tooltip(label).Label(label)
		}
		if on {
			b.Background(p.EditBg)
		} else if b.Hovered() {
			b.Background(p.Hover)
		}
		b.Children(func() {
			ic := ui.Icon(c, s).Size(14, 14).TextColor(p.Muted)
			if compact {
				if on {
					ic.TextColor(p.Accent)
				}
				return
			}
			t := ui.Text(c, label).FontSize(12)
			if on {
				t.TextColor(p.Accent).FontWeight(600)
			} else {
				t.TextColor(p.Muted)
			}
		})
		return b
	}
	flabel := "Filter"
	if n := len(q.conds); n > 0 {
		flabel += " · " + strconv.Itoa(n)
	}
	fb := button("filter", flabel, iconFilter, len(q.conds) > 0)
	if fb.Clicked() {
		q.filterOpen = !q.filterOpen
	}
	ui.Popover(c, fb, &q.filterOpen, func() { a.filterEditor(c, t) })
	slabel := tr.T("Urutkan")
	if q.sortProp != "" {
		slabel = q.sortProp
	}
	sb := button("sort", slabel, iconSort, q.sortProp != "")
	if sb.Clicked() {
		q.sortOpen = !q.sortOpen
	}
	ui.Popover(c, sb, &q.sortOpen, func() { a.sortEditor(c, t) })
}

func (a *App) filterEditor(c *ui.Context, t *tableView) {
	p := a.pal()
	q := &t.query
	ui.Column(c).Width(300).Padding(6).Gap(10).Children(func() {
		ui.Text(c, "Filter").FontSize(12).FontWeight(600).TextColor(p.Faint)
		for k, cond := range q.conds {
			ui.Row(c.Key("cond"+strconv.Itoa(k))).AlignItems(ui.Center).Gap(6).Padding(6, 8).Radius(7).Background(p.Sidebar).Children(func() {
				ui.Text(c, cond.prop).FontSize(13).FontWeight(600)
				ui.Text(c, map[bool]string{true: "=", false: "berisi"}[func() bool {
					col, _ := t.column(cond.prop)
					return col.Type != "title" && col.Type != "rich_text"
				}()]).FontSize(12).TextColor(p.Faint)
				shown := cond.value
				if col, _ := t.column(cond.prop); col.Type == "checkbox" {
					shown = map[string]string{"true": tr.T("Dicentang"), "false": tr.T("Belum dicentang")}[cond.value]
				}
				ui.Text(c, shown).FontSize(13).Grow(1).SingleLine().MinWidth(0)
				if smallButton(c, p, iconClose, tr.T("Hapus filter")) {
					q.conds = append(q.conds[:k], q.conds[k+1:]...)
					a.requery(t)
				}
			})
		}
		var names []string
		for _, col := range t.columns {
			if filterable(col) {
				names = append(names, col.Name)
			}
		}
		if len(names) == 0 {
			return
		}
		if q.newProp == "" {
			q.newProp = names[0]
		}
		if ui.Select(c, &q.newProp, names).Label(tr.T("Properti")).Changed() {
			q.newValue = ""
		}
		col, _ := t.column(q.newProp)
		switch col.Type {
		case "select", "status", "multi_select":
			ui.Row(c).Wrap().Gap(4).Children(func() {
				for _, o := range col.Options {
					b := ui.Box(c.Key("fo" + o.Name)).Radius(5).Cursor(ui.CursorPointer).Padding(1)
					if q.newValue == o.Name {
						b.Border(1.5, p.Accent)
					}
					b.Children(func() { a.chip(c, notion.Chip{Name: o.Name, Color: o.Color}) })
					if b.Clicked() {
						q.newValue = o.Name
					}
				}
			})
		case "checkbox":
			a.segmented(c, "fcheck", &q.newValue, []struct{ id, label string }{{"true", tr.T("Dicentang")}, {"false", tr.T("Belum dicentang")}})
		default:
			ui.TextInput(c, &q.newValue).Placeholder(tr.T("Berisi…")).Label(tr.T("Nilai")).FontSize(13)
		}
		value := q.newValue
		if col.Type == "checkbox" && value == "" {
			value = "true"
		}
		if ui.PrimaryButton(c, tr.T("Tambah filter")).Disabled(value == "").Clicked() {
			q.conds = append(q.conds, condition{prop: q.newProp, value: value})
			q.newValue = ""
			a.requery(t)
		}
	})
}

func (a *App) sortEditor(c *ui.Context, t *tableView) {
	p := a.pal()
	q := &t.query
	ui.Column(c).Width(260).Padding(6).Gap(10).Children(func() {
		ui.Text(c, tr.T("Urutkan")).FontSize(12).FontWeight(600).TextColor(p.Faint)
		var names []string
		for _, col := range t.columns {
			names = append(names, col.Name)
		}
		prop := q.sortProp
		if prop == "" && len(names) > 0 {
			prop = names[0]
		}
		changed := ui.Select(c, &prop, names).Label(tr.T("Properti")).Changed()
		dir := "asc"
		if q.sortDesc {
			dir = "desc"
		}
		old := dir
		a.segmented(c, "sdir", &dir, []struct{ id, label string }{{"asc", tr.T("Naik")}, {"desc", tr.T("Turun")}})
		if changed || dir != old || q.sortProp == "" && ui.PrimaryButton(c, tr.T("Urutkan")).Clicked() {
			q.sortProp, q.sortDesc = prop, dir == "desc"
			a.requery(t)
		}
		if q.sortProp != "" && a.btn(c, tr.T("Hapus urutan"), nil, false, false) {
			q.sortProp = ""
			a.requery(t)
		}
	})
}

// trashRow puts a row of a table in Notion's trash.
func (a *App) trashRow(t *tableView, r *notion.Page) {
	client := a.client
	go func() {
		err := client.TrashPage(context.Background(), r.ID, true)
		a.update(func() {
			if err != nil {
				a.toast(tr.T("Gagal membuang baris: ") + errText(err))
				return
			}
			for k, x := range t.rows {
				if x == r {
					t.rows = append(t.rows[:k], t.rows[k+1:]...)
					break
				}
			}
			a.set.Trashed = append(a.set.Trashed, trashed{ID: r.ID, Title: r.Title(), Emoji: r.Emoji(), When: nowFunc()})
			a.saveSettings()
			a.toast(tr.T("Baris dibuang ke sampah"))
		})
	}()
}

// nowFunc is the time now; tests stand in for it.
var nowFunc = time.Now
