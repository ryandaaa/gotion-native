package app

import (
	"context"
	"encoding/json"
	"fmt"
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
	TitleCol  string          `json:"titleCol"`
	PeopleCol string          `json:"peopleCol"`
	DateCol   string          `json:"dateCol"`
	DoneCol   string          `json:"doneCol"`
	DoneProp  notion.Property `json:"doneProp"`
	StatusCol string          `json:"statusCol"`
}

// todaySource is a data source eligible for the "Hari ini" view.
type todaySource struct {
	ID        string          `json:"id"`
	Title     string          `json:"title"`
	TitleCol  string          `json:"titleCol"`
	PeopleCol string          `json:"peopleCol"`
	DateCol   string          `json:"dateCol"`
	DoneProp  notion.Property `json:"doneProp"`
	StatusCol string          `json:"statusCol"`
}

// todayGroups holds items grouped into Overdue, Today, Week, and DoneToday.
type todayGroups struct {
	Overdue   []todayItem
	Today     []todayItem
	Week      []todayItem
	DoneToday []todayItem
}

type todayState struct {
	items       []todayItem
	eligible    []todaySource
	doneToday   map[string]todayItem // completed in-session today (keyed by page ID)
	doneDate    string               // YYYY-MM-DD for doneToday
	selectedDay string               // YYYY-MM-DD selected in week strip ("" = today)
	weekOpen    bool
	doneOpen    bool
	loading     bool
	loaded      bool
	err         string
	lastFetch   time.Time
	picker      bool
	quick       quickAddState
	focusPulse  int
	tv          tableView
	cancel      context.CancelFunc
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
		Items     []todayItem          `json:"items"`
		Eligible  []todaySource        `json:"eligible"`
		DoneToday map[string]todayItem `json:"doneToday"`
		DoneDate  string               `json:"doneDate"`
	}
	if json.Unmarshal(b, &cached) == nil {
		a.today.items = cached.Items
		a.today.eligible = cached.Eligible
		if cached.DoneDate == nowFunc().In(time.Local).Format("2006-01-02") {
			a.today.doneToday = cached.DoneToday
			a.today.doneDate = cached.DoneDate
		}
		if len(cached.Items) > 0 || len(cached.Eligible) > 0 || len(a.today.doneToday) > 0 {
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
		Items     []todayItem          `json:"items"`
		Eligible  []todaySource        `json:"eligible"`
		DoneToday map[string]todayItem `json:"doneToday"`
		DoneDate  string               `json:"doneDate"`
	}{
		Items:     a.today.items,
		Eligible:  a.today.eligible,
		DoneToday: a.today.doneToday,
		DoneDate:  a.today.doneDate,
	}
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

// weekMonday returns the Monday 00:00 of now's week in now.Location().
func weekMonday(now time.Time) time.Time {
	loc := now.Location()
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	offset := (int(day.Weekday()) + 6) % 7 // Monday = 0, Sunday = 6
	return day.AddDate(0, 0, -offset)
}

// buildTodayFilter constructs the Notion query filter for a data source:
// people contains userID, date <= endDateStr, and incomplete when a status or
// checkbox column exists.
func buildTodayFilter(peopleCol, dateCol, userID string, doneProp notion.Property, endDateStr string) any {
	var conds []any
	if userID != "" && peopleCol != "" {
		conds = append(conds, map[string]any{
			"property": peopleCol,
			"people":   map[string]any{"contains": userID},
		})
	}
	if dateCol != "" && endDateStr != "" {
		conds = append(conds, map[string]any{
			"property": dateCol,
			"date":     map[string]any{"on_or_before": endDateStr},
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

// parseLocalDateTime parses a Notion date property's start into loc, reporting
// whether a time-of-day component was present.
func parseLocalDateTime(raw json.RawMessage, loc *time.Location) (t time.Time, hasTime bool, ok bool) {
	var v struct {
		Date *struct {
			Start string `json:"start"`
		} `json:"date"`
	}
	if json.Unmarshal(raw, &v) != nil || v.Date == nil || v.Date.Start == "" {
		return time.Time{}, false, false
	}
	s := v.Date.Start
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

// parseLocalDate parses a Notion date property's start into midnight in loc.
func parseLocalDate(raw json.RawMessage, loc *time.Location) (time.Time, bool) {
	t, _, ok := parseLocalDateTime(raw, loc)
	if !ok {
		return time.Time{}, false
	}
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc), true
}

// sortTodayUrgent orders open tasks by urgency based on nearest time
// (items with a time-of-day rank first by proximity to now, then date-only
// items by earliest date).
func sortTodayUrgent(items []todayItem, now time.Time) {
	loc := now.Location()
	absDur := func(d time.Duration) time.Duration {
		if d < 0 {
			return -d
		}
		return d
	}
	sort.SliceStable(items, func(i, j int) bool {
		ti, hi, _ := parseLocalDateTime(items[i].Page.Properties[items[i].DateCol], loc)
		tj, hj, _ := parseLocalDateTime(items[j].Page.Properties[items[j].DateCol], loc)
		if hi != hj {
			return hi
		}
		if hi && hj {
			di, dj := absDur(ti.Sub(now)), absDur(tj.Sub(now))
			if di != dj {
				return di < dj
			}
			if !ti.Equal(tj) {
				return ti.Before(tj)
			}
		} else if !ti.Equal(tj) {
			return ti.Before(tj)
		}
		return items[i].Page.Title() < items[j].Page.Title()
	})
}

// classifyTodayItems filters out completed items and buckets remaining items
// into Overdue, Today, Week (rest of current week through Sunday), and DoneToday.
func classifyTodayItems(items []todayItem, now time.Time) todayGroups {
	loc := now.Location()
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	weekEnd := weekMonday(now).AddDate(0, 0, 6)
	var out todayGroups
	for _, it := range items {
		if it.Page == nil {
			continue
		}
		d, ok := parseLocalDate(it.Page.Properties[it.DateCol], loc)
		if !ok {
			continue
		}
		if it.DoneCol != "" && it.DoneProp.IsComplete(it.Page.Properties[it.DoneCol]) {
			if d.Equal(todayStart) {
				out.DoneToday = append(out.DoneToday, it)
			}
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
		sort.SliceStable(xs, func(i, j int) bool {
			di, _, _ := parseLocalDateTime(xs[i].Page.Properties[xs[i].DateCol], loc)
			dj, _, _ := parseLocalDateTime(xs[j].Page.Properties[xs[j].DateCol], loc)
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
	return len(g.Overdue) + len(g.Today)
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
	now := nowFunc().In(time.Local)
	weekEnd := weekMonday(now).AddDate(0, 0, 6)
	if weekEnd.Before(now) {
		weekEnd = now
	}
	endDateStr := weekEnd.Format("2006-01-02")

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

				var titleCol, peopleCol, dateCol, statusCol string
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
					case "title":
						if titleCol == "" {
							titleCol = p.Name
						}
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
				eligible = append(eligible, todaySource{
					ID:        dsID,
					Title:     title,
					TitleCol:  nonEmpty(titleCol, "Name"),
					PeopleCol: peopleCol,
					DateCol:   dateCol,
					DoneProp:  doneProp,
					StatusCol: statusCol,
				})
				mu.Unlock()

				if off[dsID] || off[n.ID] {
					return
				}

				filter := buildTodayFilter(peopleCol, dateCol, userID, doneProp, endDateStr)
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
						TitleCol:  nonEmpty(titleCol, "Name"),
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
			if (a.bot == nil || a.bot.PersonID() == "") && bot != nil {
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

var (
	idDaysLong  = [...]string{"Minggu", "Senin", "Selasa", "Rabu", "Kamis", "Jumat", "Sabtu"}
	idDaysShort = [...]string{"Min", "Sen", "Sel", "Rab", "Kam", "Jum", "Sab"}
	enDaysShort = [...]string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"}
	idMonNames  = [...]string{"", "Januari", "Februari", "Maret", "April", "Mei", "Juni", "Juli", "Agustus", "September", "Oktober", "November", "Desember"}
)

func formatFullDate(t time.Time) string {
	if tr.Lang() == tr.ID {
		return fmt.Sprintf("%s, %d %s %d", idDaysLong[t.Weekday()], t.Day(), idMonNames[t.Month()], t.Year())
	}
	return fmt.Sprintf("%s, %d %s %d", t.Weekday().String(), t.Day(), t.Month().String(), t.Year())
}

func formatMonthDay(t time.Time) string {
	if tr.Lang() == tr.ID {
		return fmt.Sprintf("%s, %d %s", idDaysLong[t.Weekday()], t.Day(), idMonNames[t.Month()])
	}
	return fmt.Sprintf("%s, %d %s", t.Weekday().String(), t.Day(), t.Month().String())
}

func formatTaskTime(it todayItem, now time.Time, longOverdue bool) (string, bool) {
	loc := now.Location()
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	dt, hasTime, ok := parseLocalDateTime(it.Page.Properties[it.DateCol], loc)
	if !ok {
		return "", false
	}
	d := time.Date(dt.Year(), dt.Month(), dt.Day(), 0, 0, 0, 0, loc)
	switch {
	case d.Before(todayStart):
		days := int(todayStart.Sub(d).Hours()/24 + 0.5)
		if !longOverdue && days == 1 {
			return tr.T("kemarin"), true
		}
		if days <= 1 {
			return tr.T("terlambat 1 hari"), true
		}
		return fmt.Sprintf(tr.T("terlambat %d hari"), days), true
	case d.Equal(todayStart):
		if hasTime {
			return dt.Format("15:04"), false
		}
		return tr.T("Hari ini"), false
	default:
		if hasTime {
			return dt.Format("15:04"), false
		}
		if d.Equal(todayStart.AddDate(0, 0, 1)) {
			return tr.T("Besok"), false
		}
		return notion.FormatProperty(it.Page.Properties[it.DateCol]), false
	}
}

func (a *App) warnColor() ui.Color {
	if a.set.Wash == "aprikot" {
		if a.dark {
			return ui.Hex("#F87171")
		}
		return ui.Hex("#B91C1C")
	}
	return a.pal().Warn
}

// allDoneToday merges items completed in-session today with completed items
// present in st.items.
func (a *App) allDoneToday(now time.Time, fromItems []todayItem) []todayItem {
	todayKey := now.Format("2006-01-02")
	seen := map[string]bool{}
	var out []todayItem
	for _, it := range fromItems {
		if it.Page != nil && !seen[it.Page.ID] {
			seen[it.Page.ID] = true
			out = append(out, it)
		}
	}
	if a.today.doneDate == todayKey {
		for id, it := range a.today.doneToday {
			if !seen[id] {
				seen[id] = true
				out = append(out, it)
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Page.Title() < out[j].Page.Title()
	})
	return out
}

// completeTodayItem marks it as complete (first "Complete" status option, or
// checkbox=true) optimistically and sends PATCH /pages/{id}.
func (a *App) completeTodayItem(it todayItem) {
	if it.Page == nil || it.DoneCol == "" || !it.DoneProp.Editable() || a.client == nil {
		return
	}
	var names []string
	var on bool
	if it.DoneProp.Type == "status" {
		opts := it.DoneProp.CompleteOptions()
		if len(opts) == 0 {
			return
		}
		names = []string{opts[0]}
	} else if it.DoneProp.Type == "checkbox" {
		on = true
	} else {
		return
	}

	send, show, err := notion.Value(it.DoneProp, "", on, names)
	if err != nil {
		a.toast(err.Error())
		return
	}
	pg := it.Page
	oldRaw := pg.Properties[it.DoneCol]
	if pg.Properties == nil {
		pg.Properties = map[string]json.RawMessage{}
	}
	pg.Properties[it.DoneCol] = show

	todayKey := nowFunc().In(time.Local).Format("2006-01-02")
	if a.today.doneDate != todayKey {
		a.today.doneToday = map[string]todayItem{}
		a.today.doneDate = todayKey
	}
	if a.today.doneToday == nil {
		a.today.doneToday = map[string]todayItem{}
	}
	a.today.doneToday[pg.ID] = it
	a.today.focusPulse = 2
	a.saveTodayCache()

	client := a.client
	go func() {
		_, err := client.SetProperties(context.Background(), pg.ID, map[string]any{it.DoneCol: send})
		if err != nil {
			a.update(func() {
				pg.Properties[it.DoneCol] = oldRaw
				delete(a.today.doneToday, pg.ID)
				a.saveTodayCache()
				a.toast(tr.T("Gagal menyimpan ") + it.DoneCol + ": " + errText(err))
			})
		}
	}()
}

// uncompleteTodayItem reverts a completed task back to incomplete.
func (a *App) uncompleteTodayItem(it todayItem) {
	if it.Page == nil || it.DoneCol == "" || !it.DoneProp.Editable() || a.client == nil {
		return
	}
	var names []string
	if it.DoneProp.Type == "status" {
		opts := it.DoneProp.OrderedOptions()
		complete := map[string]bool{}
		for _, c := range it.DoneProp.CompleteOptions() {
			complete[c] = true
		}
		for _, o := range opts {
			if !complete[o.Name] {
				names = []string{o.Name}
				break
			}
		}
	}
	send, show, err := notion.Value(it.DoneProp, "", false, names)
	if err != nil {
		a.toast(err.Error())
		return
	}
	pg := it.Page
	oldRaw := pg.Properties[it.DoneCol]
	if pg.Properties == nil {
		pg.Properties = map[string]json.RawMessage{}
	}
	pg.Properties[it.DoneCol] = show
	delete(a.today.doneToday, pg.ID)

	found := false
	for _, existing := range a.today.items {
		if existing.Page != nil && existing.Page.ID == pg.ID {
			found = true
			break
		}
	}
	if !found {
		a.today.items = append(a.today.items, it)
	}
	a.saveTodayCache()

	client := a.client
	go func() {
		_, err := client.SetProperties(context.Background(), pg.ID, map[string]any{it.DoneCol: send})
		if err != nil {
			a.update(func() {
				pg.Properties[it.DoneCol] = oldRaw
				a.today.doneToday[pg.ID] = it
				a.saveTodayCache()
				a.toast(tr.T("Gagal menyimpan ") + it.DoneCol + ": " + errText(err))
			})
		}
	}()
}

// deferTodayItem moves a task's date property to tomorrow, preserving time and
// range duration.
func (a *App) deferTodayItem(it todayItem) {
	if it.Page == nil || it.DateCol == "" || a.client == nil {
		return
	}
	now := nowFunc().In(time.Local)
	tomorrow := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, now.Location())
	dateProp := notion.Property{Name: it.DateCol, Type: "date"}
	oldRaw := it.Page.Properties[it.DateCol]
	send, show, err := notion.DeferDate(dateProp, oldRaw, tomorrow)
	if err != nil {
		a.toast(tr.T("Gagal menunda tugas: ") + err.Error())
		return
	}
	if it.Page.Properties == nil {
		it.Page.Properties = map[string]json.RawMessage{}
	}
	it.Page.Properties[it.DateCol] = show
	a.today.focusPulse = 2
	a.saveTodayCache()

	client := a.client
	go func() {
		_, err := client.SetProperties(context.Background(), it.Page.ID, map[string]any{it.DateCol: send})
		if err != nil {
			a.update(func() {
				it.Page.Properties[it.DateCol] = oldRaw
				a.saveTodayCache()
				a.toast(tr.T("Gagal menunda tugas: ") + errText(err))
			})
		}
	}()
}

// todayPanel renders the "Hari ini" task hub across eligible databases.
func (a *App) todayPanel(c *ui.Context, i int) {
	p := a.pal()
	st := &a.today
	if !st.loaded && !st.loading && a.client != nil {
		a.loadToday()
	}
	a.panelHeader(c, i, iconCalendar, tr.T("Hari ini"), "today", nil)

	now := nowFunc().In(time.Local)
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	todayKey := todayStart.Format("2006-01-02")
	if st.selectedDay == "" {
		st.selectedDay = todayKey
	}

	groups := classifyTodayItems(st.items, now)
	doneList := a.allDoneToday(now, groups.DoneToday)
	openDue := append(append([]todayItem(nil), groups.Overdue...), groups.Today...)
	sortTodayUrgent(openDue, now)

	doneCount := len(doneList)
	totalCount := doneCount + len(openDue)
	accentBg, accentFg := tintColors(p.Accent, a.dark)

	ui.Scroll(c).Grow(1).MinHeight(0).ScrollbarStyle(a.thinScrollbar()).Children(func() {
		ui.Column(c).FillWidth().Padding(36, 40, 32, 40).Gap(20).Children(func() {
			// 1. Header
			ui.Column(c).FillWidth().Gap(12).Children(func() {
				ui.Row(c).FillWidth().AlignItems(ui.End).Gap(12).Children(func() {
					ui.Column(c).Grow(1).Gap(2).Children(func() {
						ui.Text(c, formatFullDate(now)).FontSize(13).TextColor(p.Faint)
						ui.Text(c, tr.T("Hari ini")).FontSize(32).FontWeight(700)
					})
					if len(st.eligible) > 0 {
						dbBtn := ui.ButtonBase(c.Key("today-dbs")).Role(ui.RoleButton).Label("Database").
							Padding(7, 13).Radius(8).Border(1, p.Line).Cursor(ui.CursorPointer)
						if dbBtn.Hovered() {
							dbBtn.Background(p.Hover)
						}
						dbBtn.Children(func() {
							ui.Row(c).AlignItems(ui.Center).Gap(7).Children(func() {
								ui.Icon(c, iconTable).Size(15, 15).TextColor(p.Muted)
								ui.Text(c, "Database").FontSize(13).FontWeight(500).TextColor(p.Text)
							})
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
					addBtn := ui.ButtonBase(c.Key("today-add")).Role(ui.RoleButton).Label(tr.T("Tugas baru")).
						Padding(7, 14).Radius(8).Background(accentBg).Cursor(ui.CursorPointer)
					addBtn.Children(func() {
						ui.Row(c).AlignItems(ui.Center).Gap(6).Children(func() {
							ui.Icon(c, iconPlus).Size(14, 14).TextColor(accentFg)
							ui.Text(c, tr.T("Tugas baru")).FontSize(13).FontWeight(600).TextColor(accentFg)
						})
					})
					if addBtn.Clicked() {
						st.quick.open = !st.quick.open
					}
					if st.quick.open {
						ui.Popover(c, addBtn, &st.quick.open, func() {
							a.quickAddForm(c, &st.quick, st.eligible)
						})
					}
				})

				// Progress bar
				var pct float32
				if totalCount > 0 {
					pct = float32(doneCount) / float32(totalCount) * 100
				}
				ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(14).Children(func() {
					track := ui.Row(c.Key("today-track")).Grow(1).Height(4).Radius(2).Background(p.Line).Clip()
					animPct := track.AnimateTo("today-prog", pct, ui.Smooth)
					track.Children(func() {
						if animPct > 0.1 {
							ui.Box(c).WidthPercent(animPct).FillHeight().Radius(2).Background(p.Accent)
						}
					})
					ui.Text(c, fmt.Sprintf(tr.T("%d dari %d selesai"), doneCount, totalCount)).FontSize(12).TextColor(p.Faint).Shrink(0)
				})
			})

			if st.loading && len(st.items) == 0 && len(doneList) == 0 {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Padding(12, 0).Children(func() {
					ui.Spinner(c).Size(16, 16)
					ui.Text(c, tr.T("Memuat tugas…")).FontSize(13).TextColor(p.Faint)
				})
				return
			}
			if st.err != "" && len(st.items) == 0 && len(doneList) == 0 {
				ui.Text(c, tr.T("Gagal memuat tugas: ")+st.err).FontSize(13).TextColor(c.Theme().Danger)
				return
			}

			// 2. Week strip (shown when there are tasks or week tasks, or always when not in pure empty state?
			// Note mockup 2: empty state hides week strip when nothing is pending, or shows when tasks exist/selectedDay != today)
			if len(openDue) > 0 || len(groups.Week) > 0 || st.selectedDay != todayKey {
				a.todayWeekStrip(c, now, todayStart, st.items, accentBg, accentFg)
			}

			if st.selectedDay != todayKey {
				selDate, _ := time.ParseInLocation("2006-01-02", st.selectedDay, now.Location())
				var dayTasks []todayItem
				for _, it := range st.items {
					if it.Page == nil || (it.DoneCol != "" && it.DoneProp.IsComplete(it.Page.Properties[it.DoneCol])) {
						continue
					}
					if d, ok := parseLocalDate(it.Page.Properties[it.DateCol], now.Location()); ok && d.Equal(selDate) {
						dayTasks = append(dayTasks, it)
					}
				}
				sortTodayUrgent(dayTasks, now)
				unit := tr.T(" tugas")
				ui.Text(c, formatMonthDay(selDate)+" · "+strconv.Itoa(len(dayTasks))+unit).FontSize(15).FontWeight(600)
				if len(dayTasks) == 0 {
					ui.Text(c, tr.T("Tidak ada tugas yang menunggu.")).FontSize(13).TextColor(p.Faint)
				} else {
					a.todayGrid(c, i, dayTasks, now)
				}
				a.todayCollapsibles(c, i, groups.Week, doneList, now)
				return
			}

			// 6. Empty state for Today (no overdue or today open tasks)
			if len(openDue) == 0 {
				tomorrowStr := todayStart.AddDate(0, 0, 1).Format("2006-01-02")
				tomorrowCount := 0
				for _, it := range groups.Week {
					if d, ok := parseLocalDate(it.Page.Properties[it.DateCol], now.Location()); ok && d.Format("2006-01-02") == tomorrowStr {
						tomorrowCount++
					}
				}
				ui.Column(c).FillWidth().Center().Padding(40, 0, 24, 0).Gap(10).Children(func() {
					ui.Box(c).Size(44, 44).Radius(22).Background(p.OK.Alpha(0.14)).Center().Children(func() {
						ui.Icon(c, iconCheck).Size(20, 20).TextColor(p.OK)
					})
					ui.Text(c, tr.T("Semua beres hari ini")).FontSize(17).FontWeight(600)
					sub := tr.T("Tidak ada tugas yang menunggu.")
					if tomorrowCount == 1 {
						sub += " " + tr.T("Besok ada 1 tugas.")
					} else if tomorrowCount > 1 {
						sub += " " + fmt.Sprintf(tr.T("Besok ada %d tugas."), tomorrowCount)
					}
					ui.Text(c, sub).FontSize(13).TextColor(p.Faint)
				})
				a.todayCollapsibles(c, i, groups.Week, doneList, now)
				return
			}

			// 3. Focus card (openDue[0])
			a.todayFocusCard(c, i, openDue[0], now, accentBg, accentFg)

			// 4. "Berikutnya · N"
			if rest := openDue[1:]; len(rest) > 0 {
				ui.Column(c).FillWidth().Gap(10).Children(func() {
					ui.Text(c, tr.T("Berikutnya")+" · "+strconv.Itoa(len(rest))).FontSize(15).FontWeight(600)
					a.todayGrid(c, i, rest, now)
				})
			}

			// 5. Collapsibles
			a.todayCollapsibles(c, i, groups.Week, doneList, now)
		})
	})
}

func (a *App) todayWeekStrip(c *ui.Context, now, todayStart time.Time, items []todayItem, accentBg, accentFg ui.Color) {
	p := a.pal()
	mon := weekMonday(now)
	todayKey := todayStart.Format("2006-01-02")

	// Count open tasks per day of the current week.
	counts := map[string]int{}
	for _, it := range items {
		if it.Page == nil || (it.DoneCol != "" && it.DoneProp.IsComplete(it.Page.Properties[it.DoneCol])) {
			continue
		}
		if d, ok := parseLocalDate(it.Page.Properties[it.DateCol], now.Location()); ok {
			counts[d.Format("2006-01-02")]++
		}
	}

	ui.Grid(c).Columns(7).Gap(8).FillWidth().Children(func() {
		for offset := range 7 {
			d := mon.AddDate(0, 0, offset)
			key := d.Format("2006-01-02")
			selected := a.today.selectedDay == key
			isToday := key == todayKey
			isPast := d.Before(todayStart)

			dayLabel := enDaysShort[d.Weekday()]
			if tr.Lang() == tr.ID {
				dayLabel = idDaysShort[d.Weekday()]
			}
			if isToday {
				dayLabel = tr.T("Hari ini")
			}

			box := ui.Column(c.Key("wday-"+key)).Padding(10, 6).Radius(10).Center().Gap(3).Cursor(ui.CursorPointer)
			if selected {
				box.Background(accentBg)
			} else if box.Hovered() {
				box.Background(p.Hover)
			}
			box.Children(func() {
				lbl := ui.Text(c, dayLabel).FontSize(11).FontWeight(500)
				num := ui.Text(c, strconv.Itoa(d.Day())).FontSize(16).FontWeight(600)
				if selected {
					lbl.TextColor(accentFg)
					num.TextColor(accentFg)
				} else {
					lbl.TextColor(p.Faint)
					num.TextColor(p.Text)
				}
				n := min(counts[key], 4)
				ui.Row(c).Height(6).Gap(3).AlignItems(ui.Center).Children(func() {
					for k := range n {
						dotColor := p.Faint
						if isPast {
							dotColor = a.warnColor()
						}
						ui.Box(c.Key(fmt.Sprintf("dot-%d", k))).Size(5, 5).Radius(2.5).Background(dotColor)
					}
				})
			})
			if box.Clicked() {
				a.today.selectedDay = key
			}
		}
	})
}

func (a *App) todayFocusCard(c *ui.Context, i int, it todayItem, now time.Time, accentBg, accentFg ui.Color) {
	p := a.pal()
	pg := it.Page
	card := ui.Column(c.Key("focus-"+pg.ID)).FillWidth().Gap(12).Padding(20, 24).Radius(12).
		Background(p.Panel).Border(1, p.Line).Shadow(0, 4, 16, 0, ui.RGBA(0, 0, 0, 0.06))
	if a.today.focusPulse > 0 {
		if a.today.focusPulse > 1 {
			card.AnimatePose(ui.Pose{Y: 6, Fade: 0.3}, ui.Snappy)
			c.Invalidate()
		} else {
			card.AnimatePose(ui.Pose{}, ui.Snappy)
		}
		a.today.focusPulse--
	}

	timeText, overdue := formatTaskTime(it, now, true)
	card.Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(10).Children(func() {
			if em := pg.Emoji(); em != "" {
				emoji.View(c, em, 24)
			} else {
				ui.Icon(c, iconPage).Size(20, 20).TextColor(p.Faint)
			}
			ui.Text(c, nonEmpty(pg.Title(), tr.T("Tanpa judul"))).FontSize(22).FontWeight(600).Grow(1).MinWidth(0)
		})
		ui.Row(c).AlignItems(ui.Center).Gap(8).Wrap().Children(func() {
			if it.DBTitle != "" {
				ui.Text(c, it.DBTitle).FontSize(13).TextColor(p.Muted)
			}
			if timeText != "" {
				if it.DBTitle != "" {
					ui.Text(c, "·").FontSize(13).TextColor(p.Faint)
				}
				if overdue {
					wc := a.warnColor()
					ui.Row(c).AlignItems(ui.Center).Gap(4).Children(func() {
						ui.Icon(c, iconClock).Size(13, 13).TextColor(wc)
						ui.Text(c, timeText).FontSize(13).FontWeight(500).TextColor(wc)
					})
				} else {
					ui.Text(c, timeText).FontSize(13).TextColor(p.Muted)
				}
			}
			if it.StatusCol != "" {
				if chips := notion.Chips(pg.Properties[it.StatusCol]); len(chips) > 0 {
					ui.Text(c, "·").FontSize(13).TextColor(p.Faint)
					a.chip(c, chips[0])
				}
			}
		})
		ui.Row(c).AlignItems(ui.Center).Gap(8).Padding(4, 0, 0, 0).Children(func() {
			if it.DoneCol != "" && it.DoneProp.Editable() {
				doneBtn := ui.ButtonBase(c.Key("focus-done")).Role(ui.RoleButton).Label(tr.T("Selesai")).
					Padding(7, 14).Radius(8).Background(accentBg).Cursor(ui.CursorPointer)
				doneBtn.Children(func() {
					ui.Row(c).AlignItems(ui.Center).Gap(6).Children(func() {
						ui.Icon(c, iconCheck).Size(14, 14).TextColor(accentFg)
						ui.Text(c, tr.T("Selesai")).FontSize(13).FontWeight(600).TextColor(accentFg)
					})
				})
				if doneBtn.Clicked() {
					a.completeTodayItem(it)
				}
			}
			if a.btn(c, tr.T("Buka halaman"), nil, false, false) {
				a.openRow(i, &tableView{id: it.DBID}, pg)
			}
			if ghostButton(c, p, tr.T("Tunda ke besok")) {
				a.deferTodayItem(it)
			}
		})
	})
}

func (a *App) todayGrid(c *ui.Context, i int, items []todayItem, now time.Time) {
	const minCardW, gap = float32(210), float32(12)
	grid := ui.Grid(c.Key("today-grid")).FillWidth().Gap(gap)
	cols := 3
	if w := grid.LayoutBounds().W; w > 0 {
		cols = max(1, int((w+gap)/(minCardW+gap)))
	}
	grid.Columns(cols).Children(func() {
		for _, it := range items {
			a.todayGridCard(c, i, it, now)
		}
	})
}

func (a *App) todayGridCard(c *ui.Context, i int, it todayItem, now time.Time) {
	p := a.pal()
	pg := it.Page
	card := ui.Column(c.Key("tcard-"+pg.ID)).FillWidth().Gap(10).Padding(14, 16).Radius(10).
		Background(p.Panel).Border(1, p.Line).Cursor(ui.CursorPointer)
	if card.Hovered() {
		card.Border(1, p.EditLine).Shadow(0, 4, 12, 0, ui.RGBA(0, 0, 0, 0.08))
	}
	timeText, overdue := formatTaskTime(it, now, false)
	own := false
	card.Children(func() {
		ui.Row(c).AlignItems(ui.Start).Gap(10).Children(func() {
			if it.DoneCol != "" && it.DoneProp.Editable() {
				chk := ui.ButtonBase(c.Key("chk-"+pg.ID)).Role(ui.RoleCheckBox).Label(tr.T("Selesai")).
					Size(18, 18).Radius(9).Border(1.5, p.Faint).Shrink(0).Margin(2, 0, 0, 0).Cursor(ui.CursorPointer)
				if chk.Hovered() {
					chk.Border(1.5, p.Accent)
				}
				if chk.Clicked() {
					own = true
					a.completeTodayItem(it)
				}
			}
			ui.Text(c, nonEmpty(pg.Title(), tr.T("Tanpa judul"))).FontSize(14).FontWeight(500).MaxLines(2).Grow(1).MinWidth(0)
		})
		ui.Row(c).AlignItems(ui.Center).Gap(6).Children(func() {
			ui.Row(c).Grow(1).MinWidth(0).Gap(5).AlignItems(ui.Center).Children(func() {
				if it.DBTitle != "" {
					ui.Text(c, it.DBTitle).FontSize(12).TextColor(p.Faint).SingleLine().Shrink(1)
				}
				if timeText != "" {
					if it.DBTitle != "" {
						ui.Text(c, "·").FontSize(12).TextColor(p.Faint)
					}
					if overdue {
						wc := a.warnColor()
						ui.Icon(c, iconClock).Size(11, 11).TextColor(wc)
						ui.Text(c, timeText).FontSize(12).TextColor(wc).SingleLine()
					} else {
						ui.Text(c, timeText).FontSize(12).TextColor(p.Muted).SingleLine()
					}
				}
			})
			if it.StatusCol != "" {
				if chips := notion.Chips(pg.Properties[it.StatusCol]); len(chips) > 0 {
					a.chip(c, chips[0])
				}
			}
		})
	})
	if card.Clicked() && !own {
		a.openRow(i, &tableView{id: it.DBID}, pg)
	}
}

func (a *App) todayCollapsibles(c *ui.Context, i int, week, done []todayItem, now time.Time) {
	p := a.pal()
	if len(week) > 0 {
		a.todayCollapsibleRow(c, "week", tr.T("Minggu ini")+" · "+strconv.Itoa(len(week)), &a.today.weekOpen, func() {
			ui.Column(c).Gap(4).Padding(4, 0, 4, 18).Children(func() {
				for _, it := range week {
					a.todayCompactRow(c, i, it, now, false)
				}
			})
		})
	}
	if len(done) > 0 {
		a.todayCollapsibleRow(c, "done", tr.T("Selesai hari ini")+" · "+strconv.Itoa(len(done)), &a.today.doneOpen, func() {
			ui.Column(c).Gap(4).Padding(4, 0, 4, 18).Children(func() {
				for _, it := range done {
					a.todayCompactRow(c, i, it, now, true)
				}
			})
		})
	}
	_ = p
}

func (a *App) todayCollapsibleRow(c *ui.Context, key, label string, open *bool, body func()) {
	p := a.pal()
	ui.Column(c.Key("col-"+key)).FillWidth().Gap(4).Children(func() {
		hdr := ui.Row(c.Key("hdr-"+key)).AlignItems(ui.Center).Gap(8).Padding(4, 2).Radius(6).Cursor(ui.CursorPointer)
		if hdr.Hovered() {
			hdr.Background(p.Hover)
		}
		target := float32(0)
		if *open {
			target = 90
		}
		deg := hdr.AnimateTo("chev-"+key, target, ui.Snappy)
		hdr.Children(func() {
			ui.Icon(c, iconChevron).Size(13, 13).TextColor(p.Faint).Rotate(deg)
			ui.Text(c, label).FontSize(13).TextColor(p.Muted)
		})
		if hdr.Clicked() {
			*open = !*open
		}
		if *open {
			body()
		}
	})
}

func (a *App) todayCompactRow(c *ui.Context, i int, it todayItem, now time.Time, completed bool) {
	p := a.pal()
	pg := it.Page
	row := ui.Row(c.Key("crow-"+pg.ID)).FillWidth().AlignItems(ui.Center).Gap(10).Padding(6, 10).Radius(7).Cursor(ui.CursorPointer)
	if row.Hovered() {
		row.Background(p.Hover)
	}
	own := false
	timeText, _ := formatTaskTime(it, now, false)
	row.Children(func() {
		if it.DoneCol != "" && it.DoneProp.Editable() {
			chk := ui.ButtonBase(c.Key("cchk-"+pg.ID)).Role(ui.RoleCheckBox).Label(tr.T("Selesai")).
				Size(18, 18).Radius(9).Shrink(0).Center().Cursor(ui.CursorPointer)
			if completed {
				chk.Background(p.OK.Alpha(0.16)).Border(1.5, p.OK).Children(func() {
					ui.Icon(c, iconCheck).Size(11, 11).TextColor(p.OK)
				})
			} else {
				chk.Border(1.5, p.Faint)
			}
			if chk.Clicked() {
				own = true
				if completed {
					a.uncompleteTodayItem(it)
				} else {
					a.completeTodayItem(it)
				}
			}
		}
		ttl := ui.Text(c, nonEmpty(pg.Title(), tr.T("Tanpa judul"))).FontSize(13).Grow(1).MinWidth(0).SingleLine()
		if completed {
			ttl.TextColor(p.Faint).Strikethrough()
		} else {
			ttl.TextColor(p.Text)
		}
		if it.DBTitle != "" {
			ui.Text(c, it.DBTitle).FontSize(12).TextColor(p.Faint).SingleLine().Shrink(0)
		}
		if timeText != "" {
			ui.Text(c, timeText).FontSize(12).TextColor(p.Faint).SingleLine().Shrink(0)
		}
	})
	if row.Clicked() && !own {
		a.openRow(i, &tableView{id: it.DBID}, pg)
	}
}
