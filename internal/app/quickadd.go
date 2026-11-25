package app

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"gotion/internal/tr"
)

// quickAddState holds the state for the reusable "+ Tugas baru" / Quick Capture
// task creation form.
type quickAddState struct {
	open     bool
	title    string
	dbID     string
	tomorrow bool // false = Hari ini, true = Besok
	busy     bool
	err      string
}

// quickAddForm renders the task creation form inside a popover or modal.
// It is reused by "+ Tugas baru" in Today and by Quick Capture.
func (a *App) quickAddForm(c *ui.Context, q *quickAddState, sources []todaySource) {
	p := a.pal()
	bg, fg := tintColors(p.Accent, a.dark)

	if q.dbID == "" && len(sources) > 0 {
		q.dbID = a.set.TodayLastDB
		found := false
		for _, s := range sources {
			if s.ID == q.dbID {
				found = true
				break
			}
		}
		if !found {
			q.dbID = sources[0].ID
		}
	}

	ui.Column(c).Width(300).Padding(8).Gap(10).Children(func() {
		in := ui.TextInput(c.Key("qa-title"), &q.title).Placeholder(tr.T("Judul baris baru")).
			AutoFocus().Label(tr.T("Judul")).Disabled(q.busy)
		if in.Changed() {
			q.err = ""
		}

		if len(sources) > 0 {
			names := make([]string, len(sources))
			chosen := ""
			for k, s := range sources {
				label := nonEmpty(s.Title, tr.T("Database tanpa judul"))
				names[k] = label
				if s.ID == q.dbID {
					chosen = label
				}
			}
			ui.Column(c).Gap(4).Children(func() {
				ui.Text(c, tr.T("Ke database")).FontSize(11).FontWeight(600).TextColor(p.Faint)
				if ui.Select(c, &chosen, names).FillWidth().Label(tr.T("Ke database")).Changed() {
					for k, label := range names {
						if label == chosen {
							q.dbID = sources[k].ID
							a.set.TodayLastDB = q.dbID
							a.saveSettings()
							break
						}
					}
				}
			})
		}

		dayChoice := "today"
		if q.tomorrow {
			dayChoice = "tomorrow"
		}
		a.segmented(c, "qa-day", &dayChoice, []struct{ id, label string }{
			{"today", tr.T("Hari ini")},
			{"tomorrow", tr.T("Besok")},
		})
		q.tomorrow = dayChoice == "tomorrow"

		if q.err != "" {
			ui.Text(c, q.err).FontSize(12).TextColor(c.Theme().Danger)
		}

		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Grow(1)
			if a.btn(c, tr.T("Batal"), nil, false, q.busy) {
				*q = quickAddState{}
				return
			}
			addBtn := ui.ButtonBase(c.Key("qa-submit")).Role(ui.RoleButton).Label(tr.T("Tambah")).
				Padding(7, 14).Radius(8).Background(bg).Cursor(ui.CursorPointer)
			if q.busy || strings.TrimSpace(q.title) == "" || len(sources) == 0 {
				addBtn.Opacity(0.5)
			}
			addBtn.Children(func() {
				if q.busy {
					ui.Spinner(c).Size(13, 13)
				} else {
					ui.Text(c, tr.T("Tambah")+" ↵").FontSize(13).FontWeight(600).TextColor(fg)
				}
			})
			submit := (in.Submitted() || addBtn.Clicked()) && !q.busy && strings.TrimSpace(q.title) != "" && len(sources) > 0
			if submit {
				a.submitQuickAdd(q, sources)
			}
		})
	})
}

func (a *App) submitQuickAdd(q *quickAddState, sources []todaySource) {
	var src todaySource
	for _, s := range sources {
		if s.ID == q.dbID {
			src = s
			break
		}
	}
	if src.ID == "" {
		return
	}
	title := strings.TrimSpace(q.title)
	now := nowFunc().In(time.Local)
	target := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	if q.tomorrow {
		target = target.AddDate(0, 0, 1)
	}
	dateStr := target.Format("2006-01-02")

	extra := map[string]any{}
	if src.DateCol != "" {
		extra[src.DateCol] = map[string]any{"date": map[string]any{"start": dateStr}}
	}
	userID := a.bot.PersonID()
	if src.PeopleCol != "" && userID != "" {
		extra[src.PeopleCol] = map[string]any{"people": []any{map[string]any{"object": "user", "id": userID}}}
	}
	titleProp := nonEmpty(src.TitleCol, "Name")

	q.busy = true
	q.err = ""
	client := a.client
	go func() {
		pg, err := client.CreateRowWith(context.Background(), src.ID, titleProp, title, "", extra)
		a.update(func() {
			q.busy = false
			if err != nil {
				q.err = errText(err)
				a.toast(tr.T("Gagal membuat tugas: ") + q.err)
				return
			}
			if pg.Properties == nil {
				pg.Properties = map[string]json.RawMessage{}
			}
			if _, ok := pg.Properties[titleProp]; !ok {
				pg.Properties[titleProp], _ = json.Marshal(map[string]any{
					"type":  "title",
					"title": []any{map[string]any{"plain_text": title}},
				})
			}
			if src.DateCol != "" {
				if _, ok := pg.Properties[src.DateCol]; !ok {
					pg.Properties[src.DateCol], _ = json.Marshal(map[string]any{
						"type": "date",
						"date": map[string]any{"start": dateStr},
					})
				}
			}
			a.today.items = append(a.today.items, todayItem{
				Page:      pg,
				DBID:      src.ID,
				DBTitle:   src.Title,
				TitleCol:  titleProp,
				PeopleCol: src.PeopleCol,
				DateCol:   src.DateCol,
				DoneCol:   src.DoneProp.Name,
				DoneProp:  src.DoneProp,
				StatusCol: src.StatusCol,
			})
			a.saveTodayCache()
			*q = quickAddState{}
			a.toast(tr.T("Tugas ditambahkan"))
		})
	}()
}
