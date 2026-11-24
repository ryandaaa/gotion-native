package app

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/egoist/mygo/ui"

	"gotion/internal/notion"
)

// cellEdit is a property of a row being edited, in a popover by its cell.
type cellEdit struct {
	row, col string
	open     bool
	text     string
	err      string
}

// newRow is the title of a row being added, under the table.
type newRow struct {
	open  bool
	title string
	busy  bool
}

// setCell gives a row's property a new value: shown at once, sent to
// Notion behind, and put back as it was if Notion refuses it.
func (a *App) setCell(t *tableView, r *notion.Page, col notion.Property, text string, on bool, names []string) bool {
	send, show, err := notion.Value(col, text, on, names)
	if err != nil {
		t.edit.err = err.Error()
		return false
	}
	old := r.Properties[col.Name]
	if r.Properties == nil {
		r.Properties = map[string]json.RawMessage{}
	}
	r.Properties[col.Name] = show
	client := a.client
	go func() {
		_, err := client.SetProperties(context.Background(), r.ID, map[string]any{col.Name: send})
		if err != nil {
			a.update(func() {
				r.Properties[col.Name] = old
				a.toast("Gagal menyimpan " + col.Name + ": " + errText(err))
			})
		}
	}()
	return true
}

// chipNames are the names of a property's chosen options.
func chipNames(raw json.RawMessage) []string {
	var out []string
	for _, ch := range notion.Chips(raw) {
		out = append(out, ch.Name)
	}
	return out
}

// editable is a property's value in a cell that edits it: a check box
// toggles on a click, any other editable property opens its editor.
// It reports whether the click was its own.
func (a *App) editable(c *ui.Context, t *tableView, r *notion.Page, col notion.Property, cell ui.Element, body func()) bool {
	p := a.pal()
	if !col.Editable() || col.Type == "title" {
		cell.Children(body)
		return false
	}
	cell.Cursor(ui.CursorPointer)
	e := &t.edit
	editing := e.open && e.row == r.ID && e.col == col.Name
	if cell.Hovered() || editing {
		cell.Background(p.Hover)
	}
	cell.Children(body)
	clicked := cell.Clicked()
	if clicked {
		raw := r.Properties[col.Name]
		if col.Type == "checkbox" {
			a.setCell(t, r, col, "", notion.FormatProperty(raw) != "☑", nil)
			return true
		}
		text := notion.FormatProperty(raw)
		if col.Type == "date" && len(text) > 10 {
			text = text[:10]
		}
		*e = cellEdit{row: r.ID, col: col.Name, open: true, text: text}
		editing = true
	}
	if editing {
		ui.Popover(c, cell, &e.open, func() { a.propertyEditor(c, t, r, col) })
	}
	return clicked
}

