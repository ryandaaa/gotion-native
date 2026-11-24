package app

import (
	"strings"

	"github.com/egoist/mygo/ui/richtext"
)

// signature is what a read-only paragraph holds: its text and attachments.
func signature(p richtext.Paragraph) string {
	var b strings.Builder
	for _, r := range p.Runs {
		b.WriteString(r.Text)
	}
	return b.String()
}

// guardReadOnly records what panel pn's read-only paragraphs hold, to tell
// later when an edit changed one.
func (a *App) guardReadOnly(pn *panel) {
	pn.readOnly = map[string]string{}
	if pn.session == nil || pn.doc == nil {
		return
	}
	ids := pn.session.ReadOnly()
	for _, p := range pn.doc.Text().Paragraphs() {
		if ids[p.Style.ID] {
			pn.readOnly[p.Style.ID] = signature(p)
		}
	}
}

// brokeReadOnly reports whether the document changed what a read-only
// paragraph holds: typed into it, or joined another to it. Taking a
// read-only paragraph out whole is fine: that deletes its block.
func brokeReadOnly(d *richtext.Document, readOnly map[string]string) bool {
	if len(readOnly) == 0 {
		return false
	}
	for _, p := range d.Text().Paragraphs() {
		if sig, ok := readOnly[p.Style.ID]; ok && signature(p) != sig {
			return true
		}
	}
	return false
}
