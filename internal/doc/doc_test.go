package doc

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui/richtext"

	"gotion/internal/notion"
)

func blocks(t *testing.T, s string) []*notion.Block {
	t.Helper()
	var bs []*notion.Block
	if err := json.Unmarshal([]byte(s), &bs); err != nil {
		t.Fatal(err)
	}
	return bs
}

const page = `[
 {"object":"block","id":"a","type":"heading_1","heading_1":{"rich_text":[{"type":"text","plain_text":"Agenda","text":{"content":"Agenda"},"annotations":{"color":"default"}}]}},
 {"object":"block","id":"b","type":"paragraph","paragraph":{"rich_text":[
   {"type":"text","plain_text":"Hi ","text":{"content":"Hi "},"annotations":{"bold":true,"color":"default"}},
   {"type":"mention","plain_text":"@Ryan","mention":{"type":"user","user":{"id":"u1"}},"annotations":{"color":"default"}},
   {"type":"text","plain_text":" red","text":{"content":" red"},"annotations":{"color":"red"}}]}},
 {"object":"block","id":"c","type":"code","code":{"language":"go","rich_text":[{"type":"text","plain_text":"a := 1\nb := 2","text":{"content":"a := 1\nb := 2"},"annotations":{"color":"default"}}]}},
 {"object":"block","id":"d","type":"to_do","has_children":true,"to_do":{"checked":true,"rich_text":[{"type":"text","plain_text":"Done","text":{"content":"Done"},"annotations":{"color":"default"}}]},
  "children":[{"object":"block","id":"e","type":"bulleted_list_item","bulleted_list_item":{"rich_text":[{"type":"text","plain_text":"child","text":{"content":"child"},"annotations":{"color":"default"}}]}}]},
 {"object":"block","id":"f","type":"image","image":{}},
 {"object":"block","id":"g","type":"child_page","child_page":{"title":"Sub"}}
]`

func TestBuildThenItemsIsUnchanged(t *testing.T) {
	text, known, extras := Build("p", blocks(t, page))
	if len(extras) != 1 || extras[0].Title != "Sub" {
		t.Fatalf("extras = %+v", extras)
	}
	items := Items(text, known)
	if len(items) != 7 {
		t.Fatalf("got %d items, want 7 (a b c d e f g)", len(items))
	}
	if !known["f"].ReadOnly || items[5].Type != "image" || items[6].Type != "child_page" {
		t.Errorf("read-only blocks: %+v %+v", items[5], items[6])
	}
	for _, it := range items {
		if known[it.ID].Hash != it.Hash {
			t.Errorf("%s changed without an edit", it.ID)
		}
	}
	if items[4].Indent != 1 || known["e"].Parent != "d" {
		t.Errorf("child: indent %d parent %q", items[4].Indent, known["e"].Parent)
	}
	// The code block's two lines are one block again.
	rt := items[2].Payload["rich_text"].([]any)
	var content strings.Builder
	for _, r := range rt {
		if m, ok := r.(map[string]any); ok {
			content.WriteString(m["text"].(map[string]any)["content"].(string))
		}
	}
	if content.String() != "a := 1\nb := 2" || items[2].Payload["language"] != "go" {
		t.Errorf("code = %q %v", content.String(), items[2].Payload["language"])
	}
	// The mention goes back as Notion sent it, the color as its name.
	b, _ := json.Marshal(items[1].Payload["rich_text"])
	if !strings.Contains(string(b), `"mention":{"type":"user"`) || !strings.Contains(string(b), `"color":"red"`) {
		t.Errorf("paragraph b = %s", b)
	}
}

func TestEditChangesOneBlock(t *testing.T) {
	text, known, _ := Build("p", blocks(t, page))
	d := richtext.NewDocument(text)
	d.IdentifyParagraphs(NewID)
	// Type at the end of the heading.
	d.SetSelection(len("Agenda"), len("Agenda"))
	d.InsertText("!")
	changed := 0
	for _, it := range Items(d.Text(), known) {
		if k := known[it.ID]; k == nil || k.Hash != it.Hash {
			changed++
			if it.ID != "a" {
				t.Errorf("block %s changed", it.ID)
			}
		}
	}
	if changed != 1 {
		t.Errorf("%d blocks changed, want 1", changed)
	}
}

func TestStyleChangeChangesType(t *testing.T) {
	if got := TypeOf(StyleOf("toggle"), "toggle"); got != "toggle" {
		t.Errorf("toggle shown as a bullet became %s", got)
	}
	if got := TypeOf(richtext.ParagraphStyle{Heading: 2}, "paragraph"); got != "heading_2" {
		t.Errorf("got %s", got)
	}
}
