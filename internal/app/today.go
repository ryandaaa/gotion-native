package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/egoist/mygo/ui"

	"gotion/internal/emoji"
	"gotion/internal/notion"
	"gotion/internal/tr"
)

const todayRefreshInterval = 5 * time.Minute

// todayItem is a task row surfaced in the "Hari ini" view from an eligible
// data source (having both people and date columns).
type todayItem struct {
	Page      *notion.Page    `json:"page"`
	DBID      string          `json:"dbID"`
	DBTitle   string          `json:"dbTitle"`
	PeopleCol string          `json:"peopleCol"`
	DateCol   string          `json:"dateCol"`
	DoneCol   string          `json:"doneCol"`
	DoneProp  notion.Property `json:"doneProp"`
	StatusCol string          `json:"statusCol"`
}

// todaySource is a data source eligible for the "Hari ini" view.
type todaySource struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// todayGroups holds items grouped into Overdue, Today, and This Week.
type todayGroups struct {
	Overdue []todayItem
	Today   []todayItem
	Week    []todayItem
}

type todayState struct {
	items     []todayItem
	eligible  []todaySource
	loading   bool
	loaded    bool
	err       string
	lastFetch time.Time
	picker    bool
	tv        tableView // backing state for editable() popovers
	cancel    context.CancelFunc
}

func todayCachePath() string {
	dir := configDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "today.json")
}

func (a *App) loadTodayCache() {
	if a.client == nil {
		return
	}
	path := todayCachePath()
	if path == "" {
		return
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var cached struct {
		Items    []todayItem   `json:"items"`
		Eligible []todaySource `json:"eligible"`
	}
	if json.Unmarshal(b, &cached) == nil {
		a.today.items = cached.Items
		a.today.eligible = cached.Eligible
		if len(cached.Items) > 0 || len(cached.Eligible) > 0 {
			a.today.loaded = true
		}
	}
}

func (a *App) saveTodayCache() {
	path := todayCachePath()
	if path == "" {
		return
	}
	payload := struct {
		Items    []todayItem   `json:"items"`
		Eligible []todaySource `json:"eligible"`
	}{Items: a.today.items, Eligible: a.today.eligible}
	if b, err := json.Marshal(payload); err == nil {
		os.MkdirAll(filepath.Dir(path), 0o700)
		os.WriteFile(path, b, 0o600)
	}
}

func (a *App) startTodayTimer() {
	if a.today.cancel != nil || a.client == nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.today.cancel = cancel
	go func() {
		tk := time.NewTicker(todayRefreshInterval)
		defer tk.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tk.C:
				a.update(func() { a.loadToday() })
			}
		}
	}()
}

func (a *App) stopTodayTimer() {
	if a.today.cancel != nil {
		a.today.cancel()
		a.today.cancel = nil
	}
}

// buildTodayFilter constructs the Notion query filter for a data source:
// people contains userID, date <= todayStr, and incomplete when a status or
// checkbox column exists.
func buildTodayFilter(peopleCol, dateCol, userID string, doneProp notion.Property, todayStr string) any {
	var conds []any
	if userID != "" && peopleCol != "" {
		conds = append(conds, map[string]any{
			"property": peopleCol,
			"people":   map[string]any{"contains": userID},
		})
	}
	if dateCol != "" && todayStr != "" {
		conds = append(conds, map[string]any{
			"property": dateCol,
			"date":     map[string]any{"on_or_before": todayStr},
		})
	}
	switch doneProp.Type {
	case "checkbox":
		conds = append(conds, map[string]any{
			"property": doneProp.Name,
			"checkbox": map[string]any{"equals": false},
		})
	case "status":
		for _, opt := range doneProp.CompleteOptions() {
			conds = append(conds, map[string]any{
				"property": doneProp.Name,
				"status":   map[string]any{"does_not_equal": opt},
			})
		}
	}
	switch len(conds) {
	case 0:
		return nil
	case 1:
		return conds[0]
	default:
		return map[string]any{"and": conds}
	}
}

