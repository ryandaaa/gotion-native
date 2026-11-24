package doc

import (
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/richtext"

	"gotion/internal/notion"
)

// TableColors are the colors tables draw with.
var TableColors = struct{ Line, Head, Text ui.Color }{ui.Hex("#E7E5E4"), ui.Hex("#F5F5F4"), ui.Hex("#1C1917")}

const (
	rowHeight = 32
	cellFont  = 13
)

// tableAttachment draws a table block, read-only, as a grid.
func tableAttachment(b *notion.Block) *richtext.Attachment {
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
	var head struct {
		HasColumnHeader bool `json:"has_column_header"`
	}
	head.HasColumnHeader = strings.Contains(string(b.Payload), `"has_column_header":true`)
	w := float32(TextWidth)
	h := float32(len(rows)*rowHeight) + 1
	return &richtext.Attachment{Name: "table", Width: w, Height: h, Draw: func(p *ui.Painter, r ui.Rect) {
		cw := r.W / float32(cols)
		for i, row := range rows {
			y := r.Y + float32(i*rowHeight)
			if i == 0 && head.HasColumnHeader {
				p.Fill(ui.Rect{X: r.X, Y: y, W: r.W, H: rowHeight}, TableColors.Head, 0)
			}
			for j := 0; j < cols; j++ {
				text := ""
				if j < len(row) {
					text = row[j]
				}
				p.Text(r.X+float32(j)*cw+8, y+9, fit(text, cw-16), cellFont, TableColors.Text)
			}
		}
		for i := 0; i <= len(rows); i++ {
			y := r.Y + float32(i*rowHeight)
			p.Fill(ui.Rect{X: r.X, Y: y, W: r.W, H: 1}, TableColors.Line, 0)
		}
		for j := 0; j <= cols; j++ {
			x := min(r.X+float32(j)*cw, r.X+r.W-1)
			p.Fill(ui.Rect{X: x, Y: r.Y, W: 1, H: r.H}, TableColors.Line, 0)
		}
	}}
}

// fit cuts s to about the characters that fit width w.
func fit(s string, w float32) string {
	s = strings.ReplaceAll(s, "\n", " ")
	n := int(w / (cellFont * 0.55))
	r := []rune(s)
	if len(r) > n && n > 1 {
		return string(r[:n-1]) + "…"
	}
	return s
}
