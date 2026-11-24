package app

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"gotion/internal/notion"
)

func TestRowPageProperties(t *testing.T) {
	rec := &recorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		path := strings.TrimPrefix(r.URL.Path, "/v1")
		rec.mu.Lock()
		rec.sent = append(rec.sent, r.Method+" "+path+" "+string(b))
		rec.mu.Unlock()
		switch {
		case path == "/pages/row1" && r.Method == "GET":
			io.WriteString(w, `{"object":"page","id":"row1","parent":{"type":"data_source_id","data_source_id":"ds1"},"last_edited_by":{"id":"u"},
			 "properties":{"Nama":{"type":"title","title":[{"plain_text":"Login"}]},
			  "Status":{"id":"s","type":"status","status":{"name":"Doing","color":"yellow"}},
			  "Tenggat":{"id":"d","type":"date","date":{"start":"2026-12-01"}}}}`)
		case path == "/data_sources/ds1":
			io.WriteString(w, `{"id":"ds1","properties":{
			 "Nama":{"id":"t","name":"Nama","type":"title","title":{}},
			 "Status":{"id":"s","name":"Status","type":"status","status":{"options":[{"name":"Doing","color":"yellow"},{"name":"Blocked","color":"red"}]}},
			 "Tenggat":{"id":"d","name":"Tenggat","type":"date","date":{}}}}`)
		case strings.HasSuffix(path, "/children"):
			io.WriteString(w, `{"results":[],"has_more":false}`)
		default:
			io.WriteString(w, `{}`)
		}
	}))
	defer srv.Close()
	a := tabsApp(t)
	a.client = notion.New("x")
	a.client.Base = srv.URL + "/v1"
	a.nodes["row1"] = &node{ID: "row1", Title: "Login", Kind: "page"}
	a.open(a.nodes["row1"], 0)
	if !waitFor2(func() bool { return a.panels[0].props != nil }) {
		t.Fatal("no properties")
	}
	tester := ui.NewTester(a.View, 1200, 560)
	tester.Advance(time.Second)
	snapshot(t, tester, "row-props")
	if _, ok := tester.Find("Tenggat"); !ok {
		t.Error("the date property is not shown")
	}
	if err := tester.Click("Doing"); err != nil {
		t.Fatal(err)
	}
	tester.Advance(time.Second)
	if err := tester.Click("Blocked"); err != nil {
		t.Fatal(err)
	}
	if !waitSent(rec, `PATCH /pages/row1 {"properties":{"Status":{"status":{"color":"red","name":"Blocked"}}}}`) {
		t.Errorf("sent:\n%s", rec.all())
	}
}
