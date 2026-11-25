package app

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

// liveEvent is an event of the workspace, as the Worker relays Notion's
// webhooks.
type liveEvent struct {
	ID        string `json:"id"`
	Type      string `json:"type"`
	Timestamp string `json:"timestamp"`
	Entity    struct {
		ID   string `json:"id"`
		Type string `json:"type"`
	} `json:"entity"`
	Data struct {
		PageID string `json:"page_id"`
		Parent struct {
			ID   string `json:"id"`
			Type string `json:"type"`
		} `json:"parent"`
	} `json:"data"`
}

// livePoll is how often the app asks the Worker for events.
var livePoll = 8 * time.Second

// startLive asks the Worker for the workspace's events while signed in
// with Notion's login, whose integration has the webhook: pages made,
// moved or deleted show in the sidebar at once, and a page open goes
// fresh when it changes elsewhere. Without it, polling keeps them fresh.
func (a *App) startLive() {
	if a.liveCancel != nil || a.client == nil || a.client.Refresh == nil || oauthURL() == "" {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.liveCancel = cancel
	client, base := a.client, oauthURL()
	go func() {
		since := time.Now().UTC().Format(time.RFC3339)
		httpc := &http.Client{Timeout: 15 * time.Second}
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(livePoll):
			}
			req, err := http.NewRequestWithContext(ctx, "GET", base+"/events?since="+since, nil)
			if err != nil {
				continue
			}
			req.Header.Set("Authorization", "Bearer "+client.CurrentToken())
			resp, err := httpc.Do(req)
			if err != nil {
				continue
			}
			var out struct {
				Events []liveEvent `json:"events"`
			}
			json.NewDecoder(resp.Body).Decode(&out)
			resp.Body.Close()
			if len(out.Events) == 0 {
				continue
			}
			since = out.Events[len(out.Events)-1].Timestamp
			events := out.Events
			a.update(func() { a.onEvents(events) })
		}
	}()
}

func (a *App) stopLive() {
	if a.liveCancel != nil {
		a.liveCancel()
		a.liveCancel = nil
	}
}

// onEvents does what the workspace's events ask: the sidebar again for
// pages made, moved or gone; a page open, fresh; its comments, again.
func (a *App) onEvents(events []liveEvent) {
	tree := false
	today := false
	for _, e := range events {
		switch e.Type {
		case "page.created", "page.deleted", "page.undeleted", "page.moved", "page.properties_updated",
			"database.created", "database.deleted", "data_source.created", "data_source.deleted":
			tree = true
			today = true
		}
		if len(e.Type) > 8 && e.Type[:8] == "comment." {
			a.handleCommentEvent(e)
		}
		for _, pn := range a.panels {
			if pn == nil || pn.id == "" {
				continue
			}
			switch {
			case e.Type == "page.content_updated" && e.Entity.ID == pn.id && pn.session != nil:
				pn.session.Refresh()
			case len(e.Type) > 8 && e.Type[:8] == "comment." && pn.comments.loaded:
				a.loadComments(pn)
			}
		}
	}
	if tree {
		a.refresh()
	} else if today {
		a.loadToday()
	}
}
