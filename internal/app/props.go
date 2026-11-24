package app

import (
	"context"
	"gotion/internal/tr"
	"sort"

	"github.com/egoist/mygo/ui"

	"gotion/internal/notion"
)

// loadProps fetches the schema of the data source a page is a row of, so
// its properties show and edit above its text, as in Notion.
func (a *App) loadProps(p *panel) {
	if p.session == nil || p.session.Page == nil {
		return
	}
	page := p.session.Page
	ds := page.Parent.DataSourceID
	if page.Parent.Type != "data_source_id" || ds == "" {
		return
	}
	client := a.client
	go func() {
		src, err := client.DataSource(context.Background(), ds)
		if err != nil {
			return // the page shows without them
		}
		a.update(func() {
			t := &tableView{id: ds, dsID: ds, rows: []*notion.Page{page}}
			for _, c := range src.Properties {
				if c.Type != "title" {
					t.columns = append(t.columns, c)
				}
			}
			sort.Slice(t.columns, func(i, j int) bool { return t.columns[i].Name < t.columns[j].Name })
			p.props = t
		})
	}()
}

// propsView is a row page's properties, one line each: its name, and its
// value, which a click edits.
func (a *App) propsView(c *ui.Context, pn *panel, pad float32) {
	t := pn.props
	if t == nil || len(t.rows) == 0 {
		return
	}
	p := a.pal()
	r := t.rows[0]
	ui.Column(c).PaddingX(pad).Gap(2).Margin(0, 0, 6, 0).Children(func() {
		for _, col := range t.columns {
			ui.Row(c.Key("prop" + col.Name)).AlignItems(ui.Center).Gap(8).MinHeight(32).Children(func() {
				ui.Row(c).Width(170).Shrink(0).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Icon(c, propIcon(col.Type)).Size(14, 14).TextColor(p.Faint)
					ui.Text(c, col.Name).FontSize(14).TextColor(p.Muted).SingleLine().MinWidth(0)
				})
				cell := ui.Row(c.Key("v")).Grow(1).MinWidth(0).MinHeight(30).AlignItems(ui.Center).Padding(0, 8).Radius(6)
				raw := r.Properties[col.Name]
				a.editable(c, t, r, col, cell, func() {
					if hasValue(raw) || col.Type == "checkbox" {
						a.value(c, raw, col.Type)
					} else {
						ui.Text(c, tr.T("Kosong")).FontSize(14).TextColor(p.Faint)
					}
				})
			})
		}
	})
	ui.Box(c).Height(1).Background(p.Line).Margin(0, pad, 4, pad)
}
