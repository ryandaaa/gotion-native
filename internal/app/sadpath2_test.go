package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo"

	"gotion/internal/notion"
)

// Closing the window must never hide it when there is no tray to bring it
// back from: on a desktop without one the app would be lost.
func TestSadStayInTrayWithoutTray(t *testing.T) {
	a := New()
	a.set.StayInTray = true
	if a.closeHides() {
		t.Fatal("window hidden with no tray")
	}
	a.tray = &mygo.Tray{}
	if !a.closeHides() {
		t.Fatal("window not hidden with a tray")
	}
	a.quitting = true
	if a.closeHides() {
		t.Fatal("window hidden while quitting")
	}
}

// A page's old comments are not news: the first comment event on a page
// notifies only the comments written since gotion started.
func TestSadOldCommentsNeverNotify(t *testing.T) {
	a, got := newTestAppForNotify(t)
	a.bot = &notion.User{ID: "u1", Name: "gotion", Type: "bot"}
	a.notifyFrom = time.Now()
	own := &notion.Page{ID: "pg", CreatedBy: notion.User{ID: "u1"}}
	old := notion.Comment{ID: "old", CreatedTime: time.Now().Add(-72 * time.Hour), CreatedBy: notion.User{ID: "u2"}}
	before := notion.Comment{ID: "before", CreatedTime: time.Now().Add(-10 * time.Minute), CreatedBy: notion.User{ID: "u2"}}
	fresh := notion.Comment{ID: "fresh", CreatedTime: time.Now(), CreatedBy: notion.User{ID: "u2"},
		RichText: []notion.RichText{{Type: "text", PlainText: "baru"}}}
	a.processCommentNotifications("pg", own, []notion.Comment{old, before, fresh})
	if len(*got) != 1 || (*got)[0].Body != "baru" {
		t.Fatalf("want only the fresh comment, got %+v", *got)
	}
	// A week later, when the dedupe record is pruned, still nothing again.
	a.notified.Comments = map[string]string{}
	a.processCommentNotifications("pg", own, []notion.Comment{old, before})
	if len(*got) != 1 {
		t.Fatalf("old comments notified after prune: %+v", *got)
	}
	// The integration's own name is not the person's: "@gotion" is no mention.
	other := &notion.Page{ID: "pg2", CreatedBy: notion.User{ID: "u3"}}
	a.processCommentNotifications("pg2", other, []notion.Comment{{ID: "n", CreatedTime: time.Now(),
		CreatedBy: notion.User{ID: "u2"}, RichText: []notion.RichText{{Type: "text", PlainText: "@gotion halo"}}}})
	if len(*got) != 1 {
		t.Fatalf("bot name taken as a mention: %+v", *got)
	}
}

func refusingServer(t *testing.T, posts *int32) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/data_sources/"):
			w.Write([]byte(`{"id":"ds1","properties":{"Name":{"name":"Name","type":"title"}}}`))
		case r.Method == "POST" && r.URL.Path == "/pages":
			atomic.AddInt32(posts, 1)
			w.WriteHeader(400)
			w.Write([]byte(`{"object":"error","status":400,"code":"validation_error","message":"Name is not a property that exists."}`))
		default:
			w.WriteHeader(404)
			w.Write([]byte(`{"object":"error","status":404,"code":"object_not_found","message":"no"}`))
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func waitUntil(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// Notion refusing a quick capture must give the words back, not drop them.
func TestSadCaptureRefusedKeepsWords(t *testing.T) {
	var posts int32
	srv := refusingServer(t, &posts)
	a := New()
	a.nodes["ds1"] = &node{ID: "ds1", Title: "Inbox", Kind: "data_source"}
	a.databases = []*node{a.nodes["ds1"]}
	a.set.CaptureMode = "inbox"
	a.client = notion.New("tok")
	a.client.Base = srv.URL
	a.capture.title = "Beli kopi"
	a.submitCapture()
	waitUntil(t, "the refusal", func() bool { return a.capture.err != "" })
	if a.capture.title != "Beli kopi" {
		t.Fatalf("words lost: %q", a.capture.title)
	}
	if len(a.captureQueue) != 0 {
		t.Fatalf("a refused capture was queued: %+v", a.captureQueue)
	}
}

// No destination at all: say so in the capture window, keep the words.
func TestSadCaptureNoTarget(t *testing.T) {
	a := New()
	a.capture.title = "Ide"
	a.submitCapture()
	if a.capture.err == "" || a.capture.title != "Ide" {
		t.Fatalf("err %q title %q", a.capture.err, a.capture.title)
	}
}

// A queued capture Notion refuses on flush leaves the queue, and the user is
// told its words instead of losing them silently.
func TestSadCaptureQueueRefusedIsReported(t *testing.T) {
	os.Remove(captureQueuePath())
	defer os.Remove(captureQueuePath())
	var posts int32
	srv := refusingServer(t, &posts)
	a := New()
	var notes []notification
	a.sendNotify = func(n notification) { notes = append(notes, n) }
	a.enqueueCapture(captureItem{ID: "q1", Mode: "inbox", TargetID: "ds1", Target: "Inbox", Title: "Catatan rapat"})
	notes = nil
	a.client = notion.New("tok")
	a.client.Base = srv.URL
	a.flushCaptureQueue()
	waitUntil(t, "the flush", func() bool { return !a.captureFlushing && len(a.captureQueue) == 0 })
	if len(notes) != 1 || notes[0].Body != "Catatan rapat" {
		t.Fatalf("refused capture not reported: %+v", notes)
	}
	if b, err := os.ReadFile(captureQueuePath()); err == nil && len(b) > 0 {
		t.Fatalf("queue file left behind: %s", b)
	}
}

// A damaged queue file is ignored, not a crash.
func TestSadCaptureQueueDamaged(t *testing.T) {
	os.WriteFile(captureQueuePath(), []byte("{not json"), 0o600)
	defer os.Remove(captureQueuePath())
	a := New()
	if len(a.captureQueue) != 0 {
		t.Fatalf("damaged queue loaded: %+v", a.captureQueue)
	}
}

// A damaged notified.json starts fresh.
func TestSadNotifiedDamaged(t *testing.T) {
	os.WriteFile(notifiedPath(), []byte("[[["), 0o600)
	defer os.Remove(notifiedPath())
	a := New()
	if a.notified.Due == nil || a.notified.Comments == nil {
		t.Fatal("notified maps missing after a damaged file")
	}
}

// A range moved across a daylight-saving change keeps its length in days.
func TestSadDeferDateAcrossDST(t *testing.T) {
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skip("no tz data")
	}
	p := notion.Property{ID: "d", Name: "Tanggal", Type: "date"}
	raw := json.RawMessage(`{"type":"date","date":{"start":"2026-03-02","end":"2026-03-04"}}`)
	// DST starts 8 March 2026 in New York.
	target := time.Date(2026, 3, 7, 0, 0, 0, 0, ny)
	send, _, err := notion.DeferDate(p, raw, target)
	if err != nil {
		t.Fatal(err)
	}
	d := send.(map[string]any)["date"].(map[string]any)
	if d["start"] != "2026-03-07" || d["end"] != "2026-03-09" {
		t.Fatalf("moved to %v – %v, want 2026-03-07 – 2026-03-09", d["start"], d["end"])
	}
	// A timed range keeps its hours too.
	raw = json.RawMessage(`{"type":"date","date":{"start":"2026-03-02T09:00:00-05:00","end":"2026-03-03T17:00:00-05:00"}}`)
	send, _, _ = notion.DeferDate(p, raw, target)
	d = send.(map[string]any)["date"].(map[string]any)
	if !strings.HasPrefix(d["start"].(string), "2026-03-07T09:00") || !strings.HasPrefix(d["end"].(string), "2026-03-08T17:00") {
		t.Fatalf("timed range moved to %v – %v", d["start"], d["end"])
	}
}

