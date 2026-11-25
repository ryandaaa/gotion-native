package app

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/egoist/mygo/ui"

	"gotion/internal/emoji"
	"gotion/internal/notion"
	"gotion/internal/tr"
)

type calendarState struct {
	month       time.Time
	queriedKey  string
	moreDay     string
	addDay      string
	monthPicker bool
	pickYear    int
	moved       map[string]int
	// grabDays is how many days into its span a dragged bar was taken,
	// so a range keeps its place under the pointer when dropped.
	grabDays int
}

type allCalendarSource struct {
	ID       string
	Title    string
	DateCol  string
	DateProp notion.Property
	DateCols []notion.Property
}

type allCalendarItem struct {
	Page     *notion.Page
	DBID     string
	DBTitle  string
	DateCol  string
	DateProp notion.Property
}

type allCalendarState struct {
	sources    []allCalendarSource
	items      []allCalendarItem
	cal        calendarState
	loading    bool
	loaded     bool
	err        string
	picker     bool
	queriedKey string
}

type calendarSpan struct {
	row       *notion.Page
	dbID      string
	dbTitle   string
	dateCol   string
	dateProp  notion.Property
	startDay  time.Time
	endDay    time.Time
	timeLabel string
	multiDay  bool
}

type weekPlacement struct {
	span      calendarSpan
	colStart  int
	colSpan   int
	contLeft  bool
	contRight bool
	track     int
}

// calendarColumns returns all date columns in t.
func calendarColumns(t *tableView) []notion.Property {
	var cols []notion.Property
	for _, c := range t.columns {
		if c.Type == "date" {
			cols = append(cols, c)
		}
	}
	return cols
}

// calendarColumn picks the active date column: saved preference, else first date column.
func (a *App) calendarColumn(t *tableView) (notion.Property, bool) {
	cols := calendarColumns(t)
	if len(cols) == 0 {
		return notion.Property{}, false
	}
	if saved := a.set.CalendarDate[t.id]; saved != "" {
		for _, c := range cols {
			if c.Name == saved {
				return c, true
			}
		}
	}
	return cols[0], true
}

// calendarLookBack is how many days before the grid the queries reach, for
// ranges that start earlier.
const calendarLookBack = 31

// calendarGridRange returns the Monday-first grid start, Sunday-last grid end,
// and number of week rows for month in month.Location().
func calendarGridRange(month time.Time) (gridStart, gridEnd time.Time, weeks int) {
	loc := month.Location()
	first := time.Date(month.Year(), month.Month(), 1, 0, 0, 0, 0, loc)
	startOffset := (int(first.Weekday()) + 6) % 7 // Monday = 0, Sunday = 6
	gridStart = first.AddDate(0, 0, -startOffset)

	last := first.AddDate(0, 1, -1)
	endOffset := 6 - ((int(last.Weekday()) + 6) % 7)
	gridEnd = last.AddDate(0, 0, endOffset)
	days := int(gridEnd.Sub(gridStart).Hours()/24+0.5) + 1
	weeks = max(1, days/7)
	return gridStart, gridEnd, weeks
}

// calendarQueryFilter builds the Notion filter for the visible calendar grid
// range combined with any active table filters.
func calendarQueryFilter(t *tableView, colName string, gridStart, gridEnd time.Time) any {
	conds := []any{
		// Notion filters a range by its start: reach a month back so a
		// range begun before the grid but running into it still shows.
		map[string]any{"property": colName, "date": map[string]any{"on_or_after": gridStart.AddDate(0, 0, -calendarLookBack).Format("2006-01-02")}},
		map[string]any{"property": colName, "date": map[string]any{"on_or_before": gridEnd.Format("2006-01-02")}},
	}
	if base := t.filterBody(); base != nil {
		conds = append([]any{base}, conds...)
	}
	return map[string]any{"and": conds}
}

func (a *App) calendarRequery(t *tableView, col notion.Property, gridStart, gridEnd time.Time) {
	if a.client == nil || t.dsID == "" {
		return
	}
	t.loading = true
	client, ds := a.client, t.dsID
	filter := calendarQueryFilter(t, col.Name, gridStart, gridEnd)
	sorts := t.sortsBody()
	go func() {
		rows, err := client.QueryWith(context.Background(), ds, filter, sorts, 500)
		a.update(func() {
			t.loading = false
			if err != nil {
				a.toast(tr.T("Gagal memfilter: ") + errText(err))
				return
			}
			t.rows = rows
		})
	}()
}

// parseDateValue parses a single Notion date string (RFC3339 or YYYY-MM-DD) into loc.
func parseDateValue(s string, loc *time.Location) (t time.Time, hasTime bool, ok bool) {
	if s == "" {
		return time.Time{}, false, false
	}
	if len(s) > 10 && s[10] == 'T' {
		if parsed, err := time.Parse(time.RFC3339, s); err == nil {
			return parsed.In(loc), true, true
		}
	}
	if len(s) >= 10 {
		if parsed, err := time.ParseInLocation("2006-01-02", s[:10], loc); err == nil {
			return parsed, false, true
		}
	}
	return time.Time{}, false, false
}

