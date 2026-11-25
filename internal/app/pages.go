package app

import (
	"context"
	"gotion/internal/tr"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"gotion/internal/emoji"
)

// The app's own pages, which open in tabs as Notion's do.
const (
	todayID   = "gotion:today"
	calID     = "gotion:calendar"
	libraryID = "gotion:library"
	trashID   = "gotion:trash"
)

// special is the node of one of the app's own pages.
func (a *App) special(id string) *node {
	if n := a.nodes[id]; n != nil {
		// Its name follows the language.
		n.Title = map[string]string{todayID: tr.T("Hari ini"), calID: tr.T("Kalender"), libraryID: tr.T("Pustaka"), trashID: tr.T("Sampah")}[id]
		return n
	}
	n := map[string]*node{
		todayID:   {ID: todayID, Title: tr.T("Hari ini"), Kind: "today"},
		calID:     {ID: calID, Title: tr.T("Kalender"), Kind: "calendar"},
		libraryID: {ID: libraryID, Title: tr.T("Pustaka"), Kind: "library"},
		trashID:   {ID: trashID, Title: tr.T("Sampah"), Kind: "trash"},
	}[id]
	if n != nil {
		if a.nodes == nil {
			a.nodes = map[string]*node{} // no page known yet: no cache, not loaded
		}
		a.nodes[id] = n
	}
	return n
}

func isSpecial(kind string) bool {
	return kind == "library" || kind == "trash" || kind == "today" || kind == "calendar"
}

// trashed is a page put in Notion's trash from gotion. Notion's API lists
// no trash, so gotion keeps its own.
type trashed struct {
	ID, Title, Emoji string
	When             time.Time
	ParentID         string
}

// libraryState is the Pustaka page: how it filters and sorts.
type libraryState struct {
	kind  string // "", "page" or "db"
	order string // "edited" or "title"
}

// libraryPanel lists every page and database shared with gotion.
func (a *App) libraryPanel(c *ui.Context, i int) {
	p := a.pal()
	l := &a.library
	a.panelHeader(c, i, iconLibrary, tr.T("Pustaka"), "pustaka", nil)
	var items []*node
	for _, n := range a.nodes {
		if isSpecial(n.Kind) {
			continue
		}
		isDB := n.Kind != "page"
		if l.kind == "page" && isDB || l.kind == "db" && !isDB {
			continue
		}
		items = append(items, n)
	}
	sort.Slice(items, func(x, y int) bool {
		if l.order == "title" {
			return byTitleThenID(items[x], items[y])
		}
		return byEditedThenTitle(items[x], items[y])
	})
	ui.Column(c).Grow(1).MinHeight(0).Padding(24, 32, 0, 32).Gap(14).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(10).Wrap().Children(func() {
			ui.Text(c, tr.T("Pustaka")).FontSize(26).FontWeight(700)
			ui.Text(c, itoa(len(items))+tr.T(" item")).FontSize(13).TextColor(p.Faint)
			ui.Box(c).Grow(1)
			a.segmented(c, "lib-kind", &l.kind, []struct{ id, label string }{{"", tr.T("Semua")}, {"page", tr.T("Halaman")}, {"db", "Database"}})
			a.segmented(c, "lib-order", &l.order, []struct{ id, label string }{{"edited", tr.T("Terakhir diubah")}, {"title", tr.T("Judul")}})
			if a.btn(c, tr.T("Impor Markdown"), iconImport, false, false) {
				a.importMarkdownDialog(i)
			}
		})
		ui.Scroll(c).Grow(1).MinHeight(0).ScrollbarStyle(a.thinScrollbar()).Children(func() {
			// Room on the right: the scroll bar runs beside the rows, not on them.
			ui.Column(c).Gap(1).Padding(0, 14, 24, 0).Children(func() {
				if len(items) == 0 {
					ui.Text(c, tr.T("Tidak ada yang cocok.")).FontSize(14).TextColor(p.Faint).Padding(12)
				}
				for _, n := range items {
					row := ui.Row(c.Key("lib"+n.ID)).AlignItems(ui.Center).Gap(10).Padding(9, 10).Radius(8).Cursor(ui.CursorPointer)
					if row.Hovered() {
						row.Background(p.Hover)
					}
					row.Children(func() {
						switch {
						case n.Emoji != "":
							emoji.View(c, n.Emoji, 16)
						case n.Kind != "page":
							ui.Icon(c, iconTable).Size(16, 16).TextColor(p.Faint)
						default:
							ui.Icon(c, iconPage).Size(16, 16).TextColor(p.Faint)
						}
						ui.Text(c, n.Title).FontSize(14).FontWeight(500).SingleLine().MinWidth(0).Shrink(1)
						var crumb []string
						for _, up := range a.path(n.ID) {
							crumb = append(crumb, up.Title)
						}
						ui.Text(c, strings.Join(crumb, " / ")).FontSize(12).TextColor(p.Faint).SingleLine().MinWidth(0).Grow(1)
						ui.Text(c, ago(n.Edited)).FontSize(12).TextColor(p.Faint).SingleLine().Shrink(0)
					})
					if row.Clicked() {
						a.open(n, i)
					}
				}
			})
		})
	})
}

