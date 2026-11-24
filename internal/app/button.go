package app

import "github.com/egoist/mygo/ui"

// btn is the app's own button: a quiet fill and a hairline that read on
// the card in light and in dark, an optional icon before its label, red
// for what deletes. It reports a click; a disabled one takes none.
func (a *App) btn(c *ui.Context, label string, icon *ui.SVG, danger, disabled bool) bool {
	p := a.pal()
	b := ui.Row(c.Key("btn"+label)).AlignItems(ui.Center).Gap(7).Padding(7, 13).Radius(8).Shrink(0).
		Background(p.Text.Alpha(0.05)).Border(1, p.Text.Alpha(0.13)).Label(label)
	color := p.Text
	if danger {
		color = c.Theme().Danger
	}
	if disabled {
		b.Opacity(0.45)
	} else {
		b.Cursor(ui.CursorPointer).Focusable()
		if b.Hovered() {
			b.Background(p.Text.Alpha(0.09))
		}
		if b.Pressed() {
			b.Background(p.Text.Alpha(0.13))
		}
	}
	b.Children(func() {
		if icon != nil {
			ui.Icon(c, icon).Size(15, 15).TextColor(color)
		}
		ui.Text(c, label).FontSize(13).FontWeight(500).TextColor(color).SingleLine()
	})
	return !disabled && b.Clicked()
}

// btnRow holds a button at its own width in a column, which would stretch
// it across.
func (a *App) btnRow(c *ui.Context, button func() bool) bool {
	clicked := false
	ui.Row(c).Children(func() { clicked = button() })
	return clicked
}