// parseLocalDateRange parses a Notion date property's start and optional end
// into midnight dates in loc.
func parseLocalDateRange(raw json.RawMessage, loc *time.Location) (calendarSpan, bool) {
	var v struct {
		Date *struct {
			Start string `json:"start"`
			End   string `json:"end"`
		} `json:"date"`
	}
	if json.Unmarshal(raw, &v) != nil || v.Date == nil || v.Date.Start == "" {
		return calendarSpan{}, false
	}
	st, hasTime, ok := parseDateValue(v.Date.Start, loc)
	if !ok {
		return calendarSpan{}, false
	}
	startDay := time.Date(st.Year(), st.Month(), st.Day(), 0, 0, 0, 0, loc)
	endDay := startDay
	if v.Date.End != "" {
		if et, _, ok2 := parseDateValue(v.Date.End, loc); ok2 {
			ed := time.Date(et.Year(), et.Month(), et.Day(), 0, 0, 0, 0, loc)
			if !ed.Before(startDay) {
				endDay = ed
			}
		}
	}
	timeLabel := ""
	if hasTime {
		timeLabel = st.Format("15:04")
	}
	return calendarSpan{
		startDay:  startDay,
		endDay:    endDay,
		timeLabel: timeLabel,
		multiDay:  endDay.After(startDay),
	}, true
}

// placeWeekEvents lays out events overlapping [weekStart, weekStart+6d] into
// non-overlapping horizontal tracks. Multi-day spans sort before single-day items.
func placeWeekEvents(spans []calendarSpan, weekStart time.Time) []weekPlacement {
	weekEnd := weekStart.AddDate(0, 0, 6)
	var active []calendarSpan
	for _, sp := range spans {
		if sp.endDay.Before(weekStart) || sp.startDay.After(weekEnd) {
			continue
		}
		active = append(active, sp)
	}
	sort.SliceStable(active, func(i, j int) bool {
		if active[i].multiDay != active[j].multiDay {
			return active[i].multiDay
		}
		if !active[i].startDay.Equal(active[j].startDay) {
			return active[i].startDay.Before(active[j].startDay)
		}
		durI := active[i].endDay.Sub(active[i].startDay)
		durJ := active[j].endDay.Sub(active[j].startDay)
		if durI != durJ {
			return durI > durJ
		}
		return active[i].row.Title() < active[j].row.Title()
	})

	var tracks [][]bool
	var out []weekPlacement
	for _, sp := range active {
		cs := 0
		if sp.startDay.After(weekStart) {
			cs = int(sp.startDay.Sub(weekStart).Hours()/24 + 0.5)
		}
		ce := 6
		if sp.endDay.Before(weekEnd) {
			ce = int(sp.endDay.Sub(weekStart).Hours()/24 + 0.5)
		}
		cs = max(0, min(cs, 6))
		ce = max(cs, min(ce, 6))

		trackIdx := -1
		for ti, occ := range tracks {
			free := true
			for d := cs; d <= ce; d++ {
				if occ[d] {
					free = false
					break
				}
			}
			if free {
				trackIdx = ti
				break
			}
		}
		if trackIdx < 0 {
			tracks = append(tracks, make([]bool, 7))
			trackIdx = len(tracks) - 1
		}
		for d := cs; d <= ce; d++ {
			tracks[trackIdx][d] = true
		}
		out = append(out, weekPlacement{
			span:      sp,
			colStart:  cs,
			colSpan:   ce - cs + 1,
			contLeft:  sp.startDay.Before(weekStart),
			contRight: sp.endDay.After(weekEnd),
			track:     trackIdx,
		})
	}
	return out
}

func formatCalendarMonthTitle(month time.Time) string {
	if tr.Lang() == tr.ID {
		return idMonNames[month.Month()] + " " + strconv.Itoa(month.Year())
	}
	return month.Month().String() + " " + strconv.Itoa(month.Year())
}