// parseLocalDate parses a Notion date property's start into midnight in loc.
func parseLocalDate(raw json.RawMessage, loc *time.Location) (time.Time, bool) {
	var v struct {
		Date *struct {
			Start string `json:"start"`
		} `json:"date"`
	}
	if json.Unmarshal(raw, &v) != nil || v.Date == nil || v.Date.Start == "" {
		return time.Time{}, false
	}
	s := v.Date.Start
	if len(s) > 10 && s[10] == 'T' {
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			lt := t.In(loc)
			return time.Date(lt.Year(), lt.Month(), lt.Day(), 0, 0, 0, 0, loc), true
		}
	}
	if len(s) >= 10 {
		if t, err := time.ParseInLocation("2006-01-02", s[:10], loc); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// classifyTodayItems filters out completed items and buckets remaining items
// into Overdue, Today, and This Week using now's local timezone.
func classifyTodayItems(items []todayItem, now time.Time) todayGroups {
	loc := now.Location()
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	weekEnd := todayStart.AddDate(0, 0, 7)
	var out todayGroups
	for _, it := range items {
		if it.Page == nil {
			continue
		}
		if it.DoneCol != "" && it.DoneProp.IsComplete(it.Page.Properties[it.DoneCol]) {
			continue
		}
		d, ok := parseLocalDate(it.Page.Properties[it.DateCol], loc)
		if !ok {
			continue
		}
		switch {
		case d.Before(todayStart):
			out.Overdue = append(out.Overdue, it)
		case d.Equal(todayStart):
			out.Today = append(out.Today, it)
		case !d.After(weekEnd):
			out.Week = append(out.Week, it)
		}
	}
	byDate := func(xs []todayItem) {
		sort.Slice(xs, func(i, j int) bool {
			di, _ := parseLocalDate(xs[i].Page.Properties[xs[i].DateCol], loc)
			dj, _ := parseLocalDate(xs[j].Page.Properties[xs[j].DateCol], loc)
			if !di.Equal(dj) {
				return di.Before(dj)
			}
			return xs[i].Page.Title() < xs[j].Page.Title()
		})
	}
	byDate(out.Overdue)
	byDate(out.Today)
	byDate(out.Week)
	return out
}

func (a *App) todayActiveCount() int {
	g := classifyTodayItems(a.today.items, nowFunc().In(time.Local))
	return len(g.Overdue) + len(g.Today) + len(g.Week)
}

// loadToday queries all eligible data sources (people + date) concurrently
// with a cap of 3 in-flight requests to respect Notion's rate limit.
func (a *App) loadToday() {
	if a.client == nil || a.today.loading {
		return
	}
	var dbNodes []*node
	for _, n := range a.nodes {
		if n.Kind == "data_source" || n.Kind == "database" {
			dbNodes = append(dbNodes, &node{ID: n.ID, Title: n.Title, Kind: n.Kind})
		}
	}
	a.today.loading, a.today.err = true, ""
	client := a.client
	bot := a.bot
	off := map[string]bool{}
	for k, v := range a.set.TodayOff {
		off[k] = v
	}
	todayStr := nowFunc().In(time.Local).Format("2006-01-02")

	go func() {
		ctx := context.Background()
		if bot == nil || bot.PersonID() == "" {
			if me, err := client.Me(ctx); err == nil {
				bot = me
			}
		}
		userID := bot.PersonID()

		sem := make(chan struct{}, 3)
		var wg sync.WaitGroup
		var mu sync.Mutex
		var eligible []todaySource
		var items []todayItem
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

				var peopleCol, dateCol, statusCol string
				var doneProp, cbProp, stProp notion.Property
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
					switch p.Type {
					case "people":
						if peopleCol == "" {
							peopleCol = p.Name
						}
					case "date":
						if dateCol == "" {
							dateCol = p.Name
						}
					case "status":
						if statusCol == "" {
							statusCol = p.Name
							stProp = p
						}
					case "checkbox":
						if cbProp.Name == "" {
							cbProp = p
						}
					}
				}
				if peopleCol == "" || dateCol == "" {
					return
				}
				switch {
				case stProp.Name != "":
					doneProp = stProp
				case cbProp.Name != "":
					doneProp = cbProp
				}

				title := nonEmpty(notion.PlainText(ds.Title), n.Title)
				mu.Lock()
				eligible = append(eligible, todaySource{ID: dsID, Title: title})
				mu.Unlock()

				if off[dsID] || off[n.ID] {
					return
				}

				filter := buildTodayFilter(peopleCol, dateCol, userID, doneProp, todayStr)
				sorts := []any{map[string]any{"property": dateCol, "direction": "ascending"}}
				pages, err := client.QueryWith(ctx, dsID, filter, sorts, 200)
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
					items = append(items, todayItem{
						Page:      pg,
						DBID:      dsID,
						DBTitle:   title,
						PeopleCol: peopleCol,
						DateCol:   dateCol,
						DoneCol:   doneProp.Name,
						DoneProp:  doneProp,
						StatusCol: statusCol,
					})
				}
				mu.Unlock()
			}(n)
		}
		wg.Wait()

		sort.Slice(eligible, func(i, j int) bool {
			return eligible[i].Title < eligible[j].Title
		})

		a.update(func() {
			a.today.loading = false
			a.today.loaded = true
			a.today.lastFetch = nowFunc()
			if a.bot == nil && bot != nil {
				a.bot = bot
			}
			if firstErr != nil && len(items) == 0 && len(eligible) == 0 {
				a.today.err = errText(firstErr)
				return
			}
			a.today.eligible = eligible
			a.today.items = items
			a.saveTodayCache()
		})
	}()
}

