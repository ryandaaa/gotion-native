package notion

import (
	"encoding/json"
	"errors"
	"fmt"
	"gotion/internal/tr"
	"strconv"
	"strings"
	"time"
)

// Value builds a property's new value from what the user entered, as two
// things: what to send Notion, and the property as Notion would return it,
// to show at once. text is the text entered for text, numbers, links and
// dates; on, a check box; names, the chosen options.
func Value(p Property, text string, on bool, names []string) (send any, show json.RawMessage, err error) {
	text = strings.TrimSpace(text)
	richText := func() []any {
		if text == "" {
			return []any{}
		}
		return []any{map[string]any{"type": "text", "text": map[string]any{"content": text}, "plain_text": text}}
	}
	option := func(name string) map[string]any {
		o := map[string]any{"name": name}
		for _, opt := range p.Options {
			if opt.Name == name && opt.Color != "" {
				o["color"] = opt.Color
			}
		}
		return o
	}
	var v any
	switch p.Type {
	case "title", "rich_text":
		v = richText()
	case "number":
		if text == "" {
			v = nil
			break
		}
		n, perr := strconv.ParseFloat(strings.ReplaceAll(text, ",", "."), 64)
		if perr != nil {
			return nil, nil, fmt.Errorf(tr.T("bukan angka: %q"), text)
		}
		v = n
	case "checkbox":
		v = on
	case "select", "status":
		if len(names) == 0 {
			v = nil
		} else {
			v = option(names[0])
		}
	case "multi_select":
		opts := []any{}
		for _, n := range names {
			opts = append(opts, option(n))
		}
		v = opts
	case "date":
		if text == "" {
			v = nil
			break
		}
		if _, perr := time.Parse("2006-01-02", text); perr != nil {
			return nil, nil, errors.New(tr.T("tanggal harus TTTT-BB-HH, misalnya 2026-12-31"))
		}
		v = map[string]any{"start": text}
	case "url", "email", "phone_number":
		if text == "" {
			v = nil
		} else {
			v = text
		}
	default:
		return nil, nil, fmt.Errorf(tr.T("properti %s tidak bisa diubah dari sini"), p.Type)
	}
	send = map[string]any{p.Type: v}
	shown := map[string]any{"type": p.Type, "id": p.ID, p.Type: v}
	show, err = json.Marshal(shown)
	return send, show, err
}

// DeferDate moves a date property's start to targetDay (at midnight in
// targetDay.Location()), preserving any time-of-day component and end-date
// range duration.
func DeferDate(p Property, raw json.RawMessage, targetDay time.Time) (send any, show json.RawMessage, err error) {
	var v struct {
		Date *struct {
			Start string `json:"start"`
			End   string `json:"end"`
		} `json:"date"`
	}
	json.Unmarshal(raw, &v)

	loc := targetDay.Location()
	shift := func(orig string, newDay time.Time) string {
		if len(orig) > 10 && orig[10] == 'T' {
			if t, perr := time.Parse(time.RFC3339, orig); perr == nil {
				localT := t.In(loc)
				moved := time.Date(newDay.Year(), newDay.Month(), newDay.Day(),
					localT.Hour(), localT.Minute(), localT.Second(), localT.Nanosecond(), loc)
				return moved.Format(time.RFC3339)
			}
		}
		return newDay.Format("2006-01-02")
	}
	parseDay := func(s string) (time.Time, bool) {
		if len(s) > 10 && s[10] == 'T' {
			if t, perr := time.Parse(time.RFC3339, s); perr == nil {
				lt := t.In(loc)
				return time.Date(lt.Year(), lt.Month(), lt.Day(), 0, 0, 0, 0, loc), true
			}
		}
		if len(s) >= 10 {
			if t, perr := time.ParseInLocation("2006-01-02", s[:10], loc); perr == nil {
				return t, true
			}
		}
		return time.Time{}, false
	}

	dateObj := map[string]any{}
	if v.Date != nil && v.Date.Start != "" {
		dateObj["start"] = shift(v.Date.Start, targetDay)
		if v.Date.End != "" {
			if sDay, ok1 := parseDay(v.Date.Start); ok1 {
				if eDay, ok2 := parseDay(v.Date.End); ok2 {
					dur := eDay.Sub(sDay)
					endTarget := targetDay.Add(dur)
					dateObj["end"] = shift(v.Date.End, endTarget)
				}
			}
		}
	} else {
		dateObj["start"] = targetDay.Format("2006-01-02")
	}

	send = map[string]any{"date": dateObj}
	show, err = json.Marshal(map[string]any{"type": "date", "id": p.ID, "date": dateObj})
	return send, show, err
}

// Linked is a person or a page a people or relation property points at.
type Linked struct {
	ID, Name string
}

// LinksValue builds a people or relation property's new value: the people
// or pages it points at, as Notion takes them and as it would return them.
func LinksValue(p Property, links []Linked) (send any, show json.RawMessage, err error) {
	items := []any{}
	shown := []any{}
	for _, l := range links {
		switch p.Type {
		case "people":
			items = append(items, map[string]any{"object": "user", "id": l.ID})
			shown = append(shown, map[string]any{"object": "user", "id": l.ID, "name": l.Name})
		case "relation":
			items = append(items, map[string]any{"id": l.ID})
			shown = append(shown, map[string]any{"id": l.ID, "name": l.Name})
		default:
			return nil, nil, fmt.Errorf(tr.T("properti %s tidak menunjuk orang atau halaman"), p.Type)
		}
	}
	send = map[string]any{p.Type: items}
	show, err = json.Marshal(map[string]any{"type": p.Type, "id": p.ID, p.Type: shown})
	return send, show, err
}

// Links reads the people or pages of a people or relation property.
func Links(raw json.RawMessage) []Linked {
	var v struct {
		People   []Linked `json:"people"`
		Relation []Linked `json:"relation"`
	}
	json.Unmarshal(raw, &v)
	return append(v.People, v.Relation...)
}
