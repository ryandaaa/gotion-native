package app

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"gotion/internal/tr"
)

// The way in for someone who has never used gotion: a welcome, one button
// that signs in through Notion's own page, and what to do when no page
// was shared. The integration token stays for those who want it, folded
// away.

// mark draws gotion's icon: a heading, two lines and the caret.
func (a *App) mark(c *ui.Context, size float32) {
	p := a.pal()
	k := size / 64
	ui.Box(c).Size(size, size).Shrink(0).Radius(14*k).Background(ui.Hex("#FFFFFF")).Border(2*k, ui.Hex("#E4E2DF")).Children(func() {
		bar := func(x, y, w, h float32, col ui.Color) {
			ui.Box(c).Absolute().Left(x*k).Top(y*k).Size(w*k, h*k).Radius(h * k / 2).Background(col)
		}
		bar(14, 16, 30, 7, ui.Hex("#1D1D1F"))
		bar(14, 29, 36, 5, ui.Hex("#B5B2AD"))
		bar(14, 40, 26, 5, ui.Hex("#B5B2AD"))
		bar(43, 38, 2.5, 9, ui.Hex("#2563EB"))
	})
	_ = p
}

// onboarding is the first screen, split in two: on the left a drawing on
// the wash, on the right a card that signs in, step by step: connect, then
// choose the pages in Notion's own page.
func (a *App) onboarding(c *ui.Context) {
	p := a.pal()
	w := washByID(a.set.Wash)
	ui.Row(c).Grow(1).MinHeight(0).AlignItems(ui.Stretch).Background(p.Window).Children(func() {
		// The drawing's side: hidden when the window is too narrow for both.
		if ww, _ := c.Size(); ww >= 860 {
			left := ui.Column(c.Key("onb-art")).WidthPercent(54).Shrink(0).Padding(36, 44).Gap(16)
			if w.plain() {
				left.Background(p.Sidebar)
			} else {
				left.LinearGradient(w.base(a.dark))
			}
			left.Children(func() {
				ui.Column(c).Grow(1).MinHeight(0).Center().Children(func() {
					ui.Image(c, welcomeArt(p.Accent, a.dark)).Size(460, 372).MaxWidthPercent(100)
				})
				ui.Column(c).Gap(8).MaxWidth(520).Children(func() {
					ui.Text(c, tr.T("Notion-mu, sekarang di desktop.")).FontSize(28).FontWeight(700).LineHeight(1.2)
					ui.Text(c, tr.T("Buka halaman dalam sekejap, tetap bisa menulis saat offline, dan lihat tugas hari ini tanpa membuka browser.")).
						FontSize(15).LineHeight(1.6).TextColor(p.Muted)
				})
			})
		}
		ui.Column(c).Grow(1).MinWidth(0).Children(func() {
			ui.Row(c).Padding(14, 18).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Box(c).Grow(1)
				a.themeButton(c)
				a.languagePicker(c)
			})
			ui.Column(c).Grow(1).Center().Padding(0, 24, 40, 24).Children(func() {
				a.signInCard(c)
			})
		})
	})
}

// onboardingStep is where signing in is: 1 to connect, 2 while Notion's
// page is open in the browser or the workspace is checked.
func (a *App) onboardingStep() int {
	if a.oauth != nil || a.checking {
		return 2
	}
	return 1
}

// The step's content slides in from the side it goes to and out the other,
// through a blur; what it holds rises one after another.
var stepTransition = ui.ElementTransition{
	Animation:  &ui.Snappy,
	Enter:      &ui.Motion{X: 36, Opacity: 0, Blur: 6, Scale: 0.985},
	AfterExits: true,
}

func riseIn(i int) ui.ElementTransition {
	an := ui.Snappy
	an.Delay = time.Duration(60+70*i) * time.Millisecond
	return ui.ElementTransition{Animation: &an, Enter: &ui.Motion{Y: 10, Opacity: 0}}
}

