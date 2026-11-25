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

// recorder is a Notion that records what it is sent.
type recorder struct {
	mu   sync.Mutex
	sent []string
}

func (r *recorder) ServeHTTP(w http.ResponseWriter, q *http.Request) {
	b, _ := io.ReadAll(q.Body)
	r.mu.Lock()
	r.sent = append(r.sent, q.Method+" "+strings.TrimPrefix(q.URL.Path, "/v1")+" "+string(b))
	r.mu.Unlock()
	io.WriteString(w, `{"object":"page","id":"new-row","properties":{"Nama":{"type":"title","title":[{"plain_text":"Rapat"}]}}}`)
}

func (r *recorder) all() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return strings.Join(r.sent, "\n")
}

func editApp(t *testing.T) (*App, *tableView, *recorder, *ui.Tester) {
	var rows []*notion.Page
	if err := json.Unmarshal([]byte(sampleRows), &rows); err != nil {
		t.Fatal(err)
	}
	rec := &recorder{}
	srv := httptest.NewServer(rec)
	t.Cleanup(srv.Close)
	a := tableApp(t)
	a.client = notion.New("x")
	a.client.Base = srv.URL + "/v1"
	a.nodes["tasks"] = &node{ID: "tasks", Title: "Tasks", Kind: "data_source"}
	a.regroup()
	cols := []notion.Property{
		{Name: "Nama", Type: "title"},
		{Name: "Selesai", Type: "checkbox"},
		{Name: "Status", Type: "status", Options: []notion.Option{{Name: "Todo", Color: "default"}, {Name: "Doing", Color: "yellow"}, {Name: "Done", Color: "green"}, {Name: "Blocked", Color: "red"}}},
		{Name: "Deadline", Type: "date"},
	}
	tv := &tableView{id: "tasks", dsID: "ds1", title: "Tasks", columns: cols, rows: rows}
	a.panels[0] = &panel{id: "tasks", table: tv}
	tester := ui.NewTester(a.View, 1280, 700)
	tester.Frame()
	return a, tv, rec, tester
}

func waitSent(rec *recorder, want string) bool {
	for i := 0; i < 60; i++ {
		if strings.Contains(rec.all(), want) {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false
}

func TestEditStatusFromTheTable(t *testing.T) {
	_, tv, rec, tester := editApp(t)
	// The second row's status is Doing; pick Blocked, in its popover only.
	if err := tester.Click("Doing"); err != nil {
		t.Fatal(err)
	}
	tester.Advance(time.Second)
	if err := tester.Click("Blocked"); err != nil {
		t.Fatalf("no option in the popover: %v; texts %v", err, tester.Texts())
	}
	tester.Frame()
	if got := notion.FormatProperty(tv.rows[1].Properties["Status"]); got != "Blocked" {
		t.Errorf("shown %q", got)
	}
	if !waitSent(rec, `PATCH /pages/r2 {"properties":{"Status":{"status":{"color":"red","name":"Blocked"}}}}`) {
		t.Errorf("sent:\n%s", rec.all())
	}
}

func TestAddRow(t *testing.T) {
	_, tv, rec, tester := editApp(t)
	if err := tester.Click("Baris baru"); err != nil {
		t.Fatal(err)
	}
	tester.Frame()
	tester.Type("Rapat")
	tester.Key(0, ui.KeyEnter)
	if !waitSent(rec, `"parent":{"data_source_id":"ds1","type":"data_source_id"}`) || !strings.Contains(rec.all(), `"content":"Rapat"`) {
		t.Fatalf("sent:\n%s", rec.all())
	}
	for i := 0; i < 40 && len(tv.rows) < 4; i++ {
		time.Sleep(50 * time.Millisecond)
	}
	if len(tv.rows) != 4 {
		t.Errorf("%d rows", len(tv.rows))
	}
}

func TestToggleCheckboxCell(t *testing.T) {
	a, tv, rec, tester := editApp(t)
	_ = a
	// Click the check box cell of the first row (checked): it unchecks.
	before := notion.FormatProperty(tv.rows[0].Properties["Selesai"])
	r, ok := tester.Find("Halaman login")
	if !ok {
		t.Fatal("no first row")
	}
	// The check box column comes right after the title's 300 DIPs.
	tester.ClickAt(r.X+300, r.Y+r.H/2)
	tester.Frame()
	after := notion.FormatProperty(tv.rows[0].Properties["Selesai"])
	if before != "☑" || after != "☐" {
		t.Errorf("before %q after %q", before, after)
	}
	if !waitSent(rec, `{"properties":{"Selesai":{"checkbox":false}}}`) {
		t.Errorf("sent:\n%s", rec.all())
	}
}

func TestRefusedEditGoesBack(t *testing.T) {
	a, tv, _, tester := editApp(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		io.WriteString(w, `{"code":"validation_error","message":"nope"}`)
	}))
	defer srv.Close()
	a.client.Base = srv.URL + "/v1"
	r, _ := tester.Find("Halaman login")
	tester.ClickAt(r.X+300, r.Y+r.H/2)
	for i := 0; i < 60 && notion.FormatProperty(tv.rows[0].Properties["Selesai"]) != "☑"; i++ {
		time.Sleep(50 * time.Millisecond)
	}
	if got := notion.FormatProperty(tv.rows[0].Properties["Selesai"]); got != "☑" {
		t.Errorf("a refused edit stayed: %q", got)
	}
	if len(a.toasts) == 0 || !strings.Contains(a.toasts[0], "Gagal menyimpan Selesai") {
		t.Errorf("toasts %v", a.toasts)
	}
}