func (a *App) calendarMonthPicker(c *ui.Context, key string, cal *calendarState, fontSize float32) {
	p := a.pal()
	accentBg, accentFg := tintColors(p.Accent, a.dark)
	btn := ui.Row(c.Key(key)).AlignItems(ui.Center).Gap(6).Padding(4, 8).Radius(8).Cursor(ui.CursorPointer)
	if btn.Hovered() || cal.monthPicker {
		btn.Background(p.Hover)
	}
	btn.Children(func() {
		ui.Text(c, formatCalendarMonthTitle(cal.month)).FontSize(fontSize).FontWeight(700)
		ui.Icon(c, iconChevronDown).Size(14, 14).TextColor(p.Muted)
	})
	if btn.Clicked() {
		cal.monthPicker = !cal.monthPicker
		if cal.monthPicker {
			cal.pickYear = cal.month.Year()
		}
	}
	if cal.monthPicker {
		if cal.pickYear == 0 {
			cal.pickYear = cal.month.Year()
		}
		ui.Popover(c, btn, &cal.monthPicker, func() {
			ui.Column(c).Width(240).Padding(10).Gap(10).Children(func() {
				ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
					py := ui.ButtonBase(c.Key(key+"-py")).Role(ui.RoleButton).Label(tr.T("Bulan sebelumnya")).
						Size(26, 26).Radius(6).Border(1, p.Line).Center().Cursor(ui.CursorPointer)
					if py.Hovered() {
						py.Background(p.Hover)
					}
					py.Children(func() { ui.Icon(c, iconBack).Size(12, 12).TextColor(p.Muted) })
					if py.Clicked() {
						cal.pickYear--
					}

					ui.Text(c, strconv.Itoa(cal.pickYear)).FontSize(14).FontWeight(600).Grow(1).TextAlign(ui.Center)

					ny := ui.ButtonBase(c.Key(key+"-ny")).Role(ui.RoleButton).Label(tr.T("Bulan berikutnya")).
						Size(26, 26).Radius(6).Border(1, p.Line).Center().Cursor(ui.CursorPointer)
					if ny.Hovered() {
						ny.Background(p.Hover)
					}
					ny.Children(func() { ui.Icon(c, iconChevron).Size(12, 12).TextColor(p.Muted) })
					if ny.Clicked() {
						cal.pickYear++
					}
				})

				ui.Grid(c).Columns(3).Gap(6).FillWidth().Children(func() {
					for m := time.January; m <= time.December; m++ {
						name := m.String()[:3]
						if tr.Lang() == tr.ID {
							name = idMonNames[m][:3]
						}
						active := cal.month.Year() == cal.pickYear && cal.month.Month() == m
						cell := ui.ButtonBase(c.Key(fmt.Sprintf("%s-m-%d", key, m))).Role(ui.RoleButton).Label(name).
							Padding(7, 0).Radius(7).Center().Cursor(ui.CursorPointer)
						if active {
							cell.Background(accentBg)
						} else if cell.Hovered() {
							cell.Background(p.Hover)
						}
						cell.Children(func() {
							tc := p.Text
							if active {
								tc = accentFg
							}
							ui.Text(c, name).FontSize(13).FontWeight(500).TextColor(tc)
						})
						if cell.Clicked() {
							cal.month = time.Date(cal.pickYear, m, 1, 0, 0, 0, 0, cal.month.Location())
							cal.moreDay = ""
							cal.addDay = ""
							cal.monthPicker = false
						}
					}
				})
			})
		})
	}
}

// calendar renders the monthly calendar view for table t.
func (a *App) calendar(c *ui.Context, i int, t *tableView) {
	p := a.pal()
	col, ok := a.calendarColumn(t)
	if !ok {
		ui.Column(c).Fill().Center().Gap(12).Children(func() {
			ui.Text(c, tr.T("Tambahkan kolom Tanggal untuk memakai tampilan Kalender.")).FontSize(14).TextColor(p.Muted)
			if a.btn(c, tr.T("Buat kolom Tanggal"), iconPlus, false, false) {
				a.changeSchema(t, map[string]any{"Tanggal": map[string]any{"date": map[string]any{}}}, tr.T("Kolom “")+"Tanggal"+tr.T("” ditambahkan"))
			}
		})
		return
	}

	now := nowFunc().In(time.Local)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	if t.cal.month.IsZero() {
		t.cal.month = time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, today.Location())
	}

	gridStart, gridEnd, weeks := calendarGridRange(t.cal.month)
	qKey := t.cal.month.Format("2006-01") + "|" + col.Name
	if t.cal.queriedKey != qKey && t.dsID != "" && !t.loading && a.client != nil {
		t.cal.queriedKey = qKey
		a.calendarRequery(t, col, gridStart, gridEnd)
	}

	// Parse spans & count undated rows.
	var spans []calendarSpan
	undated := 0
	for _, r := range t.rows {
		sp, has := parseLocalDateRange(r.Properties[col.Name], now.Location())
		if !has {
			undated++
			continue
		}
		sp.row = r
		spans = append(spans, sp)
	}

	// Toolbar: Month YYYY, < > Hari ini, Menurut: col, Tanpa tanggal · N
	ui.Row(c).Padding(12, 24, 10, 24).AlignItems(ui.Center).Gap(10).Children(func() {
		a.calendarMonthPicker(c, "cal-mp", &t.cal, 18)
		ui.Row(c).Gap(4).AlignItems(ui.Center).Children(func() {
			prev := ui.ButtonBase(c.Key("cal-prev")).Role(ui.RoleButton).Label(tr.T("Bulan sebelumnya")).
				Size(26, 26).Radius(6).Border(1, p.Line).Center().Cursor(ui.CursorPointer)
			if prev.Hovered() {
				prev.Background(p.Hover)
			}
			prev.Children(func() { ui.Icon(c, iconBack).Size(13, 13).TextColor(p.Muted) })
			if prev.Clicked() {
				t.cal.month = t.cal.month.AddDate(0, -1, 0)
				t.cal.moreDay = ""
				t.cal.addDay = ""
			}

			next := ui.ButtonBase(c.Key("cal-next")).Role(ui.RoleButton).Label(tr.T("Bulan berikutnya")).
				Size(26, 26).Radius(6).Border(1, p.Line).Center().Cursor(ui.CursorPointer)
			if next.Hovered() {
				next.Background(p.Hover)
			}
			next.Children(func() { ui.Icon(c, iconChevron).Size(13, 13).TextColor(p.Muted) })
			if next.Clicked() {
				t.cal.month = t.cal.month.AddDate(0, 1, 0)
				t.cal.moreDay = ""
				t.cal.addDay = ""
			}

			if a.btn(c, tr.T("Hari ini"), nil, false, false) {
				t.cal.month = time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, today.Location())
				t.cal.moreDay = ""
				t.cal.addDay = ""
			}
		})

		ui.Box(c).Grow(1)

		cols := calendarColumns(t)
		if len(cols) > 0 {
			ui.Row(c).AlignItems(ui.Center).Gap(6).Children(func() {
				ui.Icon(c, iconCalendar).Size(14, 14).TextColor(p.Muted)
				ui.Text(c, tr.T("Menurut:")).FontSize(12).TextColor(p.Muted)
				names := make([]string, len(cols))
				for k, dc := range cols {
					names[k] = dc.Name
				}
				chosen := col.Name
				if ui.Select(c, &chosen, names).Width(130).Label(tr.T("Menurut:")).Changed() {
					if a.set.CalendarDate == nil {
						a.set.CalendarDate = map[string]string{}
					}
					a.set.CalendarDate[t.id] = chosen
					a.saveSettings()
				}
			})
		}
		if undated > 0 {
			ui.Text(c, tr.T("Tanpa tanggal")+" · "+strconv.Itoa(undated)).FontSize(12).TextColor(p.Faint)
		}
	})

	// Weekday header row (Monday first).
	dayNames := []string{enDaysShort[1], enDaysShort[2], enDaysShort[3], enDaysShort[4], enDaysShort[5], enDaysShort[6], enDaysShort[0]}
	if tr.Lang() == tr.ID {
		dayNames = []string{idDaysShort[1], idDaysShort[2], idDaysShort[3], idDaysShort[4], idDaysShort[5], idDaysShort[6], idDaysShort[0]}
	}
	ui.Grid(c).Columns(7).FillWidth().Padding(0, 24, 6, 24).Children(func() {
		for _, dn := range dayNames {
			ui.Text(c, dn).FontSize(11).FontWeight(600).TextColor(p.Faint).Padding(0, 6)
		}
	})

	a.renderCalendarGrid(c, i, &t.cal, today, gridStart, weeks, spans,
		func(r *notion.Page, day time.Time) {
			a.moveCalendarRow(t, r, col, day)
		},
		func(r *notion.Page, sp calendarSpan) {
			a.openRow(i, t, r)
		},
		func(cell ui.Element, dayKey string) {
			if t.cal.addDay == dayKey && t.adding.open && t.adding.groupCol == col.Name && t.adding.groupVal == dayKey {
				openFlag := true
				ui.Popover(c, cell, &openFlag, func() {
					ui.Column(c).Width(250).Padding(10).Gap(8).Children(func() {
						ui.Text(c, notion.FormatProperty(json.RawMessage(`{"type":"date","date":{"start":"`+dayKey+`"}}`))).
							FontSize(12).FontWeight(600).TextColor(p.Faint)
						a.addRowForm(c, i, t, true)
					})
				})
				if !openFlag {
					t.cal.addDay = ""
					t.adding = newRow{}
				}
			}
		},
		func(dayKey string) {
			t.cal.moreDay = ""
			t.cal.addDay = dayKey
			t.adding = newRow{open: true, groupCol: col.Name, groupVal: dayKey}
			a.loadTemplates(t)
		},
	)
}

