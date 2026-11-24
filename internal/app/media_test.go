package app

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/richtext"

	"gotion/internal/doc"
	"gotion/internal/notion"
	"gotion/internal/syncer"
)

func TestSafeAndFreeNames(t *testing.T) {
	for in, want := range map[string]string{"laporan.pdf": "laporan.pdf", "../../etc/passwd": "passwd", "a\\..\\b.txt": "b.txt", "..": "lampiran", "": "lampiran"} {
		if got := safeName(in); got != want {
			t.Errorf("safeName(%q) = %q, want %q", in, got, want)
		}
	}
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.pdf"), nil, 0o600)
	os.WriteFile(filepath.Join(dir, "a (1).pdf"), nil, 0o600)
	if got := filepath.Base(freeName(dir, "a.pdf")); got != "a (2).pdf" {
		t.Errorf("freeName: %q", got)
	}
}

func TestUploadLimit(t *testing.T) {
	a := &App{}
	if a.uploadLimit() != maxSinglePart {
		t.Error("no bot: one part's limit")
	}
	a.bot = &notion.User{}
	a.bot.Bot = &struct {
		WorkspaceName   string `json:"workspace_name"`
		WorkspaceLimits struct {
			MaxFileUploadSize int64 `json:"max_file_upload_size_in_bytes"`
		} `json:"workspace_limits"`
	}{}
	a.bot.Bot.WorkspaceLimits.MaxFileUploadSize = 5 << 20
	if a.uploadLimit() != 5<<20 {
		t.Errorf("free plan: %d", a.uploadLimit())
	}
	// Too big: refused before any request, with a toast saying why.
	pn := &panel{}
	a.dropFiles(pn, nil)
	big := filepath.Join(t.TempDir(), "besar.png")
	os.WriteFile(big, make([]byte, 6<<20), 0o600)
	a.dropFiles(pn, []string{big})
	if len(a.toasts) != 1 || !contains(a.toasts[0], "terlalu besar") || !contains(a.toasts[0], "5.0 MB") {
		t.Errorf("toasts %v", a.toasts)
	}
}

func contains(s, sub string) bool { return strings.Contains(s, sub) }

func TestUploadGoesAfterTheCaret(t *testing.T) {
	rec := &recorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		path := strings.TrimPrefix(r.URL.Path, "/v1")
		rec.mu.Lock()
		rec.sent = append(rec.sent, r.Method+" "+path+" "+string(b))
		rec.mu.Unlock()
		switch {
		case path == "/pages/p":
			io.WriteString(w, `{"object":"page","id":"p","last_edited_by":{"id":"u"}}`)
		case path == "/blocks/p/children" && r.Method == "GET":
			io.WriteString(w, `{"results":[
			 {"object":"block","id":"a","type":"paragraph","paragraph":{"rich_text":[{"type":"text","plain_text":"satu","annotations":{"color":"default"}}]}},
			 {"object":"block","id":"b","type":"paragraph","paragraph":{"rich_text":[{"type":"text","plain_text":"dua","annotations":{"color":"default"}}]}}],"has_more":false}`)
		case path == "/file_uploads":
			io.WriteString(w, `{"id":"up1","status":"pending"}`)
		case path == "/file_uploads/up1/send":
			io.WriteString(w, `{"id":"up1","status":"uploaded"}`)
		default:
			io.WriteString(w, `{"results":[]}`)
		}
	}))
	defer srv.Close()
	a := tableApp(t)
	a.client = notion.New("x")
	a.client.Base = srv.URL + "/v1"
	s, text, _, err := syncer.Open(context.Background(), a.client, "bot", "p", doc.Shape{})
	if err != nil {
		t.Fatal(err)
	}
	pn := &panel{id: "p", session: s, doc: richtext.NewDocument(text)}
	pn.doc.SetSelection(1, 1) // in "satu"
	a.upload(pn, "foto.png", []byte("\x89PNG\r\n\x1a\nxx"), caretParagraph(pn))
	if !waitSent(rec, `PATCH /blocks/p/children`) {
		t.Fatalf("not attached:\n%s", rec.all())
	}
	all := rec.all()
	for _, want := range []string{`"type":"image"`, `"file_upload":{"id":"up1"}`, `"after_block":{"id":"a"}`} {
		if !strings.Contains(all, want) {
			t.Errorf("missing %s in\n%s", want, all)
		}
	}
}

// TestClickImageOpensViewer clicks a picture in the editor: its link opens
// it full size.
func TestClickImageOpensViewer(t *testing.T) {
	var buf bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 400, 200))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = 200, 200, 200, 255
	}
	png.Encode(&buf, img)
	src, err := ui.DecodeImage(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	var blocks []*notion.Block
	json.Unmarshal([]byte(`[{"object":"block","id":"img1","type":"image","image":{"type":"external","external":{"url":"x"}}},
	 {"object":"block","id":"p","type":"paragraph","paragraph":{"rich_text":[{"type":"text","plain_text":"di bawah gambar","annotations":{"color":"default"}}]}}]`), &blocks)
	a := tableApp(t)
	text, _, _ := doc.Build("ppsi", blocks, doc.Shape{Images: map[string]ui.ImageSource{"img1": src}})
	pn := &panel{id: "ppsi", title: "Gambar", doc: richtext.NewDocument(text)}
	a.panels[0] = pn
	images := map[string]ui.ImageSource{"img1": src}
	a.imageOf = func(id string) ui.ImageSource { return images[id] }
	tester := ui.NewTester(a.View, 1280, 700)
	tester.Frame()
	// Find the picture by its gray, as drawn, and click its middle.
	shot := tester.Image()
	minX, minY, maxX, maxY := 1<<30, 1<<30, -1, -1
	for y := shot.Rect.Min.Y; y < shot.Rect.Max.Y; y++ {
		for x := shot.Rect.Min.X; x < shot.Rect.Max.X; x++ {
			if c := shot.RGBAAt(x, y); c.R >= 198 && c.R <= 202 && c.G >= 198 && c.G <= 202 && c.B >= 198 && c.B <= 202 {
				minX, minY, maxX, maxY = min(minX, x), min(minY, y), max(maxX, x), max(maxY, y)
			}
		}
	}
	if maxX < 0 {
		t.Fatal("the picture is not drawn")
	}
	scale := float32(shot.Rect.Dx()) / 1280
	tester.ClickAt(float32(minX+maxX)/2/scale, float32(minY+maxY)/2/scale)
	tester.Advance(time.Second)
	snapshot(t, tester, "image-viewer")
	if !a.viewer.open {
		t.Error("a click on the picture did not open it")
	}
}
