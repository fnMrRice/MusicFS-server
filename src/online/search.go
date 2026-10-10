package online

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/domain"
)

type searchCursor struct {
	Binding string `json:"b"`
	Offset  int    `json:"o"`
	Expires int64  `json:"e"`
}

func (c *Client) Search(ctx context.Context, request SearchRequest) (SearchPage, error) {
	if err := ctx.Err(); err != nil {
		return SearchPage{}, err
	}
	if c.platform == AppleMusic {
		return SearchPage{}, c.requireNetease(ctx)
	}
	query, err := textValue(request.Query, 256)
	if err != nil || query == "" || request.Limit < 1 || request.Limit > MaxPageItems {
		return SearchPage{}, domain.Fail("INVALID_ARGUMENT", "Search requires a query and a limit between 1 and 50")
	}
	request.Query = query
	offset, err := c.readCursor(request)
	if err != nil {
		return SearchPage{}, err
	}
	var tracks []Track
	var more bool
	if c.platform == Netease {
		tracks, more, err = c.neteaseSearch(ctx, request, offset)
	} else {
		tracks, more, err = c.qqSearch(ctx, request, offset)
	}
	if err != nil {
		return SearchPage{}, err
	}
	page := SearchPage{Tracks: tracks, Consistency: Unversioned, ObservedAt: time.Now().UTC()}
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
		Scope    Scope
		Query    string
		Limit    int
	}{c.platform, c.scope, r.Query, r.Limit})
}

func (c *Client) readCursor(r SearchRequest) (int, error) {
	if r.Cursor == "" {
		return 0, nil
	}
	parts := strings.Split(r.Cursor, ".")
	if len(r.Cursor) > 1024 || len(parts) != 2 {
		return 0, invalidCursor()
	}
	body, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return 0, invalidCursor()
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return 0, invalidCursor()
	}
	mac := hmac.New(sha256.New, c.key[:])
	mac.Write(body)
	if !hmac.Equal(sig, mac.Sum(nil)) {
		return 0, invalidCursor()
	}
	var token searchCursor
	if json.Unmarshal(body, &token) != nil || token.Binding != c.cursorBinding(r) || token.Offset < 1 || token.Offset >= 10000 || token.Expires <= time.Now().Unix() {
		return 0, invalidCursor()
	}
	if c.platform == QQMusic && token.Offset%r.Limit != 0 {
		return 0, invalidCursor()
	}
	return token.Offset, nil
}

func (c *Client) writeCursor(r SearchRequest, offset int) string {
	body, _ := json.Marshal(searchCursor{c.cursorBinding(r), offset, time.Now().Add(5 * time.Minute).Unix()})
	mac := hmac.New(sha256.New, c.key[:])
	mac.Write(body)
	return base64.RawURLEncoding.EncodeToString(body) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func invalidCursor() error {
	return domain.Fail("INVALID_ARGUMENT", "Online search cursor is invalid or expired")
}

func (c *Client) neteaseSearch(ctx context.Context, request SearchRequest, offset int) ([]Track, bool, error) {
	var result struct {
		Code   *int `json:"code"`
		Result *struct {
			Songs     []neteaseSong `json:"songs"`
			SongCount *int64        `json:"songCount"`
			HasMore   *bool         `json:"hasMore"`
		} `json:"result"`
	}
	err := c.get(ctx, "/api/search/get", url.Values{"s": {request.Query}, "type": {"1"}, "limit": {strconv.Itoa(request.Limit)}, "offset": {strconv.Itoa(offset)}}, &result)
	if err != nil {
		return nil, false, err
	}
	if result.Code == nil {
		return nil, false, upstreamInvalid()
	}
	if err = c.businessError(*result.Code, 200); err != nil {
		return nil, false, err
	}
	if result.Result == nil || result.Result.SongCount == nil || *result.Result.SongCount < 0 || len(result.Result.Songs) > request.Limit {
		return nil, false, upstreamInvalid()
	}
	tracks := make([]Track, 0, len(result.Result.Songs))
	for _, song := range result.Result.Songs {
		track, err := song.track(false)
		if err != nil {
			return nil, false, err
		}
		tracks = append(tracks, track)
	}
	more := int64(offset+len(tracks)) < *result.Result.SongCount
	if result.Result.HasMore != nil {
		more = more && *result.Result.HasMore
	}
	return tracks, more, nil
}
