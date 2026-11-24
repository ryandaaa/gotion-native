package notion

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"path"
	"strings"
)

// Upload is a file sent to Notion, to attach to a block within the hour.
type Upload struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

// UploadFile sends a file of at most 20 MiB to Notion in one part.
func (c *Client) UploadFile(ctx context.Context, name, contentType string, data []byte) (*Upload, error) {
	var up Upload
	if err := c.Do(ctx, "POST", "/file_uploads", map[string]any{"filename": name, "content_type": contentType}, &up); err != nil {
		return nil, err
	}
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename=%q`, name))
	h.Set("Content-Type", contentType)
	part, err := w.CreatePart(h)
	if err != nil {
		return nil, err
	}
	part.Write(data)
	w.Close()
	if err := c.wait(ctx); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", c.Base+"/file_uploads/"+up.ID+"/send", &body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token())
	req.Header.Set("Notion-Version", Version)
	req.Header.Set("Content-Type", w.FormDataContentType())
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		e := &Error{Status: resp.StatusCode}
		json.Unmarshal(out, e)
		e.Status = resp.StatusCode
		return nil, e
	}
	json.Unmarshal(out, &up)
	return &up, nil
}

// MediaBlock is a block showing an upload: an image for pictures, a file
// for anything else.
func MediaBlock(uploadID, contentType string) NewBlock {
	t := "file"
	if strings.HasPrefix(contentType, "image/") {
		t = "image"
	}
	return NewBlock{Type: t, Payload: map[string]any{"type": "file_upload", "file_upload": map[string]string{"id": uploadID}}}
}

// imageTypes are the pictures Notion takes in an image block.
var imageTypes = map[string]string{
	".gif": "image/gif", ".heic": "image/heic", ".jpeg": "image/jpeg", ".jpg": "image/jpeg", ".png": "image/png",
	".svg": "image/svg+xml", ".tif": "image/tiff", ".tiff": "image/tiff", ".webp": "image/webp", ".bmp": "image/bmp",
	".avif": "image/avif", ".apng": "image/apng", ".ico": "image/vnd.microsoft.icon",
}

// ContentType is a file's type by its name, sniffed from data when the
// name says nothing.
func ContentType(name string, data []byte) string {
	if t, ok := imageTypes[strings.ToLower(path.Ext(name))]; ok {
		return t
	}
	return http.DetectContentType(data)
}

// FileInfo returns the name and the download URL of a file, image, PDF,
// video or audio block.
func (b *Block) FileInfo() (name, url string) {
	var p struct {
		Name    string     `json:"name"`
		Caption []RichText `json:"caption"`
		File    *struct {
			URL string `json:"url"`
		} `json:"file"`
		External *struct {
			URL string `json:"url"`
		} `json:"external"`
	}
	json.Unmarshal(b.Payload, &p)
	switch {
	case p.File != nil:
		url = p.File.URL
	case p.External != nil:
		url = p.External.URL
	}
	name = p.Name
	if name == "" {
		name = PlainText(p.Caption)
	}
	if name == "" && url != "" {
		name = path.Base(strings.SplitN(url, "?", 2)[0])
	}
	return name, url
}

// Block returns one block, with its file's URL fresh: they last an hour.
func (c *Client) Block(ctx context.Context, id string) (*Block, error) {
	var b Block
	return &b, c.Do(ctx, "GET", "/blocks/"+id, nil, &b)
}
