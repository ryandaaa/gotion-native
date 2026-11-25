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
	"github.com/egoist/mygo/ui/richtext"

	"gotion/internal/doc"
	"gotion/internal/notion"
)

func TestSlashToggleCalloutAndReloadFromCache(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/blocks/p1/children"):
			w.Write([]byte(`{"results":[],"has_more":false}`))
		case r.Method == "PATCH" && strings.HasSuffix(r.URL.Path, "/blocks/p1/children"):
			w.Write([]byte(`{"results":[{"object":"block","id":"b-new","type":"toggle","toggle":{"rich_text":[{"type":"text","plain_text":"halo"}]}}]}`))
		default:
			w.Write([]byte(`{}`))
		}
	}))
	defer srv.Close()

	c := notion.New("tok")
	c.Base = srv.URL
	a := &App{
		client: c,
		panels: [2]*panel{{id: "p1"}},
		set:    settings{Toggles: map[string]bool{}},
	}
	pn := a.panels[0]
	pn.id = "p1"
	pn.doc = richtext.NewDocument(richtext.TextOf(richtext.Paragraph{Style: richtext.ParagraphStyle{ID: doc.NewID()}}))
	pn.doc.InsertText("/toggle")
	a.slashUpdate(pn)
	if !pn.slash.open {
		t.Fatalf("slash menu did not open for /toggle")
	}
	a.slashKey(pn, ui.KeyEnter, 0)
	pn.doc.InsertText("halo")

	items := doc.Items(pn.doc.Text(), map[string]*doc.Known{})
	if len(items) == 0 || items[0].Type != "toggle" {
		t.Fatalf("expected toggle item after slash command, got %+v", items)
	}
}

func TestTableCellEditRollbackAndOffline(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "PATCH" && strings.Contains(r.URL.Path, "/blocks/row1") {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"message":"validation_error"}`))
			return
		}
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c := notion.New("tok")
	c.Base = srv.URL
	a := &App{
		client: c,
		panels: [2]*panel{{id: "p1"}},
		set:    settings{Toggles: map[string]bool{}},
	}
	pn := a.panels[0]
	meta := &doc.TableMeta{
		BlockID: "tbl1",
		RowIDs:  []string{"row1"},
		Rows:    [][]string{{"old", ""}},
		Cols:    2,
	}

	// Offline (no client): should remain read-only.
	savedClient := a.client
	a.client = nil
	a.saveTableCell(pn, meta, 0, 0, "new")
	if meta.Rows[0][0] != "old" {
		t.Fatalf("offline edit should not mutate cell, got %q", meta.Rows[0][0])
	}

	// Online, Notion rejects: optimistic update then rollback + toast.
	a.client = savedClient
	a.saveTableCell(pn, meta, 0, 0, "new")
	if meta.Rows[0][0] != "new" {
		t.Fatalf("expected optimistic update to 'new', got %q", meta.Rows[0][0])
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && meta.Rows[0][0] != "old" {
		time.Sleep(10 * time.Millisecond)
	}
	if meta.Rows[0][0] != "old" {
		t.Fatalf("expected rollback to 'old', got %q", meta.Rows[0][0])
	}
}

func TestDeepNestedDeletedInNotion(t *testing.T) {
	// Depth 5 block exists initially, then is deleted in Notion on reload.
	leaf := &notion.Block{ID: "d5", Type: "paragraph", Payload: json.RawMessage(`{"rich_text":[{"type":"text","plain_text":"deep"}]}`)}
	cur := leaf
	for d := 4; d >= 0; d-- {
		cur = &notion.Block{
			ID:          "d" + string(rune('0'+d)),
			Type:        "bulleted_list_item",
			HasChildren: true,
			Payload:     json.RawMessage(`{"rich_text":[{"type":"text","plain_text":"item"}]}`),
			Children:    []*notion.Block{cur},
		}
	}
	_, known1, _ := doc.Build("p1", []*notion.Block{cur}, doc.Shape{})
	if known1["d5"] == nil {
		t.Fatalf("expected d5 in known1")
	}
	// Simulate d5 deleted in Notion.
	cur.Children[0].Children[0].Children[0].Children[0].Children = nil
	_, known2, _ := doc.Build("p1", []*notion.Block{cur}, doc.Shape{})
	if known2["d5"] != nil {
		t.Fatalf("expected d5 to be absent after deletion in Notion")
	}
}

// Saving one cell sends the row's other cells back as Notion has them, bold
// and links included, and a second save keeps the first edit.
func TestTableCellEditKeepsOtherCells(t *testing.T) {
	var bodies []string
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(b))
		mu.Unlock()
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	c := notion.New("tok")
	c.Base = srv.URL
	a := &App{client: c, panels: [2]*panel{{id: "p1"}}, set: settings{Toggles: map[string]bool{}}}
	bold := json.RawMessage(`[{"type":"text","text":{"content":"tebal"},"annotations":{"bold":true}}]`)
	meta := &doc.TableMeta{BlockID: "tbl", RowIDs: []string{"r1"}, Rows: [][]string{{"a", "tebal", "c"}},
		Raw: [][]json.RawMessage{{nil, bold, nil}}, Cols: 3}
	a.saveTableCell(a.panels[0], meta, 0, 0, "A")
	waitUntil(t, "first save", func() bool { mu.Lock(); defer mu.Unlock(); return len(bodies) == 1 })
	a.saveTableCell(a.panels[0], meta, 0, 2, "C")
	waitUntil(t, "second save", func() bool { mu.Lock(); defer mu.Unlock(); return len(bodies) == 2 })
	mu.Lock()
	defer mu.Unlock()
	for i, b := range bodies {
		if !strings.Contains(b, `"bold":true`) {
			t.Fatalf("save %d lost the bold cell: %s", i, b)
		}
	}
	if !strings.Contains(bodies[1], `"content":"A"`) || !strings.Contains(bodies[1], `"content":"C"`) {
		t.Fatalf("second save reverted the first edit: %s", bodies[1])
	}
}
