package app

import (
	"context"
	"errors"
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

// toggleComments opens or closes panel pn's comments, loading them.
func (a *App) toggleComments(pn *panel) {
	cs := &pn.comments
	cs.open = !cs.open
	if cs.open && !cs.loaded && !cs.loading {
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

// commentsDrawer is the comments beside the page.
func (a *App) commentsDrawer(c *ui.Context, pn *panel) {
	p := a.pal()
	cs := &pn.comments
	ui.Column(c.Key("comments")).Width(320).Shrink(0).BorderWidth(0, 0, 0, 1).BorderColor(p.Line).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Padding(14, 12, 10, 18).Children(func() {
			ui.Text(c, tr.T("Komentar")).FontSize(15).FontWeight(600).Grow(1)
			if iconButton(c, p, iconRefresh, tr.T("Muat ulang komentar")) {
				a.loadComments(pn)
			}
			if iconButton(c, p, iconClose, tr.T("Tutup")) {
				cs.open = false
			}
		})
		ui.Scroll(c).Grow(1).MinHeight(0).ScrollbarStyle(a.thinScrollbar()).Children(func() {
			ui.Column(c).Padding(0, 18, 12, 18).Gap(16).Children(func() {
				switch {
				case cs.loading && len(cs.list) == 0:
					ui.Spinner(c).Size(18, 18)
				case cs.err != "":
					ui.Text(c, cs.err).FontSize(13).TextColor(p.Muted)
				case len(cs.list) == 0:
					ui.Text(c, tr.T("Belum ada komentar di halaman ini.")).FontSize(13).TextColor(p.Faint)
				}
				for _, d := range discussions(cs.list) {
					a.discussion(c, pn, d)
				}
			})
		})
		if cs.err == "" {
			ui.Column(c).Padding(10, 14, 14, 14).Gap(8).BorderWidth(1, 0, 0, 0).BorderColor(p.Line).Children(func() {
				in := ui.TextArea(c, &cs.draft).Lines(1, 5).Placeholder(tr.T("Tambah komentar…")).Label(tr.T("Komentar baru")).FontSize(14)
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Text(c, tr.T("Ctrl+Enter untuk kirim")).FontSize(11).TextColor(p.Faint).Grow(1)
					send := ui.PrimaryButton(c, tr.T("Kirim")).Disabled(cs.sending || strings.TrimSpace(cs.draft) == "")
					if send.Clicked() || in.Shortcut(ui.Cmd, ui.KeyEnter) {
						a.sendComment(pn, "", cs.draft)
					}
				})
			})
		}
	})
}

// discussion is one thread: its comments, and replying to it.
func (a *App) discussion(c *ui.Context, pn *panel, d []notion.Comment) {
	p := a.pal()
	cs := &pn.comments
	id := d[0].DiscussionID
	ui.Column(c.Key("d" + id + d[0].ID)).Gap(10).Padding(12).Radius(10).Background(p.Sidebar).Children(func() {
		for _, cm := range d {
			ui.Column(c.Key("c" + cm.ID)).Gap(3).Children(func() {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					name := nonEmpty(cm.DisplayName.ResolvedName, nonEmpty(cm.CreatedBy.Name, tr.T("Seseorang")))
					ui.Text(c, name).FontSize(13).FontWeight(600)
					ui.Text(c, ago(cm.CreatedTime)).FontSize(11).TextColor(p.Faint)
				})
				ui.Text(c, notion.PlainText(cm.RichText)).FontSize(14).Selectable()
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
