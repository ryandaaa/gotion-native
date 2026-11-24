// Package syncer keeps one open page in step with Notion: it sends the
// blocks the user changed a moment after they stop typing, and brings in
// changes made elsewhere.
package syncer

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/egoist/mygo/ui"
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
	// Page is the page as it was opened.
	Page   *notion.Page
	client *notion.Client
	botID  string
	// OnRemote is called, from the session's goroutine, with the page's
	// text after it changed elsewhere.
	OnRemote func(text richtext.Text, extras []doc.Extra)
	// OnStatus is called when the status changes.
	OnStatus func()
	// OnError is called with a message when sending fails.
	OnError func(msg string)

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

	shape   doc.Shape
	force   bool // pull on the next tick, changed or not
	unsaved bool // pending holds edits not yet written to the cache
	sent    bool // pending was restored from disk, and pushed since
}

// CacheDir is where pages are kept between runs; "" keeps none.
var CacheDir string

type cached struct {
	Page   *notion.Page    `json:"page"`
	Blocks []*notion.Block `json:"blocks"`
}

type unsent struct {
	Text  richtext.Text         `json:"text"`
	Known map[string]*doc.Known `json:"known"`
}

func cacheFile(id, kind string) string {
	if CacheDir == "" {
		return ""
	}
	return filepath.Join(CacheDir, id+"."+kind+".json")
}

func readJSON(path string, v any) bool {
	if path == "" {
		return false
	}
	b, err := os.ReadFile(path)
	return err == nil && json.Unmarshal(b, v) == nil
}

func writeJSON(path string, v any) {
	if path == "" {
		return
	}
	if b, err := json.Marshal(v); err == nil {
		os.MkdirAll(filepath.Dir(path), 0o700)
		tmp := path + ".tmp"
		if os.WriteFile(tmp, b, 0o600) == nil {
			os.Rename(tmp, path)
		}
	}
}

// fetch loads a page, its blocks and their images.
func fetch(ctx context.Context, client *notion.Client, pageID string) (*notion.Page, []*notion.Block, error) {
	page, err := client.Page(ctx, pageID)
	if err != nil {
		return nil, nil, err
	}
	blocks, err := client.Tree(ctx, pageID, doc.MaxDepth)
	if err != nil {
		return nil, nil, err
	}
	writeJSON(cacheFile(pageID, "page"), cached{page, blocks})
	return page, blocks, nil
}

