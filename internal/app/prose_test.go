package app

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/richtext"

	"gotion/internal/doc"
	"gotion/internal/emoji"
	"gotion/internal/notion"
)

func txt(s string, extra string) string {
	return `{"type":"text","plain_text":` + jsonString(s) + `,"annotations":{"color":"default"` + extra + `}}`
}

func jsonString(s string) string { b, _ := json.Marshal(s); return string(b) }

var ppsiPage = `[
 {"object":"block","id":"h1","type":"heading_2","heading_2":{"rich_text":[` + txt("Dokumen Proyek", "") + `]}},
 {"object":"block","id":"cl","type":"column_list","has_children":true,"column_list":{},"children":[
  {"object":"block","id":"c1","type":"column","has_children":true,"column":{},"children":[
   {"object":"block","id":"l1","type":"bulleted_list_item","bulleted_list_item":{"rich_text":[` + txt("Laporan Proyek", `,"bold":true`) + `,` + txt(" — ", "") + `,{"type":"text","plain_text":"https://1drv.ms/w/c/cd77ef6413fdaf7b","text":{"content":"https://1drv.ms/w/c/cd77ef6413fdaf7b","link":{"url":"https://1drv.ms/w/c/cd77ef6413fdaf7b"}},"annotations":{"color":"default"}}]}},
   {"object":"block","id":"l2","type":"bulleted_list_item","bulleted_list_item":{"rich_text":[` + txt("Figma", `,"bold":true`) + `,` + txt(" — [Placeholder: link Figma]", "") + `]}}]},
  {"object":"block","id":"c2","type":"column","has_children":true,"column":{},"children":[
   {"object":"block","id":"l3","type":"bulleted_list_item","bulleted_list_item":{"rich_text":[` + txt("UI HTML Prototype", `,"bold":true`) + `,` + txt(" — ", "") + `,{"type":"text","plain_text":"Prototype Frontend","text":{"content":"Prototype Frontend","link":{"url":"https://example.com"}},"annotations":{"color":"default"}}]}}]}]},
 {"object":"block","id":"h2","type":"heading_2","heading_2":{"rich_text":[` + txt("Pembagian Peran", "") + `]}},
 {"object":"block","id":"p1","type":"paragraph","paragraph":{"rich_text":[` + txt("Meskipun proyek dikerjakan secara bersama-sama, setiap anggota disarankan memiliki penanggung jawab untuk peran tertentu, yaitu:", "") + `]}},
 {"object":"block","id":"p2","type":"paragraph","paragraph":{"rich_text":[` + txt("Paragraf kedua, supaya jarak antar paragraf kelihatan.", "") + `]}},
 {"object":"block","id":"co","type":"callout","callout":{"icon":{"type":"emoji","emoji":"💡"},"rich_text":[` + txt("Jangan lupa backup database sebelum migrasi.", "") + `]}},
 {"object":"block","id":"dv","type":"divider","divider":{}},
 {"object":"block","id":"h3","type":"heading_2","heading_2":{"rich_text":[` + txt("Fase Proyek", "") + `]}},
 {"object":"block","id":"s1","type":"child_page","child_page":{"title":"01. INISIASI PROYEK (Minggu 1-3)"}},
 {"object":"block","id":"s2","type":"child_page","child_page":{"title":"02. ANALISIS & PERANCANGAN SISTEM (Minggu 4-7)"}},
 {"object":"block","id":"db","type":"child_database","child_database":{"title":"Deliverables"}}
]`

func TestProse(t *testing.T) {
	var blocks []*notion.Block
	if err := json.Unmarshal([]byte(ppsiPage), &blocks); err != nil {
		t.Fatal(err)
	}
	_, known, _ := doc.Build("ppsi", blocks, doc.Shape{})
	// Columns show their blocks, and are never deleted for it.
	for _, id := range []string{"l1", "l2", "l3"} {
		if known[id] == nil || known[id].Hidden {
			t.Errorf("block %s in a column is not shown", id)
		}
	}
	if !known["cl"].Hidden || !known["c1"].Hidden {
		t.Error("columns are not kept")
	}
	for _, theme := range []string{"light", "dark"} {
		a := tableApp(t)
		a.set.Theme = theme
		a.nodes["ppsi"].Emoji = "📚"
		emoji.Load(context.Background(), "📚")
		p := &panel{id: "ppsi", title: "PPSI"}
		shape := doc.Shape{Width: &p.textWidth, Emoji: func(e string) ui.ImageSource {
			if s := emoji.Load(context.Background(), e); s != nil {
				return s
			}
			return nil
		}}
		text, _, _ := doc.Build("ppsi", blocks, shape)
		p.doc = richtext.NewDocument(text)
		a.panels[0] = p
		tester := ui.NewTester(a.View, 1280, 900)
		tester.Frame()
		tester.Frame()
		snapshot(t, tester, "prose-"+theme)
	}
}
