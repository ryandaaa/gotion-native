package app

import (
	"context"
	"errors"
	"sort"

	"github.com/egoist/mygo/ui"

	"gotion/internal/notion"
)

// tableView is a data source shown read-only.
type tableView struct {
	id      string
	title   string
	columns []notion.Property
	rows    []*notion.Page
	loading bool
	err     string
	list    ui.ListState
	chosen  int
}

func (a *App) openTable(pn *panel, n *node) {
	t := &tableView{id: n.ID, title: n.Title, loading: true, chosen: -1}
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
		a.win.Update(func() {
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
			t.rows = rows
		})
	}()
}

var selectColors = map[string][2]string{
	"green": {"#DCFCE7", "#166534"}, "yellow": {"#FEF3C7", "#92400E"}, "orange": {"#FFEDD5", "#9A3412"},
	"red": {"#FEE2E2", "#991B1B"}, "blue": {"#DBEAFE", "#1E40AF"}, "purple": {"#EDE9FE", "#5B21B6"},
	"pink": {"#FCE7F3", "#9D174D"}, "brown": {"#F5E6DA", "#7C4A2D"}, "gray": {"#F5F5F4", "#44403C"},
	"default": {"#F5F5F4", "#44403C"},
}

func (a *App) tablePanel(c *ui.Context, i int, pn *panel) {
	p := a.pal()
	t := pn.table
	if n := a.nodes[t.id]; n != nil && n.Title != "Memuat…" {
		t.title = n.Title
	}
	a.panelHeader(c, i, iconTable, t.title, "db · read-only", nil)
	switch {
	case t.loading:
		ui.Column(c).Fill().Center().Children(func() { ui.Spinner(c).Size(22, 22) })
		return
	case t.err != "":
		ui.Column(c).Fill().Center().Children(func() {
			ui.Text(c, "Gagal memuat database: "+t.err).FontSize(14).TextColor(c.Theme().Danger)
		})
		return
	}
	cols := make([]ui.TableColumn, len(t.columns))
	for i, col := range t.columns {
		cols[i] = ui.TableColumn{Title: col.Name}
		if col.Type == "title" {
			cols[i].Width = 260
		}
	}
	t.list.Selected = &t.chosen
	ui.Column(c).Grow(1).Padding(8, 12).Children(func() {
		tbl := ui.Table(c, &t.list, cols, len(t.rows), func(row, col int) {
			raw := t.rows[row].Properties[t.columns[col].Name]
			text := notion.FormatProperty(raw)
			if color := notion.PropertyColor(raw); color != "" && text != "" {
				cs := selectColors[color]
				if cs[0] == "" {
					cs = selectColors["default"]
				}
				bg, fg := ui.Hex(cs[0]), ui.Hex(cs[1])
				if a.dark {
					bg, fg = fg, bg
				}
				ui.Text(c, text).FontSize(12).Padding(2, 8).Radius(6).Background(bg).TextColor(fg).SingleLine()
				return
			}
			ui.Text(c, text).FontSize(14).TextColor(p.Text).SingleLine()
		}).Grow(1)
		if tbl.Submitted() && t.chosen >= 0 && t.chosen < len(t.rows) {
			r := t.rows[t.chosen]
			n := &node{ID: r.ID, Title: nonEmpty(r.Title(), "Tanpa judul"), Kind: "page", ParentID: t.id, Edited: r.LastEditedTime}
			a.nodes[r.ID] = n
			a.open(n, i)
		}
		ui.Text(c, "Enter / klik ganda untuk membuka baris sebagai halaman").FontSize(12).TextColor(p.Faint).Padding(6, 4)
	})
}
