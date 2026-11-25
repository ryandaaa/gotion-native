package app

import (
	"context"
	"errors"
	"fmt"
	"gotion/internal/tr"
	"strings"

	"github.com/egoist/mygo/ui"

	"gotion/internal/notion"
)

// commentsState is a page's comments drawer.
type commentsState struct {
	open    bool
	loaded  bool
	loading bool
	err     string
	list    []notion.Comment
	draft   string
	replyTo string // the discussion being replied to
	reply   string
	sending bool
}

// toggleComments opens the right pane to show panel pn's comments, loading them.
func (a *App) toggleComments(pn *panel) {
	cs := &pn.comments
	if a.rightPaneFits {
		// The comments live in the right pane.
		a.set.RightPane = true
		a.saveSettings()
		cs.open = false
	} else {
		// No room for the pane: a drawer beside the page instead.
		cs.open = !cs.open
	}
	if !cs.loaded && !cs.loading {
		a.loadComments(pn)
	}
}

func (a *App) loadComments(pn *panel) {
	cs := &pn.comments
	cs.loading, cs.err = true, ""
	client, id := a.client, pn.id
	go func() {
		list, err := client.Comments(context.Background(), id)
		a.update(func() {
			cs.loading, cs.loaded = false, true
			if err != nil {
				cs.err = commentError(err)
				return
			}
			cs.list = list
		})
	}()
}

// commentError says what to do when Notion refuses comments: an
// integration has them only when its capabilities say so.
func commentError(err error) string {
	var e *notion.Error
	if errors.As(err, &e) && e.Status == 403 {
		return tr.T("Integrasi ini belum boleh membaca atau menulis komentar. Aktifkan “Read comments” dan “Insert comments” di pengaturan integrasi Notion.")
	}
	return errText(err)
}

// sendComment posts a comment on the page, or a reply in a discussion.
func (a *App) sendComment(pn *panel, discussion, text string) {
	text = strings.TrimSpace(text)
	cs := &pn.comments
	if text == "" || cs.sending {
		return
	}
	cs.sending = true
	client, id := a.client, pn.id
	go func() {
		c, err := client.AddComment(context.Background(), id, discussion, text)
		a.update(func() {
			cs.sending = false
			if err != nil {
				a.toast(tr.T("Gagal mengirim komentar: ") + commentError(err))
				return
			}
			if c.DiscussionID == "" {
				c.DiscussionID = discussion
			}
			if len(c.RichText) == 0 {
				c.RichText = []notion.RichText{{PlainText: text}}
			}
			if c.DisplayName.ResolvedName == "" {
				c.DisplayName.ResolvedName = "gotion"
			}
			cs.list = append(cs.list, *c)
			if discussion == "" {
				cs.draft = ""
			} else {
				cs.reply, cs.replyTo = "", ""
			}
		})
	}()
}

// discussions groups comments by their thread, in the order each began.
func discussions(list []notion.Comment) [][]notion.Comment {
	var order []string
	by := map[string][]notion.Comment{}
	for _, c := range list {
		k := c.DiscussionID
		if k == "" {
			k = c.ID
		}
		if _, ok := by[k]; !ok {
			order = append(order, k)
		}
		by[k] = append(by[k], c)
	}
	out := make([][]notion.Comment, len(order))
	for i, k := range order {
		out[i] = by[k]
	}
	return out
}

