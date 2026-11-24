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
	"gotion/internal/notion"
	"gotion/internal/syncer"
)

const secretKey = "notion-token"

// node is a page or data source in the sidebar.
type node struct {
	ID, Title, Emoji, Kind, ParentID string // Kind: "page" or "data_source"
	Edited                           time.Time
	Children                         []*node
}

// App is the state of the window.
type App struct {
	win    *mygo.Window
	client *notion.Client
	bot    *notion.User

	// onboarding
	tokenInput string
	checking   bool
	tokenErr   string

	// sidebar
	nodes     map[string]*node
	groups    []*node // roots with their pages
	databases []*node
	loading   bool
	loadErr   string
	query     string
	selected  string
	closed    map[string]bool

	// the open page
	session *syncer.Session
	doc     *richtext.Document
	extras  []doc.Extra
	opening string
	openErr string

	// the open data source
	table *tableView

	dark bool

	// panelWidth is the page panel's width last frame.
	panelWidth float32
}

// New returns the app, signed in when a token is saved.
func New() *App {
	a := &App{closed: map[string]bool{}, dark: false}
	if tok := loadToken(); tok != "" {
		a.client = notion.New(tok)
	}
	a.loadCache()
	return a
}

// Attach gives the app its window, and starts loading.
func (a *App) Attach(w *mygo.Window) {
	a.win = w
	if a.client != nil {
		a.refresh()
	}
}

// Close sends unsaved edits.
func (a *App) Close() {
	if a.session != nil {
		a.session.Close()
	}
}

func (a *App) pal() palette {
	if a.dark {
		return dark
	}
	return light
}

// View builds the window.
func (a *App) View(c *ui.Context) {
	p := a.pal()
	c.SetTheme(theme(p, a.dark))
	if a.client == nil || a.bot == nil && a.checking {
		a.onboarding(c)
		return
	}
	ui.Row(c).Fill().AlignItems(ui.Stretch).Background(p.Sidebar).Children(func() {
		a.sidebar(c)
		ui.Column(c).Grow(1).MinWidth(0).Padding(10, 10, 10, 0).Children(func() {
			ui.Column(c).Fill().Background(p.Panel).Radius(12).Clip().
				Shadow(0, 1, 3, 0, ui.RGBA(0, 0, 0, 0.08)).Children(func() {
				switch {
				case a.table != nil:
					a.tablePanel(c)
				case a.doc != nil || a.opening != "":
					a.pagePanel(c)
				default:
					a.empty(c)
				}
			})
		})
	})
}

func (a *App) empty(c *ui.Context) {
	p := a.pal()
	ui.Column(c).Fill().Center().Gap(8).Children(func() {
		ui.Icon(c, iconPage).Size(28, 28).TextColor(p.Faint)
		ui.Text(c, "Pilih halaman di sidebar").FontSize(15).TextColor(p.Muted)
		if len(a.nodes) == 0 && !a.loading {
			ui.Text(c, "Belum ada halaman yang terhubung. Di Notion: ••• → Connections → pilih integrasi kamu.").
				FontSize(13).TextColor(p.Faint).MaxWidth(420).TextAlign(ui.Center)
		}
	})
}

// --- token ---

func configDir() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "gotion")
}

