package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"gotion/internal/notion"
	"gotion/internal/tr"
)

type captureState struct {
	title   string
	focus   bool
	pulse   int
	sending bool
	err     string
}

type captureItem struct {
	ID        string    `json:"id"`
	Mode      string    `json:"mode"`     // "inbox" or "page"
	TargetID  string    `json:"targetID"` // data_source/database ID or parent page ID
	Target    string    `json:"target"`   // display name
	TitleCol  string    `json:"titleCol"`
	Title     string    `json:"title"`
	CreatedAt time.Time `json:"createdAt"`
}

func captureQueuePath() string {
	dir := configDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "capture_queue.json")
}

func (a *App) loadCaptureQueue() {
	path := captureQueuePath()
	if path == "" {
		return
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var q []captureItem
	if json.Unmarshal(b, &q) == nil {
		a.captureQueue = q
	}
}

func (a *App) saveCaptureQueue() {
	path := captureQueuePath()
	if path == "" {
		return
	}
	if len(a.captureQueue) == 0 {
		os.Remove(path)
		return
	}
	if b, err := json.Marshal(a.captureQueue); err == nil {
		os.MkdirAll(filepath.Dir(path), 0o700)
		os.WriteFile(path, b, 0o600)
	}
}

func (a *App) registerCaptureShortcut() {
	if a.win == nil {
		return
	}
	if a.captureReg != "" {
		mygo.GlobalShortcut.Unregister(a.captureReg)
		a.captureReg = ""
	}
	acc := strings.TrimSpace(a.set.CaptureShortcut)
	if acc == "" {
		acc = "CmdOrCtrl+Shift+Space"
	}
	err := mygo.GlobalShortcut.Register(acc, func() {
		a.openCaptureWindow()
	})
	if err != nil {
		a.captureErr = errText(err)
	} else {
		a.captureReg = acc
		a.captureErr = ""
	}
}

func (a *App) openCaptureWindow() {
	a.capture.title = ""
	a.capture.focus = true
	a.capture.pulse = 2
	a.capture.err = ""
	if a.set.CaptureMode == "" {
		if _, _, _, ok := a.captureTarget("inbox"); ok {
			a.set.CaptureMode = "inbox"
		} else {
			a.set.CaptureMode = "page"
		}
	}
	if a.win == nil {
		return
	}
	if a.captureWin == nil {
		w := mygo.NewWindow(mygo.WindowOptions{
			Title:         tr.T("Tangkap cepat"),
			Width:         520,
			Height:        204,
			Frameless:     true,
			Transparent:   true,
			AlwaysOnTop:   true,
			SkipTaskbar:   true,
			DisableResize: true,
			Content:       ui.View(a.captureView),
		})
		w.OnClose(func(e *mygo.CloseEvent) {
			if !a.quitting {
				e.PreventDefault()
				a.closeCaptureWindow()
			}
		})
		a.captureWin = w
	}
	a.captureWin.Show()
	a.captureWin.Focus()
	a.captureWin.Invalidate()
}

func (a *App) closeCaptureWindow() {
	if a.captureWin != nil {
		a.captureWin.Hide()
	}
}

// captureInboxNodes returns databases/data sources available as Inbox targets.
func (a *App) captureInboxNodes() []*node {
	out := make([]*node, 0, len(a.databases))
	out = append(out, a.databases...)
	sort.SliceStable(out, func(i, j int) bool {
		return strings.ToLower(out[i].Title) < strings.ToLower(out[j].Title)
	})
	return out
}

// capturePageNodes returns pages available as parent targets for "Halaman baru".
func (a *App) capturePageNodes() []*node {
	var out []*node
	for _, n := range a.nodes {
		if n != nil && n.Kind == "page" && !isSpecial(n.Kind) {
			out = append(out, n)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return strings.ToLower(out[i].Title) < strings.ToLower(out[j].Title)
	})
	return out
}

// captureTarget resolves the destination ID, display label, and title column
// for mode ("inbox" or "page").
func (a *App) captureTarget(mode string) (targetID, label, titleCol string, ok bool) {
	if mode == "page" {
		if id := a.set.CaptureParentID; id != "" {
			if n := a.nodes[id]; n != nil && n.Kind == "page" {
				return n.ID, nonEmpty(n.Title, tr.T("Tanpa judul")), "", true
			}
		}
		if len(a.groups) > 0 {
			for _, g := range a.groups {
				if g.Kind == "page" {
					return g.ID, nonEmpty(g.Title, tr.T("Tanpa judul")), "", true
				}
			}
		}
		if pages := a.capturePageNodes(); len(pages) > 0 {
			return pages[0].ID, nonEmpty(pages[0].Title, tr.T("Tanpa judul")), "", true
		}
		return "", tr.T("Halaman baru"), "", false
	}

	// Default: "inbox"
	if id := a.set.CaptureInboxID; id != "" {
		if n := a.nodes[id]; n != nil && (n.Kind == "data_source" || n.Kind == "database") {
			return n.ID, nonEmpty(n.Title, tr.T("Database tanpa judul")), "Name", true
		}
	}
	if dbs := a.captureInboxNodes(); len(dbs) > 0 {
		return dbs[0].ID, nonEmpty(dbs[0].Title, tr.T("Database tanpa judul")), "Name", true
	}
	return "", "Inbox", "Name", false
}

func (a *App) captureView(c *ui.Context) {
	if a.win != nil {
		if a.set.Theme == "wallpaper" && a.wallDark != 0 {
			a.dark = a.wallDark == 2
		} else {
			a.dark = a.set.Theme == "dark" || (a.set.Theme != "light" && mygo.Theme.IsDark())
		}
	}
	p := a.pal()
	c.SetTheme(theme(p, a.dark))
	accentBg, accentFg := tintColors(p.Accent, a.dark)

	if c.Shortcut(0, ui.KeyEscape) {
		a.closeCaptureWindow()
	}

	root := ui.Column(c.Key("capture-root")).Fill().Padding(10).Background(ui.Color{})
	root.Children(func() {
		card := ui.Column(c.Key("capture-card")).Fill().Padding(16, 18).Gap(12).Radius(14).
			Background(p.Panel).Border(1, p.Line).Shadow(0, 12, 32, 0, ui.RGBA(0, 0, 0, 0.22)).DragWindow()
		if a.capture.pulse > 0 {
			if a.capture.pulse > 1 {
				card.AnimatePose(ui.Pose{Scale: 0.96, Fade: 0.2}, ui.Snappy)
				c.Invalidate()
			} else {
				card.AnimatePose(ui.Pose{}, ui.Snappy)
			}
			a.capture.pulse--
		}

		mode := a.set.CaptureMode
		if mode != "page" {
			mode = "inbox"
		}
		_, inboxLabel, _, hasInbox := a.captureTarget("inbox")
		_, pageLabel, _, hasPage := a.captureTarget("page")

		card.Children(func() {
			in := ui.TextInput(c.Key("cap-title"), &a.capture.title).
				Placeholder(tr.T("Catat sesuatu…")).FontSize(16).FillWidth()
			if a.capture.focus {
				in.Focus()
				a.capture.focus = false
			}
			if in.Changed() {
				a.capture.err = ""
			}
			if a.capture.err != "" {
				ui.Text(c, a.capture.err).FontSize(12).TextColor(c.Theme().Danger).MaxLines(2)
			}

			ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
				for _, opt := range []struct {
					id    string
					label string
					ok    bool
				}{
					{"inbox", "Inbox: " + inboxLabel, hasInbox},
					{"page", tr.T("Halaman baru") + ": " + pageLabel, hasPage},
				} {
					active := mode == opt.id
					pill := ui.ButtonBase(c.Key("cap-mode-"+opt.id)).Role(ui.RoleButton).Label(opt.label).
						Padding(5, 10).Radius(8).Cursor(ui.CursorPointer)
					if active {
						pill.Background(accentBg)
					} else if pill.Hovered() {
						pill.Background(p.Hover)
					} else {
						pill.Border(1, p.Line)
					}
					pill.Children(func() {
						tc := p.Muted
						if active {
							tc = accentFg
						}
						ui.Text(c, opt.label).FontSize(12).FontWeight(500).TextColor(tc).SingleLine()
					})
					if pill.Clicked() {
						a.set.CaptureMode = opt.id
						a.saveSettings()
					}
				}

				ui.Box(c).Grow(1)
				ui.Text(c, tr.T("Enter · Esc")).FontSize(11).TextColor(p.Faint)

				saveBtn := ui.ButtonBase(c.Key("cap-save")).Role(ui.RoleButton).Label(tr.T("Simpan")).
					Padding(5, 12).Radius(8).Background(accentBg).Cursor(ui.CursorPointer)
				saveBtn.Children(func() {
					ui.Text(c, tr.T("Simpan")).FontSize(12).FontWeight(600).TextColor(accentFg)
				})
				if in.Submitted() || saveBtn.Clicked() {
					a.submitCapture()
				}
			})
		})
	})
}

