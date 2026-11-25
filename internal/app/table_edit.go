package app

import (
	"context"
	"encoding/json"

	"github.com/egoist/mygo/ui"

	"gotion/internal/doc"
	"gotion/internal/notion"
	"gotion/internal/tr"
)

type cellEditState struct {
	open    bool
	tableID string
	row     int
	col     int
	text    string
	rect    ui.Rect
	focus   bool
}

func findTableMeta(pn *panel, id string) *doc.TableMeta {
	if pn == nil || pn.doc == nil {
		return nil
	}
	for _, p := range pn.doc.Text().Paragraphs() {
		for _, r := range p.Runs {
			if r.Attributes.Attachment != nil {
				if m, ok := r.Attributes.Attachment.Data.(*doc.TableMeta); ok && m.BlockID == id {
					return m
				}
			}
		}
	}
	return nil
}

func (a *App) clickTable(pn *panel, id string) {
	if a.client == nil {
		return
	}
	meta := findTableMeta(pn, id)
	if meta == nil {
		return
	}
	row, col, addRow, ok := doc.TableHit(meta, pn.pointerX, pn.pointerY)
	if !ok {
		return
	}
	if addRow {
		a.addTableRow(pn, meta)
		return
	}
	if row < 0 || row >= len(meta.Rows) || col < 0 || col >= meta.Cols {
		return
	}
	val := ""
	if col < len(meta.Rows[row]) {
		val = meta.Rows[row][col]
	}
	cw := meta.Width / float32(meta.Cols)
	pn.cellEdit = cellEditState{
		open:    true,
		tableID: id,
		row:     row,
		col:     col,
		text:    val,
		rect:    ui.Rect{X: meta.Rect.X + float32(col)*cw, Y: meta.Rect.Y + float32(row*32), W: max(cw, 160), H: 32},
		focus:   true,
	}
}

func richTextCell(s string) []any {
	if s == "" {
		return []any{}
	}
	return []any{map[string]any{"type": "text", "text": map[string]any{"content": s}}}
}

func (a *App) saveTableCell(pn *panel, meta *doc.TableMeta, row, col int, newText string) {
	if a.client == nil || meta == nil {
		return
	}
	if row < 0 || row >= len(meta.Rows) || row >= len(meta.RowIDs) || col < 0 || col >= meta.Cols {
		return
	}
	for len(meta.Rows[row]) < meta.Cols {
		meta.Rows[row] = append(meta.Rows[row], "")
	}
	oldText := meta.Rows[row][col]
	if oldText == newText {
		return
	}
	meta.Rows[row][col] = newText

	cells := make([]any, meta.Cols)
	for j := 0; j < meta.Cols; j++ {
		cells[j] = richTextCell(meta.Rows[row][j])
		if j != col && row < len(meta.Raw) && j < len(meta.Raw[row]) && len(meta.Raw[row][j]) > 0 {
			cells[j] = meta.Raw[row][j] // untouched: as Notion has it
		}
	}
	rowID := meta.RowIDs[row]
	// The edited cell is plain text from now on, for the next save of this row.
	var oldRaw json.RawMessage
	if row < len(meta.Raw) {
		for len(meta.Raw[row]) < meta.Cols {
			meta.Raw[row] = append(meta.Raw[row], nil)
		}
		oldRaw = meta.Raw[row][col]
		meta.Raw[row][col], _ = json.Marshal(cells[col])
	}
	client := a.client
	go func() {
		_, err := client.UpdateBlock(context.Background(), rowID, "table_row", map[string]any{"cells": cells})
		if err != nil {
			a.update(func() {
				meta.Rows[row][col] = oldText
				if row < len(meta.Raw) {
					meta.Raw[row][col] = oldRaw
				}
				a.toast(tr.T("Gagal menyimpan ke Notion: ") + err.Error())
			})
		}
	}()
}

func (a *App) addTableRow(pn *panel, meta *doc.TableMeta) {
	if a.client == nil || meta == nil {
		return
	}
	cells := make([]any, meta.Cols)
	for j := 0; j < meta.Cols; j++ {
		cells[j] = []any{}
	}
	go func() {
		created, err := a.client.Append(context.Background(), meta.BlockID, "", notion.NewBlock{
			Type:    "table_row",
			Payload: map[string]any{"cells": cells},
		})
		a.update(func() {
			if err != nil {
				a.toast(tr.T("Gagal menyimpan ke Notion: ") + err.Error())
				return
			}
			for _, b := range created {
				meta.RowIDs = append(meta.RowIDs, b.ID)
				meta.Rows = append(meta.Rows, make([]string, meta.Cols))
				meta.Raw = append(meta.Raw, nil)
			}
		})
	}()
}

func (a *App) cellEditView(c *ui.Context, pn *panel) {
	ce := &pn.cellEdit
	if !ce.open {
		return
	}
	p := a.pal()
	ui.Row(c.Key("cell-edit")).Absolute().Left(ce.rect.X).Top(ce.rect.Y).Width(ce.rect.W).ZIndex(25).
		Padding(4).Gap(4).AlignItems(ui.Center).
		Background(p.Panel).Radius(6).Border(1, p.Line).Shadow(0, 4, 16, 0, ui.RGBA(0, 0, 0, 0.16)).
		Children(func() {
			in := ui.TextInput(c.Key("cell-input"), &ce.text).FillWidth().FontSize(13).
				Padding(4, 6).Radius(4).Background(p.Panel).Border(1, p.Line).
				OnSubmit(func() {
					meta := findTableMeta(pn, ce.tableID)
					a.saveTableCell(pn, meta, ce.row, ce.col, ce.text)
					ce.open = false
				})
			if ce.focus {
				ce.focus = false
				in.Focus()
			}
			if in.Shortcut(0, ui.KeyEscape) {
				ce.open = false
			}
		})
}
