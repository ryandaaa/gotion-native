package app

import (
	"encoding/json"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"gotion/internal/notion"
)

func sampleTodayStatusProp(t *testing.T) notion.Property {
	t.Helper()
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
	return statusProp
}

func mkTodayItem(id, title, dbTitle, dateVal, statusVal string, checked bool, doneProp notion.Property) todayItem {
	props := map[string]json.RawMessage{
		"Nama": json.RawMessage(`{"type":"title","title":[{"plain_text":"` + title + `"}]}`),
		"Tgl":  json.RawMessage(`{"type":"date","date":{"start":"` + dateVal + `"}}`),
	}
	if statusVal != "" {
		props["Status"] = json.RawMessage(`{"type":"status","status":{"name":"` + statusVal + `","color":"blue"}}`)
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
		DBID:      "ds-tasks",
		DBTitle:   dbTitle,
		TitleCol:  "Nama",
		DateCol:   "Tgl",
		DoneCol:   doneProp.Name,
		DoneProp:  doneProp,
		StatusCol: "Status",
	}
}

func TestTodayTimezoneAndGrouping(t *testing.T) {
	loc := time.FixedZone("WIB", 7*3600)
	now := time.Date(2026, 11, 25, 9, 0, 0, 0, loc)
	statusProp := sampleTodayStatusProp(t)
	cbProp := notion.Property{Name: "Cek", Type: "checkbox"}

	items := []todayItem{
		mkTodayItem("1", "Kemarin", "Proyek", "2026-11-24", "Todo", false, statusProp),
		mkTodayItem("2", "Hari ini pas", "Proyek", "2026-11-25", "Doing", false, statusProp),
		// 2026-11-25T18:00:00Z is 2026-11-26 01:00 WIB -> Week (not Today)
		mkTodayItem("3", "Besok WIB", "Proyek", "2026-11-25T18:00:00Z", "Todo", false, statusProp),
		// Completed via "Complete" status group -> DoneToday
		mkTodayItem("4", "Sudah beres status", "Proyek", "2026-11-25", "Selesai", false, statusProp),
		// Completed via checkbox -> DoneToday
		mkTodayItem("5", "Sudah dicentang", "Proyek", "2026-11-25", "", true, cbProp),
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
	if len(g.DoneToday) != 2 {
		t.Errorf("DoneToday = %d, want 2", len(g.DoneToday))
	}
}

func TestTodayFocusUrgencySelection(t *testing.T) {
	loc := time.FixedZone("WIB", 7*3600)
	now := time.Date(2026, 11, 25, 10, 0, 0, 0, loc)
	st := sampleTodayStatusProp(t)

	items := []todayItem{
		mkTodayItem("no-time-overdue", "Revisi proposal", "Proyek", "2026-11-23", "Doing", false, st),
		mkTodayItem("far-time", "Review PR", "Proyek", "2026-11-25T14:00:00+07:00", "Doing", false, st),
		mkTodayItem("near-time", "Standup tim", "Kerja", "2026-11-25T09:30:00+07:00", "Todo", false, st),
		mkTodayItem("no-time-today", "Tulis changelog", "Proyek", "2026-11-25", "Todo", false, st),
	}
	sortTodayUrgent(items, now)

	got := []string{items[0].Page.ID, items[1].Page.ID, items[2].Page.ID, items[3].Page.ID}
	want := []string{"near-time", "far-time", "no-time-overdue", "no-time-today"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("urgency order = %v, want %v", got, want)
		}
	}
}