// todayPanel renders the "Hari ini" task list across eligible databases.
func (a *App) todayPanel(c *ui.Context, i int) {
	p := a.pal()
	st := &a.today
	if !st.loaded && !st.loading && a.client != nil {
		a.loadToday()
	}
	a.panelHeader(c, i, iconCalendar, tr.T("Hari ini"), "today", nil)

	now := nowFunc().In(time.Local)
	groups := classifyTodayItems(st.items, now)
	total := len(groups.Overdue) + len(groups.Today) + len(groups.Week)

	ui.Column(c).Grow(1).MinHeight(0).Padding(24, 32, 0, 32).Gap(14).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(10).Wrap().Children(func() {
			ui.Text(c, tr.T("Hari ini")).FontSize(26).FontWeight(700)
			ui.Text(c, strconv.Itoa(total)+tr.T(" tugas")).FontSize(13).TextColor(p.Faint)
			ui.Box(c).Grow(1)
			if len(st.eligible) > 0 {
				dbBtn := ui.Row(c.Key("today-dbs")).AlignItems(ui.Center).Gap(6).Padding(6, 12).Radius(8).
					Background(p.Sidebar).Cursor(ui.CursorPointer)
				if dbBtn.Hovered() {
					dbBtn.Background(p.Hover)
				}
				dbBtn.Children(func() {
					ui.Icon(c, iconTable).Size(14, 14).TextColor(p.Muted)
					ui.Text(c, "Database").FontSize(13).TextColor(p.Muted)
				})
				if dbBtn.Clicked() {
					st.picker = !st.picker
				}
				ui.Popover(c, dbBtn, &st.picker, func() {
					ui.Column(c).Width(260).Padding(6).Gap(4).Children(func() {
						ui.Text(c, tr.T("Database di Hari ini")).FontSize(12).FontWeight(600).TextColor(p.Faint).Padding(2, 4)
						for _, src := range st.eligible {
							on := !a.set.TodayOff[src.ID]
							if ui.Checkbox(c.Key("td-"+src.ID), &on, nonEmpty(src.Title, tr.T("Database tanpa judul"))).Changed() {
								if a.set.TodayOff == nil {
									a.set.TodayOff = map[string]bool{}
								}
								if on {
									delete(a.set.TodayOff, src.ID)
								} else {
									a.set.TodayOff[src.ID] = true
								}
								a.saveSettings()
								a.loadToday()
							}
						}
					})
				})
			}
			if a.btn(c, tr.T("Muat ulang"), iconRefresh, false, st.loading) {
				a.loadToday()
			}
		})

		if st.loading && len(st.items) == 0 {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Padding(12, 0).Children(func() {
				ui.Spinner(c).Size(16, 16)
				ui.Text(c, tr.T("Memuat tugas…")).FontSize(13).TextColor(p.Faint)
			})
			return
		}
		if st.err != "" && len(st.items) == 0 {
			ui.Text(c, tr.T("Gagal memuat tugas: ")+st.err).FontSize(13).TextColor(c.Theme().Danger)
			return
		}
		if total == 0 {
			msg := tr.T("Tidak ada tugas untuk hari ini.")
			if len(st.eligible) == 0 && st.loaded {
				msg = tr.T("Belum ada database dengan kolom Orang dan Tanggal.")
			}
			ui.Text(c, msg).FontSize(14).TextColor(p.Faint).Padding(12, 0)
			return
		}

		ui.Scroll(c).Grow(1).MinHeight(0).ScrollbarStyle(a.thinScrollbar()).Children(func() {
			ui.Column(c).Gap(18).Padding(0, 0, 24, 0).Children(func() {
				a.todaySection(c, i, tr.T("Terlambat"), groups.Overdue, true)
				a.todaySection(c, i, tr.T("Hari ini"), groups.Today, false)
				a.todaySection(c, i, tr.T("Minggu ini"), groups.Week, false)
			})
		})
	})
}

