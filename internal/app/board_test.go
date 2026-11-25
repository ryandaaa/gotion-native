package app

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"gotion/internal/notion"
)

func TestBoardGroupingOrderAndMove(t *testing.T) {
	// Status options intentionally out of order; groups define To-do -> In progress -> Complete.
	var statusProp notion.Property
	err := json.Unmarshal([]byte(`{
		"id":"st","name":"Status","type":"status",
		"status":{
			"options":[
				{"id":"o3","name":"Done","color":"green"},
				{"id":"o1","name":"Todo","color":"default"},
				{"id":"o2","name":"Doing","color":"yellow"}
			],
			"groups":[
				{"id":"g3","name":"Complete","color":"green","option_ids":["o3"]},
				{"id":"g1","name":"To-do","color":"gray","option_ids":["o1"]},
				{"id":"g2","name":"In progress","color":"blue","option_ids":["o2"]}
			]
		}
	}`), &statusProp)
	if err != nil {
		t.Fatal(err)
	}

	a, tv, rec, tester := editApp(t)
	tv.columns = []notion.Property{
		{Name: "Nama", Type: "title"},
		statusProp,
		{Name: "Prioritas", Type: "select", Options: []notion.Option{{Name: "Tinggi", Color: "red"}, {Name: "Rendah", Color: "gray"}}},
	}
	// Make row 3 unassigned for Status.
	tv.rows[2].Properties["Status"] = json.RawMessage(`{"type":"status","status":null}`)
	a.set.Views = map[string]string{"tasks": "board"}
	tester.Frame()

	// Check lane order: Todo < Doing < Done < Tanpa Status.
	rTodo, ok1 := tester.Find("Todo")
	rDoing, ok2 := tester.Find("Doing")
	rDone, ok3 := tester.Find("Done")
	rNone, ok4 := tester.Find("Tanpa Status")
	if !ok1 || !ok2 || !ok3 || !ok4 {
		t.Fatalf("missing lanes: Todo=%v Doing=%v Done=%v Tanpa=%v; texts=%v", ok1, ok2, ok3, ok4, tester.Texts())
	}
	if !(rTodo.X < rDoing.X && rDoing.X < rDone.X && rDone.X < rNone.X) {
		t.Fatalf("unexpected lane X order: Todo=%.0f Doing=%.0f Done=%.0f Tanpa=%.0f", rTodo.X, rDoing.X, rDone.X, rNone.X)
	}

	// Drag "Sync engine" (currently in Doing) into Done lane.
	card, ok := tester.Find("Sync engine")
	if !ok {
		t.Fatal("Sync engine card not found")
	}
	fromX, fromY := card.X+card.W/2, card.Y+card.H/2
	toX, toY := rDone.X+40, rDone.Y+80
	tester.Press(fromX, fromY)
	for step := 1; step <= 4; step++ {
		f := float32(step) / 4
		tester.Move(fromX+(toX-fromX)*f, fromY+(toY-fromY)*f)
	}
	tester.Release(toX, toY)
	tester.Frame()

	if got := notion.FormatProperty(tv.rows[1].Properties["Status"]); got != "Done" {
		t.Errorf("optimistic status = %q, want %q", got, "Done")
	}
	if !waitSent(rec, `PATCH /pages/r2 {"properties":{"Status":{"status":{"color":"green","name":"Done"}}}}`) {
		t.Errorf("sent:\n%s", rec.all())
	}

	// Drag "Halaman login" (currently in Done) to "Tanpa Status" clears it.
	card1, ok := tester.Find("Halaman login")
	if !ok {
		t.Fatal("Halaman login card not found")
	}
	fx1, fy1 := card1.X+card1.W/2, card1.Y+card1.H/2
	tx1, ty1 := rNone.X+40, rNone.Y+80
	tester.Press(fx1, fy1)
	for step := 1; step <= 4; step++ {
		f := float32(step) / 4
		tester.Move(fx1+(tx1-fx1)*f, fy1+(ty1-fy1)*f)
	}
	tester.Release(tx1, ty1)
	tester.Frame()

	if got := notion.FormatProperty(tv.rows[0].Properties["Status"]); got != "" {
		t.Errorf("cleared status = %q, want empty", got)
	}
	if !waitSent(rec, `PATCH /pages/r1 {"properties":{"Status":{"status":null}}}`) {
		t.Errorf("sent:\n%s", rec.all())
	}
}

func TestBoardMoveRollbackAndNoGroupColumn(t *testing.T) {
	a, tv, _, tester := editApp(t)
	// Point client to failing server to verify rollback + toast.
	failSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		io.WriteString(w, `{"code":"validation_error","message":"denied"}`)
	}))
	defer failSrv.Close()
	a.client.Base = failSrv.URL + "/v1"

	a.set.Views = map[string]string{"tasks": "board"}
	tester.Frame()

	card, _ := tester.Find("Sync engine")
	rDone, _ := tester.Find("Done")
	fx, fy := card.X+card.W/2, card.Y+card.H/2
	tx, ty := rDone.X+40, rDone.Y+80
	tester.Press(fx, fy)
	for step := 1; step <= 4; step++ {
		f := float32(step) / 4
		tester.Move(fx+(tx-fx)*f, fy+(ty-fy)*f)
	}
	tester.Release(tx, ty)
	tester.Frame()

	for i := 0; i < 60 && notion.FormatProperty(tv.rows[1].Properties["Status"]) != "Doing"; i++ {
		time.Sleep(50 * time.Millisecond)
	}
	if got := notion.FormatProperty(tv.rows[1].Properties["Status"]); got != "Doing" {
		t.Errorf("after failed PATCH, status = %q, want rollback to Doing", got)
	}
	if len(a.toasts) == 0 && !tester.HasText("Gagal menyimpan Status") {
		t.Errorf("expected error toast, got toasts=%v texts=%v", a.toasts, tester.Texts())
	}

	// Database without any status or select column shows prompt + button.
	tv.columns = []notion.Property{{Name: "Nama", Type: "title"}}
	tester.Frame()
	if _, ok := tester.Find("Buat kolom Status"); !ok {
		t.Fatalf("missing Create Status column button; texts=%v", tester.Texts())
	}
}

func TestBoardAddRowInLane(t *testing.T) {
	a, tv, rec, tester := editApp(t)
	a.set.Views = map[string]string{"tasks": "board"}
	tester.Frame()

	// Click "Baris baru" inside the "Doing" lane (second lane).
	rDoing, ok := tester.Find("Doing")
	if !ok {
		t.Fatal("Doing lane not found")
	}
	// Click near the bottom of the Doing lane's add-card button by setting adding directly
	// or locating the button in that lane's X range.
	_ = rDoing
	tv.adding = newRow{open: true, groupCol: "Status", groupVal: "Doing"}
	tester.Frame()
	tester.Type("Tugas Board")
	tester.Key(0, ui.KeyEnter)

	if !waitSent(rec, `"Status":{"status":{"color":"yellow","name":"Doing"}}`) || !strings.Contains(rec.all(), `"content":"Tugas Board"`) {
		t.Fatalf("sent:\n%s", rec.all())
	}
}
