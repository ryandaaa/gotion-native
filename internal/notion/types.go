package notion

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Parent is where a page, data source or block lives.
type Parent struct {
	Type         string `json:"type"`
	PageID       string `json:"page_id,omitempty"`
	DatabaseID   string `json:"database_id,omitempty"`
	DataSourceID string `json:"data_source_id,omitempty"`
	BlockID      string `json:"block_id,omitempty"`
	Workspace    bool   `json:"workspace,omitempty"`
}

// ID returns the ID of the parent, "" for the workspace.
func (p Parent) ID() string {
	switch p.Type {
	case "page_id":
		return p.PageID
	case "database_id":
		return p.DatabaseID
	case "data_source_id":
		return p.DataSourceID
	case "block_id":
		return p.BlockID
	}
	return ""
}

// User is a person or a bot.
type User struct {
	Object string `json:"object"`
	ID     string `json:"id"`
	Name   string `json:"name"`
	Type   string `json:"type"`
	Bot    *struct {
		WorkspaceName   string `json:"workspace_name"`
		WorkspaceLimits struct {
			// MaxFileUploadSize is the largest file the workspace takes:
			// 5 MiB on a free plan.
			MaxFileUploadSize int64 `json:"max_file_upload_size_in_bytes"`
		} `json:"workspace_limits"`
	} `json:"bot,omitempty"`
}

// Annotations style a piece of rich text.
type Annotations struct {
	Bold          bool   `json:"bold"`
	Italic        bool   `json:"italic"`
	Strikethrough bool   `json:"strikethrough"`
	Underline     bool   `json:"underline"`
	Code          bool   `json:"code"`
	Color         string `json:"color"`
}

// RichText is one item of a rich text array. Raw keeps the item as Notion
// sent it, so mentions and equations go back unchanged.
type RichText struct {
	Type        string      `json:"type"`
	PlainText   string      `json:"plain_text"`
	Href        string      `json:"href,omitempty"`
	Annotations Annotations `json:"annotations"`
	Text        *struct {
		Content string `json:"content"`
		Link    *struct {
			URL string `json:"url"`
		} `json:"link"`
	} `json:"text,omitempty"`
	Raw json.RawMessage `json:"-"`
}

func (r *RichText) UnmarshalJSON(b []byte) error {
	type plain RichText
	if err := json.Unmarshal(b, (*plain)(r)); err != nil {
		return err
	}
	r.Raw = append(json.RawMessage(nil), b...)
	return nil
}

// PlainText joins the plain text of rich text.
func PlainText(rt []RichText) string {
	var b strings.Builder
	for _, r := range rt {
		b.WriteString(r.PlainText)
	}
	return b.String()
}

// Block is a block of a page. Payload is the object named after its type.
type Block struct {
	Object         string          `json:"object"`
	ID             string          `json:"id"`
	Type           string          `json:"type"`
	HasChildren    bool            `json:"has_children"`
	InTrash        bool            `json:"in_trash"`
	LastEditedTime time.Time       `json:"last_edited_time"`
	Parent         Parent          `json:"parent"`
	Payload        json.RawMessage `json:"-"`
	// Children are filled in by the app, not by Notion.
	Children []*Block `json:"children,omitempty"`
}

func (b *Block) UnmarshalJSON(data []byte) error {
	type plain Block
	if err := json.Unmarshal(data, (*plain)(b)); err != nil {
		return err
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		return err
	}
	b.Payload = m[b.Type]
	return nil
}

func (b Block) MarshalJSON() ([]byte, error) {
	type plain Block
	data, err := json.Marshal(plain(b))
	if err != nil || len(b.Payload) == 0 {
		return data, err
	}
	var m map[string]json.RawMessage
	json.Unmarshal(data, &m)
	m[b.Type] = b.Payload
	return json.Marshal(m)
}

// TextPayload is the payload of a block that holds text.
type TextPayload struct {
	RichText []RichText `json:"rich_text"`
	Checked  bool       `json:"checked,omitempty"`
	Language string     `json:"language,omitempty"`
	Color    string     `json:"color,omitempty"`
	Icon     *struct {
		Type  string `json:"type"`
		Emoji string `json:"emoji,omitempty"`
	} `json:"icon,omitempty"`
	Title string `json:"title,omitempty"` // child_page, child_database
	URL   string `json:"url,omitempty"`   // bookmark, embed
}

// Text decodes the block's payload as text, the zero value when it has none.
func (b *Block) Text() TextPayload {
	var p TextPayload
	json.Unmarshal(b.Payload, &p)
	return p
}

// Page is a page, or a row of a data source.
type Page struct {
	Object         string                     `json:"object"`
	ID             string                     `json:"id"`
	URL            string                     `json:"url"`
	InTrash        bool                       `json:"in_trash"`
	LastEditedTime time.Time                  `json:"last_edited_time"`
	LastEditedBy   User                       `json:"last_edited_by"`
	Parent         Parent                     `json:"parent"`
	Properties     map[string]json.RawMessage `json:"properties"`
	Icon           *struct {
		Type  string `json:"type"`
		Emoji string `json:"emoji,omitempty"`
	} `json:"icon,omitempty"`
	// Title of a data source (search returns them alongside pages).
	TitleText []RichText `json:"title,omitempty"`
}

