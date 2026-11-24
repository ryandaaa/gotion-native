// Package app is gotion's window: the page tree, the editor and the
// database table, drawn with MyGo's native UI.
package app

import (
	"context"
	"encoding/json"
	"errors"
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
	width float32
	// textWidth is the width of the page's text, which tables draw across.
	textWidth float32
	// readOnly holds what each read-only paragraph holds (guardReadOnly).
	readOnly map[string]string
	// restore is the paragraph to scroll to once the editor shows, -1 none.
	restore int
	warned  bool
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

	// sidebar
	nodes     map[string]*node
	groups    []*node
	databases []*node
	loading   bool
	loadErr   string
	query     string

	// panels: the first always, the second when split
	panels [2]*panel
	focus  int

	// history of the first panel
	history []string
	hpos    int

	switcher switcherState
	// oauth is a sign-in through Notion's page in progress.
	oauth *oauthState
	// viewer shows a picture full size.
	viewer viewer
	// imageOf finds a picture by its block where no session holds it: tests.
	imageOf func(id string) ui.ImageSource
	// themeOpen shows the theme picker.
	themeOpen bool
	sideList  ui.ListState
	toasts    []string
	dark      bool

	// widths are the last widths of the panels, for pages opened into them.
	widths [2]float32
}

// New returns the app, signed in when a token is saved.
func New() *App {
	a := &App{panels: [2]*panel{{}, nil}}
	a.set = loadSettings()
	if raw := loadToken(); raw != "" {
		a.client = a.clientFor(parseCreds(raw))
	}
	a.loadCache()
	return a
}

// Attach gives the app its window, and starts loading.
func (a *App) Attach(w *mygo.Window) {
	a.win = w
	a.applyGlass()
	if a.client == nil {
		return
	}
	a.refresh()
	if n := a.nodes[a.set.Last]; n != nil {
		a.open(n, 0)
	}
	if a.set.Second != "" {
		if n := a.nodes[a.set.Second]; n != nil {
			a.panels[1] = &panel{}
			a.open(n, 1)
		}
	}
}

// Close sends unsaved edits.
func (a *App) Close() {
	for _, p := range a.panels {
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
	Theme        string          `json:"theme"` // "system", "light" or "dark"
	SidebarWidth float32         `json:"sidebarWidth"`
	SidebarOff   bool            `json:"sidebarOff"`
	Open         map[string]bool `json:"open"`   // expanded pages
	Closed       map[string]bool `json:"closed"` // collapsed groups
	Last         string          `json:"last"`
	Second       string          `json:"second"`
	SplitSize    float32         `json:"splitSize"`
	FullWidth    bool            `json:"fullWidth"`
	Toggles      map[string]bool `json:"toggles"` // toggle blocks shown open
	Zoom         float32         `json:"zoom"`    // 1 is 100%
	Wash         string          `json:"wash"`    // the color behind the cards
	Glass        bool            `json:"glass"`   // the system's glass behind them
	// Scroll is the first paragraph in view of each page, to come back to.
	Scroll map[string]int `json:"scroll"`
	// Views is how each database shows: "table" or "cards".
	Views map[string]string `json:"views"`
}

func loadSettings() settings {
	s := settings{Theme: "system", SidebarWidth: 270}
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
	if s.Scroll == nil {
		s.Scroll = map[string]int{}
	}
	if s.Views == nil {
		s.Views = map[string]string{}
	}
	if s.SidebarWidth < 200 {
		s.SidebarWidth = 270
	}
	if s.Zoom < zooms[0] || s.Zoom > zooms[len(zooms)-1] {
		s.Zoom = 1
	}
	return s
}

func (a *App) saveSettings() {
	if b, err := json.MarshalIndent(a.set, "", "  "); err == nil {
		os.MkdirAll(configDir(), 0o700)
		os.WriteFile(filepath.Join(configDir(), "settings.json"), b, 0o600)
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
		a.tokenErr = "Tempel token dulu."
		return
	}
	if !strings.HasPrefix(tok, "ntn_") && !strings.HasPrefix(tok, "secret_") {
		a.tokenErr = "Token Notion biasanya diawali ntn_ (atau secret_ untuk yang lama). Cek lagi yang kamu salin."
		return
	}
	a.signIn(creds{Access: tok})
}

func (a *App) signOut() {
	for _, p := range a.panels {
		if p != nil && p.session != nil {
			p.session.Close()
		}
	}
	deleteToken()
	set := a.set
	set.Last, set.Second = "", ""
	*a = App{win: a.win, set: set, panels: [2]*panel{{}, nil}, dark: a.dark}
	a.saveSettings()
}

func errText(err error) string {
	var e *notion.Error
	if errors.As(err, &e) {
		if e.Message != "" {
			return e.Message
		}
		return e.Code
	}
	return err.Error()
}

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
			a.setPages(pages)
			a.saveCache(pages)
		})
	}()
}

