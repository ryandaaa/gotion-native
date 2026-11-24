package app

import (
	"encoding/json"
	"testing"

	"github.com/egoist/mygo/ui"

	"gotion/internal/notion"
)

const sampleRows = `[
 {"object":"page","id":"r1","last_edited_time":"2026-11-20T10:00:00Z","icon":{"type":"emoji","emoji":"🔐"},"properties":{
  "Nama":{"type":"title","title":[{"plain_text":"Halaman login"}]},
  "Status":{"type":"status","status":{"name":"Done","color":"green"}},
  "Tag":{"type":"multi_select","multi_select":[{"name":"frontend","color":"blue"},{"name":"auth","color":"purple"}]},
  "Deadline":{"type":"date","date":{"start":"2026-11-20"}},
  "Selesai":{"type":"checkbox","checkbox":true}}},
 {"object":"page","id":"r2","last_edited_time":"2026-11-24T08:00:00Z","properties":{
  "Nama":{"type":"title","title":[{"plain_text":"Sync engine"}]},
  "Status":{"type":"status","status":{"name":"Doing","color":"yellow"}},
  "Tag":{"type":"multi_select","multi_select":[{"name":"backend","color":"orange"}]},
  "Deadline":{"type":"date","date":{"start":"2026-11-28"}},
  "Selesai":{"type":"checkbox","checkbox":false}}},
 {"object":"page","id":"r3","last_edited_time":"2026-11-22T08:00:00Z","properties":{
  "Nama":{"type":"title","title":[{"plain_text":"Packaging .deb"}]},
  "Status":{"type":"status","status":{"name":"Todo","color":"default"}},
  "Tag":{"type":"multi_select","multi_select":[]},
  "Deadline":{"type":"date","date":{"start":"2026-12-05"}},
  "Selesai":{"type":"checkbox","checkbox":false}}}
]`

func TestDatabaseViews(t *testing.T) {
	var rows []*notion.Page
	if err := json.Unmarshal([]byte(sampleRows), &rows); err != nil {
		t.Fatal(err)
	}
	cols := []notion.Property{{Name: "Nama", Type: "title"}, {Name: "Deadline", Type: "date"}, {Name: "Selesai", Type: "checkbox"}, {Name: "Status", Type: "status"}, {Name: "Tag", Type: "multi_select"}}
	for _, shot := range []struct{ name, theme, view string }{
		{"db-table", "light", "table"}, {"db-table-dark", "dark", "table"}, {"db-cards", "light", "cards"},
	} {
		a := tableApp(t)
		a.set.Theme = shot.theme
		a.nodes["tasks"] = &node{ID: "tasks", Title: "Tasks", Kind: "data_source"}
		a.regroup()
		a.set.Views = map[string]string{"tasks": shot.view}
		a.panels[0] = &panel{id: "tasks", table: &tableView{id: "tasks", title: "Tasks", columns: cols, rows: rows}}
		tester := ui.NewTester(a.View, 1280, 600)
		tester.Frame()
		tester.Frame()
		snapshot(t, tester, shot.name)
	}
}

func TestPageTableDark(t *testing.T) {
	a := tableApp(t)
	a.set.Theme = "dark"
	tester := ui.NewTester(a.View, 1100, 560)
	tester.Frame()
	tester.Frame()
	snapshot(t, tester, "page-table-dark")
}
