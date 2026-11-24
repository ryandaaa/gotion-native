// Package emoji draws emoji as Twemoji's SVGs, the same on every system,
// instead of the system's own font. Each one is fetched once, then kept
// on disk.
//
// Twemoji graphics are by Twitter, Inc and other contributors, CC-BY 4.0
// (https://github.com/jdecked/twemoji).
package emoji

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo/ui"
)

// BaseURL is where the SVGs come from, by code point.
const BaseURL = "https://cdn.jsdelivr.net/gh/jdecked/twemoji@15.1.0/assets/svg/"

var (
	// Dir keeps the SVGs fetched; "" keeps none.
	Dir string
	// OnLoad is called, from any goroutine, when an emoji asked for with
	// Get has come: the app draws again.
	OnLoad func()

	mu      sync.Mutex
	loaded  = map[string]*ui.SVG{}
	failed  = map[string]bool{}
	pending = map[string]bool{}
	client  = &http.Client{Timeout: 8 * time.Second}
)

// Code returns Twemoji's name for an emoji: its code points in hex,
// joined by "-", the variation selector left out unless the emoji joins
// others (a ZWJ sequence).
func Code(e string) string {
	joins := strings.ContainsRune(e, '‍')
	var parts []string
	for _, r := range e {
		if r == '️' && !joins {
			continue
		}
		parts = append(parts, fmt.Sprintf("%x", r))
	}
	return strings.Join(parts, "-")
}

// Get returns the emoji's SVG, or nil while it is coming or when it
// cannot: then fetches it in the background. Safe from any goroutine.
func Get(e string) *ui.SVG {
	code := Code(e)
	if code == "" {
		return nil
	}
	mu.Lock()
	if s, ok := loaded[code]; ok || failed[code] {
		mu.Unlock()
		return s
	}
	if s := fromDisk(code); s != nil {
		loaded[code] = s
		mu.Unlock()
		return s
	}
	if !pending[code] {
		pending[code] = true
		go func() {
			fetch(context.Background(), code)
			if OnLoad != nil {
				OnLoad()
			}
		}()
	}
	mu.Unlock()
	return nil
}

// Load returns the emoji's SVG, fetching it now when it is not kept: for
// goroutines that build a page in the background.
func Load(ctx context.Context, e string) *ui.SVG {
	if s := Get(e); s != nil {
		return s
	}
	code := Code(e)
	return fetch(ctx, code)
}

func fromDisk(code string) *ui.SVG {
	if Dir == "" {
		return nil
	}
	data, err := os.ReadFile(filepath.Join(Dir, code+".svg"))
	if err != nil {
		return nil
	}
	s, err := ui.ParseSVG(data)
	if err != nil {
		return nil
	}
	return s
}

func fetch(ctx context.Context, code string) *ui.SVG {
	mu.Lock()
	if s, ok := loaded[code]; ok {
		mu.Unlock()
		return s
	}
	mu.Unlock()
	s := download(ctx, code)
	mu.Lock()
	defer mu.Unlock()
	delete(pending, code)
	if s == nil {
		failed[code] = true
		return nil
	}
	loaded[code] = s
	return s
}

func download(ctx context.Context, code string) *ui.SVG {
	req, err := http.NewRequestWithContext(ctx, "GET", BaseURL+code+".svg", nil)
	if err != nil {
		return nil
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 256<<10))
	if err != nil {
		return nil
	}
	s, err := ui.ParseSVG(data)
	if err != nil {
		return nil
	}
	if Dir != "" {
		os.MkdirAll(Dir, 0o700)
		os.WriteFile(filepath.Join(Dir, code+".svg"), data, 0o600)
	}
	return s
}

// View shows an emoji size DIPs square: its SVG, or the system's glyph
// until the SVG comes.
func View(c *ui.Context, e string, size float32) ui.Element {
	if s := Get(e); s != nil {
		return ui.Image(c, s).Size(size, size).Shrink(0)
	}
	return ui.Text(c, e).FontSize(size * 0.85).Shrink(0)
}