func TestEditorShot(t *testing.T) {
	_, _, _, tester := editApp(t)
	tester.Click("Doing")
	tester.Advance(time.Second)
	snapshot(t, tester, "db-edit-status")
}

func TestFilterAndSortQuery(t *testing.T) {
	a, tv, rec, tester := editApp(t)
	_ = a
	tv.query.conds = []condition{{prop: "Status", value: "Done"}, {prop: "Selesai", value: "true"}}
	tv.query.sortProp, tv.query.sortDesc = "Nama", true
	a.requery(tv)
	want := `POST /data_sources/ds1/query {"filter":{"and":[{"property":"Status","status":{"equals":"Done"}},{"checkbox":{"equals":true},"property":"Selesai"}]},"page_size":100,"sorts":[{"direction":"descending","property":"Nama"}]}`
	if !waitSent(rec, want) {
		t.Errorf("sent:\n%s", rec.all())
	}
	tv.query.filterOpen = true
	tester.Frame()
	tester.Advance(time.Second)
	snapshot(t, tester, "db-filter")
	if _, ok := tester.Find("Tambah filter"); !ok {
		t.Error("no filter editor")
	}
}

func TestSchemaLinksTemplates(t *testing.T) {
	a, tv, rec, _ := editApp(t)
	col, _ := tv.column("Status")
	a.changeSchema(tv, map[string]any{"Biaya": map[string]any{"number": map[string]any{}}}, "ok")
	a.changeSchema(tv, map[string]any{"Status": map[string]any{"name": "Tahap"}}, "ok")
	a.changeSchema(tv, map[string]any{"Selesai": nil}, "ok")
	for _, want := range []string{
		`PATCH /data_sources/ds1 {"properties":{"Biaya":{"number":{}}}}`,
		`PATCH /data_sources/ds1 {"properties":{"Status":{"name":"Tahap"}}}`,
		`PATCH /data_sources/ds1 {"properties":{"Selesai":null}}`,
	} {
		if !waitSent(rec, want) {
			t.Errorf("missing %s in\n%s", want, rec.all())
		}
	}
	// A new option keeps the others: Notion takes the whole list.
	a.addOption(tv, tv.rows[0], col, "Ditunda")
	if !waitSent(rec, `{"name":"Blocked"},{"name":"Ditunda"}]}}}}`) {
		t.Errorf("option:\n%s", rec.all())
	}
	people := notion.Property{Name: "PIC", Type: "people"}
	a.setLinks(tv, tv.rows[0], people, []notion.Linked{{ID: "u1", Name: "Ryan"}})
	if !waitSent(rec, `{"properties":{"PIC":{"people":[{"id":"u1","object":"user"}]}}}`) {
		t.Errorf("people:\n%s", rec.all())
	}
	if got := notion.FormatProperty(tv.rows[0].Properties["PIC"]); got != "Ryan" {
		t.Errorf("shown %q", got)
	}
	rel := notion.Property{Name: "Proyek", Type: "relation", Target: "ds2"}
	a.setLinks(tv, tv.rows[0], rel, []notion.Linked{{ID: "pg9", Name: "Gotion"}})
	if !waitSent(rec, `{"properties":{"Proyek":{"relation":[{"id":"pg9"}]}}}`) {
		t.Errorf("relation:\n%s", rec.all())
	}
	tv.templates = []notion.Template{{ID: "tpl1", Name: "Rapat"}}
	tv.adding = newRow{open: true, title: "Rapat Senin", template: "Rapat"}
	a.createRow(0, tv)
	if !waitSent(rec, `"template":{"template_id":"tpl1","type":"template_id"}`) {
		t.Errorf("template:\n%s", rec.all())
	}
}

