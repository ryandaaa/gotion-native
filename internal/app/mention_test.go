package app

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui/richtext"

	"gotion/internal/doc"
)

func TestMention(t *testing.T) {
	a := tabsApp(t)
	pn := &panel{id: "satu", doc: richtext.NewDocument(richtext.TextOf(richtext.Paragraph{Style: richtext.ParagraphStyle{ID: "p1"}, Runs: []richtext.Run{{Text: "Lihat @hal"}}}))}
	pn.doc.SetSelection(10, 10)
	a.mentionUpdate(pn)
	if !pn.mention.open || pn.mention.query != "hal" || pn.mention.start != 6 {
		t.Fatalf("state %+v", pn.mention)
	}
	// An e-mail address is no mention.
	pe := &panel{doc: richtext.NewDocument(richtext.TextOf(richtext.Paragraph{Runs: []richtext.Run{{Text: "nama@hal"}}}))}
	pe.doc.SetSelection(8, 8)
	a.mentionUpdate(pe)
	if pe.mention.open {
		t.Error("an @ inside a word opened the popup")
	}
	items := a.mentionMatches("dua")
	if len(items) != 1 || items[0].id != "dua" {
		t.Fatalf("matches %+v", items)
	}
	a.insertMention(pn, items[0])
	got := doc.Items(pn.doc.Text(), nil)
	b, _ := json.Marshal(got[0].Payload["rich_text"])
	if !strings.Contains(string(b), `"mention":{"page":{"id":"dua"},"type":"page"}`) || strings.Contains(string(b), "@hal") {
		t.Errorf("rich text %s", b)
	}
}
