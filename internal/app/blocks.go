package app

import (
	"gotion/internal/tr"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/richtext"

	"gotion/internal/doc"
)

// handleState is the grip beside the paragraph under the pointer: a click
// opens the block's menu, a drag moves the block.
type handleState struct {
	hover    int // the paragraph under the pointer, -1 none
	menu     bool
	menuAt   int // the paragraph the menu is for
	dragging bool
	from     int
	dropAt   int     // the paragraph the block goes before; len for the end
	dropY    float32 // where the drop line shows
	pulled   float32 // how far the grip has been pulled since pressed
}

// block is a block's paragraphs in the editor: its first and the lines
// that follow it (cont:), from index first to last.
func blockRange(paras []richtext.Paragraph, i int) (first, last int) {
	first, last = i, i
	// A later line of a block belongs to the block it continues.
	for first > 0 && strings.HasPrefix(paras[first].Style.ID, "cont:") {
		first--
	}
	id := paras[first].Style.ID
	for last+1 < len(paras) && strings.HasPrefix(paras[last+1].Style.ID, "cont:"+id+":") {
		last++
	}
	return first, last
}

func paragraphsOf(d *richtext.Document) []richtext.Paragraph {
	var out []richtext.Paragraph
	for _, p := range d.Text().Paragraphs() {
		out = append(out, p)
	}
	return out
}

// renamed gives paragraphs new IDs: to Notion, new blocks.
func renamed(ps []richtext.Paragraph) []richtext.Paragraph {
	out := make([]richtext.Paragraph, len(ps))
	for i, p := range ps {
		p.Style.ID = doc.NewID()
		out[i] = p
	}
	return out
}

// moveBlock moves the block at paragraph i before paragraph to (len for
// the end). Notion moves no blocks: under a new ID, the block is deleted
// where it was and made where it goes.
func moveBlock(d *richtext.Document, i, to int) bool {
	paras := paragraphsOf(d)
	first, last := blockRange(paras, i)
	if to >= first && to <= last+1 {
		return false // where it is
	}
	moved := renamed(paras[first : last+1])
	var out []richtext.Paragraph
	for j := 0; j <= len(paras); j++ {
		if j == to {
			out = append(out, moved...)
		}
		if j < len(paras) && (j < first || j > last) {
			out = append(out, paras[j])
		}
	}
	d.SetText(richtext.TextOf(out...))
	return true
}

// duplicateBlock puts a copy of the block at paragraph i after it.
func duplicateBlock(d *richtext.Document, i int) {
	paras := paragraphsOf(d)
	first, last := blockRange(paras, i)
	out := append([]richtext.Paragraph{}, paras[:last+1]...)
	out = append(out, renamed(paras[first:last+1])...)
	out = append(out, paras[last+1:]...)
	d.SetText(richtext.TextOf(out...))
}

// deleteBlock takes the block at paragraph i out.
func deleteBlock(d *richtext.Document, i int) {
	paras := paragraphsOf(d)
	first, last := blockRange(paras, i)
	out := append(append([]richtext.Paragraph{}, paras[:first]...), paras[last+1:]...)
	if len(out) == 0 {
		out = []richtext.Paragraph{{Style: richtext.ParagraphStyle{ID: doc.NewID()}}}
	}
	d.SetText(richtext.TextOf(out...))
}

// restyleBlock gives the block at paragraph i a style: the kind of block
// it is, keeping its ID, its indent and, for tasks, its check.
func restyleBlock(d *richtext.Document, i int, style richtext.ParagraphStyle) {
	paras := paragraphsOf(d)
	first, last := blockRange(paras, i)
	for j := first; j <= last; j++ {
		s := style
		s.ID, s.Indent = paras[j].Style.ID, paras[j].Style.Indent
		if s.List == richtext.Tasks {
			s.Checked = paras[j].Style.Checked
		}
		paras[j].Style = s
	}
	d.SetText(richtext.TextOf(paras...))
}

