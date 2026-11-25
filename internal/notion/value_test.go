package notion

import (
	"encoding/json"
	"gotion/internal/tr"
	"strings"
	"testing"
	"time"
)

func TestValue(t *testing.T) {
	status := Property{Name: "Status", Type: "status", Options: []Option{{"Done", "green"}, {"Doing", "yellow"}}}
	send, show, err := Value(status, "", false, []string{"Done"})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(send)
	if string(b) != `{"status":{"color":"green","name":"Done"}}` {
		t.Errorf("send %s", b)
	}
	// What shows reads back as Notion's own.
	if FormatProperty(show) != "Done" || PropertyColor(show) != "green" {
		t.Errorf("show %s", show)
	}

	_, show, _ = Value(Property{Type: "checkbox"}, "", true, nil)
	if FormatProperty(show) != "☑" {
		t.Errorf("checkbox %s", show)
	}
	_, show, _ = Value(Property{Type: "number"}, "3,5", false, nil)
	if FormatProperty(show) != "3.5" {
		t.Errorf("number %s", show)
	}
	if _, _, err := Value(Property{Type: "number"}, "tiga", false, nil); err == nil {
		t.Error("a word taken for a number")
	}
	if _, _, err := Value(Property{Type: "date"}, "31/12/2026", false, nil); err == nil || !strings.Contains(err.Error(), "TTTT") {
		t.Errorf("a date in another form: %v", err)
	}
	send, show, _ = Value(Property{Type: "multi_select", Options: []Option{{"a", "blue"}}}, "", false, []string{"a", "b"})
	if chips := Chips(show); len(chips) != 2 || chips[0].Color != "blue" {
		t.Errorf("multi %s", show)
	}
	_ = send
	send, _, _ = Value(Property{Type: "select"}, "", false, nil)
	if b, _ := json.Marshal(send); string(b) != `{"select":null}` {
		t.Errorf("clearing a select: %s", b)
	}
	if _, _, err := Value(Property{Type: "formula"}, "x", false, nil); err == nil {
		t.Error("a formula set")
	}
}

func TestPropertyOptions(t *testing.T) {
	var p Property
	json.Unmarshal([]byte(`{"id":"x","name":"Status","type":"status","status":{"options":[{"name":"Done","color":"green"}]}}`), &p)
	if len(p.Options) != 1 || p.Options[0].Color != "green" || !p.Editable() {
		t.Errorf("%+v", p)
	}
}

func TestDateFormattingAndStart(t *testing.T) {
	prev := tr.Lang()
	t.Cleanup(func() { tr.Set(prev) })

	tr.Set(tr.ID)
	casesID := []struct {
		raw       string
		wantFmt   string
		wantStart string
	}{
		{`{"type":"date","date":{"start":"2026-12-05"}}`, "05 Desember 2026", "2026-12-05"},
		{`{"type":"date","date":{"start":"2026-08-09T14:30:00.000Z"}}`, "09 Agustus 2026 14:30", "2026-08-09"},
		{`{"type":"date","date":{"start":"2026-11-20","end":"2026-12-05"}}`, "20 November 2026 → 05 Desember 2026", "2026-11-20"},
		{`{"type":"created_time","created_time":"2026-01-03T08:05:00Z"}`, "03 Januari 2026 08:05", ""},
		{`{"type":"date","date":null}`, "", ""},
	}
	for _, tc := range casesID {
		if got := FormatProperty(json.RawMessage(tc.raw)); got != tc.wantFmt {
			t.Errorf("ID FormatProperty(%s) = %q, want %q", tc.raw, got, tc.wantFmt)
		}
		if got := DateStart(json.RawMessage(tc.raw)); got != tc.wantStart {
			t.Errorf("ID DateStart(%s) = %q, want %q", tc.raw, got, tc.wantStart)
		}
	}

	tr.Set(tr.EN)
	casesEN := []struct {
		raw     string
		wantFmt string
	}{
		{`{"type":"date","date":{"start":"2026-12-05"}}`, "05 December 2026"},
		{`{"type":"date","date":{"start":"2026-08-09T14:30:00.000Z"}}`, "09 August 2026 14:30"},
	}
	for _, tc := range casesEN {
		if got := FormatProperty(json.RawMessage(tc.raw)); got != tc.wantFmt {
			t.Errorf("EN FormatProperty(%s) = %q, want %q", tc.raw, got, tc.wantFmt)
		}
	}
}

func TestDeferDate(t *testing.T) {
	loc := time.FixedZone("WIB", 7*3600)
	tomorrow := time.Date(2026, 11, 26, 0, 0, 0, 0, loc)
	prop := Property{ID: "d", Name: "Tenggat", Type: "date"}

	// 1. Date with time preserves hour and minute.
	send, show, err := DeferDate(prop, json.RawMessage(`{"type":"date","date":{"start":"2026-11-24T09:30:00+07:00"}}`), tomorrow)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(send)
	if !strings.Contains(string(b), `"start":"2026-11-26T09:30:00+07:00"`) {
		t.Errorf("time not preserved: %s (show=%s)", b, show)
	}

	// 2. Date range preserves duration (3 days).
	send, _, err = DeferDate(prop, json.RawMessage(`{"type":"date","date":{"start":"2026-11-22","end":"2026-11-25"}}`), tomorrow)
	if err != nil {
		t.Fatal(err)
	}
	b, _ = json.Marshal(send)
	if string(b) != `{"date":{"end":"2026-11-29","start":"2026-11-26"}}` {
		t.Errorf("range duration not preserved: %s", b)
	}
}
