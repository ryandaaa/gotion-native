package app

import (
	"gotion/internal/tr"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/richtext"

	"gotion/internal/doc"
)

// slashState is the menu typing / at the start of a line opens.
type slashState struct {
	open  bool
	start int // the offset of the slash
	query string
	sel   int
	link  bool // the format bar asks for a link
	url   string
}

type command struct {
	label, hint, keys string
	run               func(a *App, pn *panel)
}

var commands = []command{
	{tr.N("Teks"), tr.N("Paragraf biasa"), "text paragraf teks", func(a *App, pn *panel) { plain(pn.doc) }},
	{"Heading 1", tr.N("Judul besar"), "h1 heading judul", func(a *App, pn *panel) { plain(pn.doc); pn.doc.SetHeading(1) }},
	{"Heading 2", tr.N("Judul sedang"), "h2 heading subjudul", func(a *App, pn *panel) { plain(pn.doc); pn.doc.SetHeading(2) }},
	{"Heading 3", tr.N("Judul kecil"), "h3 heading", func(a *App, pn *panel) { plain(pn.doc); pn.doc.SetHeading(3) }},
	{"To-do", tr.N("Daftar dengan centang"), "todo task checklist centang", func(a *App, pn *panel) { plain(pn.doc); pn.doc.ToggleList(richtext.Tasks) }},
	{"Bullet", tr.N("Daftar berpoin"), "bullet list poin", func(a *App, pn *panel) { plain(pn.doc); pn.doc.ToggleList(richtext.Bullets) }},
	{tr.N("Bernomor"), tr.N("Daftar bernomor"), "numbered number nomor list", func(a *App, pn *panel) { plain(pn.doc); pn.doc.ToggleList(richtext.Numbers) }},
	{tr.N("Kutipan"), "Quote", "quote kutipan", func(a *App, pn *panel) { plain(pn.doc); pn.doc.ToggleQuote() }},
	{tr.N("Kode"), tr.N("Blok kode"), "code kode", func(a *App, pn *panel) { plain(pn.doc); pn.doc.ToggleCodeBlock() }},
	{tr.N("Pembatas"), tr.N("Garis pemisah"), "divider pembatas garis hr", func(a *App, pn *panel) {
		pn.doc.Insert(doc.Divider())
		pn.doc.InsertText("\n")
	}},
	{tr.N("Sub-halaman"), tr.N("Sub-halaman baru"), "page halaman", func(a *App, pn *panel) { a.newPage(pn.id) }},
	{"Database", tr.N("Tabel database baru"), "database tabel table db", func(a *App, pn *panel) { a.newDatabase(pn) }},
}

// plain makes the paragraph at the caret a body paragraph.
func plain(d *richtext.Document) {
	d.FormatParagraphs(func(s *richtext.ParagraphStyle) {
		id, indent := s.ID, s.Indent
		*s = richtext.ParagraphStyle{ID: id, Indent: indent}
	})
}

func (s *slashState) matches() []command {
	q := strings.ToLower(s.query)
	var out []command
	for _, cmd := range commands {
		if q == "" || strings.Contains(strings.ToLower(tr.T(cmd.label)), q) || strings.Contains(strings.ToLower(cmd.label), q) || strings.Contains(cmd.keys, q) {
			out = append(out, cmd)
		}
	}
	return out
}

// lineBefore returns the start of the caret's paragraph and its text up to
// the caret, found without a pass over the whole text.
func lineBefore(d *richtext.Document) (start int, text string, ok bool) {
	anchor, caret := d.Selection()
	if anchor != caret {
		return 0, "", false
	}
	t := d.Text()
	start, _ = t.ParagraphRange(caret)
	return start, t.Slice(start, caret).String(), true
}

// slashUpdate opens, narrows or closes the menu after an edit.
func (a *App) slashUpdate(pn *panel) {
	s := &pn.slash
	start, before, ok := lineBefore(pn.doc)
	if ok && strings.HasPrefix(before, "/") && !strings.ContainsAny(before, " \t") && len([]rune(before)) <= 20 {
		if !s.open || s.query != before[1:] {
			s.sel = 0
		}
		s.open, s.start, s.query = true, start, before[1:]
		if len(s.matches()) == 0 {
			s.open = false
		}
		return
	}
	s.open = false
}

// slashKey takes the menu's keys while it is open.
func (a *App) slashKey(pn *panel, k ui.Key, m ui.Modifiers) bool {
	s := &pn.slash
	if !s.open || m != 0 {
		return false
	}
	items := s.matches()
	switch k {
	case ui.KeyDown:
		s.sel = (s.sel + 1) % len(items)
	case ui.KeyUp:
		s.sel = (s.sel + len(items) - 1) % len(items)
	case ui.KeyEnter, ui.KeyTab:
		a.runCommand(pn, items[min(s.sel, len(items)-1)])
	case ui.KeyEscape:
		s.open = false
	default:
		return false
	}
	return true
}

func (a *App) runCommand(pn *panel, cmd command) {
	s := &pn.slash
	_, caret := pn.doc.Selection()
	// Take out what was typed: the slash and the query.
	pn.doc.SetSelection(s.start, caret)
	pn.doc.InsertText("")
	s.open = false
	cmd.run(a, pn)
	if pn.session != nil {
		pn.session.Edited(pn.doc.Text())
	}
}

