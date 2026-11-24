package app

import (
	"context"
	"errors"
	"fmt"
	"gotion/internal/tr"
	"gotion/internal/wallpaper"
	"os"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"gotion/internal/notion"
)

// Version is gotion's, set at build time with
// -ldflags "-X gotion/internal/app.Version=…".
var Version = "0.1.0-dev"

// CheckForUpdates looks for a newer gotion and offers it, reporting false
// where updates are off (a development build); main sets it.
var CheckForUpdates = func() bool { return false }

// settingsView is the settings dialog: what shows on the left, and the
// section open.
type settingsView struct {
	open    bool
	section string
	confirm bool // asking before signing out

	// the workspace's members, loaded when the account shows
	members        []notion.User
	membersLoaded  bool
	membersLoading bool
	membersErr     string
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
			modes := []struct{ id, label string }{{"system", tr.T("Sistem")}, {"light", tr.T("Terang")}, {"dark", tr.T("Gelap")}}
			if wallpaper.Supported() {
				modes = append(modes, struct{ id, label string }{"wallpaper", "Wallpaper"})
			}
			for _, m := range modes {
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
		if a.set.Theme == "wallpaper" {
			ui.Text(c, tr.T("Terang atau gelap mengikuti wallpaper di belakang sidebar. Jendela aplikasi lain di belakangnya tidak terhitung.")).
				FontSize(11).TextColor(p.Faint)
		}
	})
	ui.Column(c).Gap(8).Children(func() {
		ui.Text(c, tr.T("Warna latar")).FontSize(12).FontWeight(600).TextColor(p.Faint)
		ui.Row(c).Wrap().Gap(8).Children(func() {
			for _, w := range washes {
				cur := a.set.Wash == w.id || a.set.Wash == "" && w.plain()
				if a.swatch(c, w, cur).Clicked() {
					a.set.Wash = w.id
					a.saveSettings()
				}
			}
		})
		ui.Text(c, tr.T(washByID(a.set.Wash).label)).FontSize(12).TextColor(p.Muted)
	})
	ui.Column(c).Gap(6).Children(func() {
		on := a.set.Glass
		ok := glassSupported()
		if ui.Checkbox(c, &on, tr.T("Kaca (blur) di sekitar kartu")).Disabled(!ok).Changed() && ok {
			a.set.Glass = on
			a.saveSettings()
			a.applyGlass()
		}
		if !ok {
			ui.Text(c, tr.T("Belum didukung di Linux: sistemnya tidak menyediakan efek kaca. Tersedia di macOS dan Windows 11.")).
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
				ui.Text(c, tr.T("Pengaturan")).FontSize(15).FontWeight(600).Padding(0, 10, 12, 10)
				for _, sec := range []struct{ id, label string }{{"tampilan", tr.T("Tampilan")}, {"akun", tr.T("Akun")}, {"tentang", tr.T("Tentang")}} {
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
					if iconButton(c, p, iconClose, tr.T("Tutup (Esc)")) {
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
	a.sectionTitle(c, tr.T("Tampilan"), tr.T("Bagaimana jendela gotion terlihat."))
	a.appearance(c)
	ui.Column(c).Gap(8).Children(func() {
		ui.Text(c, "Bahasa · Language").FontSize(12).FontWeight(600).TextColor(p.Faint)
		a.languagePicker(c)
	})
	ui.Column(c).Gap(8).Children(func() {
		ui.Text(c, "Zoom").FontSize(12).FontWeight(600).TextColor(p.Faint)
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			if a.btn(c, "−", nil, false, false) {
				a.zoom(-1)
			}
			ui.Text(c, fmt.Sprintf("%d%%", int(a.set.Zoom*100+0.5))).FontSize(14).Width(56).TextAlign(ui.Center)
			if a.btn(c, "+", nil, false, false) {
				a.zoom(+1)
			}
			if a.btn(c, tr.T("Atur ulang"), nil, false, a.set.Zoom > 0.99 && a.set.Zoom < 1.01) {
				a.zoom(0)
			}
			ui.Text(c, "Ctrl+= · Ctrl+- · Ctrl+0").FontSize(12).TextColor(p.Faint)
		})
	})
	ui.Column(c).Gap(6).Children(func() {
		full := a.set.FullWidth
		if ui.Checkbox(c, &full, tr.T("Teks halaman selebar panel")).Changed() {
			a.set.FullWidth = full
			a.saveSettings()
		}
		ui.Text(c, tr.T("Mati: teks di tengah selebar baca yang nyaman, seperti Notion.")).FontSize(12).TextColor(p.Faint)
	})
}

func (a *App) accountSection(c *ui.Context) {
	p := a.pal()
	s := &a.settings
	a.sectionTitle(c, tr.T("Akun"), tr.T("Workspace Notion yang terhubung ke gotion."))
	ui.Column(c).Gap(10).Padding(14, 16).Radius(10).Background(p.Sidebar).Children(func() {
		name := "Workspace"
		if a.bot != nil && a.bot.Bot != nil && a.bot.Bot.WorkspaceName != "" {
			name = a.bot.Bot.WorkspaceName
		}
		ui.Text(c, name).FontSize(15).FontWeight(600)
		how := tr.T("Masuk dengan token integrasi")
		if a.client != nil && a.client.Refresh != nil {
			how = tr.T("Masuk dengan akun Notion")
		}
		ui.Text(c, how).FontSize(13).TextColor(p.Muted)
		if a.bot != nil && a.bot.Bot != nil {
			if l := a.bot.Bot.WorkspaceLimits.MaxFileUploadSize; l > 0 {
				ui.Text(c, tr.T("Batas unggah per file: ")+sizeText(l)).FontSize(13).TextColor(p.Muted)
			}
		}
	})
	if a.client != nil && a.client.Refresh != nil && oauthURL() != "" {
		ui.Column(c).Gap(6).Children(func() {
			if a.btn(c, tr.T("Pilih halaman lain"), nil, false, a.oauth != nil) {
				a.startOAuth()
			}
			ui.Text(c, tr.T("Buka halaman izin Notion untuk menambah atau mengurangi halaman yang boleh dibuka gotion.")).FontSize(12).TextColor(p.Faint)
		})
	}
	a.members(c)
	ui.Column(c).Gap(6).Children(func() {
		if a.btnRow(c, func() bool { return a.btn(c, tr.T("Hapus cache halaman"), iconTrash, false, false) }) {
			os.RemoveAll(filepath.Join(configDir(), "cache"))
			a.toast(tr.T("Cache dihapus: halaman dimuat ulang dari Notion saat dibuka"))
		}
		ui.Text(c, tr.T("Salinan halaman di komputer ini. Edit yang belum terkirim ikut terhapus.")).FontSize(12).TextColor(p.Faint)
	})
	ui.Column(c).Gap(8).Children(func() {
		if !s.confirm {
			if a.btnRow(c, func() bool { return a.btn(c, tr.T("Keluar"), iconLogout, true, false) }) {
				s.confirm = true
			}
			ui.Text(c, tr.T("Menghapus token dan salinan halaman dari komputer ini. Halamanmu di Notion tidak berubah.")).FontSize(12).TextColor(p.Faint)
			return
		}
		ui.Text(c, tr.T("Yakin keluar? Edit yang belum terkirim akan dikirim dulu.")).FontSize(13)
		ui.Row(c).Gap(8).Children(func() {
			if ui.PrimaryButton(c, tr.T("Keluar")).Clicked() {
				s.open = false
				a.signOut()
			}
			if a.btn(c, tr.T("Batal"), nil, false, false) {
				s.confirm = false
			}
		})
	})
}

func (a *App) aboutSection(c *ui.Context) {
	p := a.pal()
	a.sectionTitle(c, tr.T("Tentang gotion"), tr.T("Notion di desktop, ringan dan native."))
	ui.Row(c).Gap(10).AlignItems(ui.Center).Wrap().Children(func() {
		ui.Text(c, tr.T("Versi ")+Version).FontSize(14)
		if a.btn(c, tr.T("Periksa pembaruan"), iconRefresh, false, false) && !CheckForUpdates() {
			a.toast(tr.T("Pembaruan otomatis hanya ada di versi rilis. Unduh yang terbaru dari gotion.ryanda.my.id."))
		}
	})
	ui.Column(c).Gap(6).Children(func() {
		for _, l := range []struct{ label, url string }{
			{tr.T("Situs gotion"), "https://gotion.ryanda.my.id"},
			{tr.T("Kebijakan Privasi"), "https://gotion.ryanda.my.id/privacy"},
			{tr.T("Ketentuan"), "https://gotion.ryanda.my.id/terms"},
		} {
			if ui.Link(c, l.label+" ↗", "").FontSize(14).Clicked() {
				go mygo.Shell.OpenExternal(l.url)
			}
		}
	})
	ui.Column(c).Gap(4).Children(func() {
		ui.Text(c, tr.T("Atribusi")).FontSize(12).FontWeight(600).TextColor(p.Faint)
		for _, line := range []string{
			tr.T("Emoji: Twemoji, © Twitter, Inc. dan kontributor, CC-BY 4.0."),
			tr.T("Huruf: Inter, SIL Open Font License 1.1."),
			tr.T("Dibangun dengan MyGo (MIT)."),
			tr.T("Tidak berafiliasi dengan Notion Labs, Inc."),
		} {
			ui.Text(c, line).FontSize(13).TextColor(p.Muted)
		}
	})
}

// members lists the workspace's people (GET /users), when the integration
// may read them.
func (a *App) members(c *ui.Context) {
	p := a.pal()
	s := &a.settings
	if !s.membersLoaded && !s.membersLoading {
		a.loadMembers()
	}
	ui.Column(c).Gap(8).Children(func() {
		ui.Text(c, tr.T("Anggota workspace")).FontSize(12).FontWeight(600).TextColor(p.Faint)
		switch {
		case s.membersLoading:
			ui.Spinner(c).Size(16, 16)
		case s.membersErr != "":
			ui.Text(c, s.membersErr).FontSize(12).TextColor(p.Faint)
		case len(s.members) == 0:
			ui.Text(c, tr.T("Tidak ada anggota yang terlihat.")).FontSize(12).TextColor(p.Faint)
		}
		for _, u := range s.members {
			ui.Row(c.Key("m" + u.ID)).Gap(10).AlignItems(ui.Center).Children(func() {
				name := nonEmpty(u.Name, tr.T("Tanpa nama"))
				initial := strings.ToUpper(string([]rune(name)[:1]))
				ui.Box(c).Size(26, 26).Radius(13).Background(p.Accent.Alpha(0.16)).Center().Children(func() {
					ui.Text(c, initial).FontSize(12).FontWeight(600).TextColor(p.Accent)
				})
				ui.Column(c).Children(func() {
					ui.Text(c, name).FontSize(14)
					if u.Person != nil && u.Person.Email != "" {
						ui.Text(c, u.Person.Email).FontSize(12).TextColor(p.Faint)
					}
				})
			})
		}
	})
}

// loadMembers fetches the workspace's people (GET /users), once.
func (a *App) loadMembers() {
	s := &a.settings
	if a.client == nil {
		return
	}
	s.membersLoading = true
	client := a.client
	go func() {
		users, err := client.Users(context.Background())
		a.update(func() {
			s.membersLoading, s.membersLoaded = false, true
			if err != nil {
				var e *notion.Error
				if errors.As(err, &e) && e.Status == 403 {
					s.membersErr = tr.T("Integrasi ini belum boleh melihat anggota. Aktifkan “Read user information” di pengaturan integrasi Notion.")
				} else {
					s.membersErr = errText(err)
				}
				return
			}
			for _, u := range users {
				if u.Type == "person" {
					s.members = append(s.members, u)
				}
			}
		})
	}()
}