// signInCard is the floating card: the steps, and what the current one asks.
func (a *App) signInCard(c *ui.Context) {
	p := a.pal()
	step := a.onboardingStep()
	dir := float32(1)
	if step < a.onbLastStep {
		dir = -1
	}
	a.onbLastStep = step
	card := ui.Column(c.Key("signin-card")).Width(400).MaxWidthPercent(100).Padding(30).Gap(22).
		Background(p.Panel).Radius(18).Border(1, p.Line).Clip().
		Shadow(0, 24, 60, 0, ui.RGBA(0, 0, 0, 0.12)).
		Transition(ui.ElementTransition{Animation: &ui.Smooth, Enter: &ui.Motion{Y: 18, Opacity: 0, Scale: 0.98}})
	card.Children(func() {
		a.onboardingSteps(c, step)
		body := ui.Column(c.Key("signin-body")).FillWidth().ChildExit(ui.Motion{X: -36 * dir, Opacity: 0, Blur: 6})
		body.Children(func() {
			tr0 := stepTransition
			enter := *tr0.Enter
			enter.X = 36 * dir
			tr0.Enter = &enter
			ui.Column(c.Key(fmt.Sprintf("step-%d", step))).FillWidth().Gap(18).Transition(tr0).Children(func() {
				if step == 1 {
					a.stepConnect(c)
				} else {
					a.stepChoose(c)
				}
			})
		})
		ui.Text(c, tr.T("Token login disimpan di keychain komputer ini. gotion tidak berafiliasi dengan Notion Labs, Inc.")).
			FontSize(12).LineHeight(1.45).TextColor(p.Faint)
	})
}

// onboardingSteps is the row of steps: a number, a check once done, and a
// line between them that fills as the first is done.
func (a *App) onboardingSteps(c *ui.Context, step int) {
	p := a.pal()
	tintBg, tintFg := tintColors(p.Accent, a.dark)
	dot := func(n int, label string) {
		done, cur := step > n, step == n
		ui.Row(c).Gap(8).AlignItems(ui.Center).Shrink(0).Children(func() {
			bg := p.Hover
			switch {
			case done:
				bg = p.Accent
			case cur:
				bg = tintBg
			}
			// Keyed by its state: a dot that turns done comes in anew, popping.
			ui.Box(c.Key(fmt.Sprintf("dot-%d-%v", n, done))).Size(24, 24).Radius(12).Center().Background(bg).
				Transition(ui.ElementTransition{Animation: &ui.Bouncy, Enter: &ui.Motion{Scale: 0.4, Opacity: 0}}).
				Children(func() {
					switch {
					case done:
						ui.Icon(c, iconCheck).Size(13, 13).TextColor(ui.Hex("#FFFFFF"))
					case cur:
						ui.Text(c, fmt.Sprint(n)).FontSize(12).FontWeight(700).TextColor(tintFg)
					default:
						ui.Text(c, fmt.Sprint(n)).FontSize(12).FontWeight(700).TextColor(p.Faint)
					}
				})
			t := ui.Text(c, label).FontSize(13).SingleLine()
			if cur {
				t.FontWeight(600)
			} else if !done {
				t.TextColor(p.Faint)
			}
		})
	}
	ui.Row(c).FillWidth().Gap(10).AlignItems(ui.Center).Children(func() {
		dot(1, tr.T("Sambungkan"))
		track := ui.Box(c.Key("step-line")).Grow(1).Height(2).Radius(1).Background(p.Hover).Clip()
		var target float32
		if step > 1 {
			target = 1
		}
		f := track.AnimateTo("fill", target, ui.Smooth)
		track.Children(func() {
			ui.Box(c).Height(2).WidthPercent(f * 100).Background(p.Accent)
		})
		dot(2, tr.T("Pilih halaman"))
	})
}

// solidAccent is the accent dark enough under white text (4.5:1), or, on a
// dark theme, the accent itself under dark text.
func solidAccent(accent ui.Color, isDark bool) (bg, fg ui.Color) {
	if isDark {
		return accent, ui.Hex("#1C1917")
	}
	bg = accent
	white := ui.Hex("#FFFFFF")
	for i := 0; i < 24 && contrastRatio(bg, white) < 4.6; i++ {
		bg = ui.Color{R: uint8(float32(bg.R) * 0.93), G: uint8(float32(bg.G) * 0.93), B: uint8(float32(bg.B) * 0.93), A: 255}
	}
	return bg, white
}

