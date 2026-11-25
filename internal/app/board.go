package app

import (
	"strconv"

	"github.com/egoist/mygo/ui"

	"gotion/internal/notion"
	"gotion/internal/tr"
)

type boardLane struct {
	key    string
	option notion.Option // Name == "" for the unassigned lane
	rows   []*notion.Page
}

// boardColumns returns all status and select columns in t.
func boardColumns(t *tableView) []notion.Property {
	var out []notion.Property
	for _, col := range t.columns {
		if col.Type == "status" || col.Type == "select" {
			out = append(out, col)
		}
	}
	return out
}

// boardColumn picks the active grouping column: saved preference, else first
// status column, else first select column.
func (a *App) boardColumn(t *tableView) (notion.Property, bool) {
	cols := boardColumns(t)
	if len(cols) == 0 {
		return notion.Property{}, false
	}
	if saved := a.set.BoardGroup[t.id]; saved != "" {
		for _, c := range cols {
			if c.Name == saved {
				return c, true
			}
		}
	}
	for _, c := range cols {
		if c.Type == "status" {
			return c, true
		}
	}
	return cols[0], true
}

// boardLanes groups t.rows into ordered option lanes plus a trailing
// unassigned lane.
func boardLanes(t *tableView, col notion.Property) []boardLane {
	opts := col.OrderedOptions()
	lanes := make([]boardLane, 0, len(opts)+1)
	byName := make(map[string]int, len(opts))
	for _, o := range opts {
		byName[o.Name] = len(lanes)
		lanes = append(lanes, boardLane{key: o.Name, option: o})
	}
	noneIdx := len(lanes)
	lanes = append(lanes, boardLane{key: "__none__"})
	for _, r := range t.rows {
		idx := noneIdx
		if chips := notion.Chips(r.Properties[col.Name]); len(chips) > 0 {
			if pos, ok := byName[chips[0].Name]; ok {
				idx = pos
			}
		}
		lanes[idx].rows = append(lanes[idx].rows, r)
	}
	return lanes
}

// board renders the kanban board view for table t.
func (a *App) board(c *ui.Context, i int, t *tableView) {
	p := a.pal()
	col, ok := a.boardColumn(t)
	if !ok {
		ui.Column(c).Fill().Center().Gap(12).Children(func() {
			ui.Text(c, tr.T("Tambahkan kolom Status atau Pilihan untuk memakai tampilan Board.")).FontSize(14).TextColor(p.Muted)
			if a.btn(c, tr.T("Buat kolom Status"), iconPlus, false, false) {
				a.changeSchema(t, map[string]any{"Status": map[string]any{"status": map[string]any{}}}, tr.T("Kolom “")+"Status"+tr.T("” ditambahkan"))
			}
		})
		return
	}
	cols := boardColumns(t)
	ui.Row(c).Padding(10, 24, 0, 24).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Text(c, tr.T("Kelompokkan menurut")).FontSize(12).TextColor(p.Muted)
		names := make([]string, len(cols))
		for k, bc := range cols {
			names[k] = bc.Name
		}
		chosen := col.Name
		if ui.Select(c, &chosen, names).Width(170).Label(tr.T("Kelompokkan menurut")).Changed() {
			if a.set.BoardGroup == nil {
				a.set.BoardGroup = map[string]string{}
			}
			a.set.BoardGroup[t.id] = chosen
			a.saveSettings()
		}
	})
	lanes := boardLanes(t, col)
	const laneW, cardW = float32(276), float32(252)
	ui.ScrollHorizontal(c).Grow(1).MinHeight(0).Padding(12, 24, 16, 24).ScrollbarStyle(a.thinScrollbar()).Children(func() {
		ui.Row(c).Gap(14).FillHeight().AlignItems(ui.Stretch).Children(func() {
			for _, ln := range lanes {
				lane := ui.Column(c.Key("lane-" + ln.key)).Width(laneW).Shrink(0).FillHeight().
					Radius(10).Padding(10).Gap(8).Background(p.Sidebar.Alpha(0.75))
				if _, over := ui.DragOver[*notion.Page](lane); over {
					lane.Background(p.EditBg).Border(1.5, p.EditLine)
				} else {
					lane.Border(1, p.Line)
				}
				if r, dropped := ui.Drop[*notion.Page](lane); dropped {
					a.moveBoardCard(t, r, col, ln.option.Name)
				}
				lane.Children(func() {
					ui.Row(c).AlignItems(ui.Center).Gap(8).Padding(2, 4).Children(func() {
						if ln.option.Name != "" {
							a.chip(c, notion.Chip{Name: ln.option.Name, Color: ln.option.Color})
						} else {
							ui.Text(c, tr.T("Tanpa ")+col.Name).FontSize(12).FontWeight(600).TextColor(p.Muted)
						}
						ui.Text(c, strconv.Itoa(len(ln.rows))).FontSize(12).TextColor(p.Faint)
					})
					ui.Scroll(c.Key("lanescr-" + ln.key)).Grow(1).MinHeight(0).ScrollbarStyle(a.thinScrollbar()).Children(func() {
						ui.Column(c).Gap(10).Padding(2, 2, 4, 2).Children(func() {
							for _, r := range ln.rows {
								card := a.rowCard(c, i, t, r, cardW)
								card.Drag(r)
								if t.boardMoved != nil && t.boardMoved[r.ID] > 0 {
									if t.boardMoved[r.ID] > 1 {
										card.AnimatePose(ui.Pose{Y: -8, Fade: 0.4}, ui.Snappy)
										c.Invalidate()
									} else {
										card.AnimatePose(ui.Pose{}, ui.Snappy)
									}
									t.boardMoved[r.ID]--
								}
							}
							a.addBoardCard(c, i, t, col.Name, ln.option.Name, cardW)
						})
					})
				})
			}
		})
	})
}

// moveBoardCard updates row r's grouping property to optName ("" clears it)
// with optimistic UI and smooth card entrance.
func (a *App) moveBoardCard(t *tableView, r *notion.Page, col notion.Property, optName string) {
	if r == nil {
		return
	}
	cur := ""
	if chips := notion.Chips(r.Properties[col.Name]); len(chips) > 0 {
		cur = chips[0].Name
	}
	if cur == optName {
		return
	}
	var names []string
	if optName != "" {
		names = []string{optName}
	}
	if a.setCell(t, r, col, "", false, names) {
		if t.boardMoved == nil {
			t.boardMoved = map[string]int{}
		}
		t.boardMoved[r.ID] = 2
	}
}