func (a *App) renderCalendarGrid(
	c *ui.Context,
	i int,
	cal *calendarState,
	today, gridStart time.Time,
	weeks int,
	spans []calendarSpan,
	onDrop func(r *notion.Page, day time.Time),
	onOpen func(r *notion.Page, sp calendarSpan),
	dayExtra func(cell ui.Element, dayKey string),
	onCellClick func(dayKey string),
) {
	p := a.pal()
	accentBg, accentFg := tintColors(p.Accent, a.dark)
	const maxVisibleTracks = 3
	const dayHeaderH = float32(28)
	const barH = float32(22)
	const barGap = float32(4)
	const weekCellH = float32(124)

	ui.Scroll(c.Key("cal-scroll")).Grow(1).MinHeight(0).Padding(0, 24, 20, 24).ScrollbarStyle(a.thinScrollbar()).Children(func() {
		ui.Column(c).FillWidth().Border(1, p.Line).Radius(10).Clip().Children(func() {
			for w := range weeks {
				weekStart := gridStart.AddDate(0, 0, w*7)
				placements := placeWeekEvents(spans, weekStart)

				dayEvents := make([][]calendarSpan, 7)
				hiddenCounts := make([]int, 7)
				for _, pl := range placements {
					for d := pl.colStart; d < pl.colStart+pl.colSpan; d++ {
						dayEvents[d] = append(dayEvents[d], pl.span)
						if pl.track >= maxVisibleTracks {
							hiddenCounts[d]++
						}
					}
				}

				weekBox := ui.Box(c.Key("cal-w-" + weekStart.Format("2006-01-02"))).FillWidth().Height(weekCellH)
				if w > 0 {
					weekBox.BorderWidth(1, 0, 0, 0).BorderColor(p.Line)
				}
				weekBox.Children(func() {
					ui.Grid(c).Columns(7).Fill().Children(func() {
						for d := range 7 {
							day := weekStart.AddDate(0, 0, d)
							dayKey := day.Format("2006-01-02")
							inMonth := day.Month() == cal.month.Month()
							isToday := day.Equal(today)

							cell := ui.Column(c.Key("cal-d-"+dayKey)).Fill().Padding(6, 6, 4, 6).Cursor(ui.CursorPointer)
							if d > 0 {
								cell.BorderWidth(0, 0, 0, 1).BorderColor(p.Line)
							}
							if _, over := ui.DragOver[*notion.Page](cell); over {
								cell.Background(p.EditBg)
							} else if !inMonth {
								cell.Background(p.Sidebar.Alpha(0.35))
							}
							if r, dropped := ui.Drop[*notion.Page](cell); dropped && onDrop != nil {
								onDrop(r, day.AddDate(0, 0, -cal.grabDays))
								cal.grabDays = 0
							}

							cell.Children(func() {
								ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
									numText := strconv.Itoa(day.Day())
									if day.Day() == 1 && !inMonth {
										if tr.Lang() == tr.ID {
											numText += " " + idMonNames[day.Month()][:3]
										} else {
											numText += " " + day.Month().String()[:3]
										}
									}
									if isToday {
										ui.Box(c).Size(22, 22).Radius(11).Background(p.Accent).Center().Children(func() {
											ui.Text(c, numText).FontSize(11).FontWeight(600).TextColor(ui.RGB(255, 255, 255))
										})
									} else {
										tc := p.Text
										if !inMonth {
											tc = p.Faint
										}
										ui.Text(c, numText).FontSize(12).FontWeight(500).TextColor(tc)
									}
								})

								ui.Box(c).Grow(1)

								if hiddenCounts[d] > 0 {
									moreOpen := cal.moreDay == dayKey
									moreBtn := ui.Text(c.Key("more-"+dayKey), fmt.Sprintf(tr.T("+%d lagi"), hiddenCounts[d])).
										FontSize(11).FontWeight(500).TextColor(p.Muted).Padding(2, 4).Radius(4).Cursor(ui.CursorPointer)
									if moreBtn.Hovered() {
										moreBtn.Background(p.Hover)
									}
									if moreBtn.Clicked() {
										if moreOpen {
											cal.moreDay = ""
										} else {
											cal.moreDay = dayKey
										}
									}
									if moreOpen {
										openFlag := true
										ui.Popover(c, moreBtn, &openFlag, func() {
											ui.Column(c).Width(250).Padding(8).Gap(6).Children(func() {
												ui.Text(c, notion.FormatProperty(json.RawMessage(`{"type":"date","date":{"start":"`+dayKey+`"}}`))).
													FontSize(12).FontWeight(600).TextColor(p.Faint)
												for _, sp := range dayEvents[d] {
													a.calendarRowPill(c, cal, sp, p.Sidebar, p.Text, onOpen)
												}
											})
										})
										if !openFlag {
											cal.moreDay = ""
										}
									}
								}

								if dayExtra != nil {
									dayExtra(cell, dayKey)
								}
							})

							if cell.Clicked() && hiddenCounts[d] == 0 && onCellClick != nil {
								onCellClick(dayKey)
							}
						}
					})

					for _, pl := range placements {
						if pl.track >= maxVisibleTracks {
							continue
						}
						top := dayHeaderH + float32(pl.track)*(barH+barGap)
						leftPct := float32(pl.colStart) * 100 / 7
						widthPct := float32(pl.colSpan) * 100 / 7

						slot := ui.Box(c.Key(fmt.Sprintf("ev-%s-%d-%d", pl.span.row.ID, w, pl.track))).
							Absolute().Top(top).LeftPercent(leftPct).WidthPercent(widthPct).Height(barH).Padding(0, 3)

						slot.Children(func() {
							bg, fg := p.Sidebar, p.Text
							if pl.span.multiDay {
								bg, fg = accentBg, accentFg
							}
							bar := ui.Row(c.Key("bar-"+pl.span.row.ID)).Fill().Padding(0, 7).Gap(5).
								AlignItems(ui.Center).Background(bg).Cursor(ui.CursorPointer)
							switch {
							case !pl.contLeft && !pl.contRight:
								bar.Radius(5)
							case pl.contLeft && !pl.contRight:
								bar.Radius(0, 5, 5, 0)
							case !pl.contLeft && pl.contRight:
								bar.Radius(5, 0, 0, 5)
							}
							if bar.Hovered() {
								bar.Border(1, p.EditLine)
							}
							if cal.moved != nil && cal.moved[pl.span.row.ID] > 0 {
								if cal.moved[pl.span.row.ID] > 1 {
									bar.AnimatePose(ui.Pose{Y: -4, Fade: 0.4}, ui.Snappy)
									c.Invalidate()
								} else {
									bar.AnimatePose(ui.Pose{}, ui.Snappy)
								}
								cal.moved[pl.span.row.ID]--
							}
							if px, _, over := bar.PointerPosition(); over && bar.Pressed() && !bar.Dragging() {
								if b := bar.LayoutBounds(); b.W > 0 {
									col := pl.colStart + min(pl.colSpan-1, max(0, int(px/(b.W/float32(pl.colSpan)))))
									cal.grabDays = calendarDaysBetween(pl.span.startDay, weekStart.AddDate(0, 0, col))
								}
							}
							bar.Drag(pl.span.row)
							bar.Children(func() {
								if pl.span.timeLabel != "" && !pl.contLeft {
									ui.Text(c, pl.span.timeLabel).FontSize(11).TextColor(p.Muted).Shrink(0)
								}
								if em := pl.span.row.Emoji(); em != "" {
									emoji.View(c, em, 13)
								}
								title := nonEmpty(pl.span.row.Title(), tr.T("Tanpa judul"))
								ui.Text(c, title).FontSize(12).FontWeight(500).TextColor(fg).SingleLine().Grow(1).MinWidth(0)
								// The database's name only where the title has room
								// beside it, and it gives way first.
								if pl.span.dbTitle != "" && !pl.contLeft && pl.colSpan >= 2 {
									ui.Text(c, pl.span.dbTitle).FontSize(10).TextColor(p.Faint).SingleLine().Shrink(1).MinWidth(0).MaxWidth(120)
								}
							})
							if bar.Clicked() && onOpen != nil {
								onOpen(pl.span.row, pl.span)
							}
						})
					}
				})
			}
		})
	})
}

