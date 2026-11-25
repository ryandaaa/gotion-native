package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo"

	"gotion/internal/notion"
	"gotion/internal/tr"
)

// TrayIcon is the PNG image used for the system tray icon, set by main.
var TrayIcon []byte

const notifyCheckInterval = 30 * time.Second

type notification struct {
	ID    string
	Title string
	Body  string
}

type notifiedState struct {
	MorningDate string            `json:"morningDate"`
	Due         map[string]string `json:"due"`
	Comments    map[string]string `json:"comments"`
}

func notifiedPath() string {
	dir := configDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "notified.json")
}

func (a *App) loadNotified() {
	a.notified = notifiedState{
		Due:      map[string]string{},
		Comments: map[string]string{},
	}
	path := notifiedPath()
	if path == "" {
		return
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var st notifiedState
	if json.Unmarshal(b, &st) == nil {
		a.notified.MorningDate = st.MorningDate
		if st.Due != nil {
			a.notified.Due = st.Due
		}
		if st.Comments != nil {
			a.notified.Comments = st.Comments
		}
		a.pruneNotified(nowFunc().In(time.Local))
	}
}

func (a *App) saveNotified() {
	path := notifiedPath()
	if path == "" {
		return
	}
	a.pruneNotified(nowFunc().In(time.Local))
	if b, err := json.Marshal(a.notified); err == nil {
		os.MkdirAll(filepath.Dir(path), 0o700)
		os.WriteFile(path, b, 0o600)
	}
}

func (a *App) pruneNotified(now time.Time) {
	cutoff := now.AddDate(0, 0, -7)
	for k, ts := range a.notified.Due {
		if t, err := time.Parse(time.RFC3339, ts); err == nil && t.Before(cutoff) {
			delete(a.notified.Due, k)
		}
	}
	for k, ts := range a.notified.Comments {
		if t, err := time.Parse(time.RFC3339, ts); err == nil && t.Before(cutoff) {
			delete(a.notified.Comments, k)
		}
	}
}

func (a *App) initTray() {
	if a.tray != nil || a.win == nil {
		return
	}
	menu := mygo.NewMenu([]*mygo.MenuItem{
		{
			Label: tr.T("Buka gotion"),
			Click: func(*mygo.MenuItem, *mygo.Window) { a.ShowWindow() },
		},
		{
			Label: tr.T("Hari ini"),
			Click: func(*mygo.MenuItem, *mygo.Window) {
				a.ShowWindow()
				a.update(func() { a.openToday() })
			},
		},
		{
			Label: tr.T("Tangkap cepat"),
			Click: func(*mygo.MenuItem, *mygo.Window) {
				a.openCaptureWindow()
			},
		},
		mygo.Separator(),
		{
			Label: tr.T("Keluar"),
			Click: func(*mygo.MenuItem, *mygo.Window) { mygo.App.Quit() },
		},
	})
	t, err := mygo.NewTray(mygo.TrayOptions{
		ID:      "gotion",
		Icon:    TrayIcon,
		ToolTip: "gotion",
		Menu:    menu,
	})
	if err == nil {
		a.tray = t
	}
}

// ShowWindow brings the main window to the front.
func (a *App) ShowWindow() {
	if a.win == nil {
		return
	}
	a.win.Show()
	a.win.Focus()
}

func (a *App) openToday() {
	a.open(a.special(todayID), 0)
}

func (a *App) openPageWithComments(pageID string) {
	if pageID == "" {
		return
	}
	a.openID(pageID, "page", "", 0)
	if pn := a.panels[0]; pn != nil && pn.id == pageID {
		if a.rightPaneFits {
			a.set.RightPane = true
			a.saveSettings()
		} else {
			pn.comments.open = true
		}
		if !pn.comments.loaded && !pn.comments.loading {
			a.loadComments(pn)
		}
	}
}

// OnNotificationClick routes a desktop notification click to the matching view.
func (a *App) OnNotificationClick(id string) {
	a.ShowWindow()
	a.update(func() {
		switch {
		case strings.HasPrefix(id, "today:"):
			a.openToday()
		case strings.HasPrefix(id, "due:"):
			a.openID(strings.TrimPrefix(id, "due:"), "page", "", 0)
		case strings.HasPrefix(id, "comment:"):
			a.openPageWithComments(strings.TrimPrefix(id, "comment:"))
		}
	})
}

func (a *App) updateTrayBadge() {
	n := a.todayActiveCount()
	label := ""
	if n > 0 {
		label = strconv.Itoa(n)
	}
	if a.setBadge != nil {
		a.setBadge(label)
	}
	if a.tray != nil {
		tip := "gotion"
		if n > 0 {
			tip = fmt.Sprintf(tr.T("gotion · %d tugas hari ini"), n)
		}
		a.tray.SetToolTip(tip)
	}
}

func (a *App) notify(n notification) {
	if a.sendNotify != nil {
		a.sendNotify(n)
	}
}

func (a *App) startNotifyTimer() {
	if a.notifyCancel != nil || a.client == nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.notifyCancel = cancel
	go func() {
		tk := time.NewTicker(notifyCheckInterval)
		defer tk.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tk.C:
				a.update(func() { a.checkNotifications() })
			}
		}
	}()
}

