package app

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"gotion/internal/tr"
)

// TestFirstRunJourney walks the way of someone who has never used gotion:
// the welcome in their language, signing in through Notion's page with no
// page chosen, being told how to share one, choosing one, opening it, and
// signing out back to the welcome.
func TestFirstRunJourney(t *testing.T) {
	var shared atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok-1" && r.Header.Get("Authorization") != "Bearer tok-2" {
			w.WriteHeader(401)
			io.WriteString(w, `{"message":"bad token"}`)
			return
		}
		switch p := r.URL.Path; {
		case strings.HasSuffix(p, "/users/me"):
			io.WriteString(w, `{"object":"user","id":"bot","type":"bot","bot":{"workspace_name":"Ruang Kerja"}}`)
		case strings.HasSuffix(p, "/search"):
			if !shared.Load() {
				io.WriteString(w, `{"results":[],"has_more":false}`)
				return
			}
			io.WriteString(w, `{"results":[{"object":"page","id":"p1","parent":{"type":"workspace"},"last_edited_time":"2026-11-24T10:00:00Z",
			 "properties":{"title":{"type":"title","title":[{"plain_text":"Catatan Pertama"}]}}}],"has_more":false}`)
		case strings.HasSuffix(p, "/children"):
			io.WriteString(w, `{"results":[{"object":"block","id":"b1","type":"paragraph","paragraph":{"rich_text":[{"plain_text":"Halo dari Notion","text":{"content":"Halo dari Notion"}}]}}],"has_more":false}`)
		case strings.HasSuffix(p, "/pages/p1"):
			io.WriteString(w, `{"object":"page","id":"p1","parent":{"type":"workspace"},"last_edited_by":{"id":"u"},
			 "properties":{"title":{"type":"title","title":[{"plain_text":"Catatan Pertama"}]}}}`)
		default:
			io.WriteString(w, `{}`)
		}
	}))
	defer srv.Close()
	clientBase = srv.URL
	defer func() { clientBase = "" }()
	t.Setenv("GOTION_OAUTH_URL", "https://worker.example")
	defer func(saved func() []string) { PreferredLanguages = saved; tr.Set(tr.ID) }(PreferredLanguages)
	PreferredLanguages = func() []string { return []string{"en-US"} }

	// The Worker hands back a new token each time Notion's page is passed.
	logins := 0
	saved := openBrowser
	defer func() { openBrowser = saved }()
	openBrowser = browser(t, func(state string) url.Values {
		logins++
		return url.Values{"state": {state}, "access_token": {map[int]string{1: "tok-1", 2: "tok-2"}[logins]},
			"refresh_token": {"r"}, "workspace_name": {"Ruang Kerja"}}
	})

	a := New()
	RegisterFonts()
	tester := ui.NewTester(a.View, 1100, 760)
	frame := func() {
		tester.Frame()
		tester.Advance(300 * time.Millisecond)
	}
	see := func(text string) {
		t.Helper()
		for i := 0; i < 60; i++ {
			frame()
			if _, ok := tester.Find(text); ok {
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
		t.Fatalf("%q never shows (client %v bot %v searched %v loadErr %q tokenErr %q oauth %v checking %v nodes %d)", text, a.client != nil, a.bot != nil, a.searched, a.loadErr, a.tokenErr, a.oauth != nil, a.checking, len(a.nodes))
	}
	click := func(text string) {
		t.Helper()
		see(text)
		if err := tester.Click(text); err != nil {
			t.Fatal(err)
		}
		frame()
	}

	// 1. The welcome speaks the system's language, English here.
	see("Welcome to gotion")
	see("Sign in with Notion")
	snapshot(t, tester, "journey-1-welcome-en")
	// and changes at once to Indonesian.
	click("Bahasa Indonesia")
	see("Selamat datang di gotion")
	if a.set.Language != tr.ID {
		t.Errorf("language kept as %q", a.set.Language)
	}

	// 2. Signing in, with no page chosen on Notion's page.
	click("Masuk dengan Notion")
	see("Belum ada halaman yang dibagikan ke gotion")
	snapshot(t, tester, "journey-2-no-pages")
	if a.client == nil || a.bot == nil {
		t.Fatal("not signed in")
	}

	// 3. Choosing a page brings it in.
	shared.Store(true)
	click("Pilih halaman di Notion")
	see("Catatan Pertama")
	see("Pilih halaman di sidebar")
	if logins != 2 {
		t.Errorf("Notion's page passed %d times", logins)
	}
	if got := parseCreds(loadToken()).Access; got != "tok-2" {
		t.Errorf("kept token %q, not the new one", got)
	}

	snapshot(t, tester, "journey-2b-shared")
	// 4. Opening it.
	// The page is a row of the sidebar's list, named once.
	row, _ := tester.Find("Catatan Pertama")
	tester.ClickAt(row.X+row.W/2, row.Y+row.H/2)
	if !waitFor2(func() bool { frame(); pn := a.panels[0]; return pn != nil && pn.id == "p1" && pn.doc != nil }) {
		pn := a.panels[0]
		t.Fatalf("the page did not open: id %q doc %v session %v", pn.id, pn.doc != nil, pn.session != nil)
	}
	snapshot(t, tester, "journey-3-page")

	// 5. Signing out returns to the welcome, in the language chosen.
	a.signOut()
	see("Selamat datang di gotion")
	if loadToken() != "" {
		t.Error("the token stayed after signing out")
	}
}
