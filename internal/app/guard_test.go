package app

import (
	"testing"

	"github.com/egoist/mygo/ui/richtext"
)

func guarded() (*richtext.Document, map[string]string) {
	d := richtext.NewDocument(richtext.TextOf(
		richtext.Paragraph{Style: richtext.ParagraphStyle{ID: "a"}, Runs: []richtext.Run{{Text: "teks"}}},
		richtext.Paragraph{Style: richtext.ParagraphStyle{ID: "sub"}, Runs: []richtext.Run{{Text: "📄 Sub"}}},
		richtext.Paragraph{Style: richtext.ParagraphStyle{ID: "b"}, Runs: []richtext.Run{{Text: "akhir"}}},
	))
	d.IdentifyParagraphs(func() string { return "new" })
	ro := map[string]string{}
	for at, p := range d.Text().Paragraphs() {
		if p.Style.ID == "sub" {
			ro["sub"] = paragraphText(d.Text(), at)
		}
	}
	return d, ro
}

// offset of the end of paragraph i.
func endOf(d *richtext.Document, i int) int {
	n := 0
	for at := range d.Text().Paragraphs() {
		if n == i {
			return at + len([]rune(paragraphText(d.Text(), at)))
		}
		n++
	}
	return -1
}

func TestReadOnlyGuard(t *testing.T) {
	d, ro := guarded()
	// Typing into the sub-page's line breaks it.
	d.SetSelection(endOf(d, 1), endOf(d, 1))
	d.InsertText("x")
	if !brokeReadOnly(d, ro) {
		t.Error("typing into a read-only line went unnoticed")
	}
	d.Undo()
	if brokeReadOnly(d, ro) {
		t.Error("undo did not restore the line")
	}

	// Backspace at the start of the next line joins it on: broken.
	d, ro = guarded()
	start := endOf(d, 1) + 1
	d.SetSelection(start-1, start)
	d.InsertText("")
	if !brokeReadOnly(d, ro) {
		t.Error("joining a line to a read-only one went unnoticed")
	}

	// Enter at its end makes a new line below: fine.
	d, ro = guarded()
	d.SetSelection(endOf(d, 1), endOf(d, 1))
	d.InsertText("\nbaru")
	if brokeReadOnly(d, ro) {
		t.Error("a new line after a read-only one was refused")
	}

	// Taking the whole line out deletes the block: fine.
	d, ro = guarded()
	d.SetSelection(endOf(d, 0), endOf(d, 1))
	d.InsertText("")
	if brokeReadOnly(d, ro) {
		t.Error("deleting a read-only line whole was refused")
	}
}

func TestBackspaceEmptiesAPicture(t *testing.T) {
	obj := string(richtext.ObjectReplacement)
	d := richtext.NewDocument(richtext.TextOf(
		richtext.Paragraph{Style: richtext.ParagraphStyle{ID: "a"}, Runs: []richtext.Run{{Text: "teks"}}},
		richtext.Paragraph{Style: richtext.ParagraphStyle{ID: "img"}, Runs: []richtext.Run{{Text: obj}}},
	))
	ro := map[string]string{"img": obj}
	// The caret after the picture, Backspace.
	d.SetSelection(5, 6)
	d.InsertText("")
	id, emptied := emptiedReadOnly(d, ro)
	if id != "img" || !emptied {
		t.Fatalf("id %q emptied %v", id, emptied)
	}
	removeParagraph(d, "img")
	if got := paragraphsTexts(d); got != "teks" {
		t.Errorf("%q", got)
	}
}
