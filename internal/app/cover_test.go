package app

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"gotion/internal/notion"
)

func TestCover(t *testing.T) {
	var pic bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 600, 200))
	for y := 0; y < 200; y++ {
		for x := 0; x < 600; x++ {
			img.Set(x, y, color.RGBA{uint8(120 + x/8), 140, uint8(200 - y/2), 255})
		}
	}
	png.Encode(&pic, img)
	rec := &recorder{}
	var base string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		path := strings.TrimPrefix(r.URL.Path, "/v1")
		rec.mu.Lock()
		rec.sent = append(rec.sent, r.Method+" "+path+" "+string(b))
		rec.mu.Unlock()
		switch {
		case path == "/cover.png":
			w.Write(pic.Bytes())
		case path == "/pages/pc" && r.Method == "GET":
			io.WriteString(w, `{"object":"page","id":"pc","last_edited_by":{"id":"u"},"cover":{"type":"external","external":{"url":"`+base+`/cover.png"}},
			 "properties":{"title":{"type":"title","title":[{"plain_text":"Bercover"}]}}}`)
		case strings.HasSuffix(path, "/children"):
			io.WriteString(w, `{"results":[],"has_more":false}`)
		default:
			io.WriteString(w, `{}`)
		}
	}))
	defer srv.Close()
	base = srv.URL
	a := tabsApp(t)
	a.client = notion.New("x")
	a.client.Base = srv.URL + "/v1"
	a.nodes["pc"] = &node{ID: "pc", Title: "Bercover", Kind: "page"}
	a.open(a.nodes["pc"], 0)
	if !waitFor2(func() bool { return a.panels[0].cover.img != nil }) {
		t.Fatalf("no cover picture: %+v", a.panels[0].cover)
	}
	tester := ui.NewTester(a.View, 1200, 560)
	tester.Move(700, 150)
	tester.Advance(time.Second)
	snapshot(t, tester, "cover")
	if err := tester.Click("Hapus cover"); err != nil {
		t.Fatal(err)
	}
	if !waitSent(rec, `PATCH /pages/pc {"cover":null}`) {
		t.Errorf("sent:\n%s", rec.all())
	}
	a.setCover(a.panels[0], "https://contoh.id/x.png", "")
	if !waitSent(rec, `{"cover":{"external":{"url":"https://contoh.id/x.png"},"type":"external"}}`) {
		t.Errorf("sent:\n%s", rec.all())
	}
}