// blockKinds are what the menu turns a block into.
var blockKinds = []struct {
	label string
	style richtext.ParagraphStyle
}{
	{tr.N("Teks"), richtext.ParagraphStyle{}},
	{"H1", richtext.ParagraphStyle{Heading: 1}},
	{"H2", richtext.ParagraphStyle{Heading: 2}},
	{"H3", richtext.ParagraphStyle{Heading: 3}},
	{"Bullet", richtext.ParagraphStyle{List: richtext.Bullets}},
	{tr.N("Nomor"), richtext.ParagraphStyle{List: richtext.Numbers}},
	{"To-do", richtext.ParagraphStyle{List: richtext.Tasks}},
	{tr.N("Kutipan"), richtext.ParagraphStyle{Quote: true}},
	{tr.N("Kode"), richtext.ParagraphStyle{CodeBlock: true}},
}

var iconGrip = icon(`<circle cx="9" cy="6" r="1.4" fill="currentColor" stroke="none"/><circle cx="15" cy="6" r="1.4" fill="currentColor" stroke="none"/><circle cx="9" cy="12" r="1.4" fill="currentColor" stroke="none"/><circle cx="15" cy="12" r="1.4" fill="currentColor" stroke="none"/><circle cx="9" cy="18" r="1.4" fill="currentColor" stroke="none"/><circle cx="15" cy="18" r="1.4" fill="currentColor" stroke="none"/>`)

// handles draws the grip beside the paragraph under the pointer, in the
// editor's left margin, its menu and, while dragging, where the block
// goes. box is the element the editor fills.
func (a *App) handles(c *ui.Context, pn *panel, box ui.Element, pad float32) {
	p := a.pal()
	h := &pn.handle
	vis := pn.doc.VisibleParagraphs()
	if len(vis) == 0 {
		return
	}
	_, y, over := box.PointerPosition()
	at := func(y float32) int {
		for k, b := range vis {
			bottom := b.Top + b.Height
			if k+1 < len(vis) {
				bottom = vis[k+1].Top
			}
			if y < bottom {
				return k
			}
		}
		return len(vis) - 1
	}
	if over && !h.dragging && !h.menu {
		h.hover = vis[at(y)].Index
	}
	if !over && !h.dragging && !h.menu {
		h.hover = -1
	}
	if h.dragging {
		// The drop goes between paragraphs, before the nearest boundary.
		k := at(y)
		b := vis[k]
		h.dropAt, h.dropY = b.Index, b.Top-4
		if y > b.Top+b.Height/2 {
			h.dropAt = b.Index + 1
			h.dropY = b.Top + b.Height + 2
		}
		ui.Box(c.Key("drop-line")).Absolute().Left(pad - 6).Right(pad - 6).Top(h.dropY).Height(2).Radius(1).
			Background(p.Accent).ZIndex(10)
	}
	idx := h.hover
	if h.menu {
		idx = h.menuAt
	}
	if h.dragging {
		idx = h.from
	}
	var box0 *richtext.ParagraphBox
	for k := range vis {
		if vis[k].Index == idx {
			box0 = &vis[k]
		}
	}
	if box0 == nil {
		return
	}
	// A + beside the block adds an empty line under it, the caret there.
	add := ui.Box(c.Key("add-below")).Absolute().Left(max(4, pad-30)).Top(box0.Top+min(box0.Height, 26)/2-11).
		Size(22, 22).Radius(5).Center().Cursor(ui.CursorPointer).ZIndex(11).Label(tr.T("Baris baru di bawah")).Tooltip(tr.T("Baris baru di bawahnya"))
	if add.Hovered() {
		add.Background(p.Hover)
	}
	add.Children(func() { ui.Icon(c, iconPlus).Size(16, 16).TextColor(p.Faint) })
	if add.Clicked() {
		addBelow(pn.doc, idx)
		pn.focusEditor = true
		a.edited(pn)
	}
}

// addBelow puts an empty paragraph after the block at paragraph i, and
// the caret in it.
func addBelow(d *richtext.Document, i int) {
	paras := paragraphsOf(d)
	_, last := blockRange(paras, i)
	out := append([]richtext.Paragraph{}, paras[:last+1]...)
	out = append(out, richtext.Paragraph{Style: richtext.ParagraphStyle{ID: doc.NewID(), Indent: paras[last].Style.Indent}})
	out = append(out, paras[last+1:]...)
	d.SetText(richtext.TextOf(out...))
	at := 0
	for j, p := range paragraphsOf(d) {
		if j == last+1 {
			break
		}
		at += len([]rune(runsText(p))) + 1
	}
	d.SetSelection(at, at)
}