func TestTintColorsContrast(t *testing.T) {
	white := ui.Hex("#FFFFFF")
	darkSurface := ui.Hex("#252527")
	for _, w := range washes {
		accL := w.accL
		if w.plain() {
			accL = light.Accent
		}
		_, fgL := tintColors(accL, false)
		if r := contrastRatio(fgL, white); r < 5.0 {
			t.Errorf("wash %s light contrast = %.2f, want >= 5.0", w.id, r)
		}

		accD := w.accD
		if w.plain() {
			accD = dark.Accent
		}
		_, fgD := tintColors(accD, true)
		if r := contrastRatio(fgD, darkSurface); r < 5.0 {
			t.Errorf("wash %s dark contrast = %.2f, want >= 5.0", w.id, r)
		}
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

	tester := ui.NewTester(a.View, 1200, 720)
	if !waitFor2(func() bool { return a.today.loaded && !a.today.loading }) {
		t.Fatal("today did not finish loading")
	}
	tester.Frame()

	mu.Lock()
	qAll := strings.Join(queries, "\n")
	mu.Unlock()
	// End of week (Sunday 2026-11-29) is queried.
	if !strings.Contains(qAll, `"contains":"user-42"`) || !strings.Contains(qAll, `"on_or_before":"2026-11-29"`) || !strings.Contains(qAll, `"does_not_equal":"Done"`) {
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
	offTester := ui.NewTester(offline.View, 1200, 720)
	offTester.Frame()
	if _, ok := offTester.Find("Kirim rilis"); !ok {
		t.Fatalf("offline cache item not shown; texts=%v", offTester.Texts())
	}

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

func TestTodayWeekStripCompleteDeferAndEmpty(t *testing.T) {
	nowFunc = func() time.Time { return time.Date(2026, 11, 25, 10, 0, 0, 0, time.Local) }
	t.Cleanup(func() { nowFunc = time.Now })

	var failPatch atomic.Bool
	var mu sync.Mutex
	var patches []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if r.Method == "PATCH" {
			if failPatch.Load() {
				w.WriteHeader(400)
				io.WriteString(w, `{"code":"validation_error","message":"nope"}`)
				return
			}
			mu.Lock()
			patches = append(patches, strings.TrimPrefix(r.URL.Path, "/v1")+" "+string(b))
			mu.Unlock()
		}
		io.WriteString(w, `{}`)
	}))
	defer srv.Close()

	st := sampleTodayStatusProp(t)
	a := tabsApp(t)
	a.client = notion.New("x")
	a.client.Base = srv.URL + "/v1"
	a.set.Wash = "hutan"
	a.today.loaded = true
	a.today.eligible = []todaySource{{ID: "ds-tasks", Title: "Proyek", TitleCol: "Nama", PeopleCol: "PIC", DateCol: "Tgl", DoneProp: st, StatusCol: "Status"}}
	a.today.items = []todayItem{
		mkTodayItem("r1", "Revisi proposal klien", "Proyek", "2026-11-23", "Doing", false, st),
		mkTodayItem("r2", "Standup tim", "Kerja", "2026-11-25T09:30:00+07:00", "Todo", false, st),
		mkTodayItem("r3", "Meeting Jumat", "Proyek", "2026-11-27", "Todo", false, st),
	}
	a.open(a.special(todayID), 0)

	tester := ui.NewTester(a.View, 1280, 760)
	tester.Frame()
	tester.Frame()

	// Save visual snapshots if SHOT is set.
	saveShot := func(name string) {
		if dir := os.Getenv("SHOT"); dir != "" {
			f, err := os.Create(filepath.Join(dir, name+".png"))
			if err == nil {
				png.Encode(f, tester.Image())
				f.Close()
			}
		}
	}
	saveShot("today-hutan-light")

	// Check week strip days & clicking Friday 27.
	if _, ok := tester.Find("0 dari 2 selesai"); !ok {
		t.Fatalf("missing progress text; texts=%v", tester.Texts())
	}
	if err := tester.Click("27"); err != nil {
		t.Fatal(err)
	}
	tester.Frame()
	if !tester.HasText("Jumat, 27 November · 1 tugas") {
		t.Fatalf("Friday tasks heading missing; texts=%v", tester.Texts())
	}
	if _, ok := tester.Find("Meeting Jumat"); !ok {
		t.Fatalf("Friday task not shown")
	}

	// Click back to today.
	if err := tester.Click("25"); err != nil {
		t.Fatal(err)
	}
	tester.Frame()

	// Defer focus card ("Standup tim" at 09:30) to tomorrow (2026-11-26) -> only 1 open left ("Revisi proposal klien").
	if err := tester.Click("Tunda ke besok"); err != nil {
		t.Fatal(err)
	}
	tester.Frame()
	if !waitFor2(func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(patches) > 0
	}) {
		t.Fatal("defer PATCH not sent")
	}

	// Rollback test: fail PATCH on Selesai.
	failPatch.Store(true)
	if err := tester.Click("Selesai"); err != nil {
		t.Fatal(err)
	}
	tester.Frame()
	if !waitFor2(func() bool { return !notion.Property.IsComplete(st, a.today.items[0].Page.Properties["Status"]) }) {
		t.Fatal("failed Selesai did not roll back")
	}
	failPatch.Store(false)

	// Succeed Selesai on remaining focus task -> empty state ("Semua beres hari ini" + "Besok ada 1 tugas.").
	if err := tester.Click("Selesai"); err != nil {
		t.Fatal(err)
	}
	tester.Frame()
	if _, ok := tester.Find("Semua beres hari ini"); !ok {
		t.Fatalf("empty state not shown; texts=%v", tester.Texts())
	}
	if !tester.HasText("Besok ada 1 tugas.") {
		t.Fatalf("tomorrow count missing; texts=%v", tester.Texts())
	}
	saveShot("today-empty")

	// Also render dark & aprikot & polos shots if SHOT is set.
	a.today.items[0].Page.Properties["Status"] = json.RawMessage(`{"type":"status","status":{"name":"Doing","color":"blue"}}`)
	a.today.doneToday = nil
	a.set.Theme = "dark"
	a.set.Wash = "hutan"
	tester.Frame()
	saveShot("today-hutan-dark")

	a.set.Theme = "light"
	a.set.Wash = "aprikot"
	tester.Frame()
	saveShot("today-aprikot-light")

	a.set.Wash = "polos"
	tester.Frame()
	saveShot("today-polos-light")
}
