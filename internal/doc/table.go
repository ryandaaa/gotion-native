package doc

import (
	"encoding/json"
	"gotion/internal/tr"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/richtext"

	"gotion/internal/notion"
)

// tableColors are the colors a table draws with, from the theme it is
// drawn in, so that it follows light and dark.
func tableColors(t *ui.Theme) (line, head, text ui.Color) {
	return t.TextMuted.Alpha(0.3), t.TextMuted.Alpha(0.1), t.Text
}

const (
	rowHeight    = 32
	addRowHeight = 28
	cellFont     = 13
)

// TableMeta holds the layout and cell data of a rendered table block.
type TableMeta struct {
	BlockID string
	RowIDs  []string
	Rows    [][]string
	// Raw holds each row's cells as Notion sent them, so saving one cell
	// sends the others back with their bold, links and mentions intact.
	Raw   [][]json.RawMessage
	Cols  int
	Rect  ui.Rect
	Width float32
}

// TableHit maps a pointer position (x, y) to a table cell or the add-row footer.
func TableHit(m *TableMeta, x, y float32) (row, col int, addRow, ok bool) {
	if m == nil || m.Width <= 0 || m.Cols <= 0 {
		return 0, 0, false, false
	}
	relX := x - m.Rect.X
	relY := y - m.Rect.Y
	if relX < 0 || relY < 0 || relX > m.Width || relY > m.Rect.H {
		return 0, 0, false, false
	}
	rowsH := float32(len(m.Rows) * rowHeight)
	if relY >= rowsH {
		return 0, 0, true, true
	}
	if len(m.Rows) == 0 {
		return 0, 0, false, false
	}
	row = min(int(relY/rowHeight), len(m.Rows)-1)
	cw := m.Width / float32(m.Cols)
	col = min(int(relX/cw), m.Cols-1)
	return row, col, false, true
}

// tableAttachment draws a table block as a grid as wide as the page's text
// (*width), its rows 32 DIPs and its type 13 at any width: a cell too narrow
// for its text ends in "…".
func tableAttachment(b *notion.Block, width *float32) *richtext.Attachment {
	var rows [][]string
	var rowIDs []string
	var raw [][]json.RawMessage
	cols := 0
	for _, r := range b.Children {
		if r.Type != "table_row" {
			continue
		}
		cells := r.Cells()
		rows = append(rows, cells)
		rowIDs = append(rowIDs, r.ID)
		var rc struct {
			Cells []json.RawMessage `json:"cells"`
		}
		json.Unmarshal(r.Payload, &rc)
		raw = append(raw, rc.Cells)
		cols = max(cols, len(cells))
	}
	var tp struct {
		Width int `json:"table_width"`
	}
	json.Unmarshal(b.Payload, &tp)
	cols = max(cols, tp.Width, 1)
	meta := &TableMeta{
		BlockID: b.ID,
		RowIDs:  rowIDs,
		Rows:    rows,
		Raw:     raw,
		Cols:    cols,
	}
	header := strings.Contains(string(b.Payload), `"has_column_header":true`)
	att := &richtext.Attachment{Name: "table", Width: 1, Height: float32(len(rows)*rowHeight) + addRowHeight + 1, Data: meta}
	att.Draw = func(p *ui.Painter, r ui.Rect) {
		w := float32(TextWidth)
		if width != nil && *width > 0 {
			w = *width
		}
		meta.Rect = r
		meta.Width = w
		h := float32(len(meta.Rows)*rowHeight) + 1
		att.Height = h + addRowHeight
		line, headBg, textColor := tableColors(p.Theme())
		cw := w / float32(meta.Cols)
		for i, row := range meta.Rows {
			y := r.Y + float32(i*rowHeight)
			if i == 0 && header {
				p.Fill(ui.Rect{X: r.X, Y: y, W: w, H: rowHeight}, headBg, 0)
			}
			for j := 0; j < meta.Cols; j++ {
				text := ""
				if j < len(row) {
					text = row[j]
				}
				p.Text(r.X+float32(j)*cw+8, y+(rowHeight-cellFont)/2, fit(text, cw-16, cellFont), cellFont, textColor)
			}
		}
		for i := 0; i <= len(meta.Rows); i++ {
			p.Fill(ui.Rect{X: r.X, Y: r.Y + float32(i*rowHeight), W: w, H: 1}, line, 0)
		}
		for j := 0; j <= meta.Cols; j++ {
			x := min(r.X+float32(j)*cw, r.X+w-1)
			p.Fill(ui.Rect{X: x, Y: r.Y, W: 1, H: h}, line, 0)
		}
		p.Text(r.X+8, r.Y+h+(addRowHeight-12)/2, tr.T("+ Tambah baris"), 12, p.Theme().TextMuted)
	}
	return att
}

// fit cuts s to about the characters that fit width w.
func fit(s string, w, font float32) string {
	s = strings.ReplaceAll(s, "\n", " ")
	n := int(w / (font * 0.55))
	r := []rune(s)
	switch {
	case len(r) <= n:
		return s
	case n <= 1:
		return "" // not even one letter and "…" fit
	}
	return string(r[:n-1]) + "…"
}
