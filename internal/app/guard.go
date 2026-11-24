package app

import (
	"github.com/egoist/mygo/ui/richtext"
)

// paragraphText is the text of the paragraph holding rune i, attachments
// as their object character: what a read-only paragraph keeps.
func paragraphText(t richtext.Text, i int) string {
	start, end := t.ParagraphRange(i)
	return t.Slice(start, end).String()
}

// guardReadOnly records what panel pn's read-only paragraphs hold, to tell
// later when an edit changed one.
func (a *App) guardReadOnly(pn *panel) {
	pn.readOnly = map[string]string{}
	if pn.session == nil || pn.doc == nil {
		return
	}
	ids := pn.session.ReadOnly()
	t := pn.doc.Text()
	for at, p := range t.Paragraphs() {
		if ids[p.Style.ID] {
			pn.readOnly[p.Style.ID] = paragraphText(t, at)
		}
	}
}

// brokeReadOnly reports whether the last edit changed what a read-only
// paragraph holds: typed into it, or joined another to it. Either leaves
// the caret in it, so the caret's paragraph is the one to look at: no
// pass over a long page on every key. Taking a read-only paragraph out
// whole is fine: that deletes its block.
func brokeReadOnly(d *richtext.Document, readOnly map[string]string) bool {
	if len(readOnly) == 0 {
		return false
	}
	t := d.Text()
	_, caret := d.Selection()
	sig, ok := readOnly[t.ParagraphStyleAt(caret).ID]
	return ok && paragraphText(t, caret) != sig
}

// emptiedReadOnly reports whether the last edit emptied the read-only
// paragraph at the caret, as Backspace or Delete on a picture does, and
// which it is.
func emptiedReadOnly(d *richtext.Document, readOnly map[string]string) (string, bool) {
	if len(readOnly) == 0 {
		return "", false
	}
	t := d.Text()
	_, caret := d.Selection()
	id := t.ParagraphStyleAt(caret).ID
	if _, ok := readOnly[id]; !ok {
		return "", false
	}
	return id, paragraphText(t, caret) == ""
}

// removeParagraph takes the paragraph named id out of the document.
func removeParagraph(d *richtext.Document, id string) bool {
	for i, p := range paragraphsOf(d) {
		if p.Style.ID == id {
			deleteBlock(d, i)
			return true
		}
	}
	return false
}