func (a *App) stopNotifyTimer() {
	if a.notifyCancel != nil {
		a.notifyCancel()
		a.notifyCancel = nil
	}
}

// parseMorningTime parses "HH:MM" in 24-hour format.
func parseMorningTime(s string) (hour, minute int, ok bool) {
	s = strings.TrimSpace(s)
	if len(s) != 5 || s[2] != ':' {
		return 0, 0, false
	}
	h, err1 := strconv.Atoi(s[:2])
	m, err2 := strconv.Atoi(s[3:])
	if err1 != nil || err2 != nil || h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, 0, false
	}
	return h, m, true
}

// checkNotifications evaluates morning summary and 15-minute due reminders
// against nowFunc() in local time, deduplicating via a.notified.
func (a *App) checkNotifications() {
	now := nowFunc().In(time.Local)
	if a.notified.Due == nil {
		a.notified.Due = map[string]string{}
	}
	dirty := false

	// 1. Morning summary (once per day at or after MorningTime).
	if !a.set.NotifyMorningOff && a.today.loaded {
		h, m, ok := parseMorningTime(a.set.MorningTime)
		if !ok {
			h, m = 8, 0
		}
		target := time.Date(now.Year(), now.Month(), now.Day(), h, m, 0, 0, now.Location())
		todayKey := now.Format("2006-01-02")
		if !now.Before(target) && a.notified.MorningDate != todayKey {
			a.notified.MorningDate = todayKey
			dirty = true
			if n := a.todayActiveCount(); n > 0 {
				a.notify(notification{
					ID:    "today:morning:" + todayKey,
					Title: tr.T("Hari ini"),
					Body:  fmt.Sprintf(tr.T("%d tugas hari ini"), n),
				})
			}
		}
	}

	// 2. Due reminders (15 minutes before due time, for date properties with time).
	if !a.set.NotifyDueOff {
		for _, it := range a.today.items {
			if it.Page == nil {
				continue
			}
			if it.DoneCol != "" && it.DoneProp.IsComplete(it.Page.Properties[it.DoneCol]) {
				continue
			}
			dt, hasTime, ok := parseLocalDateTime(it.Page.Properties[it.DateCol], now.Location())
			if !ok || !hasTime {
				continue
			}
			remindAt := dt.Add(-15 * time.Minute)
			if now.Before(remindAt) || now.After(dt) {
				continue
			}
			dedupeKey := it.Page.ID + "|" + dt.Format(time.RFC3339)
			if a.notified.Due[dedupeKey] != "" {
				continue
			}
			a.notified.Due[dedupeKey] = now.Format(time.RFC3339)
			dirty = true
			body := fmt.Sprintf(tr.T("Tenggat %s"), dt.Format("15:04"))
			if it.DBTitle != "" {
				body = it.DBTitle + " · " + body
			}
			a.notify(notification{
				ID:    "due:" + it.Page.ID,
				Title: nonEmpty(it.Page.Title(), tr.T("Tanpa judul")),
				Body:  body,
			})
		}
	}

	if dirty {
		a.saveNotified()
	}
}