// Title returns the page's title property as plain text.
func (p *Page) Title() string {
	if p.Object == "data_source" || p.Object == "database" {
		return PlainText(p.TitleText)
	}
	for _, raw := range p.Properties {
		var prop struct {
			Type  string     `json:"type"`
			Title []RichText `json:"title"`
		}
		if json.Unmarshal(raw, &prop) == nil && prop.Type == "title" {
			return PlainText(prop.Title)
		}
	}
	return ""
}

// Emoji returns the page's emoji icon, "" when it has none.
func (p *Page) Emoji() string {
	if p.Icon != nil && p.Icon.Type == "emoji" {
		return p.Icon.Emoji
	}
	return ""
}

type list[T any] struct {
	Results    []T    `json:"results"`
	HasMore    bool   `json:"has_more"`
	NextCursor string `json:"next_cursor"`
}

// Me returns the integration's bot user.
func (c *Client) Me(ctx context.Context) (*User, error) {
	var u User
	return &u, c.Do(ctx, "GET", "/users/me", nil, &u)
}

// Search returns every page and data source shared with the integration.
func (c *Client) Search(ctx context.Context) ([]*Page, error) {
	var all []*Page
	cursor := ""
	for {
		body := map[string]any{"page_size": 100, "sort": map[string]string{"direction": "descending", "timestamp": "last_edited_time"}}
		if cursor != "" {
			body["start_cursor"] = cursor
		}
		var l list[*Page]
		if err := c.Do(ctx, "POST", "/search", body, &l); err != nil {
			return nil, err
		}
		all = append(all, l.Results...)
		if !l.HasMore || l.NextCursor == "" || len(all) >= 2000 {
			return all, nil
		}
		cursor = l.NextCursor
	}
}

// Page returns a page.
func (c *Client) Page(ctx context.Context, id string) (*Page, error) {
	var p Page
	return &p, c.Do(ctx, "GET", "/pages/"+id, nil, &p)
}

// Children returns the children of a block or page, every page of them.
func (c *Client) Children(ctx context.Context, id string) ([]*Block, error) {
	var all []*Block
	cursor := ""
	for {
		q := url.Values{"page_size": {"100"}}
		if cursor != "" {
			q.Set("start_cursor", cursor)
		}
		var l list[*Block]
		if err := c.Do(ctx, "GET", "/blocks/"+id+"/children?"+q.Encode(), nil, &l); err != nil {
			return nil, err
		}
		all = append(all, l.Results...)
		if !l.HasMore || l.NextCursor == "" {
			return all, nil
		}
		cursor = l.NextCursor
	}
}

// Tree returns the children of id with their own children, depth levels
// deep, for the block types the editor nests.
func (c *Client) Tree(ctx context.Context, id string, depth int) ([]*Block, error) {
	blocks, err := c.Children(ctx, id)
	if err != nil {
		return nil, err
	}
	if depth <= 0 {
		return blocks, nil
	}
	// The children of the blocks, four requests at a time; the client's
	// bucket keeps the pace Notion takes.
	sem := make(chan struct{}, 4)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var first error
	for _, b := range blocks {
		d := depth - 1
		switch {
		case b.Type == "column_list" || b.Type == "column":
			d = depth // containers, not levels: their blocks show at their depth
		case b.HasChildren && (Nests(b.Type) || b.Type == "table"):
		default:
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			kids, err := c.Tree(ctx, b.ID, d)
			<-sem
			mu.Lock()
			defer mu.Unlock()
			if err != nil && first == nil {
				first = err
			}
			b.Children = kids
		}()
	}
	wg.Wait()
	if first != nil {
		return nil, first
	}
	return blocks, nil
}

// Nests reports whether the editor shows the children of a block type.
func Nests(t string) bool {
	switch t {
	case "paragraph", "bulleted_list_item", "numbered_list_item", "to_do", "toggle", "quote", "callout":
		return true
	}
	return false
}

// UpdateBlock replaces the payload of a block of type t.
func (c *Client) UpdateBlock(ctx context.Context, id, t string, payload any) (*Block, error) {
	var b Block
	return &b, c.Do(ctx, "PATCH", "/blocks/"+id, map[string]any{t: payload}, &b)
}

// NewBlock is a block to create.
type NewBlock struct {
	Type    string
	Payload any
}

func (n NewBlock) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]any{"object": "block", "type": n.Type, n.Type: n.Payload})
}

// Append creates blocks under parent, after the block with ID after, or
// first when after is "start", or last when after is "".
func (c *Client) Append(ctx context.Context, parent, after string, blocks ...NewBlock) ([]*Block, error) {
	body := map[string]any{"children": blocks}
	switch after {
	case "":
	case "start":
		body["position"] = map[string]any{"type": "start"}
	default:
		body["position"] = map[string]any{"type": "after_block", "after_block": map[string]string{"id": after}}
	}
	var l list[*Block]
	return l.Results, c.Do(ctx, "PATCH", "/blocks/"+parent+"/children", body, &l)
}

