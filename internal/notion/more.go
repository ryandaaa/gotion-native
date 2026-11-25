package notion

import (
	"context"
	"gotion/internal/tr"
	"net/url"
	"time"
)

// Comment is a comment on a page or a block.
type Comment struct {
	ID           string     `json:"id"`
	DiscussionID string     `json:"discussion_id"`
	CreatedTime  time.Time  `json:"created_time"`
	CreatedBy    User       `json:"created_by"`
	RichText     []RichText `json:"rich_text"`
	DisplayName  struct {
		Type         string `json:"type"`
		ResolvedName string `json:"resolved_name"`
	} `json:"display_name"`
}

// Comments returns the open comments of a page or block, oldest first.
func (c *Client) Comments(ctx context.Context, blockID string) ([]Comment, error) {
	var all []Comment
	cursor := ""
	for {
		q := url.Values{"block_id": {blockID}, "page_size": {"100"}}
		if cursor != "" {
			q.Set("start_cursor", cursor)
		}
		var l list[Comment]
		if err := c.Do(ctx, "GET", "/comments?"+q.Encode(), nil, &l); err != nil {
			return nil, err
		}
		all = append(all, l.Results...)
		if !l.HasMore || l.NextCursor == "" {
			return all, nil
		}
		cursor = l.NextCursor
	}
}

// AddComment comments on a page, or replies in a discussion when
// discussion is set.
func (c *Client) AddComment(ctx context.Context, pageID, discussion, text string) (*Comment, error) {
	body := map[string]any{"rich_text": []any{map[string]any{"text": map[string]any{"content": text}}}}
	if discussion != "" {
		body["discussion_id"] = discussion
	} else {
		body["parent"] = map[string]any{"page_id": pageID}
	}
	var cm Comment
	return &cm, c.Do(ctx, "POST", "/comments", body, &cm)
}

// Users returns the workspace's people and bots (not its guests).
func (c *Client) Users(ctx context.Context) ([]User, error) {
	var all []User
	cursor := ""
	for {
		q := url.Values{"page_size": {"100"}}
		if cursor != "" {
			q.Set("start_cursor", cursor)
		}
		var l list[User]
		if err := c.Do(ctx, "GET", "/users?"+q.Encode(), nil, &l); err != nil {
			return nil, err
		}
		all = append(all, l.Results...)
		if !l.HasMore || l.NextCursor == "" {
			return all, nil
		}
		cursor = l.NextCursor
	}
}

// MovePage puts a page under another page.
func (c *Client) MovePage(ctx context.Context, id, parentPage string) error {
	body := map[string]any{"parent": map[string]any{"type": "page_id", "page_id": parentPage}}
	return c.Do(ctx, "POST", "/pages/"+id+"/move", body, nil)
}

// TrashPage moves a page to Notion's trash, or back out of it.
func (c *Client) TrashPage(ctx context.Context, id string, trash bool) error {
	return c.Do(ctx, "PATCH", "/pages/"+id, map[string]any{"in_trash": trash}, nil)
}

// SetIcon gives a page an emoji as its icon, or none for "".
func (c *Client) SetIcon(ctx context.Context, id, emoji string) error {
	var icon any
	if emoji != "" {
		icon = map[string]any{"type": "emoji", "emoji": emoji}
	}
	return c.Do(ctx, "PATCH", "/pages/"+id, map[string]any{"icon": icon}, nil)
}

// Markdown returns a page's content as Notion-flavored Markdown.
func (c *Client) Markdown(ctx context.Context, id string) (string, error) {
	var out struct {
		Markdown string `json:"markdown"`
	}
	err := c.Do(ctx, "GET", "/pages/"+id+"/markdown?markdown_version=v2", nil, &out)
	return out.Markdown, err
}

// CreatePageMarkdown makes a page under parent from Markdown.
func (c *Client) CreatePageMarkdown(ctx context.Context, parent, title, markdown string) (*Page, error) {
	body := map[string]any{
		"parent":           map[string]any{"type": "page_id", "page_id": parent},
		"properties":       map[string]any{"title": titleValue(title)},
		"markdown":         markdown,
		"markdown_version": "v2",
	}
	var p Page
	return &p, c.Do(ctx, "POST", "/pages", body, &p)
}

// QueryWith returns up to limit rows of a data source that pass filter
// (nil for all), in the order sorts gives (nil for Notion's).
func (c *Client) QueryWith(ctx context.Context, id string, filter any, sorts []any, limit int) ([]*Page, error) {
	var all []*Page
	cursor := ""
	for {
		body := map[string]any{"page_size": 100}
		if filter != nil {
			body["filter"] = filter
		}
		if len(sorts) > 0 {
			body["sorts"] = sorts
		}
		if cursor != "" {
			body["start_cursor"] = cursor
		}
		var l list[*Page]
		if err := c.Do(ctx, "POST", "/data_sources/"+id+"/query", body, &l); err != nil {
			return nil, err
		}
		all = append(all, l.Results...)
		if !l.HasMore || l.NextCursor == "" || len(all) >= limit {
			return all, nil
		}
		cursor = l.NextCursor
	}
}

// Condition builds a data source filter for one property: what the user
// picks in a filter, as Notion takes it. Empty op means "contains" for
// text and "equals" otherwise.
func Condition(p Property, op, value string) any {
	typ := p.Type
	key := typ
	switch typ {
	case "title", "rich_text", "url", "email", "phone_number":
		if op == "" {
			op = "contains"
		}
	case "multi_select":
		if op == "" {
			op = "contains"
		}
	case "checkbox":
		return map[string]any{"property": p.Name, key: map[string]any{"equals": value == "true"}}
	default:
		if op == "" {
			op = "equals"
		}
	}
	var v any = value
	switch op {
	case "is_empty", "is_not_empty":
		v = true
	}
	return map[string]any{"property": p.Name, key: map[string]any{op: v}}
}