// slashView draws the menu under the caret.
func (a *App) slashView(c *ui.Context, pn *panel) {
	s := &pn.slash
	if !s.open {
		return
	}
	r, ok := pn.doc.CaretRect()
	if !ok {
		return
	}
	p := a.pal()
	items := s.matches()
	ui.Column(c.Key("slash-menu")).Absolute().Left(r.X).Top(r.Y+r.H+6).Width(280).ZIndex(20).Padding(6).Gap(1).
		TransformOrigin(0, 0).Transition(popIn).
		Background(p.Panel).Radius(10).Border(1, p.Line).Shadow(0, 8, 28, 0, ui.RGBA(0, 0, 0, 0.16)).Children(func() {
		ui.Text(c, tr.T("Block dasar")).FontSize(11).FontWeight(600).TextColor(p.Faint).Padding(4, 8)
		for j, cmd := range items {
			row := ui.Row(c.Key("cmd"+cmd.label)).AlignItems(ui.Center).Gap(10).Padding(6, 8).Radius(7).Cursor(ui.CursorPointer)
			if j == s.sel || row.Hovered() {
				row.Background(p.Hover)
			}
			row.Children(func() {
				ui.Column(c).Children(func() {
					ui.Text(c, tr.T(cmd.label)).FontSize(14).FontWeight(500)
					ui.Text(c, tr.T(cmd.hint)).FontSize(12).TextColor(p.Faint)
				})
			})
			if row.Clicked() {
				a.runCommand(pn, cmd)
			}
		}
		ui.Text(c, tr.T("↑↓ pilih · Enter masukkan · Esc tutup")).FontSize(11).TextColor(p.Faint).Padding(6, 8, 2, 8)
	})
}

// formatColors are the text colors the format bar offers, as Notion names them.
var formatColors = []struct {
	name  string
	color ui.Color
}{
	{"default", ui.Color{}}, {"gray", ui.Hex("#787774")}, {"red", ui.Hex("#D44C47")},
	{"orange", ui.Hex("#D9730D")}, {"green", ui.Hex("#448361")}, {"blue", ui.Hex("#337EA9")}, {"purple", ui.Hex("#9065B0")},
}

// formatBar floats over a selection with its formats.
func (a *App) formatBar(c *ui.Context, pn *panel) {
	anchor, caret := pn.doc.Selection()
	if anchor == caret {
		pn.slash.link = false
		return
	}
	r, ok := pn.doc.CaretRect()
	if !ok {
		return
	}
	p := a.pal()
	f := pn.doc.Formatting()
	top := r.Y - 46
	if top < 0 {
		top = r.Y + r.H + 8
	}
	ui.Row(c.Key("format-bar")).Absolute().Left(max(8, r.X-120)).Top(top).ZIndex(20).Padding(4).Gap(2).AlignItems(ui.Center).
		Transition(fadeIn).
		Background(p.Panel).Radius(9).Border(1, p.Line).Shadow(0, 6, 20, 0, ui.RGBA(0, 0, 0, 0.14)).Children(func() {
		fmtButton(c, p, "B", tr.T("Tebal (Ctrl+B)"), f.Bold == richtext.FormatOn, pn, pn.doc.ToggleBold).Bold()
		fmtButton(c, p, "I", tr.T("Miring (Ctrl+I)"), f.Italic == richtext.FormatOn, pn, pn.doc.ToggleItalic).Italic()
		fmtButton(c, p, "U", tr.T("Garis bawah (Ctrl+U)"), f.Underline == richtext.FormatOn, pn, pn.doc.ToggleUnderline).Underline()
		fmtButton(c, p, "S", tr.T("Coret"), f.Strikethrough == richtext.FormatOn, pn, pn.doc.ToggleStrikethrough).Strikethrough()
		fmtButton(c, p, "</>", tr.T("Kode (Ctrl+E)"), f.Code == richtext.FormatOn, pn, pn.doc.ToggleCode)
		fmtButton(c, p, "Link", tr.T("Tautan (Ctrl+K di editor)"), f.Link != "", pn, func() {
			pn.slash.link, pn.slash.url = !pn.slash.link, f.Link
		})
		ui.Box(c).Width(1).Height(18).Background(p.Line).Margin(0, 4)
		for _, fc := range formatColors {
			dot := ui.Box(c.Key("color"+fc.name)).Size(16, 16).Radius(8).Cursor(ui.CursorPointer).Tooltip(tr.T("Warna ") + fc.name).KeepFocus()
			if fc.name == "default" {
				dot.Border(1.5, p.Faint)
			} else {
				dot.Background(fc.color)
			}
			if dot.Clicked() {
				color := fc.color
				pn.doc.Format(func(at *richtext.Attributes) { at.Color, at.Background = color, ui.Color{} })
				if pn.session != nil {
					pn.session.Edited(pn.doc.Text())
				}
			}
		}
		if pn.slash.link {
			in := ui.TextInput(c, &pn.slash.url).Placeholder("https://").Width(220).AutoFocus().Label("URL").FontSize(13)
			if in.Submitted() {
				pn.doc.SetLink(strings.TrimSpace(pn.slash.url))
				pn.slash.link = false
				if pn.session != nil {
					pn.session.Edited(pn.doc.Text())
				}
			}
		}
	})
}

func fmtButton(c *ui.Context, p palette, label, tip string, on bool, pn *panel, run func()) ui.Element {
	b := ui.Text(c.Key("fmt"+label), label).FontSize(13).Padding(4, 9).Radius(6).Cursor(ui.CursorPointer).Tooltip(tip).KeepFocus()
	if on {
		b.Background(p.EditBg).TextColor(p.Accent)
	} else if b.Hovered() {
		b.Background(p.Hover)
	}
	if b.Clicked() {
		run()
		if pn.session != nil {
			pn.session.Edited(pn.doc.Text())
		}
	}
	return b
}
