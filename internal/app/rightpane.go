package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/richtext"

	"gotion/internal/emoji"
	"gotion/internal/notion"
	"gotion/internal/syncer"
	"gotion/internal/tr"
)

type outlineItem struct {
	Index int
	Level int
	Text  string
}

func paneCardBg(dark bool) ui.Color {
	if dark {
		return ui.RGBA(44, 44, 46, 0.72)
	}
	return ui.RGBA(255, 255, 255, 0.78)
}

// extractOutline returns H1..H3 headings from a richtext document.
func extractOutline(t richtext.Text) []outlineItem {
	var out []outlineItem
	idx := 0
	for _, p := range t.Paragraphs() {
		if p.Style.Heading >= 1 && p.Style.Heading <= 3 {
			var b strings.Builder
			for _, r := range p.Runs {
				b.WriteString(r.Text)
			}
			if txt := strings.TrimSpace(b.String()); txt != "" {
				out = append(out, outlineItem{
					Index: idx,
					Level: p.Style.Heading,
					Text:  txt,
				})
			}
		}
		idx++
	}
	return out
}

func countWords(t richtext.Text) int {
	n := 0
	for _, p := range t.Paragraphs() {
		for _, r := range p.Runs {
			n += len(strings.Fields(r.Text))
		}
	}
	return n
}

func activeOutlineIndex(pn *panel, items []outlineItem) int {
	if pn == nil || pn.doc == nil || len(items) == 0 {
		return -1
	}
	vis := pn.doc.VisibleParagraphs()
	if len(vis) == 0 {
		return items[0].Index
	}
	// First paragraph at or below the current scroll top.
	topIdx := vis[0].Index
	for _, pb := range vis {
		if pb.Top+pb.Height > pn.scroll.Y+16 {
			topIdx = pb.Index
			break
		}
	}
	active := items[0].Index
	for _, h := range items {
		if h.Index <= topIdx {
			active = h.Index
		}
	}
	return active
}

func (a *App) headingJumpAndFlash(c *ui.Context, pn *panel, pad float32) {
	if pn.doc == nil {
		return
	}
	p := a.pal()
	vis := pn.doc.VisibleParagraphs()

	if pn.jumpPara > 0 {
		target := pn.jumpPara - 1
		pn.jumpPara = 0
		var y, h float32
		found := false
		for _, pb := range vis {
			if pb.Index == target {
				y, h, found = pb.Top, pb.Height, true
				break
			}
		}
		if !found {
			total := 0
			for range pn.doc.Text().Paragraphs() {
				total++
			}
			y = editorHeight(pn.doc) * float32(target) / float32(max(1, total))
			h = 28
		}
		marker := ui.Box(c.Key("jump-" + pn.id)).Absolute().Top(y).Height(max(h, 1)).Left(0).Width(1)
		marker.ScrollIntoView(ui.ScrollIntoViewOptions{Block: ui.ScrollStart, Animation: &ui.Smooth})
	}

	if pn.flashPara > 0 {
		elapsed := nowFunc().Sub(pn.flashAt)
		if elapsed < 600*time.Millisecond {
			target := pn.flashPara - 1
			for _, pb := range vis {
				if pb.Index == target {
					fade := 1 - float32(elapsed)/float32(600*time.Millisecond)
					ui.Box(c.Key("flash-" + pn.id)).Absolute().
						Top(pb.Top - 2).Height(pb.Height + 4).Left(pad - 8).Right(pad - 8).
						Radius(6).Background(p.Accent.Alpha(0.20 * fade))
					c.AnimationFrame()
					break
				}
			}
		} else {
			pn.flashPara = 0
		}
	}
}

func (a *App) ensureUsersLoaded() {
	if a.usersAsked || a.client == nil {
		return
	}
	a.usersAsked = true
	client := a.client
	go func() {
		users, err := client.Users(context.Background())
		if err != nil {
			return
		}
		a.update(func() {
			if a.userNames == nil {
				a.userNames = map[string]string{}
			}
			for _, u := range users {
				if u.ID != "" && u.Name != "" {
					a.userNames[u.ID] = u.Name
				}
			}
		})
	}()
}

func (a *App) userNameByID(id string) string {
	if id == "" {
		return ""
	}
	if a.bot != nil && a.bot.ID == id && a.bot.Name != "" {
		return a.bot.Name
	}
	if name := a.userNames[id]; name != "" {
		return name
	}
	for _, u := range a.settings.members {
		if u.ID == id && u.Name != "" {
			return u.Name
		}
	}
	return ""
}

