package app

import (
	"sort"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"gotion/internal/emoji"
)

// switcherState is the quick switcher, Ctrl+K.
type switcherState struct {
	open  bool
	query string
	sel   int
}

func (a *App) openSwitcher() {
	a.switcher = switcherState{open: true}
}

// switcherMatches returns the pages whose title has the query, the
// recently edited first when the query is empty.
func (a *App) switcherMatches() []*node {
	q := strings.ToLower(strings.TrimSpace(a.switcher.query))
	var out []*node
	for _, n := range a.nodes {
		if q == "" || strings.Contains(strings.ToLower(n.Title), q) {
			out = append(out, n)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if q != "" {
			pi := strings.HasPrefix(strings.ToLower(out[i].Title), q)
			pj := strings.HasPrefix(strings.ToLower(out[j].Title), q)
			if pi != pj {
				return pi
			}
		}
		return out[i].Edited.After(out[j].Edited)
	})
	if len(out) > 12 {
		out = out[:12]
	}
	return out
}

func (a *App) switcherView(c *ui.Context) {
	p := a.pal()
	s := &a.switcher
	matches := a.switcherMatches()
	if s.sel >= len(matches) {
		s.sel = max(0, len(matches)-1)
	}
	pick := func(n *node, beside bool) {
		s.open = false
		if beside {
			a.openBeside(n)
		} else {
			a.open(n, a.focus)
		}
	}
	scrim := ui.Column(c.Key("switcher")).Absolute().Left(0).Top(0).Right(0).Bottom(0).ZIndex(50).
		Background(ui.RGBA(0, 0, 0, 0.28)).AlignItems(ui.Center).Padding(90, 0, 0, 0).Modal().
		Transition(fadeIn)
	if scrim.Clicked() {
		s.open = false
	}
	scrim.Children(func() {
		ui.Column(c.Key("switcher-card")).Width(560).Background(p.Panel).Radius(12).Clip().Shadow(0, 16, 48, 0, ui.RGBA(0, 0, 0, 0.25)).
			TransformOrigin(0.5, 0).Transition(popIn).Children(func() {
			ui.Row(c).AlignItems(ui.Center).Gap(10).Padding(12, 16).BorderWidth(0, 0, 1, 0).BorderColor(p.Line).Children(func() {
				ui.Icon(c, iconSearch).Size(16, 16).TextColor(p.Faint)
				in := ui.TextInput(c, &s.query).Placeholder("Cari halaman atau database…").Grow(1).AutoFocus().
					Background(ui.Color{}).Border(0, ui.Color{}).Padding(0).FocusRing(false).FontSize(16).Label("Cari")
				in.OnTextKey(func(k ui.TextKey) bool {
					switch k.Key {
					case ui.KeyDown:
						s.sel = min(s.sel+1, max(0, len(matches)-1))
					case ui.KeyUp:
						s.sel = max(s.sel-1, 0)
					case ui.KeyEscape:
						s.open = false
					default:
						return false
					}
					return true
				})
				if in.Changed() {
					s.sel = 0
				}
				if in.Submitted() && len(matches) > 0 {
					pick(matches[s.sel], false)
				}
			})
			ui.Column(c).Padding(6).Gap(1).MaxHeight(440).Children(func() {
				if len(matches) == 0 {
					ui.Text(c, "Tidak ada yang cocok").FontSize(13).TextColor(p.Faint).Padding(12)
				}
				for i, n := range matches {
					row := ui.Row(c.Key("sw"+n.ID)).AlignItems(ui.Center).Gap(10).Padding(8, 10).Radius(8).Cursor(ui.CursorPointer)
					if i == s.sel || row.Hovered() {
						row.Background(p.Hover)
					}
					row.Children(func() {
						switch {
						case n.Emoji != "":
							emoji.View(c, n.Emoji, 16)
						case n.Kind != "page":
							ui.Icon(c, iconTable).Size(15, 15).TextColor(p.Faint)
						default:
							ui.Icon(c, iconPage).Size(15, 15).TextColor(p.Faint)
						}
						ui.Text(c, n.Title).FontSize(14).SingleLine().MinWidth(0)
						var crumb []string
						for _, up := range a.path(n.ID) {
							crumb = append(crumb, up.Title)
						}
						ui.Text(c, strings.Join(crumb, " / ")).FontSize(12).TextColor(p.Faint).SingleLine().MinWidth(0).Grow(1)
						ui.Text(c, ago(n.Edited)).FontSize(12).TextColor(p.Faint)
					})
					if row.Clicked() {
						pick(n, row.ClickModifiers()&ui.Ctrl != 0)
					}
				}
			})
			ui.Text(c, "↑↓ pilih · Enter buka · Ctrl+klik buka di samping · Esc tutup").FontSize(11).TextColor(p.Faint).Padding(8, 16)
		})
	})
}

// fadeIn and popIn are how overlays come and go: the backdrop fades, the
// card fades while it grows a little and comes down, as a sheet settles.
var (
	fadeIn = ui.ElementTransition{Duration: 160 * time.Millisecond, Ease: ui.EaseOut,
		Enter: &ui.Motion{Opacity: 0}, Exit: &ui.Motion{Opacity: 0}, ExitDuration: 120 * time.Millisecond}
	popIn = ui.ElementTransition{Duration: 200 * time.Millisecond, Ease: ui.EaseOut,
		Enter: &ui.Motion{Opacity: 0, Scale: 0.96, Y: -10}, Exit: &ui.Motion{Opacity: 0, Scale: 0.98, Y: -6},
		ExitDuration: 120 * time.Millisecond}
)
