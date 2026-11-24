package notion

import (
	"context"
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
			"Nama":    map[string]any{"type": "title", "title": map[string]any{}},
			"Status":  map[string]any{"type": "select", "select": map[string]any{"options": []any{opt("Belum", "gray"), opt("Berjalan", "yellow"), opt("Selesai", "green")}}},
			"Tanggal": map[string]any{"type": "date", "date": map[string]any{}},
			"Beres":   map[string]any{"type": "checkbox", "checkbox": map[string]any{}},
		}},
	}
	var d Database
	return &d, c.Do(ctx, "POST", "/databases", body, &d)
}