func readCachedPage(id string) (pg *notion.Page, blocks []*notion.Block) {
	if syncer.CacheDir == "" || id == "" {
		return nil, nil
	}
	b, err := os.ReadFile(filepath.Join(syncer.CacheDir, id+".page.json"))
	if err != nil {
		return nil, nil
	}
	var cached struct {
		Page   *notion.Page    `json:"page"`
		Blocks []*notion.Block `json:"blocks"`
	}
	if json.Unmarshal(b, &cached) != nil {
		return nil, nil
	}
	return cached.Page, cached.Blocks
}

// pageInfoEntry is a page's cache read for the right pane's Info.
type pageInfoEntry struct {
	at     time.Time
	page   *notion.Page
	blocks []*notion.Block
}

// pageInfo is readCachedPage for the right pane, read from disk at most every
// few seconds: the pane is drawn on every frame, typing included.
func (a *App) pageInfo(id string) (*notion.Page, []*notion.Block) {
	if a.pageInfos == nil {
		a.pageInfos = map[string]pageInfoEntry{}
	}
	e, ok := a.pageInfos[id]
	if !ok || nowFunc().Sub(e.at) > 5*time.Second {
		e.page, e.blocks = readCachedPage(id)
		e.at = nowFunc()
		a.pageInfos[id] = e
	}
	return e.page, e.blocks
}

func previewParagraphs(blocks []*notion.Block, limit int) []string {
	var out []string
	for _, b := range blocks {
		if b == nil {
			continue
		}
		txt := strings.TrimSpace(notion.PlainText(b.Text().RichText))
		if txt == "" {
			continue
		}
		out = append(out, txt)
		if len(out) >= limit {
			break
		}
	}
	return out
}

func (a *App) rightPane(c *ui.Context) {
	p := a.pal()
	pn := a.panels[a.focus]
	if pn == nil {
		pn = a.panels[0]
	}
	ui.Scroll(c.Key("right-pane-scroll")).Grow(1).MinHeight(0).ScrollbarStyle(a.thinScrollbar()).Children(func() {
		ui.Column(c).FillWidth().Padding(8, 14, 16, 10).Gap(18).Children(func() {
			switch {
			case pn == nil || pn.id == "":
				ui.Text(c, tr.T("Tidak ada info tambahan untuk halaman ini.")).FontSize(12).TextColor(p.Faint)
			case a.kindOf(pn.id) == "today":
				a.rightPaneToday(c)
			case a.kindOf(pn.id) == "library" || a.kindOf(pn.id) == "trash" || a.kindOf(pn.id) == "calendar":
				ui.Text(c, tr.T("Tidak ada info tambahan untuk halaman ini.")).FontSize(12).TextColor(p.Faint)
			case pn.table != nil:
				a.rightPaneTable(c, a.focus, pn.table)
			default:
				a.rightPanePage(c, pn)
			}
		})
	})
}