func loadToken() string {
	if t := os.Getenv("NOTION_TOKEN"); t != "" {
		return cleanToken(t)
	}
	if b, err := mygo.Secrets.Service("gotion").Get(secretKey); err == nil {
		return cleanToken(string(b))
	}
	if b, err := os.ReadFile(filepath.Join(configDir(), "token")); err == nil {
		return cleanToken(string(b))
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
	if err := mygo.Secrets.Service("gotion").Set(secretKey, []byte(tok)); err == nil {
		return
	}
	// No keychain (a Linux without a Secret Service): a file only the user reads.
	os.MkdirAll(configDir(), 0o700)
	os.WriteFile(filepath.Join(configDir(), "token"), []byte(tok), 0o600)
}

func deleteToken() {
	mygo.Secrets.Service("gotion").Delete(secretKey)
	os.Remove(filepath.Join(configDir(), "token"))
	os.Remove(filepath.Join(configDir(), "pages.json"))
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
	a.checking, a.tokenErr = true, ""
	client := notion.New(tok)
	go func() {
		me, err := client.Me(context.Background())
		a.win.Update(func() {
			a.checking = false
			if err != nil {
				a.tokenErr = "Token ditolak Notion: " + errText(err)
				return
			}
			saveToken(tok)
			a.client, a.bot, a.tokenInput = client, me, ""
			a.refresh()
		})
	}()
}

func (a *App) signOut() {
	if a.session != nil {
		a.session.Close()
	}
	deleteToken()
	*a = App{win: a.win, closed: map[string]bool{}, dark: a.dark}
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
		a.win.Update(func() {
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
	defer func() {
		// Keep the pages found inside opened pages that search does not list.
		added := false
		for id, n := range old {
			if a.nodes[id] == nil && n.ParentID != "" && (old[n.ParentID] != nil || a.nodes[n.ParentID] != nil) {
				a.nodes[id] = &node{ID: n.ID, Title: n.Title, Emoji: n.Emoji, Kind: n.Kind, ParentID: n.ParentID, Edited: n.Edited}
				added = true
			}
		}
		if added {
			a.regroup()
		}
	}()
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

// --- opening ---

func (a *App) open(n *node) {
	if a.selected == n.ID && (a.doc != nil || a.table != nil || a.opening != "") {
		return
	}
	a.selected = n.ID
	if a.session != nil {
		s := a.session
		go s.Close()
		a.session = nil
	}
	a.doc, a.extras, a.table, a.openErr = nil, nil, nil, ""
	if n.Kind == "data_source" || n.Kind == "database" {
		a.openTable(n)
		return
	}
	a.openPage(n.ID)
}

// registerChildren adds a page's sub-pages and databases to the sidebar:
// Notion's search can take a while to list pages shared just now, and
// leaves out some it never indexes.
func (a *App) registerChildren(pageID string, extras []doc.Extra) {
	parent := a.nodes[pageID]
	if parent == nil {
		return
	}
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
		if n := a.nodes[x.ID]; n != nil {
			continue
		}
		title := strings.TrimSpace(x.Title)
		if title == "" || title == "-" {
			title = map[string]string{"page": "Tanpa judul", "database": "Database tanpa judul"}[kind]
		}
		n := &node{ID: x.ID, Title: title, Kind: kind, ParentID: pageID}
		a.nodes[x.ID] = n
		parent.Children = append(parent.Children, n)
	}
}

// followLink opens a link clicked in the editor.
func (a *App) followLink(url string) {
	rest, ok := strings.CutPrefix(url, doc.LinkPrefix)
	if !ok {
		if ui.SafeToOpen(url) {
			go mygo.Shell.OpenExternal(url)
		}
		return
	}
	kind, id, _ := strings.Cut(rest, "/")
	a.win.Update(func() {
		switch kind {
		case "page", "db":
			n := a.nodes[id]
			if n == nil {
				k := map[string]string{"page": "page", "db": "database"}[kind]
				n = &node{ID: id, Title: "Memuat…", Kind: k, ParentID: a.selectedParent()}
				a.nodes[id] = n
			}
			a.open(n)
		default:
			go mygo.Shell.OpenExternal("https://www.notion.so/" + strings.ReplaceAll(id, "-", ""))
		}
	})
}

func (a *App) openPage(id string) {
	a.opening = id
	client, bot := a.client, ""
	if a.bot != nil {
		bot = a.bot.ID
	}
	go func() {
		s, text, extras, err := syncer.Open(context.Background(), client, bot, id)
		a.win.Update(func() {
			if a.opening != id {
				if s != nil {
					s.Close()
				}
				return
			}
			a.opening = ""
			if err != nil {
				a.openErr = errText(err)
				return
			}
			d := richtext.NewDocument(text)
			d.IdentifyParagraphs(doc.NewID)
			s.OnStatus = func() { a.win.Invalidate() }
			s.OnRemote = func(text richtext.Text, extras []doc.Extra) {
				a.win.Update(func() {
					if a.session == s {
						d.SetText(text)
						a.extras = extras
					}
				})
			}
			a.session, a.doc, a.extras = s, d, extras
			a.registerChildren(id, extras)
			s.Start()
		})
	}()
}