func (a *App) handleCommentEvent(e liveEvent) {
	if a.set.NotifyCommentOff || a.client == nil {
		return
	}
	client := a.client
	bot := a.bot
	go func() {
		ctx := context.Background()
		if bot == nil || bot.PersonID() == "" {
			if me, err := client.Me(ctx); err == nil {
				bot = me
			}
		}
		pageID := e.Data.PageID
		if pageID == "" && (e.Data.Parent.Type == "page" || e.Data.Parent.Type == "page_id") {
			pageID = e.Data.Parent.ID
		}
		if pageID == "" && e.Entity.Type == "page" {
			pageID = e.Entity.ID
		}
		if pageID == "" && e.Entity.Type == "comment" && e.Entity.ID != "" {
			if cm, err := client.Comment(ctx, e.Entity.ID); err == nil && cm != nil {
				pageID = cm.Parent.ID()
			}
		}
		if pageID == "" {
			return
		}
		pg, _ := client.Page(ctx, pageID)
		comments, err := client.Comments(ctx, pageID)
		if err != nil {
			return
		}
		a.update(func() {
			if (a.bot == nil || a.bot.PersonID() == "") && bot != nil {
				a.bot = bot
			}
			a.processCommentNotifications(pageID, pg, comments)
		})
	}()
}

func commentMentionsUser(cm notion.Comment, userID, userName string) bool {
	for _, rt := range cm.RichText {
		if rt.Type == "mention" && rt.Mention != nil && rt.Mention.Type == "user" && rt.Mention.User != nil {
			if userID != "" && rt.Mention.User.ID == userID {
				return true
			}
		}
		if userID != "" && len(rt.Raw) > 0 && strings.Contains(string(rt.Raw), userID) {
			return true
		}
		if userName != "" && strings.Contains(strings.ToLower(rt.PlainText), "@"+strings.ToLower(userName)) {
			return true
		}
	}
	return false
}

func truncateRunes(s string, maxLen int) string {
	r := []rune(s)
	if len(r) <= maxLen {
		return s
	}
	return string(r[:maxLen-1]) + "…"
}

func (a *App) processCommentNotifications(pageID string, pg *notion.Page, comments []notion.Comment) {
	if a.set.NotifyCommentOff || pageID == "" {
		return
	}
	if a.notified.Comments == nil {
		a.notified.Comments = map[string]string{}
	}
	userID := a.bot.PersonID()
	userName := a.userNameByID(userID)
	if a.bot != nil && userName == a.bot.Name {
		userName = "" // the integration's own name, not the person's
	}
	pageOwned := pg != nil && userID != "" && (pg.CreatedBy.ID == userID || pg.LastEditedBy.ID == userID)
	now := nowFunc().In(time.Local)
	// Only comments written while gotion runs (or in the last day) are
	// news: a page's older comments never turn into notifications.
	since := now.Add(-24 * time.Hour)
	if a.notifyFrom.After(since) {
		since = a.notifyFrom.Add(-2 * time.Minute)
	}
	dirty := false

	for _, cm := range comments {
		if cm.ID == "" || a.notified.Comments[cm.ID] != "" || cm.CreatedTime.Before(since) {
			continue
		}
		if userID != "" && cm.CreatedBy.ID == userID {
			continue
		}
		if !pageOwned && !commentMentionsUser(cm, userID, userName) {
			continue
		}
		a.notified.Comments[cm.ID] = now.Format(time.RFC3339)
		dirty = true
		title := tr.T("Komentar baru")
		if pg != nil {
			title = nonEmpty(pg.Title(), title)
		}
		body := truncateRunes(strings.TrimSpace(notion.PlainText(cm.RichText)), 120)
		a.notify(notification{
			ID:    "comment:" + pageID,
			Title: title,
			Body:  body,
		})
	}
	if dirty {
		a.saveNotified()
	}
}