// SetCover gives a page a cover: a picture at url, an upload by its ID,
// or none when both are "".
func (c *Client) SetCover(ctx context.Context, id, url, uploadID string) error {
	var cover any
	switch {
	case uploadID != "":
		cover = map[string]any{"type": "file_upload", "file_upload": map[string]string{"id": uploadID}}
	case url != "":
		cover = map[string]any{"type": "external", "external": map[string]string{"url": url}}
	}
	return c.Do(ctx, "PATCH", "/pages/"+id, map[string]any{"cover": cover}, nil)
}

// CreateDatabase makes a database under a page, its first data source with
// a starting set of columns: a title, a select of how far along, a date
// and a check box.
func (c *Client) CreateDatabase(ctx context.Context, parent, title string) (*Database, error) {
	opt := func(name, color string) map[string]string { return map[string]string{"name": name, "color": color} }
	body := map[string]any{
		"parent": map[string]any{"type": "page_id", "page_id": parent},
		"title":  []any{map[string]any{"text": map[string]any{"content": title}}},
		"initial_data_source": map[string]any{"properties": map[string]any{
			tr.T("Nama"):    map[string]any{"type": "title", "title": map[string]any{}},
			"Status":        map[string]any{"type": "select", "select": map[string]any{"options": []any{opt(tr.T("Belum"), "gray"), opt(tr.T("Berjalan"), "yellow"), opt(tr.T("Selesai"), "green")}}},
			tr.T("Tanggal"): map[string]any{"type": "date", "date": map[string]any{}},
			tr.T("Beres"):   map[string]any{"type": "checkbox", "checkbox": map[string]any{}},
		}},
	}
	var d Database
	return &d, c.Do(ctx, "POST", "/databases", body, &d)
}

// UpdateSchema changes the columns of a data source: props maps a column's
// name to its new settings ({"number": {}}), to {"name": "New"} to rename
// it, or to nil to delete it. It returns the data source after.
func (c *Client) UpdateSchema(ctx context.Context, id string, props map[string]any) (*DataSource, error) {
	var d DataSource
	return &d, c.Do(ctx, "PATCH", "/data_sources/"+id, map[string]any{"properties": props}, &d)
}

// SetDataSourceTitle changes a data source's title (and its container
// database's title when dbID is given).
func (c *Client) SetDataSourceTitle(ctx context.Context, dsID, dbID, title string) error {
	items := []any{}
	if title != "" {
		items = []any{map[string]any{"type": "text", "text": map[string]any{"content": title}}}
	}
	body := map[string]any{"title": items}
	if dsID != "" {
		if err := c.Do(ctx, "PATCH", "/data_sources/"+dsID, body, nil); err != nil {
			return err
		}
	}
	if dbID != "" && dbID != dsID {
		if err := c.Do(ctx, "PATCH", "/databases/"+dbID, body, nil); err != nil && dsID == "" {
			return err
		}
	}
	return nil
}

// ColumnTypes are the kinds of column the app adds, Notion's names and the
// app's, in the order it offers them.
var ColumnTypes = []struct{ Type, Label string }{
	{"rich_text", tr.N("Teks")}, {"number", tr.N("Angka")}, {"select", tr.N("Pilihan")}, {"multi_select", tr.N("Banyak pilihan")},
	{"date", tr.N("Tanggal")}, {"checkbox", tr.N("Centang")}, {"url", tr.N("Tautan")}, {"email", "Email"}, {"phone_number", tr.N("Telepon")},
	{"people", tr.N("Orang")}, {"files", "File"},
}

// AddOption is the schema change that adds an option to a select, status
// or multi-select: Notion takes the options as the whole list to keep.
func AddOption(p Property, name string) map[string]any {
	var opts []any
	for _, o := range p.Options {
		opts = append(opts, map[string]any{"name": o.Name})
	}
	opts = append(opts, map[string]any{"name": name})
	return map[string]any{p.Name: map[string]any{p.Type: map[string]any{"options": opts}}}
}

// Templates returns the page templates of a data source.
func (c *Client) Templates(ctx context.Context, id string) ([]Template, error) {
	var out struct {
		Templates []Template `json:"templates"`
	}
	err := c.Do(ctx, "GET", "/data_sources/"+id+"/templates?page_size=100", nil, &out)
	return out.Templates, err
}

// Template is a page template of a data source.
type Template struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	IsDefault bool   `json:"is_default"`
}

// CreateRowFrom makes a row from a template (template "" for none): Notion
// fills its content in after it answers.
func (c *Client) CreateRowFrom(ctx context.Context, dataSource, prop, title, template string) (*Page, error) {
	return c.CreateRowWith(ctx, dataSource, prop, title, template, nil)
}

// CreateRowWith makes a row with optional extra initial property values.
func (c *Client) CreateRowWith(ctx context.Context, dataSource, prop, title, template string, extra map[string]any) (*Page, error) {
	props := map[string]any{prop: titleValue(title)}
	for k, v := range extra {
		props[k] = v
	}
	body := map[string]any{
		"parent":     map[string]any{"type": "data_source_id", "data_source_id": dataSource},
		"properties": props,
	}
	if template != "" {
		body["template"] = map[string]any{"type": "template_id", "template_id": template}
	}
	var p Page
	return &p, c.Do(ctx, "POST", "/pages", body, &p)
}
