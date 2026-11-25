package doc

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui/richtext"

	"gotion/internal/notion"
)

func TestToggleAndCalloutItems(t *testing.T) {
	d := richtext.NewDocument(richtext.TextOf(
		richtext.Paragraph{Style: richtext.ParagraphStyle{ID: NewID()}},
	))
	d.Insert(Toggle(""))
	d.InsertText("Isi toggle\n")
	d.Insert(Callout("💡", Shape{}))
	d.ToggleQuote()
	d.InsertText("Isi callout")

	items := Items(d.Text(), map[string]*Known{})
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	if items[0].Type != "toggle" {
		t.Errorf("first item type = %q, want toggle", items[0].Type)
	}
	if items[1].Type != "callout" {
		t.Errorf("second item type = %q, want callout", items[1].Type)
	}
	icon, ok := items[1].Payload["icon"].(map[string]any)
	if !ok || icon["emoji"] != "💡" {
		t.Errorf("callout icon = %#v, want 💡", items[1].Payload["icon"])
	}
}

func TestBookmarkEmbedLinkPreview(t *testing.T) {
	raw := `[
		{"object":"block","id":"bm1","type":"bookmark","bookmark":{"url":"https://example.com/docs","caption":[{"type":"text","plain_text":"Docs"}]}},
		{"object":"block","id":"em1","type":"embed","embed":{"url":"https://youtu.be/abc"}},
		{"object":"block","id":"lp1","type":"link_preview","link_preview":{"url":"https://github.com/org/repo"}},
		{"object":"block","id":"bad1","type":"bookmark","bookmark":{"url":"javascript:alert(1)"}},
		{"object":"block","id":"empty1","type":"bookmark","bookmark":{"url":""}}
	]`
	text, _, _ := Build("p", blocks(t, raw), Shape{})
	var paras []richtext.Paragraph
	for _, p := range text.Paragraphs() {
		paras = append(paras, p)
	}
	if len(paras) != 5 {
		t.Fatalf("expected 5 paragraphs, got %d", len(paras))
	}
	paraText := func(p richtext.Paragraph) string {
		var s string
		for _, r := range p.Runs {
			s += r.Text
		}
		return s
	}
	if got := paraText(paras[0]); !strings.Contains(got, "Docs") || !strings.Contains(got, "https://example.com/docs") {
		t.Errorf("bookmark = %q", got)
	}
	if got := paraText(paras[1]); !strings.Contains(got, "youtu.be") {
		t.Errorf("embed = %q", got)
	}
	if got := paraText(paras[2]); !strings.Contains(got, "github.com") {
		t.Errorf("link_preview = %q", got)
	}
	for _, idx := range []int{3, 4} {
		for _, r := range paras[idx].Runs {
			if strings.HasPrefix(r.Attributes.Link, "javascript:") || r.Attributes.Link == "" && strings.Contains(r.Text, "javascript:") {
				t.Errorf("invalid bookmark %d leaked unsafe link: %+v", idx, r)
			}
		}
	}
}

func TestMaxDepth6(t *testing.T) {
	if MaxDepth != 6 {
		t.Fatalf("MaxDepth = %d, want 6", MaxDepth)
	}
	// Build a chain 6 levels deep (depths 0..5).
	leaf := &notion.Block{ID: "d5", Type: "paragraph", Payload: json.RawMessage(`{"rich_text":[{"type":"text","plain_text":"level 5"}]}`)}
	cur := leaf
	for d := 4; d >= 0; d-- {
		cur = &notion.Block{
			ID:          "d" + string(rune('0'+d)),
			Type:        "bulleted_list_item",
			HasChildren: true,
			Payload:     json.RawMessage(`{"rich_text":[{"type":"text","plain_text":"item"}]}`),
			Children:    []*notion.Block{cur},
		}
	}
	text, known, _ := Build("p", []*notion.Block{cur}, Shape{})
	if known["d5"] == nil {
		t.Fatalf("block at depth 5 missing from known")
	}
	var paras []richtext.Paragraph
	for _, p := range text.Paragraphs() {
		paras = append(paras, p)
	}
	last := paras[len(paras)-1]
	if last.Style.ID != "d5" || last.Style.Indent != 5 {
		t.Errorf("depth 5 paragraph = (%q, indent=%d), want (d5, 5)", last.Style.ID, last.Style.Indent)
	}
}

func TestTableEmptyAndHit(t *testing.T) {
	emptyTable := &notion.Block{ID: "t0", Type: "table", Payload: json.RawMessage(`{"table_width":2}`)}
	text, _, _ := Build("p", []*notion.Block{emptyTable}, Shape{})
	var paras []richtext.Paragraph
	for _, p := range text.Paragraphs() {
		paras = append(paras, p)
	}
	if len(paras) != 1 || len(paras[0].Runs) == 0 || paras[0].Runs[0].Attributes.Attachment == nil {
		t.Fatalf("empty table should still render attachment")
	}
	meta, ok := paras[0].Runs[0].Attributes.Attachment.Data.(*TableMeta)
	if !ok || len(meta.Rows) != 0 {
		t.Fatalf("expected 0 rows in empty TableMeta, got %+v", meta)
	}
	// Clicking below rows hits addRow.
	meta.Width = 200
	meta.Rect.H = addRowHeight + 1
	_, _, addRow, hit := TableHit(meta, 10, 5)
	if !hit || !addRow {
		t.Errorf("expected addRow hit on empty table, got addRow=%v hit=%v", addRow, hit)
	}
}
