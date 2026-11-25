package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"gotion/internal/notion"
)

func TestCaptureTargetSelection(t *testing.T) {
	a := New()
	db1 := &node{ID: "db1", Title: "Inbox", Kind: "data_source"}
	db2 := &node{ID: "db2", Title: "Tasks", Kind: "data_source"}
	pg1 := &node{ID: "pg1", Title: "Notes", Kind: "page"}
	pg2 := &node{ID: "pg2", Title: "Scratch", Kind: "page"}
	a.nodes["db1"] = db1
	a.nodes["db2"] = db2
	a.nodes["pg1"] = pg1
	a.nodes["pg2"] = pg2
	a.databases = []*node{db2, db1}
	a.groups = []*node{pg1, pg2}

	// Default fallback picks first sorted DB ("Inbox") and first group page ("Notes").
	id, label, col, ok := a.captureTarget("inbox")
	if !ok || id != "db1" || label != "Inbox" || col != "Name" {
		t.Fatalf("default inbox target = (%q, %q, %q, %v)", id, label, col, ok)
	}
	id, label, _, ok = a.captureTarget("page")
	if !ok || id != "pg1" || label != "Notes" {
		t.Fatalf("default page target = (%q, %q, %v)", id, label, ok)
	}

	// Configured IDs in settings override defaults.
	a.set.CaptureInboxID = "db2"
	a.set.CaptureParentID = "pg2"
	id, label, _, ok = a.captureTarget("inbox")
	if !ok || id != "db2" || label != "Tasks" {
		t.Fatalf("configured inbox target = (%q, %q, %v)", id, label, ok)
	}
	id, label, _, ok = a.captureTarget("page")
	if !ok || id != "pg2" || label != "Scratch" {
		t.Fatalf("configured page target = (%q, %q, %v)", id, label, ok)
	}

	// openCaptureWindow remembers last CaptureMode.
	a.set.CaptureMode = "page"
	a.openCaptureWindow()
	if a.set.CaptureMode != "page" {
		t.Fatalf("expected CaptureMode to stay 'page', got %q", a.set.CaptureMode)
	}
}

func TestCaptureOfflineQueueAndFlush(t *testing.T) {
	os.Remove(captureQueuePath())
	defer os.Remove(captureQueuePath())

	var mu sync.Mutex
	var created []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/data_sources/"):
			w.Write([]byte(`{"id":"ds1","properties":{"Task":{"name":"Task","type":"title"}}}`))
		case r.Method == "POST" && r.URL.Path == "/pages":
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			b, _ := json.Marshal(body)
			mu.Lock()
			created = append(created, string(b))
			mu.Unlock()
			w.Write([]byte(`{"id":"new-pg"}`))
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()

	a := New()
	var notes []notification
	a.sendNotify = func(n notification) { notes = append(notes, n) }

	// Enqueue two items while offline.
	a.enqueueCapture(captureItem{
		ID:       "c1",
		Mode:     "inbox",
		TargetID: "ds1",
		Target:   "Inbox",
		TitleCol: "Name",
		Title:    "Buy milk",
	})
	a.enqueueCapture(captureItem{
		ID:       "c2",
		Mode:     "page",
		TargetID: "pg1",
		Target:   "Notes",
		Title:    "Quick idea",
	})
	if len(notes) != 2 {
		t.Fatalf("expected 2 offline notifications, got %d", len(notes))
	}

	// Reload from disk to verify persistence across restarts.
	a2 := New()
	if len(a2.captureQueue) != 2 {
		t.Fatalf("expected 2 queued items from disk, got %d", len(a2.captureQueue))
	}

	// Point at closed server -> remains queued.
	closed := httptest.NewServer(http.NotFoundHandler())
	closed.Close()
	a2.client = notion.New("tok")
	a2.client.Base = closed.URL
	a2.flushCaptureQueue()
	deadline := time.Now().Add(2 * time.Second)
	for a2.captureFlushing && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if len(a2.captureQueue) != 2 {
		t.Fatalf("expected queue to remain 2 when offline, got %d", len(a2.captureQueue))
	}

	// Point at live fake server -> flushes both items and clears queue.
	a2.client.Base = srv.URL
	a2.flushCaptureQueue()
	deadline = time.Now().Add(2 * time.Second)
	for (a2.captureFlushing || len(a2.captureQueue) > 0) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if len(a2.captureQueue) != 0 {
		t.Fatalf("expected empty queue after flush, got %d", len(a2.captureQueue))
	}
	mu.Lock()
	defer mu.Unlock()
	if len(created) != 2 {
		t.Fatalf("expected 2 created pages on server, got %d (%v)", len(created), created)
	}
	if !strings.Contains(created[0], `"Task"`) || !strings.Contains(created[0], `"Buy milk"`) {
		t.Fatalf("unexpected row payload: %s", created[0])
	}
	if !strings.Contains(created[1], `"pg1"`) || !strings.Contains(created[1], `"Quick idea"`) {
		t.Fatalf("unexpected page payload: %s", created[1])
	}
}

func TestCaptureViewModeToggle(t *testing.T) {
	a := New()
	a.nodes["db1"] = &node{ID: "db1", Title: "Inbox", Kind: "data_source"}
	a.nodes["pg1"] = &node{ID: "pg1", Title: "Notes", Kind: "page"}
	a.databases = []*node{a.nodes["db1"]}
	a.groups = []*node{a.nodes["pg1"]}

	tt := ui.NewTester(a.captureView, 520, 176)
	tt.Click("Halaman baru: Notes")
	if a.set.CaptureMode != "page" {
		t.Fatalf("expected CaptureMode 'page' after click, got %q", a.set.CaptureMode)
	}
}
