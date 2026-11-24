package app

import (
	"context"
	"encoding/json"
	"gotion/internal/tr"
	"sort"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"gotion/internal/notion"
)

// cellEdit is a property of a row being edited, in a popover by its cell.
type cellEdit struct {
	row, col string
	open     bool
	text     string
	date     time.Time
	err      string
}

// newRow is the title of a row being added, under the table.
type newRow struct {
	open     bool
	title    string
	busy     bool
	template string // the chosen template's name, "" for none
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
				a.toast(tr.T("Gagal menyimpan ") + col.Name + ": " + errText(err))
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
		var dt time.Time
		if col.Type == "date" {
			text = notion.DateStart(raw)
			if parsed, err := time.Parse("2006-01-02", text); err == nil {
				dt = parsed
			} else {
				now := nowFunc()
				dt = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
			}
		}
		*e = cellEdit{row: r.ID, col: col.Name, open: true, text: text, date: dt}
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
				ui.Text(c, tr.T("Belum ada pilihan. Tambahkan di bawah.")).FontSize(13).TextColor(p.Faint).Padding(4)
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
			// A new option: added to the column, then chosen.
			if col.Type != "status" {
				in := ui.TextInput(c, &e.text).Placeholder(tr.T("Pilihan baru…")).Label(tr.T("Pilihan baru")).FontSize(13)
				if in.Submitted() && strings.TrimSpace(e.text) != "" {
					a.addOption(t, r, col, strings.TrimSpace(e.text))
					e.text = ""
				}
			}
			if len(chosen) > 0 {
				clear := ui.Text(c.Key("clear"), tr.T("Kosongkan")).FontSize(13).TextColor(p.Muted).Padding(6).Radius(6).Cursor(ui.CursorPointer)
				if clear.Hovered() {
					clear.Background(p.Hover)
				}
				if clear.Clicked() {
					a.setCell(t, r, col, "", false, nil)
					e.open = false
				}
			}
		case "people", "relation":
			a.linksEditor(c, t, r, col)
		case "date":
			ui.Row(c).Justify(ui.Center).Children(func() {
				if ui.Calendar(c, &e.date).Browse().AutoFocus().Changed() {
					e.err = ""
					if a.setCell(t, r, col, e.date.Format("2006-01-02"), false, nil) {
						e.open = false
					}
				}
			})
			in := ui.TextInput(c, &e.text).Placeholder(tr.T("TTTT-BB-HH")).Label(col.Name).FontSize(13)
			if in.Changed() {
				e.err = ""
				if parsed, err := time.Parse("2006-01-02", strings.TrimSpace(e.text)); err == nil {
					e.date = parsed
				}
			}
			if e.err != "" {
				ui.Text(c, e.err).FontSize(12).TextColor(c.Theme().Danger).Padding(0, 4)
			}
			ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
				todayBtn := ui.ButtonBase(c.Key("today")).Role(ui.RoleButton).Label(tr.T("Hari ini")).
					Padding(5, 8).Radius(6).Cursor(ui.CursorPointer)
				if todayBtn.Pressed() {
					todayBtn.Background(p.Line)
				} else if todayBtn.Hovered() {
					todayBtn.Background(p.Hover)
				}
				todayBtn.Children(func() {
					ui.Text(c, tr.T("Hari ini")).FontSize(13).TextColor(p.Muted)
				})
				if todayBtn.Clicked() {
					e.err = ""
					if a.setCell(t, r, col, nowFunc().Format("2006-01-02"), false, nil) {
						e.open = false
					}
				}
				if hasValue(r.Properties[col.Name]) {
					clearBtn := ui.ButtonBase(c.Key("clear")).Role(ui.RoleButton).Label(tr.T("Kosongkan")).
						Padding(5, 8).Radius(6).Cursor(ui.CursorPointer)
					if clearBtn.Pressed() {
						clearBtn.Background(p.Line)
					} else if clearBtn.Hovered() {
						clearBtn.Background(p.Hover)
					}
					clearBtn.Children(func() {
						ui.Text(c, tr.T("Kosongkan")).FontSize(13).TextColor(p.Muted)
					})
					if clearBtn.Clicked() {
						e.err = ""
						a.setCell(t, r, col, "", false, nil)
						e.open = false
					}
				}
				ui.Box(c).Grow(1)
				if (in.Submitted() || ui.PrimaryButton(c, tr.T("Simpan")).Clicked()) && a.setCell(t, r, col, e.text, false, nil) {
					e.open = false
				}
			})
		default:
			hint := map[string]string{"number": tr.T("Angka"), "url": "https://", "email": tr.T("nama@contoh.com"), "phone_number": "+62…"}[col.Type]
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
					ui.Text(c, tr.T("Enter untuk simpan")).FontSize(11).TextColor(p.Faint).Grow(1)
				}
				if (in.Submitted() || ui.PrimaryButton(c, tr.T("Simpan")).Clicked()) && a.setCell(t, r, col, e.text, false, nil) {
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
			ui.Text(c, tr.T("Baris baru")).FontSize(14).TextColor(p.Muted)
		})
		if b.Clicked() {
			*n = newRow{open: true}
			a.loadTemplates(t)
		}
		return
	}
	ui.Row(c).AlignItems(ui.Center).Gap(8).Padding(6, 8).Children(func() {
		in := ui.TextInput(c, &n.title).Placeholder(tr.T("Judul baris baru")).AutoFocus().Grow(1).Label(tr.T("Judul")).Disabled(n.busy)
		if in.Submitted() && !n.busy {
			a.createRow(i, t)
		}
		if len(t.templates) > 0 && !n.busy {
			names := []string{tr.T("Tanpa template")}
			for _, tp := range t.templates {
				names = append(names, nonEmpty(tp.Name, "Template"))
			}
			choice := nonEmpty(n.template, tr.T("Tanpa template"))
			if ui.Select(c, &choice, names).Width(180).Label("Template").Changed() {
				n.template = choice
				if choice == tr.T("Tanpa template") {
					n.template = ""
				}
			}
		}
		if n.busy {
			ui.Spinner(c).Size(14, 14)
		} else if a.btn(c, tr.T("Batal"), nil, false, false) {
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
	template := ""
	for _, tp := range t.templates {
		if tp.Name == t.adding.template && t.adding.template != "" {
			template = tp.ID
		}
	}
	go func() {
		r, err := client.CreateRowFrom(context.Background(), ds, prop, title, template)
		a.update(func() {
			t.adding = newRow{}
			if err != nil {
				a.toast(tr.T("Gagal menambah baris: ") + errText(err))
				return
			}
			t.rows = append(t.rows, r)
			a.toast(tr.T("Baris ditambahkan"))
		})
	}()
}

// addOption adds an option to a column (Notion takes the whole list) and
// gives the row it.
func (a *App) addOption(t *tableView, r *notion.Page, col notion.Property, name string) {
	client := a.client
	go func() {
		ds, err := client.UpdateSchema(context.Background(), t.dsID, notion.AddOption(col, name))
		a.update(func() {
			if err != nil {
				a.toast(tr.T("Gagal menambah pilihan: ") + errText(err))
				return
			}
			a.applySchema(t, ds)
			if nc, ok := t.column(col.Name); ok {
				names := []string{name}
				if col.Type == "multi_select" {
					names = append(chipNames(r.Properties[col.Name]), name)
				}
				a.setCell(t, r, nc, "", false, names)
			}
		})
	}()
}

// applySchema takes a data source's columns as Notion now has them,
// keeping the table's order: the title first, then by name.
func (a *App) applySchema(t *tableView, ds *notion.DataSource) {
	if ds == nil || len(ds.Properties) == 0 {
		return
	}
	var cols []notion.Property
	for name, p := range ds.Properties {
		if p.Name == "" {
			p.Name = name // the map's key is the column's name
		}
		if p.Type == "" {
			continue // not a column: an answer of another shape
		}
		cols = append(cols, p)
	}
	if len(cols) == 0 {
		return
	}
	sort.Slice(cols, func(i, j int) bool {
		if (cols[i].Type == "title") != (cols[j].Type == "title") {
			return cols[i].Type == "title"
		}
		return cols[i].Name < cols[j].Name
	})
	t.columns = cols
}

// setLinks points a people or relation property at links: shown at once,
// sent behind, put back if Notion refuses.
func (a *App) setLinks(t *tableView, r *notion.Page, col notion.Property, links []notion.Linked) {
	send, show, err := notion.LinksValue(col, links)
	if err != nil {
		a.toast(err.Error())
		return
	}
	old := r.Properties[col.Name]
	r.Properties[col.Name] = show
	client := a.client
	go func() {
		_, err := client.SetProperties(context.Background(), r.ID, map[string]any{col.Name: send})
		if err != nil {
			a.update(func() {
				r.Properties[col.Name] = old
				a.toast(tr.T("Gagal menyimpan ") + col.Name + ": " + errText(err))
			})
		}
	}()
}

// linksEditor picks the people or the pages a property points at: the
// workspace's members, or the pages of the relation's data source.
func (a *App) linksEditor(c *ui.Context, t *tableView, r *notion.Page, col notion.Property) {
	p := a.pal()
	e := &t.edit
	current := notion.Links(r.Properties[col.Name])
	has := map[string]bool{}
	for _, l := range current {
		has[l.ID] = true
	}
	var choices []notion.Linked
	if col.Type == "people" {
		if !a.settings.membersLoaded && !a.settings.membersLoading {
			a.loadMembers()
		}
		for _, u := range a.settings.members {
			choices = append(choices, notion.Linked{ID: u.ID, Name: nonEmpty(u.Name, tr.T("Tanpa nama"))})
		}
		if a.settings.membersErr != "" {
			ui.Text(c, a.settings.membersErr).FontSize(12).TextColor(p.Faint).Padding(4)
		}
	} else {
		choices = a.relationChoices(t, col)
	}
	ui.TextInput(c, &e.text).Placeholder(tr.T("Cari…")).AutoFocus().Label(tr.T("Cari")).FontSize(13)
	q := strings.ToLower(strings.TrimSpace(e.text))
	ui.Scroll(c).MaxHeight(280).Children(func() {
		ui.Column(c).Gap(1).Children(func() {
			shown := 0
			for _, ch := range choices {
				if q != "" && !strings.Contains(strings.ToLower(ch.Name), q) || shown >= 40 {
					continue
				}
				shown++
				row := ui.Row(c.Key("ln"+ch.ID)).AlignItems(ui.Center).Gap(8).Padding(6, 8).Radius(6).Cursor(ui.CursorPointer)
				if row.Hovered() {
					row.Background(p.Hover)
				}
				row.Children(func() {
					ui.Text(c, ch.Name).FontSize(13).Grow(1).SingleLine().MinWidth(0)
					if has[ch.ID] {
						ui.Icon(c, iconCheck).Size(14, 14).TextColor(p.Accent)
					}
				})
				if row.Clicked() {
					var next []notion.Linked
					for _, l := range current {
						if l.ID != ch.ID {
							next = append(next, l)
						}
					}
					if !has[ch.ID] {
						next = append(next, ch)
					}
					a.setLinks(t, r, col, next)
				}
			}
			if len(choices) == 0 {
				ui.Text(c, tr.T("Memuat…")).FontSize(12).TextColor(p.Faint).Padding(6)
			}
		})
	})
}

// relationChoices are the pages of a relation's data source, fetched once
// per table.
func (a *App) relationChoices(t *tableView, col notion.Property) []notion.Linked {
	if t.related == nil {
		t.related = map[string][]notion.Linked{}
	}
	if got, ok := t.related[col.Target]; ok {
		return got
	}
	if col.Target == "" {
		return nil
	}
	t.related[col.Target] = nil // asked
	client := a.client
	go func() {
		pages, err := client.QueryWith(context.Background(), col.Target, nil, nil, 200)
		a.update(func() {
			if err != nil {
				a.toast(tr.T("Gagal memuat halaman relasi: ") + errText(err))
				return
			}
			var out []notion.Linked
			for _, pg := range pages {
				out = append(out, notion.Linked{ID: pg.ID, Name: nonEmpty(pg.Title(), tr.T("Tanpa judul"))})
			}
			t.related[col.Target] = out
		})
	}()
	return nil
}

// loadTemplates fetches a table's page templates, once, to offer them for
// new rows; the default one starts chosen.
func (a *App) loadTemplates(t *tableView) {
	if t.templatesAsked || t.dsID == "" {
		return
	}
	t.templatesAsked = true
	client := a.client
	go func() {
		list, err := client.Templates(context.Background(), t.dsID)
		if err != nil {
			return // offered none
		}
		a.update(func() {
			t.templates = list
			for _, tp := range list {
				if tp.IsDefault && t.adding.template == "" {
					t.adding.template = tp.Name
				}
			}
		})
	}()
}
