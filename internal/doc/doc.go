// Package doc converts Notion blocks to the rich text the editor edits and
// back. Each paragraph's ID is its block's ID, so an edit maps to the one
// block it changed.
package doc

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/richtext"

	"gotion/internal/notion"
)

// MaxDepth is how deep nested blocks are shown.
const MaxDepth = 3

const (
	contPrefix  = "cont:"  // a later line of a block's text: cont:<id>:<n>
	localPrefix = "local-" // a paragraph not yet in Notion
	rawTag      = "raw:"   // a rich text item kept as Notion sent it
)

// NewID names a new paragraph.
func NewID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return localPrefix + hex.EncodeToString(b)
}

// IsLocal reports whether a paragraph ID is not a Notion block's.
func IsLocal(id string) bool {
	return strings.HasPrefix(id, localPrefix) || strings.HasPrefix(id, contPrefix)
}

// Known is what the app keeps about a block shown in the editor.
type Known struct {
	NotionID    string
	Type        string
	Parent      string // the Notion ID of its parent block or page
	HasChildren bool
	Language    string
	Hash        string // of the type and payload last synced
	// ReadOnly blocks show in place but are never written: only deleted.
	ReadOnly bool
}

// LinkPrefix starts the links the editor shows for sub-pages and the
// blocks it does not edit: gotion:page/<id>, gotion:db/<id>, gotion:open/<id>.
const LinkPrefix = "gotion:"

// DividerColor is the color dividers are drawn with.
var DividerColor = ui.Hex("#D6D3D1")

func divider() *richtext.Attachment {
	return &richtext.Attachment{Name: "divider", Width: 2000, Height: 14, Draw: func(p *ui.Painter, r ui.Rect) {
		p.Fill(ui.Rect{X: r.X, Y: r.Y + r.H/2, W: r.W, H: 1}, DividerColor, 0)
	}}
}

// readOnlyParagraph is how a block the editor does not edit shows.
func readOnlyParagraph(b *notion.Block, depth int) richtext.Paragraph {
	st := richtext.ParagraphStyle{ID: b.ID, Indent: depth}
	p := b.Text()
	muted := richtext.Attributes{Color: ui.Hex("#78716C"), Italic: true}
	link := func(prefix, label string) []richtext.Run {
		return []richtext.Run{{Text: label, Attributes: richtext.Attributes{Link: LinkPrefix + prefix + b.ID, Underline: true}}}
	}
	switch b.Type {
	case "divider":
		return richtext.Paragraph{Style: st, Runs: []richtext.Run{{Text: string(richtext.ObjectReplacement), Attributes: richtext.Attributes{Attachment: divider()}}}}
	case "child_page":
		title := strings.TrimSpace(p.Title)
		if title == "" {
			title = "Tanpa judul"
		}
		return richtext.Paragraph{Style: st, Runs: append([]richtext.Run{{Text: "📄 "}}, link("page/", title)...)}
	case "child_database":
		title := strings.TrimSpace(p.Title)
		if title == "" || title == "-" {
			title = "Database tanpa judul"
		}
		return richtext.Paragraph{Style: st, Runs: append([]richtext.Run{{Text: "▦ "}}, link("db/", title)...)}
	}
	label := "Block " + strings.ReplaceAll(b.Type, "_", " ") + " — buka di Notion ↗"
	runs := link("open/", label)
	runs[0].Attributes.Color, runs[0].Attributes.Italic = muted.Color, true
	return richtext.Paragraph{Style: st, Runs: runs}
}

// Extra is a block the editor does not edit: a sub-page, a database or a
// kind of block it does not know.
type Extra struct {
	ID, Type, Title string
}

// Editable reports whether the editor edits blocks of type t.
func Editable(t string) bool {
	switch t {
	case "paragraph", "heading_1", "heading_2", "heading_3", "bulleted_list_item",
		"numbered_list_item", "to_do", "quote", "callout", "toggle", "code":
		return true
	}
	return false
}

// StyleOf returns the paragraph style a block type shows with.
func StyleOf(t string) richtext.ParagraphStyle {
	var s richtext.ParagraphStyle
	switch t {
	case "heading_1":
		s.Heading = 1
	case "heading_2":
		s.Heading = 2
	case "heading_3":
		s.Heading = 3
	case "bulleted_list_item", "toggle":
		s.List = richtext.Bullets
	case "numbered_list_item":
		s.List = richtext.Numbers
	case "to_do":
		s.List = richtext.Tasks
	case "quote", "callout":
		s.Quote = true
	case "code":
		s.CodeBlock = true
	}
	return s
}

