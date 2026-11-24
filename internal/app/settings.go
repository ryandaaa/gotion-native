package app

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// Version is gotion's, set at build time with
// -ldflags "-X gotion/internal/app.Version=…".
var Version = "0.1.0-dev"

// settingsView is the settings dialog: what shows on the left, and the
// section open.
type settingsView struct {
	open    bool
	section string
	confirm bool // asking before signing out
}

func (a *App) openSettings(section string) {
	if section == "" {
		section = "tampilan"
	}
	a.settings = settingsView{open: true, section: section}
}

// appearance is the controls of how the window looks: the mode, the color
// behind the cards and the glass. The theme popover shows them too.
func (a *App) appearance(c *ui.Context) {
	p := a.pal()
	ui.Column(c).Gap(8).Children(func() {
		ui.Text(c, "Mode").FontSize(12).FontWeight(600).TextColor(p.Faint)
		ui.Row(c).Gap(2).Padding(2).Radius(8).Background(p.Sidebar).Children(func() {
			for _, m := range []struct{ id, label string }{{"system", "Sistem"}, {"light", "Terang"}, {"dark", "Gelap"}} {
				o := ui.Text(c.Key("mode"+m.id), m.label).FontSize(13).Padding(5, 0).Grow(1).TextAlign(ui.Center).Radius(6).Cursor(ui.CursorPointer)
				if a.set.Theme == m.id || a.set.Theme == "" && m.id == "system" {
					o.Background(p.Panel).FontWeight(600).Shadow(0, 1, 2, 0, ui.RGBA(0, 0, 0, 0.08))
				} else {
					o.TextColor(p.Muted)
				}
				if o.Clicked() {
					a.set.Theme = m.id
					a.saveSettings()
				}
			}
		})
	})
	ui.Column(c).Gap(8).Children(func() {
		ui.Text(c, "Warna latar").FontSize(12).FontWeight(600).TextColor(p.Faint)
		ui.Row(c).Wrap().Gap(8).Children(func() {
			for _, w := range washes {
				cur := a.set.Wash == w.id || a.set.Wash == "" && w.plain()
				if a.swatch(c, w, cur).Clicked() {
					a.set.Wash = w.id
					a.saveSettings()
				}
			}
		})
		ui.Text(c, washByID(a.set.Wash).label).FontSize(12).TextColor(p.Muted)
	})
	ui.Column(c).Gap(6).Children(func() {
		on := a.set.Glass
		ok := glassSupported()
		if ui.Checkbox(c, &on, "Kaca (blur) di sekitar kartu").Disabled(!ok).Changed() && ok {
			a.set.Glass = on
			a.saveSettings()
			a.applyGlass()
		}
		if !ok {
			ui.Text(c, "Belum didukung di Linux: sistemnya tidak menyediakan efek kaca. Tersedia di macOS dan Windows 11.").
				FontSize(11).TextColor(p.Faint)
		}
	})
}

// settingsDialog is the settings, over the window.
func (a *App) settingsDialog(c *ui.Context) {
	p := a.pal()
	s := &a.settings
	scrim := ui.Column(c.Key("settings")).Absolute().Left(0).Top(0).Right(0).Bottom(0).ZIndex(55).
		Background(ui.RGBA(0, 0, 0, 0.28)).Center().Modal().Transition(fadeIn)
	// An overlay takes Escape wherever the focus is inside it.
	if scrim.OverlayShortcut(0, ui.KeyEscape) {
		s.open = false
	}
	inside := false
	scrim.Children(func() {
		card := ui.Row(c.Key("settings-card")).Width(720).Height(520).Background(p.Panel).Radius(14).Clip().
			Shadow(0, 16, 48, 0, ui.RGBA(0, 0, 0, 0.25)).Transition(popIn).AlignItems(ui.Stretch)
		if card.PressedInside() {
			inside = true
		}
		card.Children(func() {
			ui.Column(c).Width(190).Shrink(0).Padding(18, 10).Gap(2).Background(p.Sidebar).Children(func() {
				ui.Text(c, "Pengaturan").FontSize(15).FontWeight(600).Padding(0, 10, 12, 10)
				for _, sec := range []struct{ id, label string }{{"tampilan", "Tampilan"}, {"akun", "Akun"}, {"tentang", "Tentang"}} {
					r := ui.Text(c.Key("sec"+sec.id), sec.label).FontSize(14).Padding(7, 10).Radius(7).Cursor(ui.CursorPointer)
					if s.section == sec.id {
						r.Background(p.Panel).FontWeight(600).Shadow(0, 1, 2, 0, ui.RGBA(0, 0, 0, 0.08))
					} else if r.Hovered() {
						r.Background(p.Hover)
					}
					if r.Clicked() {
						s.section, s.confirm = sec.id, false
					}
				}
			})
			ui.Column(c).Grow(1).MinWidth(0).Children(func() {
				ui.Row(c).Padding(14, 14, 0, 28).AlignItems(ui.Center).Children(func() {
					ui.Box(c).Grow(1)
					if iconButton(c, p, iconClose, "Tutup (Esc)") {
						s.open = false
					}
				})
				ui.Scroll(c).Grow(1).MinHeight(0).Children(func() {
					ui.Column(c).Padding(0, 32, 28, 28).Gap(22).Children(func() {
						switch s.section {
						case "akun":
							a.accountSection(c)
						case "tentang":
							a.aboutSection(c)
						default:
							a.appearanceSection(c)
						}
					})
				})
			})
		})
	})
	if scrim.Clicked() && !inside {
		s.open = false
	}
}