func (a *App) todaySection(c *ui.Context, i int, heading string, items []todayItem, overdue bool) {
	if len(items) == 0 {
		return
	}
	p := a.pal()
	ui.Column(c.Key("sec-"+heading)).Gap(4).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(6).Padding(4, 0).Children(func() {
			ui.Text(c, heading).FontSize(12).FontWeight(600).TextColor(p.Faint)
			ui.Text(c, strconv.Itoa(len(items))).FontSize(12).TextColor(p.Faint)
		})
		for _, it := range items {
			a.todayRow(c, i, it, overdue)
		}
	})
}

func (a *App) todayRow(c *ui.Context, i int, it todayItem, overdue bool) {
	p := a.pal()
	pg := it.Page
	row := ui.Row(c.Key("trow-"+pg.ID)).AlignItems(ui.Center).Gap(10).Padding(8, 12).Radius(8).
		Border(1, p.Line).Background(p.Panel)
	if row.Hovered() {
		row.Background(p.Hover)
	}
	own := false
	row.Children(func() {
		if it.DoneCol != "" && it.DoneProp.Editable() {
			if it.DoneProp.Type == "checkbox" {
				ctrl := ui.Box(c.Key("done")).Padding(2).Radius(4).AlignItems(ui.Center)
				a.today.tv.dsID = it.DBID
				if a.editable(c, &a.today.tv, pg, it.DoneProp, ctrl, func() {
					a.value(c, pg.Properties[it.DoneCol], "checkbox")
				}) {
					own = true
					a.saveTodayCache()
				}
			}
		}
		if em := pg.Emoji(); em != "" {
			emoji.View(c, em, 16)
		} else {
			ui.Icon(c, iconPage).Size(14, 14).TextColor(p.Faint)
		}
		title := ui.Row(c.Key("open")).Grow(1).MinWidth(0).Gap(8).AlignItems(ui.Center).Cursor(ui.CursorPointer)
		title.Children(func() {
			ui.Text(c, nonEmpty(pg.Title(), tr.T("Tanpa judul"))).FontSize(14).FontWeight(500).SingleLine().MinWidth(0)
			if it.DBTitle != "" {
				ui.Text(c, it.DBTitle).FontSize(12).TextColor(p.Faint).SingleLine().Shrink(0)
			}
		})
		if title.Clicked() {
			own = true
			a.openRow(i, &tableView{id: it.DBID}, pg)
		}
		if dateText := notion.FormatProperty(pg.Properties[it.DateCol]); dateText != "" {
			dt := ui.Text(c, dateText).FontSize(12).SingleLine().Shrink(0)
			if overdue {
				dt.TextColor(p.Warn)
			} else {
				dt.TextColor(p.Muted)
			}
		}
		if it.StatusCol != "" {
			chips := notion.Chips(pg.Properties[it.StatusCol])
			ctrl := ui.Row(c.Key("st")).Padding(2, 4).Radius(6).AlignItems(ui.Center)
			stProp := it.DoneProp
			if stProp.Name != it.StatusCol {
				stProp = notion.Property{Name: it.StatusCol, Type: "status"}
			}
			a.today.tv.dsID = it.DBID
			if a.editable(c, &a.today.tv, pg, stProp, ctrl, func() {
				if len(chips) > 0 {
					a.chip(c, chips[0])
				} else {
					ui.Text(c, "Status").FontSize(11).TextColor(p.Faint)
				}
			}) {
				own = true
			}
		}
	})
	if row.Clicked() && !own && !a.today.tv.edit.open {
		a.openRow(i, &tableView{id: it.DBID}, pg)
	}
}
