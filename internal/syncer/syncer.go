// Package syncer keeps one open page in step with Notion: it sends the
// blocks the user changed a moment after they stop typing, and brings in
// changes made elsewhere.
package syncer

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/egoist/mygo/ui/richtext"

	"gotion/internal/doc"
	"gotion/internal/notion"
)

// Status is how the page stands with Notion.
type Status int

const (
	Synced Status = iota
	Pending
	Syncing
	Failed
	Offline
)

func (s Status) String() string {
	return [...]string{"Tersimpan", "Belum sync", "Sinkron…", "Gagal", "Offline"}[s]
}

const (
	idle     = 1500 * time.Millisecond // send after this long without typing
	maxDelay = 5 * time.Second         // or after this long typing
	poll     = 15 * time.Second        // look for changes made elsewhere
)

// Session syncs one page.
type Session struct {
	PageID string
	client *notion.Client
	botID  string
	// OnRemote is called, from the session's goroutine, with the page's
	// text after it changed elsewhere.
	OnRemote func(text richtext.Text, extras []doc.Extra)
	// OnStatus is called when the status changes.
	OnStatus func()

	mu       sync.Mutex
	known    map[string]*doc.Known
	pending  *richtext.Text
	first    time.Time // of the edits not sent yet
	last     time.Time // of the last edit
	status   Status
	err      error
	seenTime time.Time
	seenBy   string
	cancel   context.CancelFunc
}

// Open loads a page and starts syncing it. The text it returns is for the
// editor's document.
func Open(ctx context.Context, client *notion.Client, botID, pageID string) (*Session, richtext.Text, []doc.Extra, error) {
	page, err := client.Page(ctx, pageID)
	if err != nil {
		return nil, richtext.Text{}, nil, err
	}
	blocks, err := client.Tree(ctx, pageID, doc.MaxDepth)
	if err != nil {
		return nil, richtext.Text{}, nil, err
	}
	text, known, extras := doc.Build(pageID, blocks)
	s := &Session{PageID: pageID, client: client, botID: botID, known: known,
		seenTime: page.LastEditedTime, seenBy: page.LastEditedBy.ID}
	return s, text, extras, nil
}

// Start runs the session until Close.
func (s *Session) Start() {
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	go s.run(ctx)
}

// Close sends what is left, then stops.
func (s *Session) Close() {
	s.mu.Lock()
	pending := s.pending
	s.mu.Unlock()
	if pending != nil {
		s.push(context.Background())
	}
	if s.cancel != nil {
		s.cancel()
	}
}

// Edited tells the session the editor's text after an edit.
func (s *Session) Edited(text richtext.Text) {
	s.mu.Lock()
	now := time.Now()
	if s.pending == nil {
		s.first = now
	}
	s.pending, s.last = &text, now
	changed := s.status != Pending && s.status != Syncing
	if changed {
		s.status = Pending
	}
	s.mu.Unlock()
	if changed {
		s.notify()
	}
}

// Status returns the status and the last error.
func (s *Session) Status() (Status, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status, s.err
}

func (s *Session) setStatus(st Status, err error) {
	s.mu.Lock()
	s.status, s.err = st, err
	s.mu.Unlock()
	s.notify()
}

func (s *Session) notify() {
	if s.OnStatus != nil {
		s.OnStatus()
	}
}

func (s *Session) run(ctx context.Context) {
	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()
	lastPoll := time.Now()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		s.mu.Lock()
		due := s.pending != nil && (time.Since(s.last) >= idle || time.Since(s.first) >= maxDelay)
		quiet := s.pending == nil
		s.mu.Unlock()
		if due {
			s.push(ctx)
		} else if quiet && time.Since(lastPoll) >= poll {
			lastPoll = time.Now()
			s.pull(ctx)
		}
	}
}

// push sends the blocks that changed since the last push.
func (s *Session) push(ctx context.Context) {
	s.mu.Lock()
	text := s.pending
	s.pending = nil
	s.status = Syncing
	s.mu.Unlock()
	if text == nil {
		return
	}
	s.notify()
	err := s.apply(ctx, doc.Items(*text, s.known))
	s.mu.Lock()
	next := Synced
	if s.pending != nil {
		next = Pending
	}
	if err != nil {
		next = Failed
		if _, ok := err.(*notion.Error); !ok {
			next = Offline
		}
		if s.pending == nil {
			// Try again with the same text.
			s.pending, s.first, s.last = text, time.Now(), time.Now()
		}
	}
	s.status, s.err = next, err
	s.mu.Unlock()
	s.notify()
}

