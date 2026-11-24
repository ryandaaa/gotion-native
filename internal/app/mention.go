package app

import (
	"gotion/internal/tr"
	"sort"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/richtext"

	"gotion/internal/doc"
	"gotion/internal/emoji"
)

// mentionState is the popup typing @ opens: pages and people to mention.
type mentionState struct {
	open  bool
	start int // the offset of the @
	query string
	sel   int
}

// mentionable is a page or a person the popup offers.
type mentionable struct {
	kind, id, label, emoji string
}

// mentionMatches are the pages and people whose name has the query, pages
// edited lately first, at most eight.
func (a *App) mentionMatches(q string) []mentionable {
	q = strings.ToLower(q)
	var pages []*node
	for _, n := range a.nodes {
		if n.Kind == "page" && (q == "" || strings.Contains(strings.ToLower(n.Title), q)) {
			pages = append(pages, n)
		}
	}
	sort.Slice(pages, func(i, j int) bool { return byEditedThenTitle(pages[i], pages[j]) })
	var out []mentionable
	for _, u := range a.settings.members {
		if q == "" || strings.Contains(strings.ToLower(u.Name), q) {
			out = append(out, mentionable{kind: "user", id: u.ID, label: u.Name})
		}
	}
	for _, n := range pages {
		out = append(out, mentionable{kind: "page", id: n.ID, label: n.Title, emoji: n.Emoji})
	}
	if len(out) > 8 {
		out = out[:8]
	}
	return out
}

// mentionUpdate opens, narrows or closes the popup after an edit: an @ at
// a word's start, and what follows it up to the caret.
func (a *App) mentionUpdate(pn *panel) {
	m := &pn.mention
	start, before, ok := lineBefore(pn.doc)
	at := strings.LastIndex(before, "@")
	if !ok || at < 0 {
		m.open = false
		return
	}
	query := before[at+1:]
	word := at == 0 || strings.ContainsAny(before[at-1:at], " \t")
	if !word || strings.ContainsAny(query, " \t") || len([]rune(query)) > 30 {
		m.open = false
		return
	}
	if !m.open || m.query != query {
		m.sel = 0
	}
	m.open, m.query = true, query
	m.start = start + len([]rune(before[:at]))
	if len(a.mentionMatches(query)) == 0 {
		m.open = false
	}
	// People come once the members are known: the account section loads them.
	if !a.settings.membersLoaded && !a.settings.membersLoading {
		a.loadMembers()
	}
}

// mentionKey takes the popup's keys while it is open.
func (a *App) mentionKey(pn *panel, k ui.Key, mods ui.Modifiers) bool {
	m := &pn.mention
	if !m.open || mods != 0 {
		return false
	}
	items := a.mentionMatches(m.query)
	if len(items) == 0 {
		return false
	}
	switch k {
	case ui.KeyDown:
		m.sel = (m.sel + 1) % len(items)
	case ui.KeyUp:
		m.sel = (m.sel + len(items) - 1) % len(items)
	case ui.KeyEnter, ui.KeyTab:
		a.insertMention(pn, items[min(m.sel, len(items)-1)])
	case ui.KeyEscape:
		m.open = false
	default:
		return false
	}
	return true
}

// insertMention puts a mention where the @ and its query were.
func (a *App) insertMention(pn *panel, it mentionable) {
	m := &pn.mention
	_, caret := pn.doc.Selection()
	pn.doc.SetSelection(m.start, caret)
	pn.doc.Insert(richtext.TextOf(richtext.Paragraph{Runs: []richtext.Run{doc.Mention(it.kind, it.id, it.label), {Text: " "}}}))
	m.open = false
	a.edited(pn)
}

// mentionView is the popup, under the caret.
func (a *App) mentionView(c *ui.Context, pn *panel) {
	m := &pn.mention
	if !m.open {
		return
	}
	r, ok := pn.doc.CaretRect()
	if !ok {
		return
	}
	p := a.pal()
	items := a.mentionMatches(m.query)
	ui.Column(c.Key("mention-menu")).Absolute().Left(r.X).Top(r.Y+r.H+6).Width(280).ZIndex(20).Padding(6).Gap(1).
		TransformOrigin(0, 0).Transition(popIn).
		Background(p.Panel).Radius(10).Border(1, p.Line).Shadow(0, 8, 28, 0, ui.RGBA(0, 0, 0, 0.16)).Children(func() {
		ui.Text(c, tr.T("Sebut")).FontSize(11).FontWeight(600).TextColor(p.Faint).Padding(4, 8)
		for j, it := range items {
			row := ui.Row(c.Key("mn"+it.id)).AlignItems(ui.Center).Gap(8).Padding(6, 8).Radius(7).Cursor(ui.CursorPointer)
			if j == m.sel || row.Hovered() {
				row.Background(p.Hover)
			}
			row.Children(func() {
				switch {
				case it.kind == "user":
					ui.Box(c).Size(20, 20).Radius(10).Background(p.Accent.Alpha(0.16)).Center().Children(func() {
						ui.Text(c, strings.ToUpper(string([]rune(nonEmpty(it.label, "?"))[:1]))).FontSize(11).FontWeight(600).TextColor(p.Accent)
					})
				case it.emoji != "":
					emoji.View(c, it.emoji, 16)
				default:
					ui.Icon(c, iconPage).Size(16, 16).TextColor(p.Faint)
				}
				ui.Text(c, nonEmpty(it.label, tr.T("Tanpa judul"))).FontSize(14).SingleLine().MinWidth(0)
			})
			if row.Clicked() {
				a.insertMention(pn, it)
			}
		}
	})
}