// DeleteBlock moves a block to the trash.
func (c *Client) DeleteBlock(ctx context.Context, id string) error {
	return c.Do(ctx, "DELETE", "/blocks/"+id, nil, nil)
}

// Property is a column of a data source. Options are the choices of a
// select, status or multi-select.
type Property struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Type    string   `json:"type"`
	Options []Option `json:"-"`
}

// Option is a choice of a select, status or multi-select.
type Option struct {
	Name  string `json:"name"`
	Color string `json:"color"`
}

func (p *Property) UnmarshalJSON(b []byte) error {
	type plain Property
	if err := json.Unmarshal(b, (*plain)(p)); err != nil {
		return err
	}
	var m map[string]json.RawMessage
	json.Unmarshal(b, &m)
	var choices struct {
		Options []Option `json:"options"`
	}
	if raw, ok := m[p.Type]; ok && json.Unmarshal(raw, &choices) == nil {
		p.Options = choices.Options
	}
	return nil
}

// Editable reports whether a property's value can be set: not one Notion
// computes (formulas, rollups, times and authors) nor one that points
// elsewhere (relations, people, files).
func (p Property) Editable() bool {
	switch p.Type {
	case "title", "rich_text", "number", "select", "status", "multi_select", "date", "checkbox", "url", "email", "phone_number":
		return true
	}
	return false
}

// DataSource is the schema of a database's table.
type DataSource struct {
	ID         string              `json:"id"`
	Title      []RichText          `json:"title"`
	Properties map[string]Property `json:"properties"`
	Parent     Parent              `json:"parent"`
}

// Database is a database, holding data sources.
type Database struct {
	ID          string     `json:"id"`
	Title       []RichText `json:"title"`
	DataSources []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"data_sources"`
}

func (c *Client) Database(ctx context.Context, id string) (*Database, error) {
	var d Database
	return &d, c.Do(ctx, "GET", "/databases/"+id, nil, &d)
}

func (c *Client) DataSource(ctx context.Context, id string) (*DataSource, error) {
	var d DataSource
	return &d, c.Do(ctx, "GET", "/data_sources/"+id, nil, &d)
}

// Query returns up to limit rows of a data source.
func (c *Client) Query(ctx context.Context, id string, limit int) ([]*Page, error) {
	var all []*Page
	cursor := ""
	for {
		body := map[string]any{"page_size": 100}
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

// TitleProperty returns the name of the page's title property.
func (p *Page) TitleProperty() string {
	for name, raw := range p.Properties {
		var prop struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(raw, &prop) == nil && prop.Type == "title" {
			return name
		}
	}
	return "title"
}

func titleValue(s string) map[string]any {
	return map[string]any{"title": []any{map[string]any{"type": "text", "text": map[string]any{"content": s}}}}
}

// CreatePage makes a page under the page parent.
func (c *Client) CreatePage(ctx context.Context, parent, title string) (*Page, error) {
	var p Page
	body := map[string]any{
		"parent":     map[string]any{"type": "page_id", "page_id": parent},
		"properties": map[string]any{"title": titleValue(title)},
	}
	return &p, c.Do(ctx, "POST", "/pages", body, &p)
}

// SetTitle changes a page's title, its title property named prop.
func (c *Client) SetTitle(ctx context.Context, id, prop, title string) error {
	return c.Do(ctx, "PATCH", "/pages/"+id, map[string]any{"properties": map[string]any{prop: titleValue(title)}}, nil)
}

// ImageURL returns the URL of an image block's file.
func (b *Block) ImageURL() string {
	var p struct {
		Type string `json:"type"`
		File *struct {
			URL string `json:"url"`
		} `json:"file"`
		External *struct {
			URL string `json:"url"`
		} `json:"external"`
	}
	json.Unmarshal(b.Payload, &p)
	switch {
	case p.File != nil:
		return p.File.URL
	case p.External != nil:
		return p.External.URL
	}
	return ""
}

// Cells returns the cells of a table_row block as plain text.
func (b *Block) Cells() []string {
	var p struct {
		Cells [][]RichText `json:"cells"`
	}
	json.Unmarshal(b.Payload, &p)
	out := make([]string, len(p.Cells))
	for i, c := range p.Cells {
		out[i] = PlainText(c)
	}
	return out
}

// SetProperties changes properties of a page, a row of a data source:
// each value as Notion takes it ({"checkbox": true}, {"select": {...}}).
func (c *Client) SetProperties(ctx context.Context, pageID string, props map[string]any) (*Page, error) {
	var p Page
	return &p, c.Do(ctx, "PATCH", "/pages/"+pageID, map[string]any{"properties": props}, &p)
}

// CreateRow makes a row in a data source, titled title in its title
// property prop.
func (c *Client) CreateRow(ctx context.Context, dataSource, prop, title string) (*Page, error) {
	var p Page
	body := map[string]any{
		"parent":     map[string]any{"type": "data_source_id", "data_source_id": dataSource},
		"properties": map[string]any{prop: titleValue(title)},
	}
	return &p, c.Do(ctx, "POST", "/pages", body, &p)
}
