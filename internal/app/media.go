package app

import (
	"context"
	"errors"
	"fmt"
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

// maxSinglePart is the largest file sent in one part.
const maxSinglePart = 20 << 20

// uploadLimit is the largest file the workspace takes: 5 MiB on a free
// plan, as Notion tells the bot, and never more than one part's 20 MiB.
func (a *App) uploadLimit() int64 {
	limit := int64(maxSinglePart)
	if a.bot != nil && a.bot.Bot != nil {
		if l := a.bot.Bot.WorkspaceLimits.MaxFileUploadSize; l > 0 && l < limit {
			limit = l
		}
	}
	return limit
}

func sizeText(n int64) string {
	if n >= 1<<20 {
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	}
	return fmt.Sprintf("%d KB", n/1024)
}

// caretParagraph is the editor's paragraph at the caret: where an upload
// goes, after it.
func caretParagraph(pn *panel) string {
	if pn.doc == nil {
		return ""
	}
	_, caret := pn.doc.Selection()
	return pn.doc.Text().ParagraphStyleAt(caret).ID
}

// upload sends a file to Notion and puts it in panel pn's page after the
// paragraph after: a picture as an image block, anything else as a file.
func (a *App) upload(pn *panel, name string, data []byte, after string) {
	if pn.session == nil {
		return
	}
	if limit := a.uploadLimit(); int64(len(data)) > limit {
		a.toast(fmt.Sprintf(tr.T("%s terlalu besar (%s): batas workspace ini %s per file."), name, sizeText(int64(len(data))), sizeText(limit)))
		return
	}
	ctype := notion.ContentType(name, data)
	a.toast(tr.T("Mengunggah ") + name + "…")
	client, s := a.client, pn.session
	go func() {
		ctx := context.Background()
		up, err := client.UploadFile(ctx, name, ctype, data)
		if err == nil {
			err = s.Attach(ctx, after, notion.MediaBlock(up.ID, ctype))
		}
		a.update(func() {
			if err != nil {
				a.toast(tr.T("Gagal mengunggah ") + name + ": " + errText(err))
				return
			}
			a.toast(name + tr.T(" ditambahkan"))
		})
	}()
}

// pasteImage uploads a picture on the clipboard into panel pn, and
// reports whether there was one; text pastes as it always does.
func (a *App) pasteImage(pn *panel) bool {
	data := mygo.Clipboard.ReadImage()
	if len(data) == 0 {
		return false
	}
	a.upload(pn, "tempelan-"+time.Now().Format("20060102-150405")+".png", data, caretParagraph(pn))
	return true
}

// dropFiles uploads files dropped on panel pn's page.
func (a *App) dropFiles(pn *panel, paths []string) {
	after := caretParagraph(pn)
	for _, path := range paths {
		st, err := os.Stat(path)
		if err != nil || st.IsDir() {
			continue
		}
		if st.Size() > a.uploadLimit() {
			a.toast(fmt.Sprintf(tr.T("%s terlalu besar (%s): batas workspace ini %s per file."), filepath.Base(path), sizeText(st.Size()), sizeText(a.uploadLimit())))
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			a.toast(tr.T("Tidak bisa membaca ") + filepath.Base(path))
			continue
		}
		a.upload(pn, filepath.Base(path), data, after)
	}
}

// download saves a file block's file in the Downloads folder and shows it
// there. Its URL lasts an hour, so the block is fetched again first.
func (a *App) download(id string) {
	a.toast(tr.T("Mengunduh…"))
	client := a.client
	go func() {
		path, err := func() (string, error) {
			ctx := context.Background()
			b, err := client.Block(ctx, id)
			if err != nil {
				return "", err
			}
			name, url := b.FileInfo()
			if url == "" {
				return "", errors.New(tr.T("lampiran ini tidak punya file"))
			}
			dir, err := mygo.App.Path(mygo.PathDownloads)
			if err != nil || dir == "" {
				dir, _ = os.UserHomeDir()
			}
			req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
			if err != nil {
				return "", err
			}
			resp, err := (&http.Client{Timeout: 5 * time.Minute}).Do(req)
			if err != nil {
				return "", err
			}
			defer resp.Body.Close()
			if resp.StatusCode != 200 {
				return "", fmt.Errorf("HTTP %d", resp.StatusCode)
			}
			path := freeName(dir, safeName(name))
			f, err := os.Create(path)
			if err != nil {
				return "", err
			}
			if _, err := io.Copy(f, resp.Body); err != nil {
				f.Close()
				os.Remove(path)
				return "", err
			}
			return path, f.Close()
		}()
		a.update(func() {
			if err != nil {
				a.toast(tr.T("Gagal mengunduh: ") + errText(err))
				return
			}
			a.toast(tr.T("Tersimpan di ") + path)
			mygo.Shell.ShowItemInFolder(path)
		})
	}()
}

// safeName is a file name with nothing that would put it elsewhere.
func safeName(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	if name == "." || name == "/" || name == "" || strings.HasPrefix(name, "..") {
		name = "lampiran"
	}
	return name
}

// freeName is name in dir, numbered when one of that name is there.
func freeName(dir, name string) string {
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	path := filepath.Join(dir, name)
	for i := 1; ; i++ {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return path
		}
		path = filepath.Join(dir, fmt.Sprintf("%s (%d)%s", stem, i, ext))
	}
}

// viewer shows a picture full size over the window.
type viewer struct {
	open  bool
	img   ui.ImageSource
	id    string // its block
	panel int
}

func (a *App) viewerView(c *ui.Context) {
	v := &a.viewer
	scrim := ui.Column(c.Key("viewer")).Absolute().Left(0).Top(0).Right(0).Bottom(0).ZIndex(60).
		Background(ui.RGBA(0, 0, 0, 0.82)).Center().Padding(40).Modal().Cursor(ui.CursorPointer).Transition(fadeIn)
	if scrim.OverlayShortcut(0, ui.KeyEscape) {
		v.open = false
	}
	removed := false
	scrim.Children(func() {
		ui.Image(c, v.img).Fit(ui.Contain).Grow(1).MinHeight(0).FillWidth().Radius(6).Transition(popIn)
		ui.Row(c).Gap(16).AlignItems(ui.Center).Margin(14, 0, 0, 0).Children(func() {
			ui.Text(c, tr.T("Klik di mana saja atau tekan Esc untuk menutup")).FontSize(12).TextColor(ui.RGBA(255, 255, 255, 0.6))
			if pn := a.panels[v.panel]; v.id != "" && pn != nil && pn.session != nil && pn.session.Deletable(v.id) {
				del := ui.Text(c.Key("delete-image"), tr.T("Hapus gambar")).FontSize(13).FontWeight(500).
					TextColor(ui.Hex("#ffb4ab")).Padding(6, 12).Radius(7).Background(ui.RGBA(255, 255, 255, 0.1)).Cursor(ui.CursorPointer)
				if del.Hovered() {
					del.Background(ui.RGBA(255, 255, 255, 0.18))
				}
				if del.Clicked() {
					removed = true
					if removeParagraph(pn.doc, v.id) {
						a.edited(pn)
						a.toast(tr.T("Gambar dihapus dari halaman"))
					}
				}
			}
		})
	})
	if scrim.Clicked() || removed {
		v.open = false
	}
}
