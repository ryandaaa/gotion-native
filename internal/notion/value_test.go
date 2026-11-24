package notion

import (
	"encoding/json"
	"strings"
	"testing"
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