func (a *App) rightPanePage(c *ui.Context, pn *panel) {
	p := a.pal()

	// 1. Garis besar (Outline H1-H3)
	if pn.doc != nil {
		items := extractOutline(pn.doc.Text())
		if len(items) > 0 {
			active := activeOutlineIndex(pn, items)
			ui.Column(c).FillWidth().Gap(3).Children(func() {
				ui.Text(c, tr.T("Garis besar")).FontSize(13).FontWeight(600).TextColor(p.Faint).Padding(0, 6, 4, 6)
				for _, h := range items {
					isAct := h.Index == active
					indent := float32((h.Level-1)*12 + 8)
					row := ui.Row(c.Key(fmt.Sprintf("ol-%s-%d", pn.id, h.Index))).FillWidth().
						AlignItems(ui.Center).Gap(8).Padding(5, 10, 5, indent).Radius(8).Cursor(ui.CursorPointer)
					if isAct {
						row.Background(p.Panel).Shadow(0, 1, 2, 0, ui.RGBA(0, 0, 0, 0.08))
					} else if row.Hovered() {
						row.Background(p.Hover)
					}
					row.Children(func() {
						size, weight, tc := float32(13), 500, p.Muted
						switch h.Level {
						case 1:
							size, weight = 13, 600
						case 3:
							size, weight = 12, 400
						}
						if isAct {
							weight, tc = 600, p.Text
						}
						ui.Text(c, h.Text).FontSize(size).FontWeight(weight).TextColor(tc).SingleLine().Grow(1).MinWidth(0)
					})
					if row.Clicked() {
						pn.jumpPara = h.Index + 1
						pn.flashPara = h.Index + 1
						pn.flashAt = nowFunc()
					}
				}
			})
		}
	}

	// 2. Komentar · N
	a.commentsInPane(c, pn)

	// 3. Info
	a.ensureUsersLoaded()
	cachedPg, cachedBlocks := a.pageInfo(pn.id)
	var editedTime time.Time
	var editorID string
	if n := a.nodes[pn.id]; n != nil {
		editedTime = n.Edited
	}
	if cachedPg != nil {
		if !cachedPg.LastEditedTime.IsZero() {
			editedTime = cachedPg.LastEditedTime
		}
		if cachedPg.LastEditedBy.ID != "" {
			editorID = cachedPg.LastEditedBy.ID
		}
		if cachedPg.LastEditedBy.Name != "" && a.userNames == nil {
			a.userNames = map[string]string{}
		}
		if cachedPg.LastEditedBy.Name != "" {
			a.userNames[cachedPg.LastEditedBy.ID] = cachedPg.LastEditedBy.Name
		}
	}
	words := 0
	if pn.doc != nil {
		words = countWords(pn.doc.Text())
	} else if len(cachedBlocks) > 0 {
		for _, b := range cachedBlocks {
			words += len(strings.Fields(notion.PlainText(b.Text().RichText)))
		}
	}
	mins := max(1, (words+199)/200)
	if words == 0 {
		mins = 0
	}
	editorName := a.userNameByID(editorID)
	if editorName == "" {
		editorName = "-"
	}
	editedLabel := "-"
	if !editedTime.IsZero() {
		editedLabel = ago(editedTime)
	}

	ui.Column(c).FillWidth().Gap(6).Padding(0, 6).Children(func() {
		ui.Text(c, tr.T("Info")).FontSize(13).FontWeight(600).TextColor(p.Faint).Padding(0, 0, 2, 0)
		infoRow := func(label, val string) {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
				ui.Text(c, label).FontSize(12).TextColor(p.Faint).Grow(1)
				ui.Text(c, val).FontSize(12).TextColor(p.Muted).SingleLine().Shrink(1)
			})
		}
		infoRow(tr.T("Diubah"), editedLabel)
		if a.bot != nil && editorID != "" && editorID == a.bot.ID {
			// Notion names the integration for what gotion sent.
			editorName = tr.T("kamu, lewat gotion")
		}
		infoRow(tr.T("Oleh"), editorName)
		lenStr := fmt.Sprintf(tr.T("%d kata"), words)
		if mins > 0 {
			lenStr += " · " + fmt.Sprintf(tr.T("%d mnt baca"), mins)
		}
		infoRow(tr.T("Panjang"), lenStr)
	})
}

func (a *App) tablePreviewKeys(c *ui.Context, i int, t *tableView) {
	if len(t.rows) == 0 {
		return
	}
	curIdx := -1
	for idx, r := range t.rows {
		if r.ID == t.selectedID {
			curIdx = idx
			break
		}
	}
	switch {
	case c.Shortcut(0, ui.KeyDown):
		if curIdx < len(t.rows)-1 {
			t.selectedID = t.rows[curIdx+1].ID
		} else if curIdx < 0 {
			t.selectedID = t.rows[0].ID
		}
	case c.Shortcut(0, ui.KeyUp):
		if curIdx > 0 {
			t.selectedID = t.rows[curIdx-1].ID
		} else if curIdx < 0 {
			t.selectedID = t.rows[0].ID
		}
	case c.Shortcut(0, ui.KeyEnter):
		if curIdx >= 0 {
			a.openRow(i, t, t.rows[curIdx])
		}
	}
}

func (a *App) ensurePreviewBlocks(t *tableView, pageID string) {
	if t.previewID == pageID {
		return
	}
	t.previewID = pageID
	t.previewBlocks = nil
	t.previewErr = ""
	if _, blocks := readCachedPage(pageID); len(blocks) > 0 {
		t.previewBlocks = blocks
		return
	}
	if a.client == nil {
		return
	}
	t.previewLoading = true
	client := a.client
	go func() {
		blocks, err := client.Children(context.Background(), pageID)
		a.update(func() {
			if t.previewID != pageID {
				return
			}
			t.previewLoading = false
			if err != nil {
				t.previewErr = errText(err)
				return
			}
			t.previewBlocks = blocks
		})
	}()
}