func TestAddColumnShot(t *testing.T) {
	_, tv, _, tester := editApp(t)
	tv.schema.adding, tv.schema.newType = true, "rich_text"
	tester.Frame()
	tester.Advance(time.Second)
	snapshot(t, tester, "db-add-column")
	if _, ok := tester.Find("Tambah kolom"); !ok {
		t.Error("no add column popover")
	}
}

func TestRenameDatabaseInline(t *testing.T) {
	a, tv, rec, tester := editApp(t)
	tv.dbID = "db1"
	if err := tester.Click("Judul database"); err != nil {
		t.Fatal(err)
	}
	tester.Key(ui.Cmd, ui.KeyA)
	tester.Type("Daftar Tugas")
	tester.Key(0, ui.KeyEnter)
	tester.Frame()
	if tv.title != "Daftar Tugas" {
		t.Errorf("tv.title = %q, want %q", tv.title, "Daftar Tugas")
	}
	if a.nodes["tasks"].Title != "Daftar Tugas" {
		t.Errorf("node title = %q, want %q", a.nodes["tasks"].Title, "Daftar Tugas")
	}
	for _, want := range []string{
		`PATCH /data_sources/ds1 {"title":[{"text":{"content":"Daftar Tugas"},"type":"text"}]}`,
		`PATCH /databases/db1 {"title":[{"text":{"content":"Daftar Tugas"},"type":"text"}]}`,
	} {
		if !waitSent(rec, want) {
			t.Errorf("missing %s in:\n%s", want, rec.all())
		}
	}
}