// segmented is a row of choices, one picked.
func (a *App) segmented(c *ui.Context, key string, value *string, opts []struct{ id, label string }) {
	p := a.pal()
	ui.Row(c.Key(key)).Gap(2).Padding(2).Radius(8).Background(p.Sidebar).Children(func() {
		for k, o := range opts {
			// Nothing chosen yet is the first choice.
			on := *value == o.id || *value == "" && k == 0
			b := ui.Text(c.Key(o.id+o.label), o.label).FontSize(13).Padding(4, 10).Radius(6).Cursor(ui.CursorPointer)
			if on {
				b.Background(p.Panel).FontWeight(600).Shadow(0, 1, 2, 0, ui.RGBA(0, 0, 0, 0.08))
			} else {
				b.TextColor(p.Muted)
			}
			if b.Clicked() {
				*value = o.id
			}
		}
	})
}

func itoa(n int) string { return strconv.Itoa(n) }

// trashPanel lists the pages put in the trash from gotion, to restore.
func (a *App) trashPanel(c *ui.Context, i int) {
	p := a.pal()
	a.panelHeader(c, i, iconTrash, tr.T("Sampah"), "sampah", nil)
	ui.Column(c).Grow(1).MinHeight(0).Padding(24, 32, 0, 32).Gap(10).Children(func() {
		ui.Text(c, tr.T("Sampah")).FontSize(26).FontWeight(700)
		ui.Text(c, tr.T("Halaman yang kamu buang dari gotion. Notion menyimpannya 30 hari; sampah dari Notion web tidak terlihat di sini, karena API Notion tidak menampilkannya.")).
			FontSize(13).TextColor(p.Muted).MaxWidth(640)
		ui.Scroll(c).Grow(1).MinHeight(0).Children(func() {
			ui.Column(c).Gap(2).Padding(8, 0, 24, 0).Children(func() {
				if len(a.set.Trashed) == 0 {
					ui.Text(c, tr.T("Sampah kosong.")).FontSize(14).TextColor(p.Faint).Padding(12, 0)
				}
				for k := len(a.set.Trashed) - 1; k >= 0; k-- {
					t := a.set.Trashed[k]
					ui.Row(c.Key("tr"+t.ID)).AlignItems(ui.Center).Gap(10).Padding(8, 10).Radius(8).Children(func() {
						if t.Emoji != "" {
							emoji.View(c, t.Emoji, 16)
						} else {
							ui.Icon(c, iconPage).Size(16, 16).TextColor(p.Faint)
						}
						ui.Text(c, nonEmpty(t.Title, tr.T("Tanpa judul"))).FontSize(14).Grow(1).SingleLine().MinWidth(0)
						ui.Text(c, tr.T("Dibuang ")+ago(t.When)).FontSize(12).TextColor(p.Faint)
						if a.btn(c, tr.T("Pulihkan"), iconRestore, false, false) {
							a.restorePage(t)
						}
					})
				}
			})
		})
	})
}

