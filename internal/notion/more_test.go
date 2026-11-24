package notion

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMoreEndpoints(t *testing.T) {
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got = append(got, r.Method+" "+r.URL.RequestURI()+" "+string(b))
		switch {
		case strings.HasPrefix(r.URL.Path, "/comments") && r.Method == "GET":
			io.WriteString(w, `{"results":[{"id":"c1","discussion_id":"d1","rich_text":[{"plain_text":"halo"}],"display_name":{"resolved_name":"Ryan"}}],"has_more":false}`)
		case r.URL.Path == "/users":
			io.WriteString(w, `{"results":[{"id":"u1","type":"person","name":"Ryan"}],"has_more":false}`)
		case strings.HasSuffix(r.URL.Path, "/markdown"):
			io.WriteString(w, `{"markdown":"# Judul"}`)
		default:
			io.WriteString(w, `{}`)
		}
	}))
	defer srv.Close()
	c := New("t")
	c.Base = srv.URL
	ctx := context.Background()
	cs, err := c.Comments(ctx, "p1")
	if err != nil || len(cs) != 1 || cs[0].DisplayName.ResolvedName != "Ryan" || PlainText(cs[0].RichText) != "halo" {
		t.Fatalf("comments %+v %v", cs, err)
	}
	c.AddComment(ctx, "p1", "", "baru")
	c.AddComment(ctx, "p1", "d1", "balas")
	us, _ := c.Users(ctx)
	md, _ := c.Markdown(ctx, "p1")
	c.MovePage(ctx, "p1", "p2")
	c.TrashPage(ctx, "p1", true)
	c.SetIcon(ctx, "p1", "🔥")
	c.CreatePageMarkdown(ctx, "p2", "Impor", "# Hai\nisi")
	c.CreateDatabase(ctx, "p2", "Tugas")
	c.SetDataSourceTitle(ctx, "ds1", "db1", "Proyek Baru")
	if len(us) != 1 || md != "# Judul" {
		t.Errorf("users %v md %q", us, md)
	}
	all := strings.Join(got, "\n")
	for _, want := range []string{
		"GET /comments?block_id=p1&page_size=100",
		`POST /comments {"parent":{"page_id":"p1"},"rich_text":[{"text":{"content":"baru"}}]}`,
		`POST /comments {"discussion_id":"d1"`,
		`POST /pages/p1/move {"parent":{"page_id":"p2","type":"page_id"}}`,
		`PATCH /pages/p1 {"in_trash":true}`,
		`PATCH /pages/p1 {"icon":{"emoji":"🔥","type":"emoji"}}`,
		`"markdown":"# Hai\nisi","markdown_version":"v2"`,
		"GET /pages/p1/markdown?markdown_version=v2",
		`POST /databases {"initial_data_source":{"properties":{"Beres":{"checkbox":{},"type":"checkbox"}`,
		`"parent":{"page_id":"p2","type":"page_id"},"title":[{"text":{"content":"Tugas"}}]}`,
		`PATCH /data_sources/ds1 {"title":[{"text":{"content":"Proyek Baru"},"type":"text"}]}`,
		`PATCH /databases/db1 {"title":[{"text":{"content":"Proyek Baru"},"type":"text"}]}`,
	} {
		if !strings.Contains(all, want) {
			t.Errorf("missing %s in\n%s", want, all)
		}
	}
	b, _ := json.Marshal(Condition(Property{Name: "Status", Type: "status"}, "", "Done"))
	if string(b) != `{"property":"Status","status":{"equals":"Done"}}` {
		t.Errorf("condition %s", b)
	}
	b, _ = json.Marshal(Condition(Property{Name: "Nama", Type: "title"}, "", "rapat"))
	if string(b) != `{"property":"Nama","title":{"contains":"rapat"}}` {
		t.Errorf("title condition %s", b)
	}
}