// stepConnect is step 1: the welcome and the one button most people need.
func (a *App) stepConnect(c *ui.Context) {
	p := a.pal()
	ui.Column(c).Gap(8).Transition(riseIn(0)).Children(func() {
		ui.Text(c, tr.T("Selamat datang di gotion")).FontSize(24).FontWeight(700)
		ui.Text(c, tr.T("Sambungkan workspace Notion-mu. Yang kamu ketik di sini sampai ke Notion beberapa detik kemudian.")).
			FontSize(14).LineHeight(1.5).TextColor(p.Muted)
	})
	if oauthURL() == "" {
		ui.Column(c).Transition(riseIn(1)).Children(func() { a.tokenSteps(c) })
		return
	}
	bg, fg := solidAccent(p.Accent, a.dark)
	label := tr.T("Masuk dengan Notion")
	if a.tokenErr != "" && !a.tokenOpen {
		label = tr.T("Coba masuk lagi")
	}
	b := ui.ButtonBase(c.Key("onb-signin")).Role(ui.RoleButton).Label(label).FillWidth().Height(46).Radius(12).
		Background(bg).Center().Cursor(ui.CursorPointer).Transition(riseIn(1))
	if b.Pressed() {
		b.AnimatePose(ui.Pose{Scale: 0.97}, ui.Snappy)
	} else {
		b.AnimatePose(ui.Pose{}, ui.Snappy)
	}
	b.Children(func() {
		ui.Text(c, label).FontSize(15).FontWeight(600).TextColor(fg)
	})
	if b.Clicked() {
		a.startOAuth()
	}
	if a.tokenErr != "" && !a.tokenOpen {
		ui.Text(c, a.tokenErr).FontSize(13).TextColor(c.Theme().Danger)
	}
	ui.Column(c).Transition(riseIn(2)).Children(func() { a.tokenFold(c) })
}

// stepChoose is step 2: Notion's page is open in the browser, or the
// workspace is being checked.
func (a *App) stepChoose(c *ui.Context) {
	p := a.pal()
	tintBg, tintFg := tintColors(p.Accent, a.dark)
	ui.Column(c).Gap(8).Transition(riseIn(0)).Children(func() {
		ui.Text(c, tr.T("Pilih halaman di Notion")).FontSize(22).FontWeight(700)
		ui.Text(c, tr.T("Browser sudah terbuka. Selesaikan di sana, lalu kembali ke sini.")).
			FontSize(14).LineHeight(1.5).TextColor(p.Muted)
	})
	ui.Column(c).Gap(10).Padding(14).Radius(12).Background(p.Sidebar).Transition(riseIn(1)).Children(func() {
		for i, line := range []string{tr.T("Centang halaman yang boleh dibuka gotion"), tr.T("Klik Allow access")} {
			ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
				ui.Box(c).Size(20, 20).Radius(10).Shrink(0).Background(p.Panel).Border(1, p.Line).Center().Children(func() {
					ui.Text(c, string(rune('a'+i))).FontSize(11).FontWeight(700).TextColor(p.Muted)
				})
				ui.Text(c, line).FontSize(14).Grow(1).MinWidth(0)
			})
		}
	})
	ui.Row(c).Gap(10).AlignItems(ui.Center).Transition(riseIn(2)).Children(func() {
		ui.Spinner(c).Size(16, 16)
		msg := tr.T("Menunggu Notion…")
		if a.checking {
			msg = tr.T("Menghubungkan ke workspace…")
		}
		ui.Text(c, msg).FontSize(14).TextColor(p.Muted)
	})
	if a.oauth != nil {
		ui.Row(c).Gap(8).Wrap().Transition(riseIn(3)).Children(func() {
			again := ui.ButtonBase(c.Key("onb-again")).Role(ui.RoleButton).Label(tr.T("Buka lagi halaman Notion")).
				Padding(9, 14).Radius(10).Background(tintBg).Cursor(ui.CursorPointer)
			again.Children(func() {
				ui.Text(c, tr.T("Buka lagi halaman Notion")).FontSize(14).FontWeight(600).TextColor(tintFg)
			})
			if again.Clicked() {
				openBrowser(a.oauth.url)
			}
			if a.btn(c, tr.T("Kembali"), nil, false, false) {
				a.cancelOAuth()
			}
		})
	}
}

// tokenFold is the integration token, for those who prefer it, folded.
func (a *App) tokenFold(c *ui.Context) {
	p := a.pal()
	ui.Column(c).Gap(14).Children(func() {
		h := ui.Row(c.Key("token-fold")).Gap(6).AlignItems(ui.Center).Cursor(ui.CursorPointer).Focusable().
			Label(tr.T("Pakai token integrasi"))
		h.Children(func() {
			chev := iconChevron
			if a.tokenOpen {
				chev = iconChevronDown
			}
			ui.Icon(c, chev).Size(14, 14).TextColor(p.Muted)
			t := ui.Text(c, tr.T("Pakai token integrasi")).FontSize(13).TextColor(p.Muted)
			if h.Hovered() {
				t.TextColor(p.Text)
			}
		})
		if h.Clicked() {
			a.tokenOpen = !a.tokenOpen
		}
		if a.tokenOpen {
			a.tokenSteps(c)
		}
	})
}