// trashPage puts a page in Notion's trash, closing its tabs.
func (a *App) trashPage(id string) {
	n := a.nodes[id]
	if n == nil {
		return
	}
	client := a.client
	go func() {
		err := client.TrashPage(context.Background(), id, true)
		a.update(func() {
			if err != nil {
				a.toast(tr.T("Gagal membuang: ") + errText(err))
				return
			}
			for i := range 2 {
				for k := len(a.tabs[i]) - 1; k >= 0; k-- {
					if a.tabs[i][k].id == id {
						a.closeTab(i, k)
					}
				}
			}
			a.set.Trashed = append(a.set.Trashed, trashed{ID: id, Title: n.Title, Emoji: n.Emoji, When: time.Now(), ParentID: n.ParentID})
			delete(a.nodes, id)
			a.regroup()
			a.saveSettings()
			a.toast("“" + n.Title + tr.T("” dibuang ke sampah"))
		})
	}()
}

func (a *App) restorePage(t trashed) {
	client := a.client
	go func() {
		err := client.TrashPage(context.Background(), t.ID, false)
		a.update(func() {
			if err != nil {
				a.toast(tr.T("Gagal memulihkan: ") + errText(err))
				return
			}
			for k, x := range a.set.Trashed {
				if x.ID == t.ID {
					a.set.Trashed = append(a.set.Trashed[:k], a.set.Trashed[k+1:]...)
					break
				}
			}
			a.nodes[t.ID] = &node{ID: t.ID, Title: t.Title, Emoji: t.Emoji, Kind: "page", ParentID: t.ParentID, Edited: time.Now()}
			a.regroup()
			a.saveSettings()
			a.toast("“" + t.Title + tr.T("” dipulihkan"))
		})
	}()
}

// pageMenu is what the ••• of a page offers.
type pageMenu struct {
	open    bool
	icons   bool // the emoji picker
	moving  bool // picking where to move the page
	query   string
	confirm bool // asking before trashing
}

var commonEmoji = strings.Fields("📄 📝 📚 📌 ✅ 🚀 💡 🎯 📊 📅 🗂️ 🔖 ⭐ 🔥 🌱 🧭 🛠️ 🧪 🎨 🎵 📷 🏝️ 🍜 💼 🏠 ❤️ 😀 🤖 🐱 🌙")

// pageActions is the ••• button of page panel i and its menu.
func (a *App) pageActions(c *ui.Context, i int, pn *panel) {
	p := a.pal()
	m := &pn.menu
	b := ui.Box(c.Key("page-menu")).Padding(5).Radius(6).Cursor(ui.CursorPointer).Label(tr.T("Aksi halaman")).Tooltip(tr.T("Aksi halaman"))
	if b.Hovered() || m.open {
		b.Background(p.Hover)
	}
	b.Children(func() { ui.Icon(c, iconMore).Size(16, 16).TextColor(p.Muted) })
	if b.Clicked() {
		*m = pageMenu{open: !m.open}
	}
	if !m.open {
		return
	}
	ui.Popover(c, b, &m.open, func() {
		ui.Column(c).Width(260).Padding(4).Gap(1).Children(func() {
			switch {
			case m.icons:
				a.iconPicker(c, pn)
			case m.moving:
				a.movePicker(c, pn)
			default:
				a.pageMenuItems(c, i, pn)
			}
		})
	})
}

func (a *App) menuItem(c *ui.Context, key, label string, danger bool) bool {
	p := a.pal()
	r := ui.Text(c.Key(key), label).FontSize(13).Padding(7, 10).Radius(6).Cursor(ui.CursorPointer)
	if danger {
		r.TextColor(c.Theme().Danger)
	}
	if r.Hovered() {
		r.Background(p.Hover)
	}
	return r.Clicked()
}

