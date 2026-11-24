package app

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"gotion/internal/notion"
)

func TestLiveEvents(t *testing.T) {
	var auth atomic.Value
	var searched atomic.Int32
	worker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth.Store(r.Header.Get("Authorization"))
		io.WriteString(w, `{"events":[
		 {"id":"e1","type":"page.created","timestamp":"2099-01-01T00:00:00Z","entity":{"id":"baru","type":"page"}},
		 {"id":"e2","type":"page.content_updated","timestamp":"2099-01-01T00:00:01Z","entity":{"id":"satu","type":"page"}}]}`)
	}))
	defer worker.Close()
	notionSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/search") {
			searched.Add(1)
			io.WriteString(w, `{"results":[],"has_more":false}`)
			return
		}
		io.WriteString(w, `{"object":"user","id":"bot","bot":{"workspace_id":"ws"}}`)
	}))
	defer notionSrv.Close()
	t.Setenv("GOTION_OAUTH_URL", worker.URL)
	old := livePoll
	livePoll = 50 * time.Millisecond
	defer func() { livePoll = old }()

	a := tabsApp(t)
	a.client = notion.New("tok-live")
	a.client.Base = notionSrv.URL + "/v1"
	a.client.Refresh = func(context.Context) (string, error) { return "tok-live", nil }
	a.startLive()
	defer a.stopLive()
	if !waitFor2(func() bool { return searched.Load() > 0 }) {
		t.Fatal("a page made elsewhere did not bring the sidebar in again")
	}
	if got, _ := auth.Load().(string); got != "Bearer tok-live" {
		t.Errorf("auth %q", got)
	}
	// Signed in with a token, not Notion's login: no webhook, no asking.
	b := tabsApp(t)
	b.startLive()
	if b.liveCancel != nil {
		t.Error("asked the Worker without Notion's login")
	}
}