// TypeOf returns the block type of a paragraph style, keeping old, the
// block's type until now, while the style still matches it.
func TypeOf(s richtext.ParagraphStyle, old string) string {
	if old != "" && sameKind(StyleOf(old), s) {
		return old
	}
	switch {
	case s.CodeBlock:
		return "code"
	case s.Heading >= 1 && s.Heading <= 3:
		return "heading_" + strconv.Itoa(s.Heading)
	case s.Heading > 3:
		return "heading_3"
	case s.List == richtext.Bullets:
		return "bulleted_list_item"
	case s.List == richtext.Numbers:
		return "numbered_list_item"
	case s.List == richtext.Tasks:
		return "to_do"
	case s.Quote:
		return "quote"
	}
	return "paragraph"
}

func sameKind(a, b richtext.ParagraphStyle) bool {
	return a.Heading == b.Heading && a.List == b.List && a.Quote == b.Quote && a.CodeBlock == b.CodeBlock
}

// Build turns a page's blocks into the editor's text, what it knows of each
// paragraph's block, and the blocks it does not edit.
func Build(pageID string, blocks []*notion.Block) (richtext.Text, map[string]*Known, []Extra) {
	known := map[string]*Known{}
	var paras []richtext.Paragraph
	var extras []Extra
	var walk func(parent string, bs []*notion.Block, depth int)
	walk = func(parent string, bs []*notion.Block, depth int) {
		for _, b := range bs {
			if b.InTrash {
				continue
			}
			if !Editable(b.Type) {
				p := b.Text()
				if b.Type == "child_page" || b.Type == "child_database" {
					extras = append(extras, Extra{ID: b.ID, Type: b.Type, Title: p.Title})
				}
				paras = append(paras, readOnlyParagraph(b, depth))
				known[b.ID] = &Known{NotionID: b.ID, Type: b.Type, Parent: parent, HasChildren: b.HasChildren, ReadOnly: true, Hash: "ro"}
				continue
			}
			p := b.Text()
			style := StyleOf(b.Type)
			style.Indent = depth
			style.Checked = p.Checked
			lines := linesOf(p.RichText)
			for i, runs := range lines {
				st := style
				st.ID = b.ID
				if i > 0 {
					st.ID = contPrefix + b.ID + ":" + strconv.Itoa(i)
				}
				paras = append(paras, richtext.Paragraph{Style: st, Runs: runs})
			}
			k := &Known{NotionID: b.ID, Type: b.Type, Parent: parent, HasChildren: b.HasChildren, Language: p.Language}
			known[b.ID] = k
			if depth+1 < MaxDepth && len(b.Children) > 0 {
				walk(b.ID, b.Children, depth+1)
			} else if b.HasChildren && notion.Nests(b.Type) && depth+1 >= MaxDepth {
				extras = append(extras, Extra{ID: b.ID, Type: "nested", Title: notion.PlainText(p.RichText)})
			}
		}
	}
	walk(pageID, blocks, 0)
	if len(paras) == 0 {
		paras = append(paras, richtext.Paragraph{Style: richtext.ParagraphStyle{ID: NewID()}})
	}
	text := richtext.TextOf(paras...)
	// Hash what was loaded, so only real edits are sent.
	for _, it := range Items(text, known) {
		if k := known[it.ID]; k != nil && !k.ReadOnly {
			k.Hash = it.Hash
		}
	}
	return text, known, extras
}

// linesOf splits rich text into the runs of each of its lines.
func linesOf(rt []notion.RichText) [][]richtext.Run {
	lines := [][]richtext.Run{nil}
	for _, r := range rt {
		attrs := attributesOf(r)
		if attrs.Tag != "" {
			// A mention or equation stays whole on its line.
			lines[len(lines)-1] = append(lines[len(lines)-1], richtext.Run{Text: strings.ReplaceAll(r.PlainText, "\n", " "), Attributes: attrs})
			continue
		}
		for i, part := range strings.Split(r.PlainText, "\n") {
			if i > 0 {
				lines = append(lines, nil)
			}
			if part != "" {
				lines[len(lines)-1] = append(lines[len(lines)-1], richtext.Run{Text: part, Attributes: attrs})
			}
		}
	}
	return lines
}

func attributesOf(r notion.RichText) richtext.Attributes {
	a := r.Annotations
	var at richtext.Attributes
	if r.Type != "text" {
		at.Tag = rawTag + string(r.Raw)
		at.Color = ui.Hex("#2563EB")
		return at
	}
	if a.Bold {
		at.Weight = 700
	}
	at.Italic, at.Underline, at.Strikethrough, at.Code = a.Italic, a.Underline, a.Strikethrough, a.Code
	if r.Text != nil && r.Text.Link != nil {
		at.Link = r.Text.Link.URL
	}
	if c, ok := colors[a.Color]; ok {
		if strings.HasSuffix(a.Color, "_background") {
			at.Background = c
		} else {
			at.Color = c
		}
	}
	return at
}