func (a *App) sectionTitle(c *ui.Context, title, sub string) {
	p := a.pal()
	ui.Column(c).Gap(4).Children(func() {
		ui.Text(c, title).FontSize(20).FontWeight(600)
		if sub != "" {
			ui.Text(c, sub).FontSize(13).TextColor(p.Muted)
		}
	})
}

func (a *App) appearanceSection(c *ui.Context) {
	p := a.pal()
	a.sectionTitle(c, "Tampilan", "Bagaimana jendela gotion terlihat.")
	a.appearance(c)
	ui.Column(c).Gap(8).Children(func() {
		ui.Text(c, "Zoom").FontSize(12).FontWeight(600).TextColor(p.Faint)
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			if ui.Button(c, "−").Width(36).Clicked() {
				a.zoom(-1)
			}
			ui.Text(c, fmt.Sprintf("%d%%", int(a.set.Zoom*100+0.5))).FontSize(14).Width(56).TextAlign(ui.Center)
			if ui.Button(c, "+").Width(36).Clicked() {
				a.zoom(+1)
			}
			if ui.Button(c, "Atur ulang").Disabled(a.set.Zoom > 0.99 && a.set.Zoom < 1.01).Clicked() {
				a.zoom(0)
			}
			ui.Text(c, "Ctrl+= · Ctrl+- · Ctrl+0").FontSize(12).TextColor(p.Faint)
		})
	})
	ui.Column(c).Gap(6).Children(func() {
		full := a.set.FullWidth
		if ui.Checkbox(c, &full, "Teks halaman selebar panel").Changed() {
			a.set.FullWidth = full
			a.saveSettings()
		}
		ui.Text(c, "Mati: teks di tengah selebar baca yang nyaman, seperti Notion.").FontSize(12).TextColor(p.Faint)
	})
}

func (a *App) accountSection(c *ui.Context) {
	p := a.pal()
	s := &a.settings
	a.sectionTitle(c, "Akun", "Workspace Notion yang terhubung ke gotion.")
	ui.Column(c).Gap(10).Padding(14, 16).Radius(10).Background(p.Sidebar).Children(func() {
		name := "Workspace"
		if a.bot != nil && a.bot.Bot != nil && a.bot.Bot.WorkspaceName != "" {
			name = a.bot.Bot.WorkspaceName
		}
		ui.Text(c, name).FontSize(15).FontWeight(600)
		how := "Masuk dengan token integrasi"
		if a.client != nil && a.client.Refresh != nil {
			how = "Masuk dengan akun Notion"
		}
		ui.Text(c, how).FontSize(13).TextColor(p.Muted)
		if a.bot != nil && a.bot.Bot != nil {
			if l := a.bot.Bot.WorkspaceLimits.MaxFileUploadSize; l > 0 {
				ui.Text(c, "Batas unggah per file: "+sizeText(l)).FontSize(13).TextColor(p.Muted)
			}
		}
	})
	ui.Column(c).Gap(6).Children(func() {
		if ui.Button(c, "Hapus cache halaman").AlignSelf(ui.Start).Clicked() {
			os.RemoveAll(filepath.Join(configDir(), "cache"))
			a.toast("Cache dihapus: halaman dimuat ulang dari Notion saat dibuka")
		}
		ui.Text(c, "Salinan halaman di komputer ini. Edit yang belum terkirim ikut terhapus.").FontSize(12).TextColor(p.Faint)
	})
	ui.Column(c).Gap(8).Children(func() {
		if !s.confirm {
			if ui.Button(c, "Keluar").TextColor(c.Theme().Danger).AlignSelf(ui.Start).Clicked() {
				s.confirm = true
			}
			ui.Text(c, "Menghapus token dan salinan halaman dari komputer ini. Halamanmu di Notion tidak berubah.").FontSize(12).TextColor(p.Faint)
			return
		}
		ui.Text(c, "Yakin keluar? Edit yang belum terkirim akan dikirim dulu.").FontSize(13)
		ui.Row(c).Gap(8).Children(func() {
			if ui.PrimaryButton(c, "Keluar").Clicked() {
				s.open = false
				a.signOut()
			}
			if ui.Button(c, "Batal").Clicked() {
				s.confirm = false
			}
		})
	})
}

func (a *App) aboutSection(c *ui.Context) {
	p := a.pal()
	a.sectionTitle(c, "Tentang gotion", "Notion di desktop, ringan dan native.")
	ui.Text(c, "Versi "+Version).FontSize(14)
	ui.Column(c).Gap(6).Children(func() {
		for _, l := range []struct{ label, url string }{
			{"Situs gotion", "https://gotion.ryanda.my.id"},
			{"Kebijakan Privasi", "https://gotion.ryanda.my.id/privacy"},
			{"Ketentuan", "https://gotion.ryanda.my.id/terms"},
		} {
			if ui.Link(c, l.label+" ↗", "").FontSize(14).Clicked() {
				go mygo.Shell.OpenExternal(l.url)
			}
		}
	})
	ui.Column(c).Gap(4).Children(func() {
		ui.Text(c, "Atribusi").FontSize(12).FontWeight(600).TextColor(p.Faint)
		for _, line := range []string{
			"Emoji: Twemoji, © Twitter, Inc. dan kontributor, CC-BY 4.0.",
			"Huruf: Inter, SIL Open Font License 1.1.",
			"Dibangun dengan MyGo (MIT).",
			"Tidak berafiliasi dengan Notion Labs, Inc.",
		} {
			ui.Text(c, line).FontSize(13).TextColor(p.Muted)
		}
	})
}