func (a *App) rightPaneTable(c *ui.Context, i int, t *tableView) {
	p := a.pal()
	accentBg, accentFg := tintColors(p.Accent, a.dark)

	var sel *notion.Page
	for _, r := range t.rows {
		if r.ID == t.selectedID {
			sel = r
			break
		}
	}
	ui.Text(c, tr.T("Pratinjau baris")).FontSize(13).FontWeight(600).TextColor(p.Faint).Padding(0, 4)
	if sel == nil {
		ui.Text(c, tr.T("Pilih baris untuk melihat pratinjau.")).FontSize(12).TextColor(p.Faint).Padding(0, 4)
		return
	}

	a.ensurePreviewBlocks(t, sel.ID)

	ui.Column(c.Key("prev-"+sel.ID)).FillWidth().Padding(14).Gap(12).Radius(10).
		Background(paneCardBg(a.dark)).Border(1, p.Line).Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
			if em := sel.Emoji(); em != "" {
				emoji.View(c, em, 20)
			}
			ui.Text(c, nonEmpty(sel.Title(), tr.T("Tanpa judul"))).FontSize(16).FontWeight(600).MaxLines(2).Grow(1).MinWidth(0)
		})

		// Properties (editable)
		ui.Column(c).FillWidth().Gap(4).Children(func() {
			for _, col := range t.columns {
				if col.Type == "title" {
					continue
				}
				raw := sel.Properties[col.Name]
				row := ui.Row(c.Key("pv-"+col.Name)).FillWidth().AlignItems(ui.Center).Gap(8).Padding(3, 4).Radius(6)
				a.editable(c, t, sel, col, row, func() {
					ui.Text(c, col.Name).FontSize(12).TextColor(p.Faint).Width(86).SingleLine().Shrink(0)
					ui.Box(c).Grow(1).MinWidth(0).Children(func() {
						a.value(c, raw, col.Type)
					})
				})
			}
		})

		// Action buttons
		ui.Row(c).FillWidth().Gap(8).Wrap().Children(func() {
			openBtn := ui.ButtonBase(c.Key("prev-open")).Role(ui.RoleButton).Label(tr.T("Buka halaman")).
				Padding(6, 12).Radius(7).Background(accentBg).Cursor(ui.CursorPointer)
			openBtn.Children(func() {
				ui.Text(c, tr.T("Buka halaman")).FontSize(12).FontWeight(600).TextColor(accentFg)
			})
			if openBtn.Clicked() {
				a.openRow(i, t, sel)
			}

			besideBtn := ui.ButtonBase(c.Key("prev-beside")).Role(ui.RoleButton).Label(tr.T("Buka di samping")).
				Padding(6, 12).Radius(7).Border(1, p.Line).Cursor(ui.CursorPointer)
			if besideBtn.Hovered() {
				besideBtn.Background(p.Hover)
			}
			besideBtn.Children(func() {
				ui.Text(c, tr.T("Buka di samping")).FontSize(12).FontWeight(500).TextColor(p.Text)
			})
			if besideBtn.Clicked() {
				a.openRow(-1, t, sel)
			}
		})
	})

	// Content snippet (first ~3 paragraphs)
	ui.Column(c).FillWidth().Padding(12).Gap(8).Radius(10).Background(paneCardBg(a.dark)).Border(1, p.Line).Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(6).Children(func() {
			ui.Text(c, tr.T("Isi halaman")).FontSize(12).FontWeight(600).TextColor(p.Faint).Grow(1)
			if t.previewLoading {
				ui.Spinner(c).Size(12, 12)
			}
		})
		paras := previewParagraphs(t.previewBlocks, 3)
		switch {
		case t.previewLoading && len(paras) == 0:
			ui.Text(c, tr.T("Memuat…")).FontSize(12).TextColor(p.Faint)
		case len(paras) == 0:
			ui.Text(c, tr.T("Halaman ini masih kosong.")).FontSize(12).TextColor(p.Faint)
		default:
			for idx, para := range paras {
				ui.Text(c, para).FontSize(13).TextColor(p.Muted).MaxLines(3).Key(fmt.Sprintf("pp-%d", idx))
			}
		}
	})
}

