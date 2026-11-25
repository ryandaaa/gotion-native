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

	"gotion/internal/notion"
)

func TestCalendarGridRangeAndQueryFilter(t *testing.T) {
	loc := time.FixedZone("WIB", 7*3600)
	// November 2026: Nov 1 is Sunday -> Monday-first grid starts Oct 26, ends Dec 6 (6 weeks).
	nov := time.Date(2026, 11, 1, 0, 0, 0, 0, loc)
	start, end, weeks := calendarGridRange(nov)
	if start.Format("2006-01-02") != "2026-10-26" {
		t.Fatalf("gridStart = %s, want 2026-10-26", start.Format("2006-01-02"))
	}
	if end.Format("2006-01-02") != "2026-12-06" {
		t.Fatalf("gridEnd = %s, want 2026-12-06", end.Format("2006-01-02"))
	}
	if weeks != 6 {
		t.Fatalf("weeks = %d, want 6", weeks)
	}

	tv := &tableView{id: "db1"}
	f := calendarQueryFilter(tv, "Tanggal", start, end)
	b, _ := json.Marshal(f)
	got := string(b)
	if !strings.Contains(got, `"on_or_after":"2026-09-25"`) || !strings.Contains(got, `"on_or_before":"2026-12-06"`) {
		t.Fatalf("unexpected calendar filter: %s", got)
	}
}

func TestCalendarMultiDayPlacementAndTimezone(t *testing.T) {
	wib := time.FixedZone("WIB", 7*3600)
	// 2026-11-01T20:00:00Z is 2026-11-02T03:00:00+07:00 in WIB (Monday!).
	rMulti := &notion.Page{
		ID: "r-multi",
		Properties: map[string]json.RawMessage{
			"Name": json.RawMessage(`{"type":"title","title":[{"plain_text":"Sprint 12"}]}`),
			"Date": json.RawMessage(`{"type":"date","date":{"start":"2026-11-01T20:00:00Z","end":"2026-11-04T05:00:00+07:00"}}`),
		},
	}
	rSingle := &notion.Page{
		ID: "r-single",
		Properties: map[string]json.RawMessage{
			"Name": json.RawMessage(`{"type":"title","title":[{"plain_text":"Review"}]}`),
			"Date": json.RawMessage(`{"type":"date","date":{"start":"2026-11-03"}}`),
		},
	}

	sp1, ok1 := parseLocalDateRange(rMulti.Properties["Date"], wib)
	sp1.row = rMulti
	sp2, ok2 := parseLocalDateRange(rSingle.Properties["Date"], wib)
	sp2.row = rSingle
	if !ok1 || !ok2 {
		t.Fatalf("failed to parse date ranges: ok1=%v ok2=%v", ok1, ok2)
	}
	if sp1.startDay.Format("2006-01-02") != "2026-11-02" || sp1.endDay.Format("2006-01-02") != "2026-11-04" {
		t.Fatalf("timezone conversion failed: start=%s end=%s", sp1.startDay.Format("2006-01-02"), sp1.endDay.Format("2006-01-02"))
	}
	if sp1.timeLabel != "03:00" {
		t.Fatalf("expected timeLabel 03:00 in WIB, got %q", sp1.timeLabel)
	}

	weekStart := time.Date(2026, 11, 2, 0, 0, 0, 0, wib)
	placed := placeWeekEvents([]calendarSpan{sp2, sp1}, weekStart)
	if len(placed) != 2 {
		t.Fatalf("expected 2 placements, got %d", len(placed))
	}
	// Multi-day event gets track 0 spanning Mon..Wed (colStart=0, colSpan=3).
	if placed[0].span.row.ID != "r-multi" || placed[0].colStart != 0 || placed[0].colSpan != 3 || placed[0].track != 0 {
		t.Fatalf("unexpected multi-day placement: %+v", placed[0])
	}
	// Single-day event on Tue overlaps track 0, so it moves to track 1 at colStart=1.
	if placed[1].span.row.ID != "r-single" || placed[1].colStart != 1 || placed[1].colSpan != 1 || placed[1].track != 1 {
		t.Fatalf("unexpected single-day placement: %+v", placed[1])
	}
}