func runsText(p richtext.Paragraph) string {
	var b strings.Builder
	for _, r := range p.Runs {
		b.WriteString(r.Text)
	}
	return b.String()
}

func (a *App) idAt(pn *panel, i int) string {
	paras := paragraphsOf(pn.doc)
	if i < 0 || i >= len(paras) {
		return ""
	}
	first, _ := blockRange(paras, i)
	return paras[first].Style.ID
}

func (a *App) movable(pn *panel, i int) bool {
	id := a.idAt(pn, i)
	return id != "" && (pn.session == nil || pn.session.Movable(id))
}

func (a *App) edited(pn *panel) {
	if pn.session != nil {
		pn.session.Edited(pn.doc.Text())
	}
	a.guardReadOnly(pn)
}

func (a *App) dropBlock(pn *panel, from, to int) {
	if !a.movable(pn, from) {
		a.toast(tr.T("Block ini tidak bisa dipindah dari gotion: pindahkan di Notion supaya isinya tidak hilang."))
		return
	}
	if moveBlock(pn.doc, from, to) {
		a.edited(pn)
	}
}

// blockMenu is a block's menu: what to turn it into, and moving,
// duplicating or deleting it.
func (a *App) blockMenu(c *ui.Context, pn *panel, i int) {
	p := a.pal()
	h := &pn.handle
	movable := a.movable(pn, i)
	item := func(key, label string, danger bool, run func()) {
		r := ui.Text(c.Key(key), label).FontSize(13).Padding(6, 10).Radius(6).Cursor(ui.CursorPointer)
		if danger {
			r.TextColor(c.Theme().Danger)
		}
		if r.Hovered() {
			r.Background(p.Hover)
		}
		if r.Clicked() {
			h.menu = false
			run()
			a.edited(pn)
		}
	}
	ui.Column(c).Width(220).Padding(4).Gap(1).Children(func() {
		if !movable {
			ui.Text(c, tr.T("Sub-halaman, media, tabel, dan block yang punya isi di dalamnya diubah di Notion, supaya isinya tidak hilang.")).
				FontSize(12).TextColor(p.Faint).Padding(6, 8)
			return
		}
		ui.Text(c, tr.T("Ubah jadi")).FontSize(11).FontWeight(600).TextColor(p.Faint).Padding(4, 10)
		ui.Column(c).Grid().Columns(3).Gap(2).Padding(0, 4).Children(func() {
			for _, k := range blockKinds {
				style := k.style
				b := ui.Text(c.Key("kind"+k.label), tr.T(k.label)).FontSize(13).Padding(6, 0).TextAlign(ui.Center).Radius(6).Cursor(ui.CursorPointer)
				if b.Hovered() {
					b.Background(p.Hover)
				}
				if b.Clicked() {
					h.menu = false
					restyleBlock(pn.doc, i, style)
					a.edited(pn)
				}
			}
		})
		ui.Box(c).Height(1).Background(p.Line).Margin(4, 6)
		item("dup", tr.T("Duplikat"), false, func() { duplicateBlock(pn.doc, i) })
		item("up", tr.T("Pindah ke atas"), false, func() {
			paras := paragraphsOf(pn.doc)
			first, _ := blockRange(paras, i)
			if first > 0 {
				prev, _ := blockRange(paras, first-1)
				moveBlock(pn.doc, i, prev)
			}
		})
		item("down", tr.T("Pindah ke bawah"), false, func() {
			paras := paragraphsOf(pn.doc)
			_, last := blockRange(paras, i)
			if last+1 < len(paras) {
				_, nextLast := blockRange(paras, last+1)
				moveBlock(pn.doc, i, nextLast+1)
			}
		})
		ui.Box(c).Height(1).Background(p.Line).Margin(4, 6)
		item("del", tr.T("Hapus"), true, func() { deleteBlock(pn.doc, i) })
	})
}

func abs(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}