func (a *App) setPages(pages []*notion.Page) {
	old := a.nodes
	a.nodes = map[string]*node{}
	for _, p := range pages {
		if p.InTrash {
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
			title = "Tanpa judul"
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
		sort.Slice(ns, func(i, j int) bool { return strings.ToLower(ns[i].Title) < strings.ToLower(ns[j].Title) })
	}
	byTitle(a.groups)
	byTitle(a.databases)
	for _, n := range a.nodes {
		byTitle(n.Children)
	}
}

func (a *App) saveCache(pages []*notion.Page) {
	if b, err := json.Marshal(pages); err == nil {
		os.MkdirAll(configDir(), 0o700)
		os.WriteFile(filepath.Join(configDir(), "pages.json"), b, 0o600)
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
			title = map[string]string{"page": "Tanpa judul", "database": "Database tanpa judul"}[kind]
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
		n = &node{ID: id, Title: "Memuat…", Kind: kind, ParentID: parent}
		a.nodes[id] = n
		a.regroup()
	}
	a.open(n, i)
}

// open shows n in panel i.
func (a *App) open(n *node, i int) {
	if a.panels[i] == nil {
		a.panels[i] = &panel{}
	}
	p := a.panels[i]
	a.focus = i
	if p.id == n.ID && (p.doc != nil || p.table != nil || p.opening) {
		return
	}
	if i == 0 {
		// Going somewhere new drops the pages ahead.
		if len(a.history) > 0 {
			a.history = a.history[:a.hpos+1]
		}
		if len(a.history) == 0 || a.history[len(a.history)-1] != n.ID {
			a.history = append(a.history, n.ID)
		}
		a.hpos = len(a.history) - 1
		a.set.Last = n.ID
	} else {
		a.set.Second = n.ID
	}
	// Expand the tree down to the page.
	for _, up := range a.path(n.ID) {
		a.set.Open[up.ID] = true
	}
	a.saveSettings()
	a.show(n, i)
}

// show loads n into panel i, closing what it showed.
func (a *App) show(n *node, i int) {
	p := a.panels[i]
	if p.session != nil {
		s := p.session
		go s.Close()
	}
	p = &panel{id: n.ID}
	a.panels[i] = p
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
			a.show(n, 0)
			a.set.Last = n.ID
		}
	}
}

func (a *App) forward() {
	if a.hpos < len(a.history)-1 {
		a.hpos++
		if n := a.nodes[a.history[a.hpos]]; n != nil {
			a.show(n, 0)
			a.set.Last = n.ID
		}
	}
}

// openBeside opens n in the second panel.
func (a *App) openBeside(n *node) {
	if a.panels[1] == nil {
		a.panels[1] = &panel{}
		a.guessSplit()
	}
	a.open(n, 1)
}

// guessSplit sets the widths a new split leaves, so its first frame is
// close to right.
func (a *App) guessSplit() {
	total := a.widths[0]
	if total <= 0 {
		return
	}
	first := a.set.SplitSize
	if first < 300 || first > total-300 {
		first = total / 2
	}
	a.widths[0], a.widths[1] = first, total-first
	if a.panels[0] != nil {
		a.panels[0].width = first
	}
}

func (a *App) closeSecond() {
	if p := a.panels[1]; p != nil {
		if p.session != nil {
			go p.session.Close()
		}
		a.panels[1] = nil
		a.focus = 0
		a.set.Second = ""
		a.widths[0] += a.widths[1]
		if a.panels[0] != nil {
			a.panels[0].width = a.widths[0]
		}
		a.saveSettings()
	}
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
					a.viewer = viewer{open: true, img: img}
				}
			case "file":
				a.download(id)
			case "page":
				a.openID(id, "page", parent, i)
			case "db":
				a.openID(id, "database", parent, i)
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
	return doc.Shape{OpenToggles: open, Width: &p.textWidth, Emoji: func(e string) ui.ImageSource {
		// Builds run in the background: fetching here is fine.
		if s := emoji.Load(context.Background(), e); s != nil {
			return s
		}
		return nil
	}}
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
			s.OnStatus = func() { a.win.Invalidate() }
			s.OnRemote = func(text richtext.Text, extras []doc.Extra) {
				a.update(func() {
					if p.session == s {
						d.SetText(text)
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
			p.restore = -1
			if i, ok := a.set.Scroll[id]; ok {
				p.restore = i
			}
			if s.Page != nil {
				title := s.Page.Title()
				p.title, p.titleSent = title, title
				if n := a.nodes[id]; n != nil {
					n.Title = nonEmpty(title, "Tanpa judul")
					n.Emoji = s.Page.Emoji()
				}
			}
			a.registerChildren(id, extras)
			s.Start()
		})
	}()
}

func (a *App) has(p *panel) bool { return a.panels[0] == p || a.panels[1] == p }

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
		n.Title = nonEmpty(title, "Tanpa judul")
	}
	client, id, prop := a.client, p.id, p.session.Page.TitleProperty()
	go func() {
		if err := client.SetTitle(context.Background(), id, prop, title); err != nil {
			a.update(func() { a.toast("Judul gagal disimpan: " + errText(err)) })
		}
	}()
}

// newPage makes a page under parent and opens it.
func (a *App) newPage(parent string) {
	if parent == "" {
		a.toast("Pilih halaman dulu: integrasi Notion hanya bisa membuat halaman di dalam halaman lain.")
		return
	}
	if n := a.nodes[parent]; n != nil && n.Kind != "page" {
		a.toast("Halaman baru hanya bisa dibuat di dalam halaman, bukan database.")
		return
	}
	client := a.client
	go func() {
		p, err := client.CreatePage(context.Background(), parent, "")
		a.update(func() {
			if err != nil {
				a.toast("Gagal membuat halaman: " + errText(err))
				return
			}
			n := &node{ID: p.ID, Title: "Tanpa judul", Kind: "page", ParentID: parent, Edited: time.Now()}
			a.nodes[p.ID] = n
			a.regroup()
			a.set.Open[parent] = true
			a.open(n, a.focus)
			a.toast("Halaman baru dibuat")
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
