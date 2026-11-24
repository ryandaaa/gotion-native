package notion

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestUploadFile(t *testing.T) {
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/file_uploads":
			b, _ := io.ReadAll(r.Body)
			got = append(got, "create "+string(b))
			io.WriteString(w, `{"id":"up1","status":"pending"}`)
		case "/file_uploads/up1/send":
			f, h, err := r.FormFile("file")
			if err != nil {
				t.Fatal(err)
			}
			data, _ := io.ReadAll(f)
			got = append(got, "send "+h.Filename+" "+h.Header.Get("Content-Type")+" "+string(data)+" "+r.Header.Get("Notion-Version"))
			io.WriteString(w, `{"id":"up1","status":"uploaded"}`)
		}
	}))
	defer srv.Close()
	c := New("t")
	c.Base = srv.URL
	up, err := c.UploadFile(context.Background(), "foto.png", "image/png", []byte("PNGDATA"))
	if err != nil || up.Status != "uploaded" {
		t.Fatalf("%+v %v", up, err)
	}
	all := strings.Join(got, "\n")
	for _, want := range []string{`"content_type":"image/png"`, `"filename":"foto.png"`, "send foto.png image/png PNGDATA " + Version} {
		if !strings.Contains(all, want) {
			t.Errorf("missing %q in\n%s", want, all)
		}
	}
	if b := MediaBlock("up1", "image/png"); b.Type != "image" {
		t.Error(b.Type)
	}
	if b := MediaBlock("up1", "application/pdf"); b.Type != "file" {
		t.Error(b.Type)
	}
	if ContentType("A.JPG", nil) != "image/jpeg" {
		t.Error("jpg")
	}
}
