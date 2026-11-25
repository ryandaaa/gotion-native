package app

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"gotion/internal/notion"
)

func newTestAppForNotify(t *testing.T) (*App, *[]notification) {
	t.Helper()
	os.Remove(notifiedPath())
	a := New()
	a.today.loaded = true
	var got []notification
	a.sendNotify = func(n notification) {
		got = append(got, n)
	}
	return a, &got
}

func makeTaskItem(id, title, dateStart string, done bool) todayItem {
	props := map[string]json.RawMessage{
		"Name": json.RawMessage(`{"type":"title","title":[{"plain_text":` + mustJSON(title) + `}]}`),
		"Date": json.RawMessage(`{"type":"date","date":{"start":` + mustJSON(dateStart) + `}}`),
		"Done": json.RawMessage(`{"type":"checkbox","checkbox":` + map[bool]string{true: "true", false: "false"}[done] + `}`),
	}
	return todayItem{
		Page:     &notion.Page{ID: id, Properties: props},
		DBID:     "db1",
		DBTitle:  "Tasks",
		TitleCol: "Name",
		DateCol:  "Date",
		DoneCol:  "Done",
		DoneProp: notion.Property{Name: "Done", Type: "checkbox"},
	}
}

func mustJSON(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func TestMorningSummarySchedule(t *testing.T) {
	origNow := nowFunc
	defer func() { nowFunc = origNow }()

	loc := time.Local
	cur := time.Date(2026, 11, 25, 7, 59, 0, 0, loc)
	nowFunc = func() time.Time { return cur }

	a, got := newTestAppForNotify(t)
	a.today.items = []todayItem{
		makeTaskItem("t1", "Task 1", "2026-11-25", false),
		makeTaskItem("t2", "Task 2", "2026-11-24", false),
	}

	// Before 08:00: no morning summary.
	a.checkNotifications()
	if len(*got) != 0 {
		t.Fatalf("expected 0 notifications before 08:00, got %+v", *got)
	}

	// At 08:00: fires once.
	cur = time.Date(2026, 11, 25, 8, 0, 0, 0, loc)
	a.checkNotifications()
	if len(*got) != 1 || (*got)[0].ID != "today:morning:2026-11-25" {
		t.Fatalf("expected morning notification, got %+v", *got)
	}

	// Same day 08:30: deduped.
	cur = time.Date(2026, 11, 25, 8, 30, 0, 0, loc)
	a.checkNotifications()
	if len(*got) != 1 {
		t.Fatalf("expected dedupe on same day, got %+v", *got)
	}

	// Next day at custom MorningTime 09:30: fires at 09:30, not 09:00.
	a.set.MorningTime = "09:30"
	cur = time.Date(2026, 11, 26, 9, 0, 0, 0, loc)
	a.checkNotifications()
	if len(*got) != 1 {
		t.Fatalf("expected no notification before custom 09:30, got %+v", *got)
	}
	cur = time.Date(2026, 11, 26, 9, 30, 0, 0, loc)
	a.checkNotifications()
	if len(*got) != 2 || (*got)[1].ID != "today:morning:2026-11-26" {
		t.Fatalf("expected second morning notification, got %+v", *got)
	}

	// Disabled in settings: no notification on day 3.
	a.set.NotifyMorningOff = true
	cur = time.Date(2026, 11, 27, 10, 0, 0, 0, loc)
	a.checkNotifications()
	if len(*got) != 2 {
		t.Fatalf("expected no notification when disabled, got %+v", *got)
	}
}

func TestDueReminderScheduleAndDedupe(t *testing.T) {
	origNow := nowFunc
	defer func() { nowFunc = origNow }()

	loc := time.Local
	due := time.Date(2026, 11, 25, 10, 0, 0, 0, loc)
	cur := time.Date(2026, 11, 25, 9, 30, 0, 0, loc)
	nowFunc = func() time.Time { return cur }

	a, got := newTestAppForNotify(t)
	a.set.NotifyMorningOff = true
	a.today.items = []todayItem{
		makeTaskItem("p1", "Standup", due.Format(time.RFC3339), false),
		makeTaskItem("p2", "DateOnly", "2026-11-25", false),
		makeTaskItem("p3", "Completed", due.Format(time.RFC3339), true),
	}

	// 30 minutes before: too early.
	a.checkNotifications()
	if len(*got) != 0 {
		t.Fatalf("expected 0 notifications 30m before, got %+v", *got)
	}

	// 15 minutes before: fires for p1 only.
	cur = time.Date(2026, 11, 25, 9, 45, 0, 0, loc)
	a.checkNotifications()
	if len(*got) != 1 || (*got)[0].ID != "due:p1" || (*got)[0].Title != "Standup" {
		t.Fatalf("expected due:p1 notification, got %+v", *got)
	}

	// Reload notified state (simulating app restart) and check at 09:55: must stay deduped.
	a.loadNotified()
	cur = time.Date(2026, 11, 25, 9, 55, 0, 0, loc)
	a.checkNotifications()
	if len(*got) != 1 {
		t.Fatalf("expected due notification to stay deduped after restart, got %+v", *got)
	}

	// Disabled in settings: new task does not notify.
	a.set.NotifyDueOff = true
	a.today.items = append(a.today.items, makeTaskItem("p4", "Another", due.Format(time.RFC3339), false))
	a.checkNotifications()
	if len(*got) != 1 {
		t.Fatalf("expected no due notification when disabled, got %+v", *got)
	}
}

func TestCommentMentionAndOwnerDetection(t *testing.T) {
	a, got := newTestAppForNotify(t)
	a.bot = &notion.User{ID: "u1", Name: "Ryanda", Type: "person"}

	otherPage := &notion.Page{
		ID:        "pg-other",
		CreatedBy: notion.User{ID: "u2"},
		Properties: map[string]json.RawMessage{
			"title": json.RawMessage(`{"type":"title","title":[{"plain_text":"Roadmap"}]}`),
		},
	}
	ownedPage := &notion.Page{
		ID:        "pg-own",
		CreatedBy: notion.User{ID: "u1"},
		Properties: map[string]json.RawMessage{
			"title": json.RawMessage(`{"type":"title","title":[{"plain_text":"My Notes"}]}`),
		},
	}

	comments := []notion.Comment{
		{
			ID:          "c-unrelated",
			CreatedTime: time.Now(),
			CreatedBy:   notion.User{ID: "u2"},
			RichText:    []notion.RichText{{Type: "text", PlainText: "Looks good"}},
		},
		{
			ID:          "c-self",
			CreatedTime: time.Now(),
			CreatedBy:   notion.User{ID: "u1"},
			RichText:    []notion.RichText{{Type: "text", PlainText: "@Ryanda self"}},
		},
		{
			ID:          "c-mention",
			CreatedTime: time.Now(),
			CreatedBy:   notion.User{ID: "u2"},
			RichText: []notion.RichText{
				{
					Type:      "mention",
					PlainText: "@Ryanda",
					Mention: &struct {
						Type string       `json:"type"`
						User *notion.User `json:"user,omitempty"`
					}{Type: "user", User: &notion.User{ID: "u1"}},
				},
				{Type: "text", PlainText: " please review"},
			},
		},
	}

	a.processCommentNotifications("pg-other", otherPage, comments)
	if len(*got) != 1 || (*got)[0].ID != "comment:pg-other" || (*got)[0].Title != "Roadmap" {
		t.Fatalf("expected 1 mention notification, got %+v", *got)
	}

	// Re-running with same comment IDs must dedupe.
	a.processCommentNotifications("pg-other", otherPage, comments)
	if len(*got) != 1 {
		t.Fatalf("expected comment notification dedupe, got %+v", *got)
	}

	// Comment on user's own page without explicit mention still notifies.
	a.processCommentNotifications("pg-own", ownedPage, []notion.Comment{
		{
			ID:          "c-on-owned",
			CreatedTime: time.Now(),
			CreatedBy:   notion.User{ID: "u2"},
			RichText:    []notion.RichText{{Type: "text", PlainText: "Updated section 2"}},
		},
	})
	if len(*got) != 2 || (*got)[1].ID != "comment:pg-own" || (*got)[1].Title != "My Notes" {
		t.Fatalf("expected owner notification, got %+v", *got)
	}

	// Disabled in settings: no notification.
	a.set.NotifyCommentOff = true
	a.processCommentNotifications("pg-own", ownedPage, []notion.Comment{
		{
			ID:          "c-disabled",
			CreatedTime: time.Now(),
			CreatedBy:   notion.User{ID: "u2"},
			RichText:    []notion.RichText{{Type: "text", PlainText: "Should not fire"}},
		},
	})
	if len(*got) != 2 {
		t.Fatalf("expected no notification when disabled, got %+v", *got)
	}
}

func TestTrayBadgeCount(t *testing.T) {
	origNow := nowFunc
	defer func() { nowFunc = origNow }()
	nowFunc = func() time.Time { return time.Date(2026, 11, 25, 10, 0, 0, 0, time.Local) }

	a, _ := newTestAppForNotify(t)
	var badge string
	a.setBadge = func(s string) { badge = s }

	a.today.items = []todayItem{
		makeTaskItem("t1", "Overdue", "2026-11-24", false),
		makeTaskItem("t2", "Today", "2026-11-25", false),
		makeTaskItem("t3", "Done", "2026-11-25", true),
	}
	a.updateTrayBadge()
	if badge != "2" {
		t.Fatalf("expected badge '2', got %q", badge)
	}

	a.today.items = nil
	a.updateTrayBadge()
	if badge != "" {
		t.Fatalf("expected empty badge when 0, got %q", badge)
	}
}
