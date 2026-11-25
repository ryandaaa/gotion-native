// Package app is gotion's window: the page tree, the editor and the
// database table, drawn with MyGo's native UI.
package app

import (
	"context"
	"encoding/json"
	"gotion/internal/tr"
	"gotion/internal/wallpaper"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/richtext"

	"gotion/internal/doc"
	"gotion/internal/emoji"
	"gotion/internal/notion"
	"gotion/internal/syncer"
)

const secretKey = "notion-token"

// node is a page or data source in the sidebar.
type node struct {
	ID, Title, Emoji, Kind, ParentID string // Kind: "page", "data_source" or "database"
	Edited                           time.Time
	Children                         []*node
}

// panel shows one page or one table.
type panel struct {
	id      string
	session *syncer.Session
	doc     *richtext.Document
	extras  []doc.Extra
	opening bool
	openErr string
	table   *tableView

	// the title being edited
	title      string
	titleSent  string
	titleEdit  time.Time
	titleDirty bool

	// the slash menu
	slash slashState
	// the + beside blocks
	handle handleState
	// the @ popup
	mention mentionState
	// inline table cell editor
	cellEdit cellEditState
	pointerX float32
	pointerY float32
	// the ••• menu of the page
	menu pageMenu
	// the comments beside it
	comments commentsState
	// the properties of a database row, over its text
	props *tableView
	// the cover across its top
	cover coverState
	// focusEditor gives the editor the keyboard on its next frame.
	focusEditor bool
	// caret tracking so typing or moving the caret brings it into view in
	// the page's scroll without fighting the wheel.
	caretInit   bool
	caretSeen   [2]int
	verSeen     uint64
	revealCaret bool
	width       float32
	// textWidth is the width of the page's text, which tables draw across.
	textWidth float32
	// readOnly holds what each read-only paragraph holds (guardReadOnly).
	readOnly map[string]string
	warned   bool

	// right pane outline & scroll tracking
	scroll       ui.ScrollState
	jumpPara     int
	flashPara    int
	flashAt      time.Time
	lastEditedBy string
}

// App is the state of the window.
type App struct {
	win    *mygo.Window
	client *notion.Client
	bot    *notion.User
	set    settings

	// onboarding
	tokenInput string
	checking   bool
	tokenErr   string
	tokenOpen  bool // the integration token's steps unfolded
	// onbLastStep is the sign-in step shown last, for the way the next slides.
	onbLastStep int

	// sidebar
	nodes     map[string]*node
	groups    []*node
	databases []*node
	loading   bool
	loadErr   string
	// buttonsSet is the theme the window buttons were last drawn for:
	// 0 not yet, 1 light, 2 dark.
	buttonsSet int
	// wallDark is the wallpaper behind the sidebar, for the "wallpaper"
	// mode: 0 not known, 1 light, 2 dark. watchWall reads it.
	wallDark int
	// wall is what the watcher needs of the settings, read off the UI's
	// goroutine.
	wall *wallState
	// searched tells that Notion said which pages there are, at least
	// once since signing in: none then means none were shared.
	searched bool
	query    string

	// panels: the first always, the second when split; each shows its
	// active tab
	panels [2]*panel
	tabs   [2][]*panel
	focus  int

	// history of the first panel
	history []string
	hpos    int

	switcher switcherState
	// liveCancel stops asking the Worker for events.
	liveCancel context.CancelFunc
	// oauth is a sign-in through Notion's page in progress.
	oauth *oauthState
	// viewer shows a picture full size.
	viewer viewer
	// imageOf finds a picture by its block where no session holds it: tests.
	imageOf func(id string) ui.ImageSource
	// library is the Pustaka page's search and order.
	library libraryState
	// today is the "Hari ini" task aggregator across databases.
	today todayState
	// allCal is the unified "Kalender" aggregator across databases.
	allCal allCalendarState
	// settings is the settings dialog.
	settings settingsView
	// themeOpen shows the theme picker.
	themeOpen bool
	sideList  ui.ListState
	toasts    []string
	dark      bool

	// widths are the last widths of the panels, for pages opened into them.
	widths [2]float32

	// right pane state
	rightPaneOpen bool
	pageInfos     map[string]pageInfoEntry
	rightPaneFits bool // the window has room for it beside the panels
	userNames     map[string]string
	usersAsked    bool

	// tray and notifications
	tray         *mygo.Tray
	quitting     bool
	notified     notifiedState
	notifyFrom   time.Time // comments older than this never notify
	notifyCancel context.CancelFunc
	sendNotify   func(notification)
	setBadge     func(string)

	// quick capture
	capture         captureState
	captureWin      *mygo.Window
	captureReg      string
	captureErr      string
	captureQueue    []captureItem
	captureFlushing bool
	netOff          func()
}