func (a *App) submitCapture() {
	title := strings.TrimSpace(a.capture.title)
	if title == "" {
		return
	}
	mode := a.set.CaptureMode
	if mode != "page" {
		mode = "inbox"
	}
	targetID, targetLabel, titleCol, ok := a.captureTarget(mode)
	if !ok {
		a.capture.err = tr.T("Pilih tujuan di Pengaturan terlebih dahulu")
		return
	}

	item := captureItem{
		ID:        fmt.Sprintf("cap-%d", nowFunc().UnixNano()),
		Mode:      mode,
		TargetID:  targetID,
		Target:    targetLabel,
		TitleCol:  titleCol,
		Title:     title,
		CreatedAt: nowFunc(),
	}
	a.capture.title = ""
	a.closeCaptureWindow()

	if a.client == nil {
		a.enqueueCapture(item)
		return
	}
	client := a.client
	go func() {
		err := sendCaptureItem(context.Background(), client, item)
		a.update(func() {
			if err != nil {
				if isNetworkError(err) {
					a.enqueueCapture(item)
					return
				}
				// Notion refused it: bring the words back, with why.
				a.openCaptureWindow()
				a.capture.title = item.Title
				a.capture.err = tr.T("Gagal menyimpan: ") + errText(err)
				return
			}
			a.notify(notification{
				ID:    "capture:" + item.ID,
				Title: tr.T("Tangkap cepat"),
				Body:  fmt.Sprintf(tr.T("Tersimpan ke %s"), item.Target),
			})
			a.refresh()
			a.loadToday()
		})
	}()
}