func (a *App) rightPaneToday(c *ui.Context) {
	p := a.pal()
	now := nowFunc().In(time.Local)
	tomorrow := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, now.Location())

	var tomorrowTasks []todayItem
	for _, it := range a.today.items {
		if it.Page == nil || (it.DoneCol != "" && it.DoneProp.IsComplete(it.Page.Properties[it.DoneCol])) {
			continue
		}
		if d, ok := parseLocalDate(it.Page.Properties[it.DateCol], now.Location()); ok && d.Equal(tomorrow) {
			tomorrowTasks = append(tomorrowTasks, it)
		}
	}
	sortTodayUrgent(tomorrowTasks, now)

	// 1. Besok · N
	ui.Column(c).FillWidth().Gap(8).Children(func() {
		ui.Text(c, tr.T("Besok")+" · "+strconv.Itoa(len(tomorrowTasks))).FontSize(13).FontWeight(600).TextColor(p.Faint).Padding(0, 4)
		if len(tomorrowTasks) == 0 {
			ui.Text(c, tr.T("Tidak ada tugas yang menunggu.")).FontSize(12).TextColor(p.Faint).Padding(0, 4)
		} else {
			for _, it := range tomorrowTasks {
				pg := it.Page
				card := ui.Row(c.Key("tmr-"+pg.ID)).FillWidth().AlignItems(ui.Center).Gap(8).Padding(9, 10).
					Radius(9).Background(paneCardBg(a.dark)).Border(1, p.Line).Cursor(ui.CursorPointer)
				own := false
				card.Children(func() {
					if it.DoneCol != "" && it.DoneProp.Editable() {
						chk := ui.ButtonBase(c.Key("tmrchk-"+pg.ID)).Role(ui.RoleCheckBox).Label(tr.T("Selesai")).
							Size(16, 16).Radius(8).Border(1.5, p.Faint).Shrink(0).Cursor(ui.CursorPointer)
						if chk.Hovered() {
							chk.Border(1.5, p.Accent)
						}
						if chk.Clicked() {
							own = true
							a.completeTodayItem(it)
						}
					}
					ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
						ui.Text(c, nonEmpty(pg.Title(), tr.T("Tanpa judul"))).FontSize(13).FontWeight(500).SingleLine()
						if it.DBTitle != "" {
							ui.Text(c, it.DBTitle).FontSize(11).TextColor(p.Faint).SingleLine()
						}
					})
				})
				if card.Clicked() && !own {
					a.openRow(a.focus, &tableView{id: it.DBID}, pg)
				}
			}
		}
	})

	// 2. Baru saja diubah (top 5 pages by LastEditedTime)
	var recent []*node
	for _, n := range a.nodes {
		if n != nil && n.Kind == "page" && !isSpecial(n.Kind) && !n.Edited.IsZero() {
			recent = append(recent, n)
		}
	}
	sort.SliceStable(recent, func(i, j int) bool {
		if !recent[i].Edited.Equal(recent[j].Edited) {
			return recent[i].Edited.After(recent[j].Edited)
		}
		if recent[i].Title != recent[j].Title {
			return recent[i].Title < recent[j].Title
		}
		return recent[i].ID < recent[j].ID
	})
	if len(recent) > 5 {
		recent = recent[:5]
	}
	if len(recent) > 0 {
		ui.Column(c).FillWidth().Gap(4).Children(func() {
			ui.Text(c, tr.T("Baru saja diubah")).FontSize(13).FontWeight(600).TextColor(p.Faint).Padding(0, 4, 2, 4)
			for _, n := range recent {
				row := ui.Row(c.Key("rec-"+n.ID)).FillWidth().AlignItems(ui.Center).Gap(8).
					Padding(6, 8).Radius(7).Cursor(ui.CursorPointer)
				if row.Hovered() {
					row.Background(p.Hover)
				}
				row.Children(func() {
					if n.Emoji != "" {
						emoji.View(c, n.Emoji, 14)
					} else {
						ui.Icon(c, iconPage).Size(13, 13).TextColor(p.Faint)
					}
					ui.Text(c, nonEmpty(n.Title, tr.T("Tanpa judul"))).FontSize(13).TextColor(p.Text).SingleLine().Grow(1).MinWidth(0)
					ui.Text(c, ago(n.Edited)).FontSize(11).TextColor(p.Faint).Shrink(0)
				})
				if row.Clicked() {
					a.open(n, a.focus)
				}
			}
		})
	}
}