// colors are Notion's text and background colors as the editor shows them.
var colors = map[string]ui.Color{
	"gray": ui.Hex("#787774"), "brown": ui.Hex("#9F6B53"), "orange": ui.Hex("#D9730D"),
	"yellow": ui.Hex("#CB912F"), "green": ui.Hex("#448361"), "blue": ui.Hex("#337EA9"),
	"purple": ui.Hex("#9065B0"), "pink": ui.Hex("#C14C8A"), "red": ui.Hex("#D44C47"),
	"gray_background": ui.Hex("#F1F1EF"), "brown_background": ui.Hex("#F4EEEE"),
	"orange_background": ui.Hex("#FAEBDD"), "yellow_background": ui.Hex("#FBF3DB"),
	"green_background": ui.Hex("#EDF3EC"), "blue_background": ui.Hex("#E7F3F8"),
	"purple_background": ui.Hex("#F6F3F9"), "pink_background": ui.Hex("#FAF1F5"),
	"red_background": ui.Hex("#FDEBEC"),
}

func colorName(at richtext.Attributes) string {
	for name, c := range colors {
		if strings.HasSuffix(name, "_background") {
			if at.Background == c {
				return name
			}
		} else if at.Color == c {
			return name
		}
	}
	return "default"
}

// Item is a block as the editor holds it now.
type Item struct {
	ID      string // the paragraph's ID: a block ID, or a local one
	Type    string
	Indent  int
	Payload map[string]any
	Hash    string
}

// Items groups the editor's paragraphs into blocks: a block's later lines
// join it again.
func Items(text richtext.Text, known map[string]*Known) []Item {
	var items []Item
	var runs [][]richtext.Run // the lines of the last item
	flush := func() {
		if len(items) == 0 {
			return
		}
		it := &items[len(items)-1]
		if k := known[it.ID]; k != nil && k.ReadOnly {
			it.Type, it.Hash = k.Type, k.Hash
			return
		}
		var rt []any
		for i, line := range runs {
			if i > 0 {
				rt = appendText(rt, "\n", richtext.Attributes{})
			}
			for _, r := range line {
				if strings.HasPrefix(r.Attributes.Tag, rawTag) {
					rt = append(rt, json.RawMessage(r.Attributes.Tag[len(rawTag):]))
					continue
				}
				rt = appendText(rt, r.Text, r.Attributes)
			}
		}
		if rt == nil {
			rt = []any{}
		}
		it.Payload["rich_text"] = rt
		b, _ := json.Marshal(struct {
			T string
			P map[string]any
		}{it.Type, it.Payload})
		it.Hash = string(b)
	}
	for _, p := range text.Paragraphs() {
		id := p.Style.ID
		if strings.HasPrefix(id, contPrefix) && len(items) > 0 {
			owner := strings.SplitN(strings.TrimPrefix(id, contPrefix), ":", 2)[0]
			if items[len(items)-1].ID == owner {
				runs = append(runs, p.Runs)
				continue
			}
		}
		flush()
		old := ""
		var k *Known
		if k = known[id]; k != nil {
			old = k.Type
		}
		it := Item{ID: id, Type: TypeOf(p.Style, old), Indent: p.Style.Indent, Payload: map[string]any{}}
		switch it.Type {
		case "to_do":
			it.Payload["checked"] = p.Style.Checked
		case "code":
			lang := "plain text"
			if k != nil && k.Language != "" {
				lang = k.Language
			}
			it.Payload["language"] = lang
		}
		items = append(items, it)
		runs = [][]richtext.Run{p.Runs}
	}
	flush()
	return items
}

// appendText adds text with attributes to rich text, in pieces Notion takes.
func appendText(rt []any, s string, at richtext.Attributes) []any {
	for s != "" {
		n := len(s)
		if utf8.RuneCountInString(s) > 1900 {
			n = 0
			for i := 0; i < 1900; i++ {
				_, size := utf8.DecodeRuneInString(s[n:])
				n += size
			}
		}
		part := s[:n]
		s = s[n:]
		text := map[string]any{"content": part}
		if at.Link != "" {
			text["link"] = map[string]any{"url": at.Link}
		}
		rt = append(rt, map[string]any{
			"type": "text",
			"text": text,
			"annotations": map[string]any{
				"bold": at.Bold(), "italic": at.Italic, "strikethrough": at.Strikethrough,
				"underline": at.Underline, "code": at.Code, "color": colorName(at),
			},
		})
	}
	return rt
}
