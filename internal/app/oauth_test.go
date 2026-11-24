package app

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// fakeNotion answers /users/me for the token "oauth-access" only.
func fakeNotion(t *testing.T) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer oauth-access" {
			w.WriteHeader(401)
			io.WriteString(w, `{"message":"bad token"}`)
			return
		}
		io.WriteString(w, `{"object":"user","id":"bot","type":"bot","bot":{"workspace_name":"Ruang"}}`)
	}))
}

// browser stands in for the user's browser and the Worker: it takes the
// login URL and posts what the Worker would to the app's port.
func browser(t *testing.T, fields func(state string) url.Values) func(string) {
	return func(login string) {
		u, err := url.Parse(login)
		if err != nil {
			t.Error(err)
			return
		}
		q := u.Query()
		go func() {
			resp, err := http.PostForm("http://127.0.0.1:"+q.Get("port")+"/callback", fields(q.Get("state")))
			if err != nil {
				t.Error(err)
				return
			}
			resp.Body.Close()
		}()
	}
}

func waitFor(t *testing.T, done func() bool) {
	t.Helper()
	for i := 0; i < 100 && !done(); i++ {
		time.Sleep(50 * time.Millisecond)
	}
}

func TestOAuthSignsIn(t *testing.T) {
	notion := fakeNotion(t)
	defer notion.Close()
	t.Setenv("GOTION_OAUTH_URL", "https://worker.example")

	saved := openBrowser
	defer func() { openBrowser = saved }()

	// A post with another state is turned away; the right one signs in.
	openBrowser = browser(t, func(state string) url.Values {
		return url.Values{"state": {state}, "access_token": {"oauth-access"}, "refresh_token": {"r1"}, "workspace_name": {"Ruang"}}
	})
	a := &App{}
	clientBase = notion.URL
	defer func() { clientBase = "" }()
	a.startOAuth()
	waitFor(t, func() bool { return a.bot != nil })
	if a.bot == nil || a.client == nil {
		t.Fatalf("not signed in: err %q", a.tokenErr)
	}
	if got := parseCreds(loadToken()); got.Access != "oauth-access" || got.Refresh != "r1" {
		t.Errorf("saved %+v", got)
	}
}

func TestOAuthRefusesAnotherState(t *testing.T) {
	t.Setenv("GOTION_OAUTH_URL", "https://worker.example")
	saved := openBrowser
	defer func() { openBrowser = saved }()
	openBrowser = browser(t, func(string) url.Values {
		return url.Values{"state": {"someone-elses"}, "access_token": {"stolen"}}
	})
	a := &App{}
	a.startOAuth()
	time.Sleep(400 * time.Millisecond)
	if a.client != nil {
		t.Fatal("signed in with a post of another state")
	}
	a.cancelOAuth()
	waitFor(t, func() bool { return a.oauth == nil })
	if a.oauth != nil {
		t.Error("cancel left the sign-in going")
	}
}

func TestCredsRoundTrip(t *testing.T) {
	c := creds{Access: "a", Refresh: "r", Workspace: "W \"x\""}
	if got := parseCreds(c.encode()); got != c {
		t.Errorf("%+v", got)
	}
	if got := parseCreds(" ntn_abc\n"); got.Access != "ntn_abc" || got.Refresh != "" {
		t.Errorf("a bare token: %+v", got)
	}
	if !strings.HasPrefix(c.encode(), "{") || (creds{Access: "t"}).encode() != "t" {
		t.Error("encode")
	}
}

func TestOnboardingShot(t *testing.T) {
	t.Setenv("GOTION_OAUTH_URL", "https://worker.example")
	a := &App{panels: [2]*panel{{}, nil}}
	a.set = settings{Theme: "light", Zoom: 1}
	RegisterFonts()
	tester := ui.NewTester(a.View, 1000, 760)
	tester.Frame()
	snapshot(t, tester, "onboarding")
}
