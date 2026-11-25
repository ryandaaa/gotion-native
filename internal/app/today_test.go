package app

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"gotion/internal/notion"
)

func TestTodayTimezoneAndGrouping(t *testing.T) {
	loc := time.FixedZone("WIB", 7*3600)
	now := time.Date(2026, 11, 25, 9, 0, 0, 0, loc)

	var statusProp notion.Property
	if err := json.Unmarshal([]byte(`{
		"id":"st","name":"Status","type":"status",
		"status":{
			"options":[
				{"id":"o1","name":"Todo","color":"default"},
				{"id":"o2","name":"Doing","color":"yellow"},
				{"id":"o3","name":"Selesai","color":"green"}
			],
			"groups":[
				{"id":"g1","name":"To-do","color":"gray","option_ids":["o1"]},
				{"id":"g2","name":"In progress","color":"blue","option_ids":["o2"]},
				{"id":"g3","name":"Complete","color":"green","option_ids":["o3"]}
			]
		}
	}`), &statusProp); err != nil {
		t.Fatal(err)
	}
	cbProp := notion.Property{Name: "Cek", Type: "checkbox"}

	mk := func(id, title, dateVal, statusVal string, checked bool, doneProp notion.Property) todayItem {
		props := map[string]json.RawMessage{
			"Nama": json.RawMessage(`{"type":"title","title":[{"plain_text":"` + title + `"}]}`),
			"Tgl":  json.RawMessage(`{"type":"date","date":{"start":"` + dateVal + `"}}`),
		}
		if statusVal != "" {
			props["Status"] = json.RawMessage(`{"type":"status","status":{"name":"` + statusVal + `"}}`)
		}
		if doneProp.Type == "checkbox" {
			b := "false"
			if checked {
				b = "true"
			}
			props["Cek"] = json.RawMessage(`{"type":"checkbox","checkbox":` + b + `}`)
		}
		return todayItem{
			Page:      &notion.Page{ID: id, Properties: props},
			DateCol:   "Tgl",
			DoneCol:   doneProp.Name,
			DoneProp:  doneProp,
			StatusCol: "Status",
		}
	}

	items := []todayItem{
		mk("1", "Kemarin", "2026-11-24", "Todo", false, statusProp),
		mk("2", "Hari ini pas", "2026-11-25", "Doing", false, statusProp),
		// 2026-11-25T18:00:00Z is 2026-11-26 01:00 WIB -> Mingguan (not Hari ini!)
		mk("3", "Besok WIB", "2026-11-25T18:00:00Z", "Todo", false, statusProp),
		// Completed via "Complete" status group -> excluded
		mk("4", "Sudah beres status", "2026-11-25", "Selesai", false, statusProp),
		// Completed via checkbox -> excluded
		mk("5", "Sudah dicentang", "2026-11-25", "", true, cbProp),
	}

	g := classifyTodayItems(items, now)
	if len(g.Overdue) != 1 || g.Overdue[0].Page.ID != "1" {
		t.Errorf("Overdue = %+v, want [1]", g.Overdue)
	}
	if len(g.Today) != 1 || g.Today[0].Page.ID != "2" {
		t.Errorf("Today = %+v, want [2]", g.Today)
	}
	if len(g.Week) != 1 || g.Week[0].Page.ID != "3" {
		t.Errorf("Week = %+v, want [3]", g.Week)
	}
}