// propertyEditor is the popover that edits a property: options to pick, or a
// field to type in.
func (a *App) propertyEditor(c *ui.Context, t *tableView, r *notion.Page, col notion.Property) {
	p := a.pal()
	e := &t.edit
	ui.Column(c).Width(260).Padding(4).Gap(4).Children(func() {
		ui.Text(c, col.Name).FontSize(12).FontWeight(600).TextColor(p.Faint).Padding(2, 4)
		switch col.Type {
		case "select", "status", "multi_select":
			chosen := map[string]bool{}
			for _, n := range chipNames(r.Properties[col.Name]) {
				chosen[n] = true
			}
			if len(col.Options) == 0 {
				ui.Text(c, "Belum ada pilihan. Tambahkan pilihan di Notion.").FontSize(13).TextColor(p.Faint).Padding(4)
			}
			ui.Scroll(c).MaxHeight(300).Children(func() {
				ui.Column(c).Gap(1).Children(func() {
					for _, o := range col.Options {
						row := ui.Row(c.Key("opt"+o.Name)).AlignItems(ui.Center).Gap(8).Padding(5, 6).Radius(6).Cursor(ui.CursorPointer)
						if row.Hovered() {
							row.Background(p.Hover)
						}
						row.Children(func() {
							a.chip(c, notion.Chip{Name: o.Name, Color: o.Color})
							ui.Box(c).Grow(1)
							if chosen[o.Name] {
								ui.Icon(c, iconCheck).Size(14, 14).TextColor(p.Accent)
							}
						})
						if row.Clicked() {
							if col.Type == "multi_select" {
								// Toggle it among the others, the popover staying.
								var names []string
								for _, n := range chipNames(r.Properties[col.Name]) {
									if n != o.Name {
										names = append(names, n)
									}
								}
								if !chosen[o.Name] {
									names = append(names, o.Name)
								}
								a.setCell(t, r, col, "", false, names)
							} else {
								a.setCell(t, r, col, "", false, []string{o.Name})
								e.open = false
							}
						}
					}
				})
			})
			if len(chosen) > 0 {
				clear := ui.Text(c.Key("clear"), "Kosongkan").FontSize(13).TextColor(p.Muted).Padding(6).Radius(6).Cursor(ui.CursorPointer)
				if clear.Hovered() {
					clear.Background(p.Hover)
				}
				if clear.Clicked() {
					a.setCell(t, r, col, "", false, nil)
					e.open = false
				}
			}
		default:
			hint := map[string]string{"date": "TTTT-BB-HH", "number": "Angka", "url": "https://", "email": "nama@contoh.com", "phone_number": "+62…"}[col.Type]
			in := ui.TextInput(c, &e.text).Placeholder(hint).AutoFocus().Label(col.Name)
			if col.Type == "rich_text" {
				in.Multiline(true).Lines(1, 6)
			}
			if in.Changed() {
				e.err = ""
			}
			ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
				if e.err != "" {
					ui.Text(c, e.err).FontSize(12).TextColor(c.Theme().Danger).Grow(1)
				} else {
					ui.Text(c, "Enter untuk simpan").FontSize(11).TextColor(p.Faint).Grow(1)
				}
				if (in.Submitted() || ui.PrimaryButton(c, "Simpan").Clicked()) && a.setCell(t, r, col, e.text, false, nil) {
					e.open = false
				}
			})
		}
	})
}

// addRow is the line under the table that adds a row, its title typed in
// place.
func (a *App) addRow(c *ui.Context, i int, t *tableView) {
	p := a.pal()
	n := &t.adding
	if !n.open {
		b := ui.Row(c.Key("add-row")).AlignItems(ui.Center).Gap(8).Padding(9, 12).Radius(8).Cursor(ui.CursorPointer)
		if b.Hovered() {
			b.Background(p.Hover)
		}
		b.Children(func() {
			ui.Icon(c, iconPlus).Size(14, 14).TextColor(p.Faint)
			ui.Text(c, "Baris baru").FontSize(14).TextColor(p.Muted)
		})
		if b.Clicked() {
			*n = newRow{open: true}
		}
		return
	}
	ui.Row(c).AlignItems(ui.Center).Gap(8).Padding(6, 8).Children(func() {
		in := ui.TextInput(c, &n.title).Placeholder("Judul baris baru").AutoFocus().Grow(1).Label("Judul").Disabled(n.busy)
		if in.Submitted() && !n.busy {
			a.createRow(i, t)
		}
		if n.busy {
			ui.Spinner(c).Size(14, 14)
		} else if a.btn(c, "Batal", nil, false, false) {
			*n = newRow{}
		}
	})
}

func (a *App) createRow(i int, t *tableView) {
	title := strings.TrimSpace(t.adding.title)
	prop := "Name"
	for _, col := range t.columns {
		if col.Type == "title" {
			prop = col.Name
		}
	}
	t.adding.busy = true
	client, ds := a.client, t.dsID
	go func() {
		r, err := client.CreateRow(context.Background(), ds, prop, title)
		a.update(func() {
			t.adding = newRow{}
			if err != nil {
				a.toast("Gagal menambah baris: " + errText(err))
				return
			}
			t.rows = append(t.rows, r)
			a.toast("Baris ditambahkan")
		})
	}()
}