func (a *App) calendarRowPill(c *ui.Context, cal *calendarState, sp calendarSpan, bg, fg ui.Color, onOpen func(r *notion.Page, sp calendarSpan)) {
	row := ui.Row(c.Key("pop-ev-"+sp.row.ID)).FillWidth().Padding(5, 8).Gap(6).Radius(6).
		Background(bg).AlignItems(ui.Center).Cursor(ui.CursorPointer)
	row.Children(func() {
		if sp.timeLabel != "" {
			ui.Text(c, sp.timeLabel).FontSize(11).TextColor(a.pal().Muted).Shrink(0)
		}
		if em := sp.row.Emoji(); em != "" {
			emoji.View(c, em, 13)
		}
		ui.Text(c, nonEmpty(sp.row.Title(), tr.T("Tanpa judul"))).FontSize(12).FontWeight(500).TextColor(fg).SingleLine().Grow(1).MinWidth(0)
		if sp.dbTitle != "" {
			ui.Text(c, sp.dbTitle).FontSize(10).TextColor(a.pal().Faint).SingleLine().Shrink(1).MinWidth(0).MaxWidth(120)
		}
	})
	if row.Clicked() && onOpen != nil {
		cal.moreDay = ""
		onOpen(sp.row, sp)
	}
}

// loadAllCalendar queries all databases/data sources having a date column for
// the visible month grid range [gridStart, gridEnd].
func (a *App) loadAllCalendar(gridStart, gridEnd time.Time) {
	if a.client == nil || a.allCal.loading {
		return
	}
	var dbNodes []*node
	for _, n := range a.nodes {
		if n.Kind == "data_source" || n.Kind == "database" {
			dbNodes = append(dbNodes, &node{ID: n.ID, Title: n.Title, Kind: n.Kind})
		}
	}
	a.allCal.loading, a.allCal.err = true, ""
	client := a.client
	off := map[string]bool{}
	for k, v := range a.set.CalOff {
		off[k] = v
	}
	calDatePref := map[string]string{}
	for k, v := range a.set.CalendarDate {
		calDatePref[k] = v
	}
	startStr := gridStart.AddDate(0, 0, -calendarLookBack).Format("2006-01-02")
	endStr := gridEnd.Format("2006-01-02")

	go func() {
		ctx := context.Background()
		sem := make(chan struct{}, 3)
		var wg sync.WaitGroup
		var mu sync.Mutex
		var sources []allCalendarSource
		var items []allCalendarItem
		var firstErr error
		seenDS := map[string]bool{}

		for _, n := range dbNodes {
			wg.Add(1)
			go func(n *node) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()

				dsID := n.ID
				if n.Kind == "database" {
					db, err := client.Database(ctx, n.ID)
					if err != nil {
						mu.Lock()
						if firstErr == nil {
							firstErr = err
						}
						mu.Unlock()
						return
					}
					if len(db.DataSources) == 0 {
						return
					}
					dsID = db.DataSources[0].ID
				}

				mu.Lock()
				if seenDS[dsID] {
					mu.Unlock()
					return
				}
				seenDS[dsID] = true
				mu.Unlock()

				ds, err := client.DataSource(ctx, dsID)
				if err != nil {
					mu.Lock()
					if firstErr == nil {
						firstErr = err
					}
					mu.Unlock()
					return
				}

				var dateCols []notion.Property
				var propNames []string
				for name := range ds.Properties {
					propNames = append(propNames, name)
				}
				sort.Strings(propNames)
				for _, name := range propNames {
					p := ds.Properties[name]
					if p.Name == "" {
						p.Name = name
					}
					if p.Type == "date" {
						dateCols = append(dateCols, p)
					}
				}
				if len(dateCols) == 0 {
					return
				}
				chosen := dateCols[0]
				if pref := calDatePref[dsID]; pref != "" {
					for _, dc := range dateCols {
						if dc.Name == pref {
							chosen = dc
							break
						}
					}
				} else if pref := calDatePref[n.ID]; pref != "" {
					for _, dc := range dateCols {
						if dc.Name == pref {
							chosen = dc
							break
						}
					}
				}

				title := nonEmpty(notion.PlainText(ds.Title), n.Title)
				mu.Lock()
				sources = append(sources, allCalendarSource{
					ID:       dsID,
					Title:    title,
					DateCol:  chosen.Name,
					DateProp: chosen,
					DateCols: dateCols,
				})
				mu.Unlock()

				if off[dsID] || off[n.ID] {
					return
				}

				filter := map[string]any{"and": []any{
					map[string]any{"property": chosen.Name, "date": map[string]any{"on_or_after": startStr}},
					map[string]any{"property": chosen.Name, "date": map[string]any{"on_or_before": endStr}},
				}}
				pages, err := client.QueryWith(ctx, dsID, filter, nil, 200)
				if err != nil {
					mu.Lock()
					if firstErr == nil {
						firstErr = err
					}
					mu.Unlock()
					return
				}
				mu.Lock()
				for _, pg := range pages {
					items = append(items, allCalendarItem{
						Page:     pg,
						DBID:     dsID,
						DBTitle:  title,
						DateCol:  chosen.Name,
						DateProp: chosen,
					})
				}
				mu.Unlock()
			}(n)
		}
		wg.Wait()

		sort.Slice(sources, func(i, j int) bool {
			return sources[i].Title < sources[j].Title
		})

		a.update(func() {
			a.allCal.loading = false
			a.allCal.loaded = true
			if firstErr != nil && len(items) == 0 && len(sources) == 0 {
				a.allCal.err = errText(firstErr)
				return
			}
			a.allCal.sources = sources
			a.allCal.items = items
		})
	}()
}