func isNetworkError(err error) bool {
	if err == nil {
		return false
	}
	var ne *notion.Error
	if errors.As(err, &ne) {
		return false
	}
	var ue *url.Error
	return errors.As(err, &ue)
}

func (a *App) enqueueCapture(item captureItem) {
	a.captureQueue = append(a.captureQueue, item)
	a.saveCaptureQueue()
	a.notify(notification{
		ID:    "capture:" + item.ID,
		Title: tr.T("Tangkap cepat"),
		Body:  fmt.Sprintf(tr.T("Offline: disimpan ke antrean (%s)"), item.Target),
	})
}

func sendCaptureItem(ctx context.Context, client *notion.Client, item captureItem) error {
	if item.Mode == "page" {
		_, err := client.CreatePage(ctx, item.TargetID, item.Title)
		return err
	}
	dsID := item.TargetID
	titleCol := nonEmpty(item.TitleCol, "Name")
	if ds, err := client.DataSource(ctx, dsID); err == nil {
		for name, prop := range ds.Properties {
			if prop.Type == "title" {
				titleCol = nonEmpty(prop.Name, name)
				break
			}
		}
	} else if db, err2 := client.Database(ctx, dsID); err2 == nil && len(db.DataSources) > 0 {
		dsID = db.DataSources[0].ID
		if ds, err3 := client.DataSource(ctx, dsID); err3 == nil {
			for name, prop := range ds.Properties {
				if prop.Type == "title" {
					titleCol = nonEmpty(prop.Name, name)
					break
				}
			}
		}
	}
	_, err := client.CreateRowFrom(ctx, dsID, titleCol, item.Title, "")
	return err
}

func (a *App) flushCaptureQueue() {
	if a.captureFlushing || len(a.captureQueue) == 0 || a.client == nil {
		return
	}
	a.captureFlushing = true
	client := a.client
	queue := append([]captureItem(nil), a.captureQueue...)
	go func() {
		ctx := context.Background()
		sent := 0
		var refused []captureItem
		for _, item := range queue {
			if err := sendCaptureItem(ctx, client, item); err != nil {
				if isNetworkError(err) {
					break
				}
				// Notion refuses it for good: take it off the queue so it
				// doesn't block the rest, but tell the user its words.
				refused = append(refused, item)
			}
			sent++
		}
		a.update(func() {
			a.captureFlushing = false
			for _, it := range refused {
				a.notify(notification{
					ID:    "capture:" + it.ID,
					Title: tr.T("Tangkap cepat gagal disimpan"),
					Body:  it.Title,
				})
			}
			if sent > 0 {
				if sent >= len(a.captureQueue) {
					a.captureQueue = nil
				} else {
					a.captureQueue = a.captureQueue[sent:]
				}
				a.saveCaptureQueue()
				a.refresh()
				a.loadToday()
			}
		})
	}()
}