// commentsInPane renders the comments list and input inside the right pane.
func (a *App) commentsInPane(c *ui.Context, pn *panel) {
	p := a.pal()
	cs := &pn.comments
	if !cs.loaded && !cs.loading && a.client != nil {
		a.loadComments(pn)
	}
	countLabel := tr.T("Komentar")
	if len(cs.list) > 0 {
		countLabel += " · " + fmt.Sprintf("%d", len(cs.list))
	}
	ui.Column(c).FillWidth().Gap(8).Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
			ui.Text(c, countLabel).FontSize(13).FontWeight(600).TextColor(p.Faint).Grow(1)
			if cs.loading {
				ui.Spinner(c).Size(12, 12)
			} else if iconButton(c, p, iconRefresh, tr.T("Muat ulang komentar")) {
				a.loadComments(pn)
			}
		})
		switch {
		case cs.loading && len(cs.list) == 0:
			ui.Text(c, tr.T("Memuat…")).FontSize(12).TextColor(p.Faint)
		case cs.err != "":
			ui.Text(c, cs.err).FontSize(12).TextColor(p.Muted)
		case len(cs.list) == 0:
			ui.Text(c, tr.T("Belum ada komentar di halaman ini.")).FontSize(12).TextColor(p.Faint)
		}
		for _, d := range discussions(cs.list) {
			a.discussion(c, pn, d)
		}
		if cs.err == "" {
			ui.Column(c).FillWidth().Padding(10).Radius(10).Background(paneCardBg(a.dark)).Border(1, p.Line).Gap(6).Children(func() {
				in := ui.TextArea(c.Key("pane-cm-draft"), &cs.draft).Lines(1, 4).
					Placeholder(tr.T("Tulis komentar…")).Label(tr.T("Komentar baru")).FontSize(13).
					Background(ui.Color{}).Border(0, ui.Color{}).Padding(0).FocusRing(false)
				if strings.TrimSpace(cs.draft) != "" || in.Focused() {
					ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
						ui.Text(c, tr.T("Ctrl+Enter untuk kirim")).FontSize(11).TextColor(p.Faint).Grow(1)
						send := ui.PrimaryButton(c, tr.T("Kirim")).Disabled(cs.sending || strings.TrimSpace(cs.draft) == "")
						if send.Clicked() || in.Shortcut(ui.Cmd, ui.KeyEnter) {
							a.sendComment(pn, "", cs.draft)
						}
					})
				} else if in.Shortcut(ui.Cmd, ui.KeyEnter) {
					a.sendComment(pn, "", cs.draft)
				}
			})
		}
	})
}

// discussion is one thread: its comments, and replying to it.
func (a *App) discussion(c *ui.Context, pn *panel, d []notion.Comment) {
	p := a.pal()
	cs := &pn.comments
	id := d[0].DiscussionID
	ui.Column(c.Key("d"+id+d[0].ID)).FillWidth().Gap(8).Padding(10, 12).Radius(10).
		Background(paneCardBg(a.dark)).Border(1, p.Line).Children(func() {
		for _, cm := range d {
			ui.Column(c.Key("c" + cm.ID)).Gap(4).Children(func() {
				ui.Row(c).Gap(7).AlignItems(ui.Center).Children(func() {
					name := nonEmpty(cm.DisplayName.ResolvedName, nonEmpty(cm.CreatedBy.Name, tr.T("Seseorang")))
					initial := strings.ToUpper(string([]rune(name)[:1]))
					ui.Box(c).Size(20, 20).Radius(10).Background(p.Accent.Alpha(0.18)).Center().Children(func() {
						ui.Text(c, initial).FontSize(10).FontWeight(600).TextColor(p.Accent)
					})
					ui.Text(c, name).FontSize(12).FontWeight(600).SingleLine().Shrink(1)
					ui.Text(c, ago(cm.CreatedTime)).FontSize(11).TextColor(p.Faint).Shrink(0)
				})
				ui.Text(c, notion.PlainText(cm.RichText)).FontSize(13).Selectable()
			})
		}
		if id == "" {
			return
		}
		if cs.replyTo != id {
			if ui.Link(c, tr.T("Balas"), "").FontSize(12).TextColor(p.Muted).Clicked() {
				cs.replyTo, cs.reply = id, ""
			}
			return
		}
		in := ui.TextArea(c, &cs.reply).Lines(1, 4).Placeholder(tr.T("Balas…")).AutoFocus().Label(tr.T("Balasan")).FontSize(13)
		ui.Row(c).Gap(6).Children(func() {
			if ui.PrimaryButton(c, tr.T("Kirim")).Disabled(cs.sending || strings.TrimSpace(cs.reply) == "").Clicked() || in.Shortcut(ui.Cmd, ui.KeyEnter) {
				a.sendComment(pn, id, cs.reply)
			}
			if a.btn(c, tr.T("Batal"), nil, false, false) {
				cs.replyTo = ""
			}
		})
	})
}

// commentsDrawer is the comments beside the page, for a window without room
// for the right pane.
func (a *App) commentsDrawer(c *ui.Context, pn *panel) {
	p := a.pal()
	ui.Column(c.Key("comments")).Width(300).Shrink(0).BorderWidth(0, 0, 0, 1).BorderColor(p.Line).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Padding(10, 10, 0, 14).Children(func() {
			ui.Box(c).Grow(1)
			if iconButton(c, p, iconClose, tr.T("Tutup")) {
				pn.comments.open = false
			}
		})
		ui.Scroll(c).Grow(1).MinHeight(0).ScrollbarStyle(a.thinScrollbar()).Children(func() {
			ui.Column(c).Padding(0, 14, 14, 14).Children(func() { a.commentsInPane(c, pn) })
		})
	})
}