// New returns the app, signed in when a token is saved.
func New() *App {
	a := &App{panels: [2]*panel{{}, nil}, nodes: map[string]*node{}}
	a.set = loadSettings()
	a.applyLanguage()
	if raw := loadToken(); raw != "" {
		a.client = a.clientFor(parseCreds(raw))
	}
	a.loadCache()
	a.loadTodayCache()
	a.loadNotified()
	a.notifyFrom = nowFunc()
	a.loadCaptureQueue()
	return a
}

// Attach gives the app its window, and starts loading.
func (a *App) Attach(w *mygo.Window) {
	a.win = w
	mygo.App.SetLocale(tr.Lang())
	a.applyGlass()
	if w != nil {
		w.OnClose(func(e *mygo.CloseEvent) {
			if a.closeHides() {
				e.PreventDefault()
				w.Hide()
			}
		})
		a.initTray()
		a.sendNotify = func(n notification) {
			go func() {
				_ = mygo.NewNotification(mygo.NotificationOptions{
					ID:    n.ID,
					Title: n.Title,
					Body:  n.Body,
				}).Show()
			}()
		}
		a.setBadge = func(label string) {
			mygo.App.Dock.SetBadge(label)
			if a.tray != nil {
				a.tray.SetTitle(label)
			}
		}
		a.updateTrayBadge()
		a.registerCaptureShortcut()
		a.netOff = mygo.Network.OnChanged(func(s mygo.NetworkStatus) {
			if s.Online {
				a.flushCaptureQueue()
			}
		})
	}
	if w != nil && wallpaper.Supported() && a.wall == nil {
		a.wall = &wallState{}
		go a.watchWall(a.wall)
	}
	if a.client == nil {
		return
	}
	a.refresh()
	a.startLive()
	a.startTodayTimer()
	a.startNotifyTimer()
	a.flushCaptureQueue()
	a.restoreTabs()
}

func (a *App) flushPanel(p *panel) {
	if p == nil {
		return
	}
	a.sendTitle(p)
	if p.table != nil {
		a.sendTableTitle(p.table)
	}
}

// Close sends unsaved edits.
func (a *App) Close() {
	a.quitting = true
	a.stopTodayTimer()
	a.stopNotifyTimer()
	if a.netOff != nil {
		a.netOff()
		a.netOff = nil
	}
	if a.win != nil {
		mygo.GlobalShortcut.UnregisterAll()
	}
	if a.captureWin != nil {
		a.captureWin.Close()
		a.captureWin = nil
	}
	if a.tray != nil {
		a.tray.Destroy()
		a.tray = nil
	}
	for _, list := range a.tabs {
		for _, p := range list {
			a.flushPanel(p)
			if p != nil && p.session != nil {
				p.session.Close()
			}
		}
	}
	for _, p := range a.panels {
		a.flushPanel(p)
		if p != nil && p.session != nil {
			p.session.Close()
		}
	}
	a.saveSettings()
}

func (a *App) pal() palette {
	p := light
	if a.dark {
		p = dark
	}
	return washByID(a.set.Wash).tint(p, a.dark)
}

func (a *App) toast(msg string) {
	a.toasts = append(a.toasts, msg)
	if a.win != nil {
		a.win.Invalidate()
	}
}

// --- settings ---

