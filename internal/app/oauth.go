package app

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"gotion/internal/tr"
	"html"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/egoist/mygo"

	"gotion/internal/notion"
)

// OAuthURL is the gotion OAuth Worker (worker/), set at build time with
// -ldflags "-X gotion/internal/app.OAuthURL=https://…" or by
// GOTION_OAUTH_URL. Without one, only the integration token signs in.
var OAuthURL = "https://gotion.ryanda.my.id"

func oauthURL() string {
	if u := os.Getenv("GOTION_OAUTH_URL"); u != "" {
		return strings.TrimRight(u, "/")
	}
	return strings.TrimRight(OAuthURL, "/")
}

// clientBase, when set, is where clients send their requests: tests.
var clientBase = ""

// creds is what signs in: an integration token, or OAuth's access and
// refresh tokens.
type creds struct {
	Access    string `json:"access"`
	Refresh   string `json:"refresh,omitempty"`
	Workspace string `json:"workspace,omitempty"`
}

// parseCreds reads saved credentials: JSON for OAuth, a bare token from
// before.
func parseCreds(s string) creds {
	var c creds
	if strings.HasPrefix(strings.TrimSpace(s), "{") && json.Unmarshal([]byte(s), &c) == nil && c.Access != "" {
		return c
	}
	return creds{Access: cleanToken(s)}
}

func (c creds) encode() string {
	if c.Refresh == "" && c.Workspace == "" {
		return c.Access
	}
	b, _ := json.Marshal(c)
	return string(b)
}

// clientFor is a Notion client that signs in with c, renewing an OAuth
// token through the Worker when Notion turns it down.
func (a *App) clientFor(c creds) *notion.Client {
	cl := notion.New(c.Access)
	if clientBase != "" {
		cl.Base = clientBase
	}
	if c.Refresh == "" || oauthURL() == "" {
		return cl
	}
	cur := c
	cl.Refresh = func(ctx context.Context) (string, error) {
		next, err := refreshTokens(ctx, cur.Refresh)
		if err != nil {
			return "", err
		}
		next.Workspace = cur.Workspace
		cur = next
		saveToken(cur.encode())
		return cur.Access, nil
	}
	return cl
}

