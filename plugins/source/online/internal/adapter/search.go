package adapter

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"

	"strings"
	"time"

	"github.com/fnMrRice/MusicFS-server/plugins/source/online/internal/session"
	domain "github.com/fnMrRice/MusicFS-server/sdk/fault"
)

type searchCursor = session.Cursor

func (c *Client) search(ctx context.Context, request SearchRequest) (SearchPage, error) {
	if request.Kind == "" {
		request.Kind = "tracks"
	}
	if err := ctx.Err(); err != nil {
		return SearchPage{}, err
	}
	if c.platform == AppleMusic {
		return SearchPage{}, c.requireNetease(ctx)
	}
	query, err := textValue(request.Query, 256)
	if err != nil || query == "" || !SearchKind(request.Kind) || request.Limit < 1 || request.Limit > MaxPageItems {
		return SearchPage{}, domain.Fail("INVALID_ARGUMENT", "Search requires a query and a limit between 1 and 50")
	}
	request.Query = query
	if request.Kind != "tracks" {
		return c.driver.SearchEntities(ctx, request)
	}
	offset, err := c.readCursor(request)
	if err != nil {
		return SearchPage{}, err
	}
	var tracks []Track
	var more bool
	tracks, more, err = c.driver.Search(ctx, request, offset)
	if err != nil {
		return SearchPage{}, err
	}
	page := emptySearchPage(request.Kind)
	page.Tracks = tracks
	page.Truncated = more && (len(tracks) == 0 || offset+len(tracks) >= 10000 || (c.platform == QQMusic && len(tracks) != request.Limit))
	if more && !page.Truncated {
		cursor := c.writeCursor(request, offset+len(tracks))
		page.NextCursor = &cursor
	}
	return page, nil
}

func (c *Client) cursorBinding(r SearchRequest) string {
	return digest(struct {
		Platform string
		Account  string
		Revision string
		Scope    Scope
		Query    string
		Limit    int
		Kind     string
	}{c.platform, c.account.secret.AccountID, c.account.view.Revision, c.scope, r.Query, r.Limit, r.Kind})
}

func (c *Client) readCursor(r SearchRequest) (int, error) {
	token, err := c.readCursorToken(r)
	return token.Offset, err
}

func (c *Client) readCursorToken(r SearchRequest) (searchCursor, error) {
	var empty searchCursor
	if r.Cursor == "" {
		return empty, nil
	}
	parts := strings.Split(r.Cursor, ".")
	if len(r.Cursor) > 1024 || len(parts) != 2 {
		return empty, invalidCursor()
	}
	body, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return empty, invalidCursor()
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return empty, invalidCursor()
	}
	mac := hmac.New(sha256.New, c.key[:])
	mac.Write(body)
	if !hmac.Equal(sig, mac.Sum(nil)) {
		return empty, invalidCursor()
	}
	var token searchCursor
	if json.Unmarshal(body, &token) != nil || token.Binding != c.cursorBinding(r) || token.Offset < 1 || token.Offset >= 10000 {
		return empty, invalidCursor()
	}
	if token.Expires <= time.Now().Unix() {
		return empty, domain.Fail("CURSOR_EXPIRED", "Online cursor expired; refresh the first page")
	}
	if c.platform == QQMusic && token.Offset%r.Limit != 0 {
		return empty, invalidCursor()
	}
	return token, nil
}

func (c *Client) writeCursor(r SearchRequest, offset int) string {
	return c.writeVersionCursor(r, offset, "")
}

func (c *Client) writeVersionCursor(r SearchRequest, offset int, version string) string {
	body, _ := json.Marshal(searchCursor{Binding: c.cursorBinding(r), Offset: offset, Expires: time.Now().Add(5 * time.Minute).Unix(), Version: version})
	mac := hmac.New(sha256.New, c.key[:])
	mac.Write(body)
	return base64.RawURLEncoding.EncodeToString(body) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func invalidCursor() error {
	return domain.Fail("INVALID_ARGUMENT", "Online search cursor is invalid or expired")
}
