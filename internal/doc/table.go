package doc

import (
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
	rowHeight = 32
	cellFont  = 13
)

// tableAttachment draws a table block, read-only, as a grid as wide as
// the page's text (*width), its rows 32 DIPs and its type 13 at any width:
// a cell too narrow for its text ends in "…".
//
// The box the editor lays out is one DIP wide, so the editor never shrinks
// it, and the grid is drawn across the line beside it: a table is always
// alone in its paragraph, and the editor clips what it draws.
func tableAttachment(b *notion.Block, width *float32) *richtext.Attachment {
	var rows [][]string
	cols := 0
	for _, r := range b.Children {
		if r.Type != "table_row" {
			continue
		}
		cells := r.Cells()
		rows = append(rows, cells)
		cols = max(cols, len(cells))
	}
	if cols == 0 {
		cols = 1
	}
	header := strings.Contains(string(b.Payload), `"has_column_header":true`)
	h := float32(len(rows)*rowHeight) + 1
	return &richtext.Attachment{Name: "table", Width: 1, Height: h, Draw: func(p *ui.Painter, r ui.Rect) {
		w := float32(TextWidth)
		if width != nil && *width > 0 {
			w = *width
		}
		line, headBg, textColor := tableColors(p.Theme())
		cw := w / float32(cols)
		for i, row := range rows {
			y := r.Y + float32(i*rowHeight)
			if i == 0 && header {
				p.Fill(ui.Rect{X: r.X, Y: y, W: w, H: rowHeight}, headBg, 0)
			}
			for j := 0; j < cols; j++ {
				text := ""
				if j < len(row) {
					text = row[j]
				}
				p.Text(r.X+float32(j)*cw+8, y+(rowHeight-cellFont)/2, fit(text, cw-16, cellFont), cellFont, textColor)
			}
		}
		for i := 0; i <= len(rows); i++ {
			p.Fill(ui.Rect{X: r.X, Y: r.Y + float32(i*rowHeight), W: w, H: 1}, line, 0)
		}
		for j := 0; j <= cols; j++ {
			x := min(r.X+float32(j)*cw, r.X+w-1)
			p.Fill(ui.Rect{X: x, Y: r.Y, W: 1, H: h}, line, 0)
		}
	}}
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