// tokenSteps signs in with an internal integration's token.
func (a *App) tokenSteps(c *ui.Context) {
	p := a.pal()
	step := func(n string, body func()) {
		ui.Row(c).Gap(12).AlignItems(ui.Start).Children(func() {
			ui.Box(c).Size(22, 22).Shrink(0).Radius(11).Border(1, p.Line).Center().Children(func() {
				ui.Text(c, n).FontSize(12).FontWeight(600).TextColor(p.Muted)
			})
			ui.Column(c).Grow(1).MinWidth(0).Gap(8).Children(body)
		})
	}
	ui.Column(c).Gap(16).Children(func() {
		step("1", func() {
			ui.Text(c, tr.T("Buat integrasi baru di Notion")).FontSize(14)
			if ui.Link(c, tr.T("Buka halaman integrasi ↗"), "").FontWeight(500).Clicked() {
				go mygo.Shell.OpenExternal("https://www.notion.so/profile/integrations")
			}
		})
		step("2", func() {
			ui.Text(c, tr.T("Tempel Internal Integration Secret")).FontSize(14)
			ui.Row(c).Gap(8).Children(func() {
				in := ui.TextInput(c, &a.tokenInput).Password().Placeholder("ntn_…").Grow(1).Label("Token").Border(1, p.Faint.Alpha(0.5))
				if (in.Submitted() || a.btn(c, tr.T("Cek"), nil, false, a.checking)) && !a.checking {
					a.checkToken()
				}
			})
			switch {
			case a.checking:
				ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
					ui.Spinner(c).Size(14, 14)
					ui.Text(c, tr.T("Mengecek…")).FontSize(13).TextColor(p.Muted)
				})
			case a.tokenErr != "":
				ui.Text(c, a.tokenErr).FontSize(13).TextColor(c.Theme().Danger)
			}
		})
		step("3", func() {
			ui.Text(c, tr.T("Di Notion, buka page yang mau dipakai → ••• → Connections → pilih integrasi ini. Sub-page ikut otomatis.")).
				FontSize(14).LineHeight(1.45).TextColor(p.Muted)
		})
	})
}

// noPages is the main area while the workspace shows no page: loading,
// failed, or none shared yet, with the way to share some.
func (a *App) noPages(c *ui.Context) {
	p := a.pal()
	ui.Column(c).Fill().Center().Padding(24).Children(func() {
		ui.Column(c).Width(420).MaxWidthPercent(100).Gap(14).Children(func() {
			switch {
			case a.loadErr != "":
				ui.Text(c, tr.T("Halaman tidak bisa dimuat")).FontSize(18).FontWeight(600)
				ui.Text(c, a.loadErr).FontSize(14).TextColor(c.Theme().Danger)
				if a.btn(c, tr.T("Coba lagi"), iconRefresh, false, false) {
					a.refresh()
				}
			case !a.searched:
				ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
					ui.Spinner(c).Size(16, 16)
					ui.Text(c, tr.T("Memuat halaman dari Notion…")).FontSize(14).TextColor(p.Muted)
				})
			default:
				ui.Text(c, tr.T("Belum ada halaman yang dibagikan ke gotion")).FontSize(18).FontWeight(600)
				if a.client != nil && a.client.Refresh != nil {
					ui.Text(c, tr.T("Pilih halaman di Notion, dan semua sub-halamannya ikut muncul di sini.")).
						FontSize(14).LineHeight(1.5).TextColor(p.Muted)
					ui.Row(c).Gap(8).Wrap().Children(func() {
						if ui.PrimaryButton(c, tr.T("Pilih halaman di Notion")).Padding(10, 14).Clicked() {
							a.startOAuth()
						}
						if a.btn(c, tr.T("Muat ulang"), iconRefresh, false, a.loading) {
							a.refresh()
						}
					})
					if a.oauth != nil {
						ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
							ui.Spinner(c).Size(14, 14)
							ui.Text(c, tr.T("Lanjutkan di browser: pilih halaman, lalu klik Allow.")).FontSize(13).TextColor(p.Muted)
						})
					}
				} else {
					ui.Text(c, tr.T("Di Notion, buka page yang mau dipakai → ••• → Connections → pilih integrasi ini. Sub-page ikut otomatis.")).
						FontSize(14).LineHeight(1.5).TextColor(p.Muted)
					if a.btn(c, tr.T("Muat ulang"), iconRefresh, false, a.loading) {
						a.refresh()
					}
				}
			}
		})
	})
}

