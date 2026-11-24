package app

import (
	"context"
	"gotion/internal/tr"
	"strings"

	"github.com/egoist/mygo/ui"

	"gotion/internal/notion"
)

// schemaEdit is a column being added, renamed or deleted.
type schemaEdit struct {
	renaming string // the column whose name is being changed
	renameOn bool
	name     string
	adding   bool
	newName  string
	newType  string
	deleting string // the column asked about before deleting
	deleteOn bool
}

// changeSchema sends a schema change and takes the columns Notion has after.
func (a *App) changeSchema(t *tableView, props map[string]any, done string) {
	client := a.client
	go func() {
		ds, err := client.UpdateSchema(context.Background(), t.dsID, props)
		a.update(func() {
			if err != nil {
				a.toast(tr.T("Gagal mengubah kolom: ") + errText(err))
				return
			}
			a.applySchema(t, ds)
			a.toast(done)
		})
	}()
}

// headerCell is a column's heading: right-click to rename or delete it.
func (a *App) headerCell(c *ui.Context, t *tableView, col notion.Property, e ui.Element) {
	p := a.pal()
	s := &t.schema
	e.Children(func() {
		ui.Icon(c, propIcon(col.Type)).Size(13, 13).TextColor(p.Faint)
		ui.Text(c, col.Name).FontSize(13).FontWeight(500).TextColor(p.Muted).SingleLine().MinWidth(0).MarginStart(6)
	})
	e.ContextMenu(func(m *ui.Menu) {
		if m.Item(tr.T("Ganti nama kolom")).Chosen() {
			s.renaming, s.renameOn, s.name = col.Name, true, col.Name
		}
		if col.Type != "title" && m.Item(tr.T("Hapus kolom")).Chosen() {
			s.deleting, s.deleteOn = col.Name, true
		}
	})
	if s.renameOn && s.renaming == col.Name {
		ui.Popover(c, e, &s.renameOn, func() {
			ui.Column(c).Width(240).Padding(6).Gap(8).Children(func() {
				in := ui.TextInput(c, &s.name).AutoFocus().Label(tr.T("Nama kolom")).FontSize(13)
				name := strings.TrimSpace(s.name)
				if (in.Submitted() || ui.PrimaryButton(c, tr.T("Simpan")).Clicked()) && name != "" && name != col.Name {
					s.renameOn = false
					a.changeSchema(t, map[string]any{col.Name: map[string]any{"name": name}}, tr.T("Kolom diganti nama"))
				}
			})
		})
	}
	if s.deleteOn && s.deleting == col.Name {
		ui.Popover(c, e, &s.deleteOn, func() {
			ui.Column(c).Width(260).Padding(6).Gap(8).Children(func() {
				ui.Text(c, tr.T("Hapus kolom “")+col.Name+tr.T("” dari semua baris?")).FontSize(13)
				ui.Row(c).Gap(6).Children(func() {
					if a.btn(c, tr.T("Hapus"), iconTrash, true, false) {
						s.deleteOn = false
						a.changeSchema(t, map[string]any{col.Name: nil}, tr.T("Kolom dihapus"))
					}
					if a.btn(c, tr.T("Batal"), nil, false, false) {
						s.deleteOn = false
					}
				})
			})
		})
	}
}

// addColumn is the + at the header's end that adds a column.
func (a *App) addColumn(c *ui.Context, t *tableView) {
	p := a.pal()
	s := &t.schema
	b := ui.Row(c.Key("add-col")).Width(44).Shrink(0).Height(38).Center().Cursor(ui.CursorPointer).Label(tr.T("Kolom baru")).Tooltip(tr.T("Kolom baru"))
	if b.Hovered() || s.adding {
		b.Background(p.Hover)
	}
	b.Children(func() { ui.Icon(c, iconPlus).Size(14, 14).TextColor(p.Faint) })
	if b.Clicked() {
		s.adding, s.newName, s.newType = !s.adding, "", "rich_text"
	}
	if !s.adding {
		return
	}
	ui.Popover(c, b, &s.adding, func() {
		ui.Column(c).Width(260).Padding(6).Gap(8).Children(func() {
			ui.Text(c, tr.T("Kolom baru")).FontSize(12).FontWeight(600).TextColor(p.Faint)
			in := ui.TextInput(c, &s.newName).Placeholder(tr.T("Nama kolom")).AutoFocus().Label(tr.T("Nama")).FontSize(13)
			var labels []string
			label := ""
			for _, ct := range notion.ColumnTypes {
				labels = append(labels, tr.T(ct.Label))
				if ct.Type == s.newType {
					label = tr.T(ct.Label)
				}
			}
			if ui.Select(c, &label, labels).Label(tr.T("Tipe")).Changed() {
				for _, ct := range notion.ColumnTypes {
					if tr.T(ct.Label) == label {
						s.newType = ct.Type
					}
				}
			}
			name := strings.TrimSpace(s.newName)
			if _, taken := t.column(name); taken {
				ui.Text(c, tr.T("Sudah ada kolom bernama itu")).FontSize(12).TextColor(c.Theme().Danger)
				return
			}
			if (in.Submitted() || ui.PrimaryButton(c, tr.T("Tambah kolom")).Disabled(name == "").Clicked()) && name != "" {
				s.adding = false
				a.changeSchema(t, map[string]any{name: map[string]any{s.newType: map[string]any{}}}, tr.T("Kolom “")+name+tr.T("” ditambahkan"))
			}
		})
	})
}