func (a *App) pageMenuItems(c *ui.Context, i int, pn *panel) {
	p := a.pal()
	m := &pn.menu
	url := "https://www.notion.so/" + strings.ReplaceAll(pn.id, "-", "")
	if a.menuItem(c, "icon", tr.T("Ubah ikon…"), false) {
		m.icons = true
	}
	if pn.cover.url == "" && a.menuItem(c, "cover", tr.T("Tambah cover"), false) {
		m.open = false
		pn.cover.editing, pn.cover.input = true, ""
	}
	if a.menuItem(c, "move", tr.T("Pindahkan ke…"), false) {
		m.moving = true
	}
	if a.menuItem(c, "link", tr.T("Salin tautan"), false) {
		m.open = false
		mygo.Clipboard.WriteText(url)
		a.toast(tr.T("Tautan disalin"))
	}
	if a.menuItem(c, "export", tr.T("Ekspor sebagai Markdown"), false) {
		m.open = false
		a.exportMarkdown(pn.id)
	}
	if a.menuItem(c, "web", tr.T("Buka di Notion"), false) {
		m.open = false
		go mygo.Shell.OpenExternal(url)
	}
	ui.Box(c).Height(1).Background(p.Line).Margin(4, 6)
	if !m.confirm {
		if a.menuItem(c, "trash", tr.T("Buang ke sampah"), true) {
			m.confirm = true
		}
		return
	}
	ui.Text(c, tr.T("Buang halaman ini beserta sub-halamannya?")).FontSize(12).TextColor(p.Muted).Padding(4, 10)
	ui.Row(c).Gap(6).Padding(2, 8, 6, 8).Children(func() {
		if ui.PrimaryButton(c, tr.T("Buang")).Clicked() {
			m.open = false
			a.trashPage(pn.id)
		}
		if a.btn(c, tr.T("Batal"), nil, false, false) {
			m.confirm = false
		}
	})
}

// iconPicker sets a page's emoji.
func (a *App) iconPicker(c *ui.Context, pn *panel) {
	p := a.pal()
	m := &pn.menu
	ui.Text(c, tr.T("Ikon")).FontSize(11).FontWeight(600).TextColor(p.Faint).Padding(4, 8)
	ui.Column(c).Grid().Columns(6).Gap(2).Padding(0, 4).Children(func() {
		for _, e := range commonEmoji {
			b := ui.Box(c.Key("e"+e)).Size(36, 36).Radius(7).Center().Cursor(ui.CursorPointer)
			if b.Hovered() {
				b.Background(p.Hover)
			}
			b.Children(func() { emoji.View(c, e, 22) })
			if b.Clicked() {
				m.open = false
				a.setIcon(pn.id, e)
			}
		}
	})
	if a.menuItem(c, "noicon", tr.T("Hapus ikon"), false) {
		m.open = false
		a.setIcon(pn.id, "")
	}
}

func (a *App) setIcon(id, e string) {
	n := a.nodes[id]
	old := ""
	if n != nil {
		old, n.Emoji = n.Emoji, e
	}
	client := a.client
	go func() {
		err := client.SetIcon(context.Background(), id, e)
		a.update(func() {
			if err != nil {
				if n != nil {
					n.Emoji = old
				}
				a.toast(tr.T("Gagal mengubah ikon: ") + errText(err))
			}
		})
	}()
}

// movePicker picks the page to move a page under.
func (a *App) movePicker(c *ui.Context, pn *panel) {
	p := a.pal()
	m := &pn.menu
	ui.TextInput(c, &m.query).Placeholder(tr.T("Pindahkan ke halaman…")).AutoFocus().Label(tr.T("Tujuan")).FontSize(13)
	q := strings.ToLower(m.query)
	var targets []*node
	for _, n := range a.nodes {
		if n.Kind != "page" || n.ID == pn.id || a.inside(n.ID, pn.id) {
			continue
		}
		if q == "" || strings.Contains(strings.ToLower(n.Title), q) {
			targets = append(targets, n)
		}
	}
	sort.Slice(targets, func(x, y int) bool { return byEditedThenTitle(targets[x], targets[y]) })
	if len(targets) > 8 {
		targets = targets[:8]
	}
	for _, n := range targets {
		if a.menuItem(c, "to"+n.ID, n.Title, false) {
			m.open = false
			a.movePage(pn.id, n.ID)
		}
	}
	if len(targets) == 0 {
		ui.Text(c, tr.T("Tidak ada halaman yang cocok")).FontSize(12).TextColor(p.Faint).Padding(6, 10)
	}
}

// inside reports whether page id is under page root.
func (a *App) inside(id, root string) bool {
	for _, up := range a.path(id) {
		if up.ID == root {
			return true
		}
	}
	return false
}