func TestTodayQueryOfflineCacheAndLive(t *testing.T) {
	nowFunc = func() time.Time { return time.Date(2026, 11, 25, 10, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { nowFunc = time.Now })

	var mu sync.Mutex
	var queries []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		path := strings.TrimPrefix(r.URL.Path, "/v1")
		mu.Lock()
		if strings.HasSuffix(path, "/query") {
			queries = append(queries, path+" "+string(b))
		}
		mu.Unlock()
		switch path {
		case "/users/me":
			io.WriteString(w, `{"object":"user","id":"bot1","type":"bot","bot":{"owner":{"type":"user","user":{"id":"user-42"}}}}`)
		case "/search":
			io.WriteString(w, `{"results":[{"object":"data_source","id":"ds-tasks","title":[{"plain_text":"Tugas Kerja"}]}],"has_more":false}`)
		case "/data_sources/ds-tasks":
			io.WriteString(w, `{"id":"ds-tasks","title":[{"plain_text":"Tugas Kerja"}],"properties":{
				"Nama":{"id":"t","name":"Nama","type":"title","title":{}},
				"PIC":{"id":"p","name":"PIC","type":"people","people":{}},
				"Tenggat":{"id":"d","name":"Tenggat","type":"date","date":{}},
				"Status":{"id":"s","name":"Status","type":"status","status":{
					"options":[{"id":"o1","name":"Todo","color":"default"},{"id":"o2","name":"Done","color":"green"}],
					"groups":[{"id":"g1","name":"To-do","option_ids":["o1"]},{"id":"g2","name":"Complete","option_ids":["o2"]}]
				}}
			}}`)
		case "/data_sources/ds-notes":
			// Missing people column -> should not be queried.
			io.WriteString(w, `{"id":"ds-notes","title":[{"plain_text":"Catatan"}],"properties":{
				"Nama":{"id":"t","name":"Nama","type":"title","title":{}},
				"Tanggal":{"id":"d","name":"Tanggal","type":"date","date":{}}
			}}`)
		case "/data_sources/ds-tasks/query":
			io.WriteString(w, `{"results":[{"object":"page","id":"row-today","properties":{
				"Nama":{"type":"title","title":[{"plain_text":"Kirim rilis"}]},
				"Tenggat":{"type":"date","date":{"start":"2026-11-25"}},
				"Status":{"type":"status","status":{"name":"Todo","color":"default"}}
			}}],"has_more":false}`)
		default:
			io.WriteString(w, `{}`)
		}
	}))
	defer srv.Close()

	a := tabsApp(t)
	a.client = notion.New("x")
	a.client.Base = srv.URL + "/v1"
	a.nodes["ds-tasks"] = &node{ID: "ds-tasks", Title: "Tugas Kerja", Kind: "data_source"}
	a.nodes["ds-notes"] = &node{ID: "ds-notes", Title: "Catatan", Kind: "data_source"}
	a.regroup()
	a.open(a.special(todayID), 0)

	tester := ui.NewTester(a.View, 1200, 650)
	if !waitFor2(func() bool { return a.today.loaded && !a.today.loading }) {
		t.Fatal("today did not finish loading")
	}
	tester.Frame()

	mu.Lock()
	qAll := strings.Join(queries, "\n")
	mu.Unlock()
	if !strings.Contains(qAll, `"contains":"user-42"`) || !strings.Contains(qAll, `"on_or_before":"2026-11-25"`) || !strings.Contains(qAll, `"does_not_equal":"Done"`) {
		t.Fatalf("unexpected query filters:\n%s", qAll)
	}
	if strings.Contains(qAll, "ds-notes") {
		t.Fatalf("ineligible data source queried:\n%s", qAll)
	}
	if _, ok := tester.Find("Kirim rilis"); !ok {
		t.Fatalf("today item not rendered; texts=%v", tester.Texts())
	}

	// Offline cache: a fresh signed-in App without server reads today.json immediately.
	offline := &App{client: notion.New("x"), bot: &notion.User{}, panels: [2]*panel{{}, nil}, nodes: map[string]*node{}}
	offline.set = loadSettings()
	offline.loadTodayCache()
	offline.open(offline.special(todayID), 0)
	offTester := ui.NewTester(offline.View, 1200, 650)
	offTester.Frame()
	if _, ok := offTester.Find("Kirim rilis"); !ok {
		t.Fatalf("offline cache item not shown; texts=%v", offTester.Texts())
	}

	// Live event page.properties_updated triggers a fresh loadToday query.
	mu.Lock()
	beforeCount := len(queries)
	mu.Unlock()
	a.onEvents([]liveEvent{{Type: "page.properties_updated"}})
	if !waitFor2(func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(queries) > beforeCount
	}) {
		t.Error("live event did not trigger today refresh")
	}
}