// allCalendarPanel renders the unified profile Calendar across all databases.
func (a *App) allCalendarPanel(c *ui.Context, i int) {
	p := a.pal()
	st := &a.allCal
	a.panelHeader(c, i, iconCalendar, tr.T("Kalender"), "calendar", nil)

	now := nowFunc().In(time.Local)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	if st.cal.month.IsZero() {
		st.cal.month = time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, today.Location())
	}

	gridStart, gridEnd, weeks := calendarGridRange(st.cal.month)
	qKey := st.cal.month.Format("2006-01")
	if st.queriedKey != qKey && !st.loading && a.client != nil {
		st.queriedKey = qKey
		a.loadAllCalendar(gridStart, gridEnd)
	}

	var spans []calendarSpan
	for _, it := range st.items {
		if it.Page == nil {
			continue
		}
		sp, has := parseLocalDateRange(it.Page.Properties[it.DateCol], now.Location())
		if !has {
			continue
		}
		sp.row = it.Page
		sp.dbID = it.DBID
		sp.dbTitle = it.DBTitle
		sp.dateCol = it.DateCol
		sp.dateProp = it.DateProp
		spans = append(spans, sp)
	}

	ui.Row(c).Padding(14, 24, 10, 24).AlignItems(ui.Center).Gap(10).Children(func() {
		a.calendarMonthPicker(c, "acal-mp", &st.cal, 22)
		ui.Row(c).Gap(4).AlignItems(ui.Center).Children(func() {
			prev := ui.ButtonBase(c.Key("acal-prev")).Role(ui.RoleButton).Label(tr.T("Bulan sebelumnya")).
				Size(26, 26).Radius(6).Border(1, p.Line).Center().Cursor(ui.CursorPointer)
			if prev.Hovered() {
				prev.Background(p.Hover)
			}
			prev.Children(func() { ui.Icon(c, iconBack).Size(13, 13).TextColor(p.Muted) })
			if prev.Clicked() {
				st.cal.month = st.cal.month.AddDate(0, -1, 0)
				st.cal.moreDay = ""
			}

			next := ui.ButtonBase(c.Key("acal-next")).Role(ui.RoleButton).Label(tr.T("Bulan berikutnya")).
				Size(26, 26).Radius(6).Border(1, p.Line).Center().Cursor(ui.CursorPointer)
			if next.Hovered() {
				next.Background(p.Hover)
			}
			next.Children(func() { ui.Icon(c, iconChevron).Size(13, 13).TextColor(p.Muted) })
			if next.Clicked() {
				st.cal.month = st.cal.month.AddDate(0, 1, 0)
				st.cal.moreDay = ""
			}

			if a.btn(c, tr.T("Hari ini"), nil, false, false) {
				st.cal.month = time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, today.Location())
				st.cal.moreDay = ""
			}
		})

		ui.Box(c).Grow(1)

		if len(st.sources) > 0 {
			dbBtn := ui.ButtonBase(c.Key("cal-dbs")).Role(ui.RoleButton).Label("Database").
				Padding(6, 12).Radius(8).Border(1, p.Line).Cursor(ui.CursorPointer)
			if dbBtn.Hovered() {
				dbBtn.Background(p.Hover)
			}
			dbBtn.Children(func() {
				ui.Row(c).AlignItems(ui.Center).Gap(6).Children(func() {
					ui.Icon(c, iconTable).Size(14, 14).TextColor(p.Muted)
					ui.Text(c, "Database").FontSize(12).FontWeight(500).TextColor(p.Text)
				})
			})
			if dbBtn.Clicked() {
				st.picker = !st.picker
			}
			ui.Popover(c, dbBtn, &st.picker, func() {
				ui.Column(c).Width(280).Padding(8).Gap(6).Children(func() {
					ui.Text(c, tr.T("Database di Kalender")).FontSize(12).FontWeight(600).TextColor(p.Faint).Padding(2, 4)
					for _, src := range st.sources {
						on := !a.set.CalOff[src.ID]
						if ui.Checkbox(c.Key("cd-"+src.ID), &on, nonEmpty(src.Title, tr.T("Database tanpa judul"))).Changed() {
							if a.set.CalOff == nil {
								a.set.CalOff = map[string]bool{}
							}
							if on {
								delete(a.set.CalOff, src.ID)
							} else {
								a.set.CalOff[src.ID] = true
							}
							a.saveSettings()
							a.loadAllCalendar(gridStart, gridEnd)
						}
						if on && len(src.DateCols) > 1 {
							names := make([]string, len(src.DateCols))
							for k, dc := range src.DateCols {
								names[k] = dc.Name
							}
							chosen := src.DateCol
							ui.Row(c.Key("cdcol-"+src.ID)).AlignItems(ui.Center).Gap(6).Padding(0, 0, 2, 24).Children(func() {
								ui.Text(c, tr.T("Menurut:")).FontSize(11).TextColor(p.Faint)
								if ui.Select(c.Key("cdsel-"+src.ID), &chosen, names).Width(150).Label(tr.T("Menurut:")).Changed() {
									if a.set.CalendarDate == nil {
										a.set.CalendarDate = map[string]string{}
									}
									a.set.CalendarDate[src.ID] = chosen
									a.saveSettings()
									a.loadAllCalendar(gridStart, gridEnd)
								}
							})
						}
					}
				})
			})
		}
	})

	if st.loading && len(st.items) == 0 {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Padding(12, 24).Children(func() {
			ui.Spinner(c).Size(16, 16)
			ui.Text(c, tr.T("Memuat…")).FontSize(13).TextColor(p.Faint)
		})
		return
	}
	if st.err != "" && len(st.items) == 0 {
		ui.Text(c, st.err).FontSize(13).TextColor(c.Theme().Danger).Padding(12, 24)
		return
	}

	dayNames := []string{enDaysShort[1], enDaysShort[2], enDaysShort[3], enDaysShort[4], enDaysShort[5], enDaysShort[6], enDaysShort[0]}
	if tr.Lang() == tr.ID {
		dayNames = []string{idDaysShort[1], idDaysShort[2], idDaysShort[3], idDaysShort[4], idDaysShort[5], idDaysShort[6], idDaysShort[0]}
	}
	ui.Grid(c).Columns(7).FillWidth().Padding(0, 24, 6, 24).Children(func() {
		for _, dn := range dayNames {
			ui.Text(c, dn).FontSize(11).FontWeight(600).TextColor(p.Faint).Padding(0, 6)
		}
	})

	a.renderCalendarGrid(c, i, &st.cal, today, gridStart, weeks, spans,
		func(r *notion.Page, day time.Time) {
			for idx := range st.items {
				if st.items[idx].Page == r {
					a.moveAllCalendarItem(&st.items[idx], day)
					break
				}
			}
		},
		func(r *notion.Page, sp calendarSpan) {
			a.openRow(i, &tableView{id: sp.dbID}, r)
		},
		nil,
		nil,
	)
}