// welcomeArt is the drawing beside the sign-in: a smiling note typing, a
// cup and a card, its last line in the wash's accent. Parsed once per
// accent and theme, never per frame.
func welcomeArt(accent ui.Color, isDark bool) *ui.SVG {
	key := fmt.Sprintf("%02x%02x%02x-%v", accent.R, accent.G, accent.B, isDark)
	artMu.Lock()
	defer artMu.Unlock()
	if s := artCache[key]; s != nil {
		return s
	}
	ink, paper, fold := "#1C1917", "#FFFFFF", "#EDECEA"
	if isDark {
		ink, paper, fold = "#E7E5E4", "#F2F0EC", "#D9D6D1"
	}
	acc := fmt.Sprintf("#%02x%02x%02x", accent.R, accent.G, accent.B)
	r := strings.NewReplacer("INK", ink, "PAPER", paper, "FOLD", fold, "ACC", acc)
	s := ui.MustParseSVG([]byte(r.Replace(welcomeSVG)))
	artCache[key] = s
	return s
}

var (
	artMu    sync.Mutex
	artCache = map[string]*ui.SVG{}
)

const welcomeSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 520 420">
<ellipse cx="260" cy="392" rx="190" ry="16" fill="#000000" opacity="0.07"/>
<path d="M92 74 L97 88 L111 92 L97 96 L92 110 L87 96 L73 92 L87 88 Z" fill="#F4C95D"/>
<path d="M438 58 L441 67 L450 70 L441 73 L438 82 L435 73 L426 70 L435 67 Z" fill="#F29A63"/>
<circle cx="470" cy="190" r="5" fill="#a993ff"/>
<rect x="160" y="70" width="200" height="250" rx="22" fill="PAPER" stroke="INK" stroke-width="4"/>
<path d="M300 70 L360 130 L322 130 Q300 130 300 108 Z" fill="FOLD" stroke="INK" stroke-width="4" stroke-linejoin="round"/>
<circle cx="222" cy="168" r="9" fill="#1C1917"/><circle cx="290" cy="168" r="9" fill="#1C1917"/>
<circle cx="225" cy="165" r="3" fill="#FFFFFF"/><circle cx="293" cy="165" r="3" fill="#FFFFFF"/>
<ellipse cx="204" cy="196" rx="13" ry="8" fill="#F4A6A6" opacity="0.8"/><ellipse cx="308" cy="196" rx="13" ry="8" fill="#F4A6A6" opacity="0.8"/>
<path d="M240 194 Q256 210 272 194" fill="none" stroke="#1C1917" stroke-width="4" stroke-linecap="round"/>
<rect x="196" y="236" width="128" height="9" rx="4.5" fill="#D6D3D1"/>
<rect x="196" y="256" width="96" height="9" rx="4.5" fill="#D6D3D1"/>
<rect x="196" y="276" width="58" height="9" rx="4.5" fill="ACC"/>
<rect x="260" y="272" width="4" height="17" rx="2" fill="#1C1917"/>
<path d="M160 226 Q120 236 118 270" fill="none" stroke="INK" stroke-width="4" stroke-linecap="round"/>
<circle cx="118" cy="276" r="10" fill="PAPER" stroke="INK" stroke-width="4"/>
<path d="M360 226 Q398 222 408 196" fill="none" stroke="INK" stroke-width="4" stroke-linecap="round"/>
<circle cx="410" cy="190" r="10" fill="PAPER" stroke="INK" stroke-width="4"/>
<path d="M222 320 L218 356 M298 320 L302 356" stroke="INK" stroke-width="4" stroke-linecap="round"/>
<path d="M204 358 L230 358 M290 358 L316 358" stroke="INK" stroke-width="5" stroke-linecap="round"/>
<path d="M384 312 L392 380 Q394 388 402 388 L446 388 Q454 388 456 380 L464 312 Z" fill="#F29A63" stroke="INK" stroke-width="4" stroke-linejoin="round"/>
<path d="M464 328 Q488 330 486 350 Q484 368 460 366" fill="none" stroke="INK" stroke-width="4" stroke-linecap="round"/>
<path d="M410 296 Q402 284 410 272 M434 296 Q426 282 434 268" fill="none" stroke="#A8A29E" stroke-width="3" stroke-linecap="round"/>
<rect x="52" y="336" width="78" height="52" rx="8" fill="PAPER" stroke="INK" stroke-width="4"/>
<path d="M64 352 H118 M64 364 H104 M64 376 H92" stroke="#D6D3D1" stroke-width="4" stroke-linecap="round"/>
</svg>`
