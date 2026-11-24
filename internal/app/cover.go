package app

import (
	"context"
	"gotion/internal/tr"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"gotion/internal/notion"
)

// coverState is a page's cover: the picture, and changing it.
type coverState struct {
	url     string
	img     ui.ImageSource
	loading bool
	editing bool // the URL field shows
	input   string
}

// loadCover fetches the picture of panel p's cover, if it has one.
func (a *App) loadCover(p *panel, url string) {
	p.cover.url, p.cover.img = url, nil
	if url == "" {
		return
	}
	p.cover.loading = true
	go func() {
		img := fetchImage(url)
		a.update(func() {
			if p.cover.url == url {
				p.cover.img, p.cover.loading = img, false
			}
		})
	}()
}

func fetchImage(url string) ui.ImageSource {
	resp, err := (&http.Client{Timeout: 20 * time.Second}).Get(url)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 20<<20))
	if err != nil || resp.StatusCode != 200 {
		return nil
	}
	img, err := ui.DecodeImage(data)
	if err != nil {
		return nil
	}
	return img
}

// coverView is the cover across the top of a page, with its controls
// under the pointer; nothing for a page without one.
func (a *App) coverView(c *ui.Context, pn *panel) {
	cv := &pn.cover
	if cv.url == "" && !cv.editing {
		return
	}
	p := a.pal()
	box := ui.Box(c.Key("cover")).Height(200).Shrink(0).Clip().Background(p.Sidebar)
	hovered := box.Hovered()
	box.Children(func() {
		if cv.img != nil {
			ui.Image(c, cv.img).Fit(ui.Cover).Fill()
		} else if cv.loading {
			ui.Column(c).Fill().Center().Children(func() { ui.Spinner(c).Size(18, 18) })
		}
		if hovered || cv.editing {
			// On a card of their own, to read over any picture.
			ui.Row(c).Absolute().Right(16).Bottom(12).Gap(6).Padding(5).Radius(11).
				Background(p.Panel.Alpha(0.88)).Shadow(0, 4, 14, 0, ui.RGBA(0, 0, 0, 0.15)).Children(func() {
				if cv.editing {
					ui.TextInput(c, &cv.input).Placeholder(tr.T("https://… alamat gambar")).Width(280).AutoFocus().Label(tr.T("URL cover")).FontSize(13)
					if a.btn(c, tr.T("Pakai"), nil, false, strings.TrimSpace(cv.input) == "") {
						a.setCover(pn, strings.TrimSpace(cv.input), "")
						cv.editing = false
					}
					if a.btn(c, tr.T("Batal"), nil, false, false) {
						cv.editing = false
					}
					return
				}
				if a.btn(c, tr.T("Unggah"), iconUpload, false, false) {
					a.uploadCover(pn)
				}
				if a.btn(c, tr.T("Tautan"), iconLink, false, false) {
					cv.editing, cv.input = true, ""
				}
				if a.btn(c, tr.T("Hapus cover"), nil, true, false) {
					a.setCover(pn, "", "")
				}
			})
		}
	})
}

// setCover sets panel pn's page's cover to a picture at url, an upload,
// or none.
func (a *App) setCover(pn *panel, url, upload string) {
	client, id := a.client, pn.id
	old := pn.cover.url
	if upload == "" {
		a.loadCover(pn, url)
	}
	go func() {
		err := client.SetCover(context.Background(), id, url, upload)
		var fresh string
		if err == nil && upload != "" {
			// The upload's URL comes with the page.
			if pg, perr := client.Page(context.Background(), id); perr == nil {
				fresh = pg.CoverURL()
			}
		}
		a.update(func() {
			if err != nil {
				a.loadCover(pn, old)
				a.toast(tr.T("Gagal mengubah cover: ") + errText(err))
				return
			}
			if upload != "" {
				a.loadCover(pn, fresh)
			}
		})
	}()
}

// uploadCover asks for a picture and makes it the cover.
func (a *App) uploadCover(pn *panel) {
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: a.win, Title: tr.T("Pilih gambar cover"),
			Filters: []mygo.FileFilter{{Name: tr.T("Gambar"), Extensions: []string{"png", "jpg", "jpeg", "gif", "webp"}}}})
		if err != nil || len(paths) == 0 {
			return
		}
		data, err := os.ReadFile(paths[0])
		if err != nil {
			return
		}
		a.update(func() {
			if int64(len(data)) > a.uploadLimit() {
				a.toast(tr.T("Gambar terlalu besar: batas workspace ini ") + sizeText(a.uploadLimit()))
				return
			}
			name := filepath.Base(paths[0])
			client := a.client
			a.toast(tr.T("Mengunggah cover…"))
			go func() {
				up, err := client.UploadFile(context.Background(), name, notion.ContentType(name, data), data)
				a.update(func() {
					if err != nil {
						a.toast(tr.T("Gagal mengunggah: ") + errText(err))
						return
					}
					a.setCover(pn, "", up.ID)
				})
			}()
		})
	}()
}
