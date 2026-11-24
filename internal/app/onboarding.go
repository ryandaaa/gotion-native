package app

import (
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

// onboarding is the first screen: welcome, and sign in.
func (a *App) onboarding(c *ui.Context) {
	p := a.pal()
	oauth := oauthURL() != ""
	ui.Column(c).Grow(1).Background(p.Window).Children(func() {
		// The language comes first: whoever reads neither finds theirs.
		ui.Row(c).Padding(14, 18).Children(func() {
			ui.Box(c).Grow(1)
			a.languagePicker(c)
		})
		ui.Column(c).Grow(1).Center().Padding(0, 16, 40, 16).Children(func() {
			ui.Column(c).Width(440).MaxWidthPercent(100).Padding(32).Gap(20).Background(p.Panel).Radius(16).
				Border(1, p.Line).Children(func() {
				a.mark(c, 52)
				ui.Column(c).Gap(8).Children(func() {
					ui.Text(c, tr.T("Selamat datang di gotion")).FontSize(24).FontWeight(700)
					ui.Text(c, tr.T("Baca dan tulis halaman Notion-mu di aplikasi desktop. Yang kamu ketik sampai ke Notion beberapa detik kemudian.")).
						FontSize(14).LineHeight(1.5).TextColor(p.Muted)
				})
				if oauth {
					a.signInWithNotion(c)
					a.tokenFold(c)
				} else {
					a.tokenSteps(c)
				}
				ui.Text(c, tr.T("Token login disimpan di keychain komputer ini. gotion tidak berafiliasi dengan Notion Labs, Inc.")).
					FontSize(12).LineHeight(1.45).TextColor(p.Faint)
			})
		})
	})
}

// signInWithNotion is the one button most people need, and its states:
// waiting for the browser, checking, failed.
func (a *App) signInWithNotion(c *ui.Context) {
	p := a.pal()
	ui.Column(c).Gap(10).Children(func() {
		switch {
		case a.oauth != nil:
			ui.Row(c).Gap(10).AlignItems(ui.Center).Padding(10, 12).Radius(10).Background(p.Sidebar).Children(func() {
				ui.Spinner(c).Size(16, 16)
				ui.Text(c, tr.T("Lanjutkan di browser: pilih halaman, lalu klik Allow.")).FontSize(14).Grow(1).MinWidth(0)
			})
			ui.Row(c).Gap(8).Wrap().AlignItems(ui.Center).Children(func() {
				if a.btn(c, tr.T("Buka lagi halaman Notion"), nil, false, false) {
					openBrowser(a.oauth.url)
				}
				if a.btn(c, tr.T("Batal"), nil, false, false) {
					a.cancelOAuth()
				}
			})
		case a.checking:
			ui.Row(c).Gap(10).AlignItems(ui.Center).Padding(10, 12).Children(func() {
				ui.Spinner(c).Size(16, 16)
				ui.Text(c, tr.T("Menghubungkan ke workspace…")).FontSize(14).TextColor(p.Muted)
			})
		default:
			label := tr.T("Masuk dengan Notion")
			if a.tokenErr != "" && !a.tokenOpen {
				label = tr.T("Coba masuk lagi")
			}
			if ui.PrimaryButton(c, label).Padding(12, 16).FillWidth().Clicked() {
				a.startOAuth()
			}
			if a.tokenErr != "" && !a.tokenOpen {
				ui.Text(c, a.tokenErr).FontSize(13).TextColor(c.Theme().Danger)
			}
			ui.Text(c, tr.T("Notion akan menanyakan halaman mana yang boleh dibuka gotion. Halaman lain bisa ditambahkan kapan saja.")).
				FontSize(13).LineHeight(1.45).TextColor(p.Muted)
		}
	})
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