func refreshTokens(ctx context.Context, refresh string) (creds, error) {
	body, _ := json.Marshal(map[string]string{"refresh_token": refresh})
	req, err := http.NewRequestWithContext(ctx, "POST", oauthURL()+"/refresh", bytes.NewReader(body))
	if err != nil {
		return creds{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		return creds{}, err
	}
	defer resp.Body.Close()
	var out struct {
		Access  string `json:"access_token"`
		Refresh string `json:"refresh_token"`
		Error   string `json:"error"`
	}
	json.NewDecoder(resp.Body).Decode(&out)
	if resp.StatusCode != 200 || out.Access == "" {
		return creds{}, fmt.Errorf(tr.T("sesi Notion berakhir, masuk lagi: %s"), out.Error)
	}
	if out.Refresh == "" {
		out.Refresh = refresh
	}
	return creds{Access: out.Access, Refresh: out.Refresh}, nil
}

// openBrowser opens a page in the user's browser; tests stand in for it.
var openBrowser = func(u string) { go mygo.Shell.OpenExternal(u) }

// update runs fn on the window's thread, or at once without a window, as
// in tests.
func (a *App) update(fn func()) {
	if a.win == nil {
		fn()
		return
	}
	a.win.Update(fn)
}

// invalidate asks the window for a frame; nothing without a window.
func (a *App) invalidate() {
	if a.win != nil {
		a.win.Invalidate()
	}
}

// oauthState is a sign-in in progress.
type oauthState struct {
	cancel context.CancelFunc
	url    string // Notion's consent page, through the Worker, to open again
}

// startOAuth signs in through Notion's consent page: it listens on a port
// of this machine for the Worker to post the tokens to, and opens the
// browser on the Worker's login.
func (a *App) startOAuth() {
	if a.oauth != nil {
		return
	}
	base := oauthURL()
	if base == "" {
		a.tokenErr = tr.T("Login dengan Notion belum disiapkan (GOTION_OAUTH_URL). Pakai token dulu.")
		return
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		a.tokenErr = tr.T("Tidak bisa menyiapkan login: ") + err.Error()
		return
	}
	b := make([]byte, 24)
	rand.Read(b)
	state := base64.RawURLEncoding.EncodeToString(b)
	port := ln.Addr().(*net.TCPAddr).Port
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	a.oauth = &oauthState{cancel: cancel}
	a.tokenErr = ""

	result := make(chan creds, 1)
	failed := make(chan string, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method", http.StatusMethodNotAllowed)
			return
		}
		r.ParseForm()
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		page := func(title, text string) {
			fmt.Fprintf(w, browserPage, tr.Lang(), html.EscapeString(title), html.EscapeString(text))
		}
		// Only the post that carries this sign-in's state counts.
		if r.PostFormValue("state") != state {
			page(tr.T("Login tidak dikenali"), tr.T("Mulai lagi dari gotion."))
			return
		}
		if e := r.PostFormValue("error"); e != "" {
			page(tr.T("Login batal"), tr.T("Kembali ke gotion untuk mencoba lagi."))
			failed <- e
			return
		}
		c := creds{Access: r.PostFormValue("access_token"), Refresh: r.PostFormValue("refresh_token"), Workspace: r.PostFormValue("workspace_name")}
		if c.Access == "" {
			page(tr.T("Login gagal"), tr.T("Notion tidak memberi token. Coba lagi dari gotion."))
			failed <- tr.T("tidak ada token")
			return
		}
		page(tr.T("Berhasil masuk"), tr.T("Silakan kembali ke gotion. Tab ini boleh ditutup."))
		result <- c
	})
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go srv.Serve(ln)
	a.oauth.url = fmt.Sprintf("%s/login?port=%d&state=%s", base, port, url.QueryEscape(state))
	openBrowser(a.oauth.url)

	go func() {
		var c creds
		var msg string
		select {
		case c = <-result:
		case msg = <-failed:
		case <-ctx.Done():
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				msg = tr.T("Login kelamaan, coba lagi.")
			}
		}
		// Let the browser get its page before the listener goes.
		time.Sleep(300 * time.Millisecond)
		srv.Close()
		cancel()
		a.update(func() {
			a.oauth = nil
			if c.Access == "" {
				a.tokenErr = msg
				return
			}
			a.signIn(c)
		})
	}()
}

func (a *App) cancelOAuth() {
	if a.oauth != nil {
		a.oauth.cancel()
	}
}

// signIn checks credentials with Notion and, when they work, keeps them.
func (a *App) signIn(c creds) {
	a.checking, a.tokenErr = true, ""
	client := a.clientFor(c)
	go func() {
		me, err := client.Me(context.Background())
		a.update(func() {
			a.checking = false
			if err != nil {
				a.tokenErr = tr.T("Notion menolak: ") + errText(err)
				return
			}
			saveToken(c.encode())
			a.client, a.bot, a.tokenInput = client, me, ""
			a.refresh()
			a.startLive()
		})
	}()
}

// browserPage is what the browser shows when Notion hands the sign-in back:
// gotion's mark, what happened and what to do next.
const browserPage = `<!doctype html><html lang="%s"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>gotion</title>
<style>body{margin:0;min-height:100vh;display:grid;place-items:center;background:#f5f5f4;color:#1d1d1f;font:16px/1.5 Inter,system-ui,sans-serif}
main{max-width:360px;padding:24px;text-align:center}h1{font-size:22px;margin:16px 0 6px}p{margin:0;color:#57534e}
@media (prefers-color-scheme:dark){body{background:#1c1c1e;color:#ededec}p{color:#a8a29e}}</style>
<main><svg width="56" height="56" viewBox="0 0 64 64" aria-hidden="true"><rect x="1" y="1" width="62" height="62" rx="14" fill="#fff" stroke="#e4e2df" stroke-width="2"/><rect x="14" y="16" width="30" height="7" rx="3.5" fill="#1d1d1f"/><rect x="14" y="29" width="36" height="5" rx="2.5" fill="#b5b2ad"/><rect x="14" y="40" width="26" height="5" rx="2.5" fill="#b5b2ad"/><rect x="43" y="38" width="2.5" height="9" rx="1.25" fill="#2563eb"/></svg>
<h1>%s</h1><p>%s</p></main></html>`