func (a *App) moveAllCalendarItem(it *allCalendarItem, targetDay time.Time) {
	if it == nil || it.Page == nil {
		return
	}
	oldRaw := it.Page.Properties[it.DateCol]
	if sp, ok := parseLocalDateRange(oldRaw, targetDay.Location()); ok && sp.startDay.Equal(targetDay) {
		return
	}
	send, show, err := notion.DeferDate(it.DateProp, oldRaw, targetDay)
	if err != nil {
		a.toast(tr.T("Gagal memindahkan tanggal: ") + err.Error())
		return
	}
	if it.Page.Properties == nil {
		it.Page.Properties = map[string]json.RawMessage{}
	}
	it.Page.Properties[it.DateCol] = show
	if a.allCal.cal.moved == nil {
		a.allCal.cal.moved = map[string]int{}
	}
	a.allCal.cal.moved[it.Page.ID] = 2

	client := a.client
	if client == nil {
		return
	}
	go func() {
		_, err := client.SetProperties(context.Background(), it.Page.ID, map[string]any{it.DateCol: send})
		if err != nil {
			a.update(func() {
				it.Page.Properties[it.DateCol] = oldRaw
				a.toast(tr.T("Gagal memindahkan tanggal: ") + errText(err))
			})
		}
	}()
}