func (a *App) movePage(id, to string) {
	client := a.client
	go func() {
		err := client.MovePage(context.Background(), id, to)
		a.update(func() {
			if err != nil {
				a.toast(tr.T("Gagal memindahkan: ") + errText(err))
				return
			}
			if n := a.nodes[id]; n != nil {
				n.ParentID = to
				a.regroup()
				a.set.Open[to] = true
			}
			a.toast(tr.T("Dipindahkan ke “") + a.nodes[to].Title + "”")
		})
	}()
}

// exportMarkdown saves a page as Markdown in Downloads and shows it.
func (a *App) exportMarkdown(id string) {
	title := "halaman"
	if n := a.nodes[id]; n != nil {
		title = n.Title
	}
	client := a.client
	go func() {
		md, err := client.Markdown(context.Background(), id)
		var path string
		if err == nil {
			dir, derr := mygo.App.Path(mygo.PathDownloads)
			if derr != nil || dir == "" {
				dir, _ = os.UserHomeDir()
			}
			path = freeName(dir, safeName(title)+".md")
			err = os.WriteFile(path, []byte("# "+title+"\n\n"+md), 0o644)
		}
		a.update(func() {
			if err != nil {
				a.toast(tr.T("Gagal mengekspor: ") + errText(err))
				return
			}
			a.toast(tr.T("Diekspor ke ") + path)
			mygo.Shell.ShowItemInFolder(path)
		})
	}()
}

// importMarkdown makes a page under parent from each Markdown file.
func (a *App) importMarkdown(parent string, paths []string) {
	if parent == "" || a.nodes[parent] == nil || a.nodes[parent].Kind != "page" {
		a.toast(tr.T("Buka halaman tujuan dulu: Markdown diimpor sebagai sub-halamannya."))
		return
	}
	client := a.client
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			a.toast(tr.T("Tidak bisa membaca ") + filepath.Base(path))
			continue
		}
		title := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		body := string(data)
		// A first "# Title" line names the page, not its first block.
		if first, rest, ok := strings.Cut(body, "\n"); ok && strings.HasPrefix(first, "# ") {
			title, body = strings.TrimSpace(first[2:]), strings.TrimLeft(rest, "\n")
		}
		go func() {
			pg, err := client.CreatePageMarkdown(context.Background(), parent, title, body)
			a.update(func() {
				if err != nil {
					a.toast(tr.T("Gagal mengimpor ") + title + ": " + errText(err))
					return
				}
				a.nodes[pg.ID] = &node{ID: pg.ID, Title: title, Kind: "page", ParentID: parent, Edited: time.Now()}
				a.regroup()
				a.set.Open[parent] = true
				a.toast("“" + title + tr.T("” diimpor"))
				// The parent shows its new sub-page.
				for _, pn := range a.panels {
					if pn != nil && pn.id == parent && pn.session != nil {
						pn.session.Reshape(a.shape(pn))
					}
				}
			})
		}()
	}
}

// importMarkdownDialog asks for Markdown files to import under the page
// shown in the other tabs, or the last page.
func (a *App) importMarkdownDialog(i int) {
	parent := ""
	for _, t := range a.tabs[i] {
		if n := a.nodes[t.id]; n != nil && n.Kind == "page" {
			parent = t.id
		}
	}
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: a.win, Title: tr.T("Impor Markdown"), Multiple: true,
			Filters: []mygo.FileFilter{{Name: "Markdown", Extensions: []string{"md", "markdown"}}}})
		if err != nil || len(paths) == 0 {
			return
		}
		a.update(func() { a.importMarkdown(parent, paths) })
	}()
}

// newDatabase makes a database in panel pn's page (Notion puts it at the
// page's end) and shows it in the page and the sidebar.
func (a *App) newDatabase(pn *panel) {
	client, parent := a.client, pn.id
	go func() {
		db, err := client.CreateDatabase(context.Background(), parent, tr.T("Database baru"))
		a.update(func() {
			if err != nil {
				a.toast(tr.T("Gagal membuat database: ") + errText(err))
				return
			}
			a.nodes[db.ID] = &node{ID: db.ID, Title: tr.T("Database baru"), Kind: "database", ParentID: parent, Edited: time.Now()}
			a.regroup()
			a.set.Open[parent] = true
			if pn.session != nil {
				pn.session.Reshape(a.shape(pn))
			}
			a.toast(tr.T("Database dibuat di akhir halaman"))
		})
	}()
}
