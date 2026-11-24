package app

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/richtext"

	"gotion/internal/notion"
)

func commentsApp(t *testing.T, forbid bool) (*App, *panel, *recorder) {
	rec := &recorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		rec.mu.Lock()
		rec.sent = append(rec.sent, r.Method+" "+r.URL.RequestURI()+" "+string(b))
		rec.mu.Unlock()
		if forbid {
			w.WriteHeader(403)
			io.WriteString(w, `{"code":"restricted_resource","message":"no"}`)
			return
		}
		if r.Method == "GET" {
			io.WriteString(w, `{"results":[
			 {"id":"c1","discussion_id":"d1","created_time":"2026-11-24T08:00:00Z","rich_text":[{"plain_text":"Tolong cek bagian ini"}],"display_name":{"resolved_name":"Naila"}},
			 {"id":"c2","discussion_id":"d1","created_time":"2026-11-24T09:00:00Z","rich_text":[{"plain_text":"Sudah, aman"}],"display_name":{"resolved_name":"Ryanda"}}],"has_more":false}`)
			return
		}
		io.WriteString(w, `{"id":"c3","discussion_id":"d2","rich_text":[{"plain_text":"Komentar baru"}],"display_name":{"resolved_name":"gotion"}}`)
	}))
	t.Cleanup(srv.Close)
	a := tableApp(t)
	a.client = notion.New("x")
	a.client.Base = srv.URL + "/v1"
	pn := &panel{id: "ppsi", title: "PPSI", doc: richtext.NewDocument(richtext.TextOf(richtext.Paragraph{Runs: []richtext.Run{{Text: "isi"}}}))}
	a.panels[0] = pn
	return a, pn, rec
}

func TestComments(t *testing.T) {
	a, pn, rec := commentsApp(t, false)
	a.toggleComments(pn)
	if !waitFor2(func() bool { return pn.comments.loaded }) || len(pn.comments.list) != 2 {
		t.Fatalf("loaded %v list %d err %q", pn.comments.loaded, len(pn.comments.list), pn.comments.err)
	}
	if len(discussions(pn.comments.list)) != 1 {
		t.Error("one discussion of two comments")
	}
	tester := ui.NewTester(a.View, 1280, 640)
	tester.Advance(time.Second)
	snapshot(t, tester, "comments")
	if _, ok := tester.Find("Sudah, aman"); !ok {
		t.Error("comment not shown")
	}
	a.sendComment(pn, "", "Komentar baru")
	waitFor2(func() bool { return !pn.comments.sending })
	a.sendComment(pn, "d1", "Balasan")
	if !waitSent(rec, `"parent":{"page_id":"ppsi"}`) || !waitSent(rec, `"discussion_id":"d1"`) {
		t.Errorf("sent:\n%s", rec.all())
	}
	if !strings.Contains(rec.all(), "GET /v1/comments?block_id=ppsi") {
		t.Errorf("listed:\n%s", rec.all())
	}
}

func TestCommentsForbidden(t *testing.T) {
	a, pn, _ := commentsApp(t, true)
	a.toggleComments(pn)
	waitFor2(func() bool { return pn.comments.loaded })
	if !strings.Contains(pn.comments.err, "Read comments") {
		t.Errorf("err %q", pn.comments.err)
	}
}