// apply makes Notion's blocks match items. Only the session's goroutine
// touches known.
func (s *Session) apply(ctx context.Context, items []doc.Item) error {
	present := map[string]bool{}
	for _, it := range items {
		present[it.ID] = true
	}
	// Blocks gone from the editor go to the trash.
	for id, k := range s.known {
		if present[id] {
			continue
		}
		if err := s.client.DeleteBlock(ctx, k.NotionID); err != nil && !gone(err) {
			return err
		}
		delete(s.known, id)
	}
	for i, it := range items {
		k := s.known[it.ID]
		switch {
		case k == nil:
			if isEmptyLast(items, i) {
				continue // the empty line every document ends with
			}
			if err := s.create(ctx, items, i); err != nil {
				return err
			}
		case k.Hash == it.Hash:
		case k.Type == it.Type || k.HasChildren:
			payload := it.Payload
			t := k.Type
			if t != it.Type {
				log.Printf("gotion: %s has children, its type stays %s", k.NotionID, t)
			}
			if _, err := s.client.UpdateBlock(ctx, k.NotionID, t, payload); err != nil {
				return err
			}
			k.Hash = it.Hash
		default:
			// Notion cannot change a block's type: make it again.
			if err := s.client.DeleteBlock(ctx, k.NotionID); err != nil && !gone(err) {
				return err
			}
			delete(s.known, it.ID)
			if err := s.create(ctx, items, i); err != nil {
				return err
			}
		}
	}
	return nil
}

func isEmptyLast(items []doc.Item, i int) bool {
	if i != len(items)-1 || items[i].Type != "paragraph" {
		return false
	}
	rt, _ := items[i].Payload["rich_text"].([]any)
	return len(rt) == 0
}

// create makes the block of items[i] in Notion, where it sits among the
// blocks before it.
func (s *Session) create(ctx context.Context, items []doc.Item, i int) error {
	it := items[i]
	parent, after := s.PageID, "start"
	for j := i - 1; j >= 0; j-- {
		k := s.known[items[j].ID]
		if k == nil {
			continue
		}
		if items[j].Indent == it.Indent {
			parent, after = k.Parent, k.NotionID
			break
		}
		if items[j].Indent < it.Indent {
			if notion.Nests(k.Type) {
				parent, after = k.NotionID, "start"
			} else {
				parent, after = k.Parent, k.NotionID
			}
			break
		}
	}
	made, err := s.client.Append(ctx, parent, after, notion.NewBlock{Type: it.Type, Payload: it.Payload})
	if err != nil {
		return err
	}
	if len(made) == 0 {
		return fmt.Errorf("notion made no block")
	}
	// Append returns the parent's children; find the one made after "after".
	b := made[0]
	if after != "start" {
		for j, m := range made {
			if m.ID == after && j+1 < len(made) {
				b = made[j+1]
				break
			}
		}
	}
	s.known[it.ID] = &doc.Known{NotionID: b.ID, Type: it.Type, Parent: parent, Hash: it.Hash}
	return nil
}

func gone(err error) bool {
	e, ok := err.(*notion.Error)
	return ok && (e.Status == 404 || e.Status == 400)
}

// pull brings in the page again when someone else changed it.
func (s *Session) pull(ctx context.Context) {
	page, err := s.client.Page(ctx, s.PageID)
	if err != nil {
		if _, ok := err.(*notion.Error); !ok {
			s.setStatus(Offline, err)
		}
		return
	}
	s.mu.Lock()
	if s.status == Offline {
		s.status = Synced
	}
	same := page.LastEditedTime.Equal(s.seenTime) && page.LastEditedBy.ID == s.seenBy
	s.seenTime, s.seenBy = page.LastEditedTime, page.LastEditedBy.ID
	s.mu.Unlock()
	if same || page.LastEditedBy.ID == s.botID {
		return
	}
	blocks, err := s.client.Tree(ctx, s.PageID, doc.MaxDepth)
	if err != nil {
		return
	}
	text, known, extras := doc.Build(s.PageID, blocks)
	s.mu.Lock()
	if s.pending != nil {
		// The user is typing: their edits win, as the last writer.
		s.mu.Unlock()
		return
	}
	s.known = known
	s.mu.Unlock()
	if s.OnRemote != nil {
		s.OnRemote(text, extras)
	}
}