func TestEditDateWithCalendar(t *testing.T) {
	nowFunc = func() time.Time { return time.Date(2026, 11, 24, 9, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { nowFunc = time.Now })

	a, tv, rec, tester := editApp(t)
	if _, ok := tester.Find("20 November 2026"); !ok {
		t.Fatalf("formatted date not shown; texts: %v", tester.Texts())
	}
	if err := tester.Click("20 November 2026"); err != nil {
		t.Fatal(err)
	}
	tester.Advance(time.Second)
	snapshot(t, tester, "db-edit-date")
	a.set.Theme = "dark"
	tester.Frame()
	snapshot(t, tester, "db-edit-date-dark")
	a.set.Theme = "light"
	tester.Frame()

	if err := tester.Click("Next month"); err != nil {
		t.Fatal(err)
	}
	tester.Frame()
	if !tv.edit.open {
		t.Fatal("popover closed when browsing next month")
	}
	if _, ok := tester.Find("December 2026"); !ok {
		t.Fatalf("December 2026 not shown; texts: %v", tester.Texts())
	}
	if err := tester.Click("December 15, 2026"); err != nil {
		t.Fatal(err)
	}
	tester.Frame()
	if tv.edit.open {
		t.Error("popover stayed open after picking a calendar day")
	}
	if got := notion.FormatProperty(tv.rows[0].Properties["Deadline"]); got != "15 Desember 2026" {
		t.Errorf("shown %q, want %q", got, "15 Desember 2026")
	}
	if !waitSent(rec, `PATCH /pages/r1 {"properties":{"Deadline":{"date":{"start":"2026-12-15"}}}}`) {
		t.Errorf("sent:\n%s", rec.all())
	}

	if err := tester.Click("15 Desember 2026"); err != nil {
		t.Fatal(err)
	}
	tester.Advance(time.Second)
	if err := tester.Click("Hari ini"); err != nil {
		t.Fatal(err)
	}
	tester.Frame()
	if got := notion.FormatProperty(tv.rows[0].Properties["Deadline"]); got != "24 November 2026" {
		t.Errorf("today shown %q, want %q", got, "24 November 2026")
	}
	if !waitSent(rec, `PATCH /pages/r1 {"properties":{"Deadline":{"date":{"start":"2026-11-24"}}}}`) {
		t.Errorf("sent:\n%s", rec.all())
	}

	if err := tester.Click("24 November 2026"); err != nil {
		t.Fatal(err)
	}
	tester.Advance(time.Second)
	if err := tester.Click("Kosongkan"); err != nil {
		t.Fatal(err)
	}
	tester.Frame()
	if got := notion.FormatProperty(tv.rows[0].Properties["Deadline"]); got != "" {
		t.Errorf("cleared shown %q, want empty", got)
	}
	if !waitSent(rec, `PATCH /pages/r1 {"properties":{"Deadline":{"date":null}}}`) {
		t.Errorf("sent:\n%s", rec.all())
	}
}

func TestEmptyDatabaseTable(t *testing.T) {
	_, tv, rec, tester := editApp(t)
	tv.rows = nil
	tv.columns = nil
	tester.Frame()

	for _, want := range []string{"Name", "Kolom baru", "Baris baru", "Database ini masih kosong"} {
		if _, ok := tester.Find(want); !ok {
			t.Fatalf("missing %q in empty table; texts: %v", want, tester.Texts())
		}
	}
	if err := tester.Click("Baris baru"); err != nil {
		t.Fatal(err)
	}
	tester.Frame()
	tester.Type("Baris Pertama")
	tester.Key(0, ui.KeyEnter)
	if !waitSent(rec, `"content":"Baris Pertama"`) {
		t.Fatalf("sent:\n%s", rec.all())
	}
	for i := 0; i < 40 && len(tv.rows) < 1; i++ {
		time.Sleep(50 * time.Millisecond)
	}
	if len(tv.rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(tv.rows))
	}
}

func TestEmptyDatabaseCards(t *testing.T) {
	a, tv, rec, tester := editApp(t)
	tv.rows = nil
	a.set.Views = map[string]string{"tasks": "cards"}
	tester.Frame()

	for _, want := range []string{"Baris baru", "Database ini masih kosong"} {
		if _, ok := tester.Find(want); !ok {
			t.Fatalf("missing %q in empty cards; texts: %v", want, tester.Texts())
		}
	}
	if err := tester.Click("Baris baru"); err != nil {
		t.Fatal(err)
	}
	tester.Frame()
	tester.Type("Kartu Pertama")
	tester.Key(0, ui.KeyEnter)
	if !waitSent(rec, `"content":"Kartu Pertama"`) {
		t.Fatalf("sent:\n%s", rec.all())
	}
	for i := 0; i < 40 && len(tv.rows) < 1; i++ {
		time.Sleep(50 * time.Millisecond)
	}
	if len(tv.rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(tv.rows))
	}
}