// moveCalendarRow moves row r's date property col to targetDay, preserving
// time-of-day and multi-day range duration, with optimistic UI and rollback.
func (a *App) moveCalendarRow(t *tableView, r *notion.Page, col notion.Property, targetDay time.Time) {
	if r == nil {
		return
	}
	oldRaw := r.Properties[col.Name]
	if sp, ok := parseLocalDateRange(oldRaw, targetDay.Location()); ok && sp.startDay.Equal(targetDay) {
		return
	}
	send, show, err := notion.DeferDate(col, oldRaw, targetDay)
	if err != nil {
		a.toast(tr.T("Gagal memindahkan tanggal: ") + err.Error())
		return
	}
	if r.Properties == nil {
		r.Properties = map[string]json.RawMessage{}
	}
	r.Properties[col.Name] = show
	if t.cal.moved == nil {
		t.cal.moved = map[string]int{}
	}
	t.cal.moved[r.ID] = 2

	client := a.client
	if client == nil {
		return
	}
	go func() {
		_, err := client.SetProperties(context.Background(), r.ID, map[string]any{col.Name: send})
		if err != nil {
			a.update(func() {
				r.Properties[col.Name] = oldRaw
				a.toast(tr.T("Gagal memindahkan tanggal: ") + errText(err))
			})
		}
	}()
}

// calendarDaysBetween counts calendar days from a to b (both midnights),
// right across a daylight-saving change.
func calendarDaysBetween(a, b time.Time) int {
	ua := time.Date(a.Year(), a.Month(), a.Day(), 0, 0, 0, 0, time.UTC)
	ub := time.Date(b.Year(), b.Month(), b.Day(), 0, 0, 0, 0, time.UTC)
	return int(ub.Sub(ua).Hours() / 24)
}