type settings struct {
	Theme          string          `json:"theme"` // "system", "light" or "dark"
	SidebarWidth   float32         `json:"sidebarWidth"`
	SidebarOff     bool            `json:"sidebarOff"`
	RightPane      bool            `json:"rightPane"`
	RightPaneWidth float32         `json:"rightPaneWidth"`
	Open           map[string]bool `json:"open"`   // expanded pages
	Closed         map[string]bool `json:"closed"` // collapsed groups
	Last           string          `json:"last"`
	Second         string          `json:"second"`
	Tabs           [2][]string     `json:"tabs"`   // each panel's tabs
	Active         [2]int          `json:"active"` // and which shows
	SplitSize      float32         `json:"splitSize"`
	FullWidth      bool            `json:"fullWidth"`
	Toggles        map[string]bool `json:"toggles"` // toggle blocks shown open
	Zoom           float32         `json:"zoom"`    // 1 is 100%
	Wash           string          `json:"wash"`    // the color behind the cards
	Trashed        []trashed       `json:"trashed"` // pages put in the trash from gotion
	Glass          bool            `json:"glass"`   // the system's glass behind them
	// Views is how each database shows: "table", "cards", "board" or "calendar".
	Views map[string]string `json:"views"`
	// BoardGroup is the status/select column each database groups its board by.
	BoardGroup map[string]string `json:"boardGroup"`
	// CalendarDate is the date column each database uses in calendar view.
	CalendarDate map[string]string `json:"calendarDate"`
	// TodayOff lists database IDs excluded from the "Hari ini" view.
	TodayOff map[string]bool `json:"todayOff"`
	// CalOff lists database IDs excluded from the unified "Kalender" view.
	CalOff map[string]bool `json:"calOff"`
	// TodayLastDB remembers the last database chosen in "+ Tugas baru".
	TodayLastDB string `json:"todayLastDB"`
	// Language is "id" or "en", or "" to follow the system's.
	Language string `json:"language"`
	// Tray, startup, and notification preferences.
	StayInTray       bool   `json:"stayInTray"`
	OpenAtLogin      bool   `json:"openAtLogin"`
	NotifyMorningOff bool   `json:"notifyMorningOff"`
	NotifyDueOff     bool   `json:"notifyDueOff"`
	NotifyCommentOff bool   `json:"notifyCommentOff"`
	MorningTime      string `json:"morningTime"`
	// Quick capture preferences.
	CaptureShortcut string `json:"captureShortcut"`
	CaptureInboxID  string `json:"captureInboxID"`
	CaptureParentID string `json:"captureParentID"`
	CaptureMode     string `json:"captureMode"`
}

func loadSettings() settings {
	s := settings{Theme: "system", SidebarWidth: 270, MorningTime: "08:00", CaptureShortcut: "CmdOrCtrl+Shift+Space"}
	if b, err := os.ReadFile(filepath.Join(configDir(), "settings.json")); err == nil {
		json.Unmarshal(b, &s)
	}
	if s.Open == nil {
		s.Open = map[string]bool{}
	}
	if s.Closed == nil {
		s.Closed = map[string]bool{}
	}
	if s.Toggles == nil {
		s.Toggles = map[string]bool{}
	}
	if s.Views == nil {
		s.Views = map[string]string{}
	}
	if s.BoardGroup == nil {
		s.BoardGroup = map[string]string{}
	}
	if s.CalendarDate == nil {
		s.CalendarDate = map[string]string{}
	}
	if s.TodayOff == nil {
		s.TodayOff = map[string]bool{}
	}
	if s.CalOff == nil {
		s.CalOff = map[string]bool{}
	}
	if s.SidebarWidth < 200 {
		s.SidebarWidth = 270
	}
	if s.RightPaneWidth < 240 || s.RightPaneWidth > 460 {
		s.RightPaneWidth = 300
	}
	if s.Zoom < zooms[0] || s.Zoom > zooms[len(zooms)-1] {
		s.Zoom = 1
	}
	if _, _, ok := parseMorningTime(s.MorningTime); !ok {
		s.MorningTime = "08:00"
	}
	if s.CaptureShortcut == "" {
		s.CaptureShortcut = "CmdOrCtrl+Shift+Space"
	}
	return s
}

func (a *App) saveSettings() {
	if b, err := json.MarshalIndent(a.set, "", "  "); err == nil {
		writeWhole(filepath.Join(configDir(), "settings.json"), b)
	}
}

// --- token ---

func configDir() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "gotion")
}

// secrets is where credentials are kept: the system's keychain, or what a
// test puts in its place, so that no test ever touches the user's own.
var secrets interface {
	Get(key string) ([]byte, error)
	Set(key string, value []byte) error
	Delete(key string) error
} = mygo.Secrets.Service("gotion")