func TestCalendarDragPreservesDurationAndTime(t *testing.T) {
	var mu sync.Mutex
	var patchedBody string
	doneCh := make(chan struct{}, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "PATCH" && r.URL.Path == "/pages/p1" {
			b, _ := io.ReadAll(r.Body)
			mu.Lock()
			patchedBody = string(b)
			mu.Unlock()
			select {
			case doneCh <- struct{}{}:
			default:
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"id":"p1"}`))
			return
		}
		w.WriteHeader(404)
	}))
	defer srv.Close()

	loc := time.FixedZone("WIB", 7*3600)
	a := New()
	a.client = notion.New("tok")
	a.client.Base = srv.URL

	col := notion.Property{Name: "Date", Type: "date"}
	row := &notion.Page{
		ID: "p1",
		Properties: map[string]json.RawMessage{
			"Date": json.RawMessage(`{"type":"date","date":{"start":"2026-11-10T09:30:00+07:00","end":"2026-11-12T17:00:00+07:00"}}`),
		},
	}
	tv := &tableView{id: "db1", columns: []notion.Property{col}, rows: []*notion.Page{row}}

	// Drag from Nov 10 to Nov 15 -> 2-day duration + times (09:30 and 17:00) must be preserved.
	target := time.Date(2026, 11, 15, 0, 0, 0, 0, loc)
	a.moveCalendarRow(tv, row, col, target)

	select {
	case <-doneCh:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for PATCH")
	}

	mu.Lock()
	defer mu.Unlock()
	if !strings.Contains(patchedBody, `"start":"2026-11-15T09:30:00+07:00"`) {
		t.Fatalf("expected preserved start time on Nov 15, got %s", patchedBody)
	}
	if !strings.Contains(patchedBody, `"end":"2026-11-17T17:00:00+07:00"`) {
		t.Fatalf("expected preserved 2-day duration and end time on Nov 17, got %s", patchedBody)
	}
}

func TestAllCalendarAggregation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == "GET" && r.URL.Path == "/data_sources/ds1":
			w.Write([]byte(`{"id":"ds1","title":[{"plain_text":"Tasks"}],"properties":{"Name":{"name":"Name","type":"title"},"Due":{"name":"Due","type":"date"}}}`))
		case r.Method == "GET" && r.URL.Path == "/data_sources/ds2":
			w.Write([]byte(`{"id":"ds2","title":[{"plain_text":"Events"}],"properties":{"Name":{"name":"Name","type":"title"},"When":{"name":"When","type":"date"}}}`))
		case r.Method == "POST" && r.URL.Path == "/data_sources/ds1/query":
			w.Write([]byte(`{"results":[{"id":"p1","properties":{"Name":{"type":"title","title":[{"plain_text":"Task A"}]},"Due":{"type":"date","date":{"start":"2026-11-25"}}}}]}`))
		case r.Method == "POST" && r.URL.Path == "/data_sources/ds2/query":
			w.Write([]byte(`{"results":[{"id":"p2","properties":{"Name":{"type":"title","title":[{"plain_text":"Event B"}]},"When":{"type":"date","date":{"start":"2026-11-26"}}}}]}`))
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()

	a := New()
	a.client = notion.New("tok")
	a.client.Base = srv.URL
	a.nodes["ds1"] = &node{ID: "ds1", Title: "Tasks", Kind: "data_source"}
	a.nodes["ds2"] = &node{ID: "ds2", Title: "Events", Kind: "data_source"}
	a.set.CalOff["ds2"] = true

	start := time.Date(2026, 10, 26, 0, 0, 0, 0, time.Local)
	end := time.Date(2026, 12, 6, 0, 0, 0, 0, time.Local)
	a.loadAllCalendar(start, end)
	deadline := time.Now().Add(2 * time.Second)
	for (a.allCal.loading || !a.allCal.loaded) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if len(a.allCal.sources) != 2 {
		t.Fatalf("expected 2 eligible calendar sources, got %d", len(a.allCal.sources))
	}
	if len(a.allCal.items) != 1 || a.allCal.items[0].Page.ID != "p1" || a.allCal.items[0].DBTitle != "Tasks" {
		t.Fatalf("expected 1 item from ds1 (ds2 excluded), got %+v", a.allCal.items)
	}
}