func TestSadCalendarDaysBetweenDST(t *testing.T) {
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skip("no tz data")
	}
	a := time.Date(2026, 3, 7, 0, 0, 0, 0, ny)
	b := time.Date(2026, 3, 9, 0, 0, 0, 0, ny)
	if n := calendarDaysBetween(a, b); n != 2 {
		t.Fatalf("days across DST = %d, want 2", n)
	}
	if n := calendarDaysBetween(b, a); n != -2 {
		t.Fatalf("days back = %d, want -2", n)
	}
}

// The calendar's query reaches before the grid, for ranges begun earlier.
func TestSadCalendarQueryReachesBack(t *testing.T) {
	gs := time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC)
	ge := time.Date(2026, 12, 6, 0, 0, 0, 0, time.UTC)
	b, _ := json.Marshal(calendarQueryFilter(&tableView{}, "Tanggal", gs, ge))
	if !strings.Contains(string(b), `"on_or_after":"2026-09-25"`) || !strings.Contains(string(b), `"on_or_before":"2026-12-06"`) {
		t.Fatalf("filter %s", b)
	}
}

// Comments in the right pane follow live events once they're loaded.
func TestSadLiveCommentsRefreshInPane(t *testing.T) {
	var gets int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasPrefix(r.URL.Path, "/comments") {
			atomic.AddInt32(&gets, 1)
			w.Write([]byte(`{"results":[],"has_more":false}`))
			return
		}
		w.WriteHeader(404)
		w.Write([]byte(`{"object":"error","status":404,"code":"object_not_found","message":"no"}`))
	}))
	defer srv.Close()
	a := New()
	a.client = notion.New("tok")
	a.client.Base = srv.URL
	a.set.NotifyCommentOff = true
	pn := &panel{id: "pg1"}
	pn.comments.loaded = true
	a.panels[0] = pn
	ev := liveEvent{Type: "comment.created"}
	ev.Entity.ID = "pg1"
	a.onEvents([]liveEvent{ev})
	waitUntil(t, "a comments reload", func() bool { return atomic.LoadInt32(&gets) > 0 })
}

// The comments button never does nothing: with no room for the right pane
// it opens a drawer beside the page instead.
func TestSadCommentsWithoutRoomForPane(t *testing.T) {
	a := New()
	pn := &panel{id: "pg1"}
	pn.comments.loaded = true
	a.rightPaneFits = false
	a.set.RightPane = false
	a.toggleComments(pn)
	if !pn.comments.open {
		t.Fatal("no drawer when the pane has no room")
	}
	a.rightPaneFits = true
	pn.comments.open = false
	a.toggleComments(pn)
	if !a.set.RightPane || pn.comments.open {
		t.Fatalf("pane %v drawer %v with room", a.set.RightPane, pn.comments.open)
	}
}

// Opening a split halves the window, whatever the divider was dragged to
// before; a split restored at start keeps its place.
func TestSplitOpensHalf(t *testing.T) {
	a := New()
	a.panels[0] = &panel{id: "a"}
	a.widths[0] = 1200
	a.set.SplitSize = 900
	a.openBeside(&node{ID: "b", Kind: "page", Title: "B"})
	if a.set.SplitSize != 600 || a.widths[0] != 600 || a.widths[1] != 600 {
		t.Fatalf("split %v widths %v, want 600/600", a.set.SplitSize, a.widths)
	}
	a.widths[0] = 1200
	a.set.SplitSize = 800
	a.guessSplit(false)
	if a.set.SplitSize != 800 {
		t.Fatalf("restored split moved to %v", a.set.SplitSize)
	}
}