// loadImages fetches the images among blocks, four at a time.
func loadImages(ctx context.Context, blocks []*notion.Block) map[string]ui.ImageSource {
	var urls = map[string]string{}
	var walk func([]*notion.Block)
	walk = func(bs []*notion.Block) {
		for _, b := range bs {
			if b.Type == "image" {
				if u := b.ImageURL(); u != "" {
					urls[b.ID] = u
				}
			}
			walk(b.Children)
		}
	}
	walk(blocks)
	out := map[string]ui.ImageSource{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)
	client := &http.Client{Timeout: 20 * time.Second}
	for id, u := range urls {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
			if err != nil {
				return
			}
			resp, err := client.Do(req)
			if err != nil {
				return
			}
			defer resp.Body.Close()
			data, err := io.ReadAll(io.LimitReader(resp.Body, 20<<20))
			if err != nil || resp.StatusCode != 200 {
				return
			}
			if img, err := ui.DecodeImage(data); err == nil {
				mu.Lock()
				out[id] = img
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	return out
}

// Open loads a page and starts syncing it. The text it returns is for the
// editor's document.
//
// A page kept from an earlier run opens at once and is brought up to date
// in the background; edits not sent before the app closed are sent first.
func Open(ctx context.Context, client *notion.Client, botID, pageID string, shape doc.Shape) (*Session, richtext.Text, []doc.Extra, error) {
	s := &Session{PageID: pageID, client: client, botID: botID, shape: shape}
	var c cached
	haveCache := readJSON(cacheFile(pageID, "page"), &c) && c.Page != nil
	var u unsent
	if readJSON(cacheFile(pageID, "unsent"), &u) && u.Known != nil {
		s.known, s.pending, s.status, s.sent = u.Known, &u.Text, Pending, true
		s.first, s.last = time.Now().Add(-maxDelay), time.Now().Add(-idle)
		if haveCache {
			s.Page, s.seenTime, s.seenBy = c.Page, c.Page.LastEditedTime, c.Page.LastEditedBy.ID
		}
		_, _, extras := doc.Build(pageID, c.Blocks, shape)
		return s, u.Text, extras, nil
	}
	if haveCache {
		text, known, extras := doc.Build(pageID, c.Blocks, shape)
		s.Page, s.known, s.force = c.Page, known, true
		s.seenTime, s.seenBy = c.Page.LastEditedTime, c.Page.LastEditedBy.ID
		return s, text, extras, nil
	}
	// Not kept yet: show the page's own blocks as soon as they come, and
	// bring in what is nested in them and the images right after, in the
	// background (force), as Notion takes about three requests a second.
	page, err := client.Page(ctx, pageID)
	if err != nil {
		return nil, richtext.Text{}, nil, err
	}
	blocks, err := client.Tree(ctx, pageID, 0)
	if err != nil {
		return nil, richtext.Text{}, nil, err
	}
	text, known, extras := doc.Build(pageID, blocks, shape)
	s.Page, s.known, s.force = page, known, true
	s.seenTime, s.seenBy = page.LastEditedTime, page.LastEditedBy.ID
	return s, text, extras, nil
}

// Reshape shows the page again with toggles opened or closed.
func (s *Session) Reshape(shape doc.Shape) {
	s.mu.Lock()
	images := s.shape.Images
	s.shape = shape
	s.shape.Images = images
	s.force = true
	s.mu.Unlock()
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
	s.pending, s.last, s.unsaved = &text, now, true
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
		force := s.force && quiet
		var save *unsent
		if s.unsaved && s.pending != nil {
			s.unsaved = false
			save = &unsent{Text: *s.pending, Known: s.known}
		}
		s.mu.Unlock()
		if save != nil {
			// Keep the edits on disk until Notion has them.
			writeJSON(cacheFile(s.PageID, "unsent"), save)
		}
		if due {
			s.push(ctx)
		} else if force || quiet && time.Since(lastPoll) >= poll {
			lastPoll = time.Now()
			s.pull(ctx, force)
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
	done := s.pending == nil && err == nil
	if done && s.sent {
		// Restored edits are in Notion: show the page as Notion has it.
		s.sent, s.force = false, true
	}
	s.mu.Unlock()
	if done {
		if f := cacheFile(s.PageID, "unsent"); f != "" {
			os.Remove(f)
		}
	}
	if err != nil && s.OnError != nil {
		if next == Offline {
			s.OnError("Offline: edit disimpan di komputer dan dikirim saat koneksi kembali")
		} else {
			s.OnError("Gagal menyimpan ke Notion: " + err.Error())
		}
	}
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
		if present[id] || k.Hidden {
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
func (s *Session) pull(ctx context.Context, force bool) {
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
	if !force && (same || page.LastEditedBy.ID == s.botID) {
		return
	}
	page, blocks, err := fetch(ctx, s.client, s.PageID)
	if err != nil {
		return
	}
	s.mu.Lock()
	shape := s.shape
	s.force = false
	s.mu.Unlock()
	if shape.Images == nil || force {
		imgs := loadImages(ctx, blocks)
		for k, v := range shape.Images {
			if imgs[k] == nil {
				imgs[k] = v
			}
		}
		shape.Images = imgs
	}
	text, known, extras := doc.Build(s.PageID, blocks, shape)
	s.mu.Lock()
	s.shape.Images = shape.Images
	s.Page = page
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

// ReadOnly returns the IDs of the paragraphs that show blocks the editor
// does not edit: sub-pages, images, tables and the like.
func (s *Session) ReadOnly() map[string]bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]bool{}
	for id, k := range s.known {
		if k.ReadOnly && !k.Hidden {
			out[id] = true
		}
	}
	return out
}

// Attach adds a block after the one the editor's paragraph after shows
// (at the end of the page when it is not in Notion yet), as an uploaded
// picture or file, then brings the page in again to show it.
func (s *Session) Attach(ctx context.Context, after string, b notion.NewBlock) error {
	s.mu.Lock()
	parent, at := s.PageID, ""
	if k := s.known[after]; k != nil && !k.Hidden {
		parent, at = k.Parent, k.NotionID
	}
	s.mu.Unlock()
	if _, err := s.client.Append(ctx, parent, at, b); err != nil {
		return err
	}
	s.mu.Lock()
	s.force = true
	s.mu.Unlock()
	return nil
}

// Image returns a picture of the page by its block, nil when not loaded.
func (s *Session) Image(id string) ui.ImageSource {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.shape.Images[id]
}

// Movable reports whether the editor's paragraph id shows a block that can
// be moved, duplicated or deleted from a block menu: one Notion lets go of
// and makes again. Notion moves no blocks, so a move makes the block anew,
// and with it would go a block's children, and a sub-page's whole page.
func (s *Session) Movable(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := s.known[id]
	return k == nil || !k.ReadOnly && !k.HasChildren
}
