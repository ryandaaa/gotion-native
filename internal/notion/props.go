package notion

import (
	"encoding/json"
	"fmt"
	"gotion/internal/tr"
	"strconv"
	"strings"
	"time"
)

// FormatProperty returns a property value of a page as one line of text.
func FormatProperty(raw json.RawMessage) string {
	var v struct {
		Type        string     `json:"type"`
		Title       []RichText `json:"title"`
		RichText    []RichText `json:"rich_text"`
		Number      *float64   `json:"number"`
		Select      *named     `json:"select"`
		Status      *named     `json:"status"`
		MultiSelect []named    `json:"multi_select"`
		Date        *struct {
			Start string `json:"start"`
			End   string `json:"end"`
		} `json:"date"`
		Checkbox    bool    `json:"checkbox"`
		URL         *string `json:"url"`
		Email       *string `json:"email"`
		PhoneNumber *string `json:"phone_number"`
		People      []User  `json:"people"`
		Relation    []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"relation"`
		Files []struct {
			Name string `json:"name"`
		} `json:"files"`
		Formula *struct {
			Type    string   `json:"type"`
			String  *string  `json:"string"`
			Number  *float64 `json:"number"`
			Boolean *bool    `json:"boolean"`
		} `json:"formula"`
		CreatedTime    string `json:"created_time"`
		LastEditedTime string `json:"last_edited_time"`
		UniqueID       *struct {
			Prefix *string `json:"prefix"`
			Number int     `json:"number"`
		} `json:"unique_id"`
	}
	if json.Unmarshal(raw, &v) != nil {
		return ""
	}
	str := func(s *string) string {
		if s == nil {
			return ""
		}
		return *s
	}
	switch v.Type {
	case "title":
		return PlainText(v.Title)
	case "rich_text":
		return PlainText(v.RichText)
	case "number":
		if v.Number == nil {
			return ""
		}
		return strconv.FormatFloat(*v.Number, 'f', -1, 64)
	case "select":
		if v.Select == nil {
			return ""
		}
		return v.Select.Name
	case "status":
		if v.Status == nil {
			return ""
		}
		return v.Status.Name
	case "multi_select":
		names := make([]string, len(v.MultiSelect))
		for i, n := range v.MultiSelect {
			names[i] = n.Name
		}
		return strings.Join(names, ", ")
	case "date":
		if v.Date == nil {
			return ""
		}
		if v.Date.End != "" {
			return shortDate(v.Date.Start) + " → " + shortDate(v.Date.End)
		}
		return shortDate(v.Date.Start)
	case "checkbox":
		if v.Checkbox {
			return "☑"
		}
		return "☐"
	case "url":
		return str(v.URL)
	case "email":
		return str(v.Email)
	case "phone_number":
		return str(v.PhoneNumber)
	case "people":
		names := make([]string, 0, len(v.People))
		for _, p := range v.People {
			names = append(names, p.Name)
		}
		return strings.Join(names, ", ")
	case "relation":
		if len(v.Relation) == 0 {
			return ""
		}
		var names []string
		for _, r := range v.Relation {
			if r.Name != "" {
				names = append(names, r.Name)
			}
		}
		if len(names) == len(v.Relation) {
			return strings.Join(names, ", ")
		}
		if len(v.Relation) == 1 {
			return tr.T("1 halaman")
		}
		return fmt.Sprintf(tr.T("%d halaman"), len(v.Relation))
	case "files":
		var names []string
		for _, f := range v.Files {
			names = append(names, f.Name)
		}
		return strings.Join(names, ", ")
	case "formula":
		if f := v.Formula; f != nil {
			switch {
			case f.String != nil:
				return *f.String
			case f.Number != nil:
				return strconv.FormatFloat(*f.Number, 'f', -1, 64)
			case f.Boolean != nil:
				return strconv.FormatBool(*f.Boolean)
			}
		}
	case "created_time":
		return shortDate(v.CreatedTime)
	case "last_edited_time":
		return shortDate(v.LastEditedTime)
	case "unique_id":
		if v.UniqueID != nil {
			return str(v.UniqueID.Prefix) + "-" + strconv.Itoa(v.UniqueID.Number)
		}
	}
	return ""
}

// PropertyColor returns the color of a select or status value, "" for
// other properties.
func PropertyColor(raw json.RawMessage) string {
	var v struct {
		Select *named `json:"select"`
		Status *named `json:"status"`
	}
	json.Unmarshal(raw, &v)
	switch {
	case v.Status != nil:
		return v.Status.Color
	case v.Select != nil:
		return v.Select.Color
	}
	return ""
}

type named struct {
	Name  string `json:"name"`
	Color string `json:"color"`
}

var idMonths = [...]string{
	"", "Januari", "Februari", "Maret", "April", "Mei", "Juni",
	"Juli", "Agustus", "September", "Oktober", "November", "Desember",
}

func shortDate(s string) string {
	if len(s) < 10 {
		return s
	}
	d, err := time.Parse("2006-01-02", s[:10])
	if err != nil {
		return s
	}
	month := d.Month().String()
	if tr.Lang() == tr.ID {
		month = idMonths[d.Month()]
	}
	out := fmt.Sprintf("%02d %s %04d", d.Day(), month, d.Year())
	if len(s) >= 16 && s[10] == 'T' {
		out += " " + s[11:16]
	}
	return out
}

// DateStart returns the YYYY-MM-DD start of a date property, "" when unset.
func DateStart(raw json.RawMessage) string {
	var v struct {
		Date *struct {
			Start string `json:"start"`
		} `json:"date"`
	}
	if json.Unmarshal(raw, &v) != nil || v.Date == nil {
		return ""
	}
	if len(v.Date.Start) >= 10 {
		return v.Date.Start[:10]
	}
	return v.Date.Start
}

// Chip is a value shown as a colored tag: an option of a select, status or
// multi-select.
type Chip struct {
	Name, Color string
}

// Chips returns a property's values as tags, nil for properties that are
// not select, status or multi-select.
func Chips(raw json.RawMessage) []Chip {
	var v struct {
		Type        string  `json:"type"`
		Select      *named  `json:"select"`
		Status      *named  `json:"status"`
		MultiSelect []named `json:"multi_select"`
	}
	if json.Unmarshal(raw, &v) != nil {
		return nil
	}
	var out []Chip
	switch v.Type {
	case "select":
		if v.Select != nil {
			out = append(out, Chip{v.Select.Name, v.Select.Color})
		}
	case "status":
		if v.Status != nil {
			out = append(out, Chip{v.Status.Name, v.Status.Color})
		}
	case "multi_select":
		for _, n := range v.MultiSelect {
			out = append(out, Chip{n.Name, n.Color})
		}
	default:
		return nil
	}
	if out == nil {
		out = []Chip{}
	}
	return out
}
