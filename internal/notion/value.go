package notion

import (
	"encoding/json"
	"fmt"
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
			return nil, nil, fmt.Errorf("bukan angka: %q", text)
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
			return nil, nil, fmt.Errorf("tanggal harus TTTT-BB-HH, misalnya 2026-12-31")
		}
		v = map[string]any{"start": text}
	case "url", "email", "phone_number":
		if text == "" {
			v = nil
		} else {
			v = text
		}
	default:
		return nil, nil, fmt.Errorf("properti %s tidak bisa diubah dari sini", p.Type)
	}
	send = map[string]any{p.Type: v}
	shown := map[string]any{"type": p.Type, "id": p.ID, p.Type: v}
	show, err = json.Marshal(shown)
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
			return nil, nil, fmt.Errorf("properti %s tidak menunjuk orang atau halaman", p.Type)
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