// loadToken returns the saved credentials as saved: a token, or OAuth's
// as JSON (parseCreds reads either).
func loadToken() string {
	if t := os.Getenv("NOTION_TOKEN"); t != "" {
		return t
	}
	if b, err := secrets.Get(secretKey); err == nil {
		return string(b)
	}
	if b, err := os.ReadFile(filepath.Join(configDir(), "token")); err == nil {
		return strings.TrimSpace(string(b))
	}
	return ""
}

// cleanToken drops what a paste can bring along with a token: spaces and
// line breaks anywhere, invisible characters, quotes and a "Bearer " prefix.
func cleanToken(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 7 && strings.EqualFold(s[:7], "bearer ") {
		s = s[7:]
	}
	var b strings.Builder
	for _, r := range s {
		if r > ' ' && r < 0x7f && r != '"' && r != '\'' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func saveToken(tok string) {
	if err := secrets.Set(secretKey, []byte(tok)); err == nil {
		return
	}
	// No keychain (a Linux without a Secret Service): a file only the user reads.
	os.MkdirAll(configDir(), 0o700)
	os.WriteFile(filepath.Join(configDir(), "token"), []byte(tok), 0o600)
}

func deleteToken() {
	secrets.Delete(secretKey)
	os.Remove(filepath.Join(configDir(), "token"))
	os.Remove(filepath.Join(configDir(), "pages.json"))
	os.RemoveAll(filepath.Join(configDir(), "cache"))
}

// checkToken signs in with the token typed in onboarding.
func (a *App) checkToken() {
	tok := cleanToken(a.tokenInput)
	if tok == "" {
		a.tokenErr = tr.T("Tempel token dulu.")
		return
	}
	if !strings.HasPrefix(tok, "ntn_") && !strings.HasPrefix(tok, "secret_") {
		a.tokenErr = tr.T("Token Notion biasanya diawali ntn_ (atau secret_ untuk yang lama). Cek lagi yang kamu salin.")
		return
	}
	a.signIn(creds{Access: tok})
}

func (a *App) signOut() {
	a.searched = false
	a.stopLive()
	a.stopTodayTimer()
	for _, p := range a.panels {
		if p != nil && p.session != nil {
			p.session.Close()
		}
	}
	deleteToken()
	set := a.set
	set.Last, set.Second = "", ""
	*a = App{win: a.win, set: set, panels: [2]*panel{{}, nil}, dark: a.dark, nodes: map[string]*node{}, wall: a.wall, wallDark: a.wallDark}
	a.saveSettings()
}

func errText(err error) string { return syncer.Explain(err) }

// --- the page tree ---

// refresh loads the pages shared with the integration.
func (a *App) refresh() {
	if a.loading {
		return
	}
	a.loading, a.loadErr = true, ""
	client := a.client
	go func() {
		ctx := context.Background()
		var me *notion.User
		var err error
		if a.bot == nil {
			me, err = client.Me(ctx)
		}
		var pages []*notion.Page
		if err == nil {
			pages, err = client.Search(ctx)
		}
		a.update(func() {
			a.loading = false
			if me != nil {
				a.bot = me
			}
			if err != nil {
				a.loadErr = errText(err)
				return
			}
			a.searched = true
			a.setPages(pages)
			a.saveCache(pages)
			a.loadToday()
		})
	}()
}

func (a *App) setPages(pages []*notion.Page) {
	old := a.nodes
	a.nodes = map[string]*node{}
	for _, p := range pages {
		if p == nil || p.InTrash {
			continue
		}
		kind := "page"
		switch p.Object {
		case "data_source", "database":
			kind = "data_source"
		case "page":
			if p.Parent.Type == "data_source_id" || p.Parent.Type == "database_id" {
				continue // a row of a database: shown in its table
			}
		default:
			continue
		}
		title := p.Title()
		if title == "" {
			title = tr.T("Tanpa judul")
		}
		a.nodes[p.ID] = &node{ID: p.ID, Title: title, Emoji: p.Emoji(), Kind: kind, ParentID: p.Parent.ID(), Edited: p.LastEditedTime}
	}
	// Keep the pages found inside opened pages that search does not list.
	for id, n := range old {
		if a.nodes[id] == nil && n.ParentID != "" && (old[n.ParentID] != nil || a.nodes[n.ParentID] != nil) {
			a.nodes[id] = &node{ID: n.ID, Title: n.Title, Emoji: n.Emoji, Kind: n.Kind, ParentID: n.ParentID, Edited: n.Edited}
		}
	}
	a.regroup()
}

// regroup builds the sidebar's groups from the nodes.
func (a *App) regroup() {
	for _, n := range a.nodes {
		n.Children = nil
	}
	a.groups, a.databases = nil, nil
	for _, n := range a.nodes {
		if isSpecial(n.Kind) {
			continue
		}
		if n.Kind == "data_source" {
			a.databases = append(a.databases, n)
			continue
		}
		if parent := a.nodes[n.ParentID]; parent != nil && parent.Kind == "page" {
			parent.Children = append(parent.Children, n)
		} else {
			a.groups = append(a.groups, n)
		}
	}
	byTitle := func(ns []*node) {
		sort.Slice(ns, func(i, j int) bool { return byTitleThenID(ns[i], ns[j]) })
	}
	byTitle(a.groups)
	byTitle(a.databases)
	for _, n := range a.nodes {
		byTitle(n.Children)
	}
}

func (a *App) saveCache(pages []*notion.Page) {
	if b, err := json.Marshal(pages); err == nil {
		writeWhole(filepath.Join(configDir(), "pages.json"), b)
	}
}

// writeWhole writes a file through a temporary one beside it, so a crash
// mid-write leaves the old file instead of half of the new.
func writeWhole(path string, b []byte) {
	os.MkdirAll(filepath.Dir(path), 0o700)
	tmp := path + ".tmp"
	if os.WriteFile(tmp, b, 0o600) == nil && os.Rename(tmp, path) != nil {
		os.Remove(tmp)
	}
}

func (a *App) loadCache() {
	if a.client == nil {
		return
	}
	var pages []*notion.Page
	if b, err := os.ReadFile(filepath.Join(configDir(), "pages.json")); err == nil && json.Unmarshal(b, &pages) == nil {
		a.setPages(pages)
	}
}

// registerChildren adds a page's sub-pages and databases to the sidebar:
// Notion's search can take a while to list pages shared just now, and
// leaves out some it never indexes.
func (a *App) registerChildren(pageID string, extras []doc.Extra) {
	if a.nodes[pageID] == nil {
		return
	}
	added := false
	for _, x := range extras {
		var kind string
		switch x.Type {
		case "child_page":
			kind = "page"
		case "child_database":
			kind = "database"
		default:
			continue
		}
		if a.nodes[x.ID] != nil {
			continue
		}
		title := strings.TrimSpace(x.Title)
		if title == "" || title == "-" {
			title = map[string]string{"page": tr.T("Tanpa judul"), "database": tr.T("Database tanpa judul")}[kind]
		}
		a.nodes[x.ID] = &node{ID: x.ID, Title: title, Kind: kind, ParentID: pageID}
		added = true
	}
	if added {
		a.regroup()
	}
}

// path returns the pages from the root down to id's parent.
func (a *App) path(id string) []*node {
	var out []*node
	n := a.nodes[id]
	for n != nil && n.ParentID != "" {
		n = a.nodes[n.ParentID]
		if n == nil {
			break
		}
		out = append([]*node{n}, out...)
	}
	return out
}

// --- opening ---

// openID opens a page or table by ID in panel i, adding it to the tree
// when it is not there yet.
func (a *App) openID(id, kind, parent string, i int) {
	n := a.nodes[id]
	if n == nil {
		n = &node{ID: id, Title: tr.T("Memuat…"), Kind: kind, ParentID: parent}
		a.nodes[id] = n
		a.regroup()
	}
	a.open(n, i)
}

// navigateID goes to a page or table by ID in panel i's active tab, as a
// link does.
func (a *App) navigateID(id, kind, parent string, i int) {
	n := a.nodes[id]
	if n == nil {
		n = &node{ID: id, Title: tr.T("Memuat…"), Kind: kind, ParentID: parent}
		a.nodes[id] = n
		a.regroup()
	}
	a.navigate(n, i)
}

// open shows n in panel i in a tab of its own: the tab already showing
// it, else a new one after the active tab (or the empty panel itself).
func (a *App) open(n *node, i int) {
	if a.panels[i] == nil {
		a.panels[i] = &panel{}
	}
	a.focus = i
	for _, t := range a.tabs[i] {
		if t.id == n.ID {
			a.activate(i, t)
			a.remember(n, i)
			return
		}
	}
	if a.panels[i].id == "" {
		a.remember(n, i)
		a.show(n, i)
		return
	}
	p := &panel{id: n.ID}
	k := a.tabIndex(i, a.panels[i]) + 1
	a.tabs[i] = append(a.tabs[i][:k], append([]*panel{p}, a.tabs[i][k:]...)...)
	a.panels[i] = p
	a.remember(n, i)
	a.load(p, n)
}

// navigate shows n in panel i's active tab, as a link in a page does.
func (a *App) navigate(n *node, i int) {
	if a.panels[i] == nil || a.panels[i].id == "" {
		a.open(n, i)
		return
	}
	for _, t := range a.tabs[i] {
		if t.id == n.ID {
			a.activate(i, t)
			a.remember(n, i)
			return
		}
	}
	a.remember(n, i)
	a.show(n, i)
}

// remember keeps where panel i went: its history, the tree open down to
// it, and the tabs for the next run.
func (a *App) remember(n *node, i int) {
	if i == 0 {
		// Going somewhere new drops the pages ahead.
		if len(a.history) > 0 {
			a.history = a.history[:a.hpos+1]
		}
		if len(a.history) == 0 || a.history[len(a.history)-1] != n.ID {
			a.history = append(a.history, n.ID)
		}
		a.hpos = len(a.history) - 1
	}
	for _, up := range a.path(n.ID) {
		a.set.Open[up.ID] = true
	}
	a.saveTabs()
}

// show loads n into panel i's active tab, closing what it showed.
func (a *App) show(n *node, i int) {
	old := a.panels[i]
	a.flushPanel(old)
	if old != nil && old.session != nil {
		s := old.session
		go s.Close()
	}
	p := &panel{id: n.ID}
	if k := a.tabIndex(i, old); k >= 0 {
		a.tabs[i][k] = p
	} else {
		a.tabs[i] = append(a.tabs[i], p)
	}
	a.panels[i] = p
	a.saveTabs()
	a.load(p, n)
}

// load fetches what panel p shows: a page or a table.
func (a *App) load(p *panel, n *node) {
	if isSpecial(n.Kind) {
		return // the app's own: nothing to fetch
	}
	if n.Kind == "data_source" || n.Kind == "database" {
		a.openTable(p, n)
		return
	}
	a.openPage(p, n.ID)
}

func (a *App) back() {
	if a.hpos > 0 {
		a.hpos--
		if n := a.nodes[a.history[a.hpos]]; n != nil {
			a.focus = 0
			a.showOrActivate(n, 0)
		}
	}
}

func (a *App) forward() {
	if a.hpos < len(a.history)-1 {
		a.hpos++
		if n := a.nodes[a.history[a.hpos]]; n != nil {
			a.focus = 0
			a.showOrActivate(n, 0)
		}
	}
}

// showOrActivate goes to n in panel i without adding to its history: to
// its tab if it has one, else in the active tab.
func (a *App) showOrActivate(n *node, i int) {
	for _, t := range a.tabs[i] {
		if t.id == n.ID {
			a.activate(i, t)
			return
		}
	}
	a.show(n, i)
}

// openBeside opens n in the second panel.
func (a *App) openBeside(n *node) {
	if a.panels[1] == nil {
		a.panels[1] = &panel{}
		a.guessSplit(true)
	}
	a.open(n, 1)
}

// guessSplit sets the widths a split leaves, so its first frame is right.
// A split the user opens (half) goes 50:50, whatever the divider was dragged
// to last; one restored at start keeps where it was.
func (a *App) guessSplit(half bool) {
	total := a.widths[0]
	if total <= 0 {
		return
	}
	first := a.set.SplitSize
	if half || first < 300 || first > total-300 {
		first = total / 2
		a.set.SplitSize = first
	}
	a.widths[0], a.widths[1] = first, total-first
	if a.panels[0] != nil {
		a.panels[0].width = first
	}
}

func (a *App) closeSecond() {
	if a.panels[1] == nil {
		return
	}
	for _, t := range a.tabs[1] {
		a.flushPanel(t)
		if t.session != nil {
			go t.session.Close()
		}
	}
	a.panels[1], a.tabs[1] = nil, nil
	a.focus = 0
	a.widths[0] += a.widths[1]
	if a.panels[0] != nil {
		a.panels[0].width = a.widths[0]
	}
	a.saveTabs()
}

// followLink opens a link clicked in panel i's editor.
func (a *App) followLink(i int) func(url string) {
	return func(url string) {
		rest, ok := strings.CutPrefix(url, doc.LinkPrefix)
		if !ok {
			if ui.SafeToOpen(url) {
				go mygo.Shell.OpenExternal(url)
			}
			return
		}
		kind, id, _ := strings.Cut(rest, "/")
		a.update(func() {
			p := a.panels[i]
			parent := ""
			if p != nil {
				parent = p.id
			}
			switch kind {
			case "image":
				if img := a.imageIn(p, id); img != nil {
					a.viewer = viewer{open: true, img: img, id: id, panel: i}
				}
			case "file":
				a.download(id)
			case "page":
				a.navigateID(id, "page", parent, i)
			case "db":
				a.navigateID(id, "database", parent, i)
			case "table":
				a.clickTable(p, id)
			case "toggle":
				a.set.Toggles[id] = !a.set.Toggles[id]
				a.saveSettings()
				if p != nil && p.session != nil {
					p.session.Reshape(a.shape(p))
				}
			default:
				go mygo.Shell.OpenExternal("https://www.notion.so/" + strings.ReplaceAll(id, "-", ""))
			}
		})
	}
}

// shape is how pages show: which toggles are open.
func (a *App) shape(p *panel) doc.Shape {
	open := map[string]bool{}
	for k, v := range a.set.Toggles {
		open[k] = v
	}
	// The pages' emoji, copied here: the build runs off the main thread,
	// where a.nodes changes.
	icons := map[string]string{}
	for id, n := range a.nodes {
		if n != nil && n.Emoji != "" {
			icons[id] = n.Emoji
		}
	}
	return doc.Shape{OpenToggles: open, Width: &p.textWidth, Emoji: func(e string) ui.ImageSource {
		// Builds run in the background: fetching here is fine.
		if s := emoji.Load(context.Background(), e); s != nil {
			return s
		}
		return nil
	}, PageIcon: func(id string) string { return icons[id] }}
}

func (a *App) openPage(p *panel, id string) {
	p.opening = true
	client, bot := a.client, ""
	if a.bot != nil {
		bot = a.bot.ID
	}
	shape := a.shape(p)
	go func() {
		s, text, extras, err := syncer.Open(context.Background(), client, bot, id, shape)
		a.update(func() {
			if p.id != id || !a.has(p) {
				if s != nil {
					s.Close()
				}
				return
			}
			p.opening = false
			if err != nil {
				p.openErr = errText(err)
				return
			}
			d := richtext.NewDocument(text)
			d.IdentifyParagraphs(doc.NewID)
			s.OnStatus = a.invalidate
			s.OnRemote = func(text richtext.Text, extras []doc.Extra) {
				a.update(func() {
					if p.session == s {
						d.SetText(text)
						a0, c0 := d.Selection()
						p.caretSeen, p.verSeen = [2]int{a0, c0}, d.Version()
						p.extras = extras
						a.guardReadOnly(p)
						a.registerChildren(id, extras)
						if s.Page != nil {
							if n := a.nodes[id]; n != nil {
								n.Emoji = s.Page.Emoji()
							}
						}
					}
				})
			}
			s.OnError = func(msg string) { a.update(func() { a.toast(msg) }) }
			p.session, p.doc, p.extras = s, d, extras
			a.guardReadOnly(p)
			if s.Page != nil {
				title := s.Page.Title()
				p.title, p.titleSent = title, title
				if n := a.nodes[id]; n != nil {
					n.Title = nonEmpty(title, tr.T("Tanpa judul"))
					n.Emoji = s.Page.Emoji()
				}
			}
			a.registerChildren(id, extras)
			a.loadProps(p)
			if s.Page != nil {
				a.loadCover(p, s.Page.CoverURL())
			}
			s.Start()
		})
	}()
}

// has reports whether p is a tab of either panel: a page loading into a
// tab closed meanwhile is dropped.
func (a *App) has(p *panel) bool { return a.tabIndex(0, p) >= 0 || a.tabIndex(1, p) >= 0 }

// sendTitle sends panel p's title when it changed.
func (a *App) sendTitle(p *panel) {
	if !p.titleDirty || p.session == nil || p.session.Page == nil {
		return
	}
	title := strings.TrimSpace(p.title)
	p.titleDirty = false
	if title == p.titleSent {
		return
	}
	p.titleSent = title
	if n := a.nodes[p.id]; n != nil {
		n.Title = nonEmpty(title, tr.T("Tanpa judul"))
	}
	client, id, prop := a.client, p.id, p.session.Page.TitleProperty()
	go func() {
		if err := client.SetTitle(context.Background(), id, prop, title); err != nil {
			a.update(func() { a.toast(tr.T("Judul gagal disimpan: ") + errText(err)) })
		}
	}()
}

// newPage makes a page under parent and opens it.
func (a *App) newPage(parent string) {
	if parent == "" {
		a.toast(tr.T("Pilih halaman dulu: integrasi Notion hanya bisa membuat halaman di dalam halaman lain."))
		return
	}
	if n := a.nodes[parent]; n != nil && n.Kind != "page" {
		a.toast(tr.T("Halaman baru hanya bisa dibuat di dalam halaman, bukan database."))
		return
	}
	client := a.client
	go func() {
		p, err := client.CreatePage(context.Background(), parent, "")
		a.update(func() {
			if err != nil {
				a.toast(tr.T("Gagal membuat halaman: ") + errText(err))
				return
			}
			n := &node{ID: p.ID, Title: tr.T("Tanpa judul"), Kind: "page", ParentID: parent, Edited: time.Now()}
			a.nodes[p.ID] = n
			a.regroup()
			a.set.Open[parent] = true
			a.open(n, a.focus)
			a.toast(tr.T("Halaman baru dibuat"))
		})
	}()
}

// imageIn is a picture of panel p's page by its block.
func (a *App) imageIn(p *panel, id string) ui.ImageSource {
	if p != nil && p.session != nil {
		return p.session.Image(id)
	}
	if a.imageOf != nil {
		return a.imageOf(id)
	}
	return nil
}

// PreferredLanguages are the languages the user reads, most preferred
// first, as the system says (main sets it to MyGo's).
var PreferredLanguages = func() []string { return nil }

// applyLanguage makes gotion speak the language of the settings, or the
// first of the system's it is written in.
func (a *App) applyLanguage() {
	l := a.set.Language
	if l != tr.ID && l != tr.EN {
		l = tr.Pick(PreferredLanguages())
	}
	tr.Set(l)
	if a.win != nil {
		mygo.App.SetLocale(l)
	}
}

// ApplyLanguage follows the language settings again, once the system's
// languages are known.
func (a *App) ApplyLanguage() { a.applyLanguage() }

// setLanguage changes the language now and for the next launches.
func (a *App) setLanguage(l string) {
	a.set.Language = l
	a.applyLanguage()
	a.saveSettings()
}

// languagePicker is the choice of language: the names in their own
// language, so whoever reads neither still finds theirs.
func (a *App) languagePicker(c *ui.Context) {
	cur := a.set.Language
	if cur != tr.ID && cur != tr.EN {
		cur = "auto"
	}
	before := cur
	a.segmented(c, "lang", &cur, []struct{ id, label string }{
		{"auto", tr.T("Ikut sistem")}, {tr.ID, "Bahasa Indonesia"}, {tr.EN, "English"}})
	if cur != before {
		if cur == "auto" {
			cur = ""
		}
		a.setLanguage(cur)
	}
}

// closeHides tells whether closing the window should only hide it: when the
// user asked to stay in the tray and there is a tray to come back from.
func (a *App) closeHides() bool {
	return a.set.StayInTray && a.tray != nil && !a.quitting
}
