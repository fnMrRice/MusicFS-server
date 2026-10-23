package bilibili

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/fault"
	"github.com/fnMrRice/MusicFS-server/sdk/online"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func response(t *testing.T, data any) *http.Response {
	t.Helper()
	body, err := json.Marshal(map[string]any{"code": 0, "data": data})
	if err != nil {
		t.Fatal(err)
	}
	return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(string(body)))}
}

func newTestClient(t *testing.T, f roundTrip) *Client {
	t.Helper()
	c, err := New(online.Scope{UserID: "user", InstanceID: "bilibili-test"}, Options{Transport: f})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	return c
}

func videoFixture() map[string]any {
	return map[string]any{"bvid": "BV13x41117TL", "aid": 8903802, "title": "Example", "owner": map[string]any{"mid": 10, "name": "Uploader"}, "pages": []any{map[string]any{"cid": 101, "page": 1, "part": "First", "duration": 60}, map[string]any{"cid": 102, "page": 2, "part": "Second", "duration": 70}}}
}

func errorCode(t *testing.T, err error, expected string) {
	t.Helper()
	var f *fault.Error
	if !errors.As(err, &f) || f.Code != expected {
		t.Fatalf("expected %s, got %v", expected, err)
	}
}

func TestReferenceParsing(t *testing.T) {
	for _, input := range []string{"BV13x41117TL", "av8903802", "https://www.bilibili.com/video/BV13x41117TL?p=2", "video:BV13x41117TL:101", "au1003142", "audio:1003142", "am10624", "audio-menu:10624", "favorite:1103407912", "https://space.bilibili.com/84912/favlist?fid=1103407912&ftype=create", "https://www.bilibili.com/list/ml1103407912"} {
		if _, err := parseReference(input); err != nil {
			t.Errorf("valid reference %q: %v", input, err)
		}
	}
	for _, input := range []string{"", "music", "https://b23.tv/short", "http://www.bilibili.com/video/BV13x41117TL", "https://www.bilibili.com.evil/video/BV13x41117TL", "https://www.bilibili.com:443/video/BV13x41117TL", "https://user@www.bilibili.com/video/BV13x41117TL", "https://127.0.0.1/video/BV13x41117TL", "https://www.bilibili.com/video/BV13x41117TL?p=0", "https://www.bilibili.com/video/BV13x41117TL?p=1&p=2", "https://www.bilibili.com/video/BV13x41117TL?cid=-1", "https://www.bilibili.com/video/%42V13x41117TL", "video:BV13x41117TL:0"} {
		if _, err := parseReference(input); err == nil {
			t.Errorf("unsafe reference accepted: %q", input)
		}
	}
}

func TestVideoMetadataAndUploaderDetails(t *testing.T) {
	c := newTestClient(t, func(r *http.Request) (*http.Response, error) {
		if r.URL.Scheme != "https" || r.URL.Host != "api.bilibili.com" || r.URL.Path != "/x/web-interface/view" || r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
			t.Fatalf("unexpected upstream request: %s", r.URL.Path)
		}
		return response(t, videoFixture()), nil
	})
	track, err := c.Metadata(context.Background(), "https://www.bilibili.com/video/av8903802?p=2")
	if err != nil {
		t.Fatal(err)
	}
	if track.ExternalID != "video:BV13x41117TL:102" || track.SourceURL != "https://www.bilibili.com/video/BV13x41117TL?cid=102" || len(track.Artists) != 0 || track.Album != "" || *track.DurationMs != 70000 {
		t.Fatalf("unexpected video track: %+v", track)
	}
	if err := online.ValidateTrack("bilibili", track); err != nil {
		t.Fatal(err)
	}
	details, err := c.TrackDetails(context.Background(), track.ExternalID)
	if err != nil {
		t.Fatal(err)
	}
	if err := details.Validate(track.ExternalID); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, f := range details.Sections[0].Fields {
		if f.ID == "uploader" && f.Value == "Uploader" {
			found = true
		}
	}
	if !found {
		t.Fatal("uploader attribution is missing")
	}
	_, err = c.Metadata(context.Background(), "video:BV13x41117TL:999")
	errorCode(t, err, "NOT_FOUND")
}

func TestAudioMetadataPreservesDeclaredAuthor(t *testing.T) {
	c := newTestClient(t, func(r *http.Request) (*http.Response, error) {
		return response(t, map[string]any{"id": 10, "title": "Audio", "author": "Declared artist", "uname": "Uploader", "duration": 0}), nil
	})
	track, err := c.Metadata(context.Background(), "au10")
	if err != nil {
		t.Fatal(err)
	}
	if track.DurationMs != nil || len(track.Artists) != 1 || track.Artists[0] != "Declared artist" || track.ExternalID != "audio:10" {
		t.Fatalf("unexpected audio metadata: %+v", track)
	}
	if err := online.ValidateTrack("bilibili", track); err != nil {
		t.Fatal(err)
	}
}

func TestExactSearchPagesAndExplicitFirstPart(t *testing.T) {
	calls := 0
	c := newTestClient(t, func(r *http.Request) (*http.Response, error) { calls++; return response(t, videoFixture()), nil })
	query := online.SearchRequest{Kind: "tracks", Query: "BV13x41117TL", Limit: 1}
	page, err := c.Search(context.Background(), query)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Tracks) != 1 || page.NextCursor == nil {
		t.Fatalf("unexpected first page: %+v", page)
	}
	query.Cursor = *page.NextCursor
	page, err = c.Search(context.Background(), query)
	if err != nil {
		t.Fatal(err)
	}
	if page.Tracks[0].ExternalID != "video:BV13x41117TL:102" || page.NextCursor != nil || calls != 1 {
		t.Fatal("search did not preserve its observation")
	}
	query.Query = "https://www.bilibili.com/video/BV13x41117TL?p=1"
	query.Cursor = ""
	query.Limit = 50
	page, err = c.Search(context.Background(), query)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Tracks) != 1 || page.Tracks[0].ExternalID != "video:BV13x41117TL:101" {
		t.Fatal("explicit first part expanded all parts")
	}
}

func TestRealFailureClassesAndNoRedirect(t *testing.T) {
	for _, tc := range []struct {
		code int
		want string
	}{{-101, "PLATFORM_AUTH_REQUIRED"}, {-403, "ACCESS_DENIED"}, {-404, "NOT_FOUND"}, {-352, "RISK_CONTROL"}, {-412, "RISK_CONTROL"}, {-509, "RATE_LIMITED"}} {
		t.Run(tc.want+strconv.Itoa(tc.code), func(t *testing.T) {
			c := newTestClient(t, func(*http.Request) (*http.Response, error) {
				body, _ := json.Marshal(map[string]any{"code": tc.code})
				return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(string(body)))}, nil
			})
			_, err := c.Metadata(context.Background(), "au10")
			errorCode(t, err, tc.want)
		})
	}
	count := 0
	c := newTestClient(t, func(*http.Request) (*http.Response, error) {
		count++
		return &http.Response{StatusCode: 302, Header: http.Header{"Location": {"http://127.0.0.1/private"}}, Body: io.NopCloser(strings.NewReader(""))}, nil
	})
	_, err := c.Metadata(context.Background(), "au10")
	errorCode(t, err, "TEMPORARY_FAILURE")
	if count != 1 {
		t.Fatal("metadata followed a redirect")
	}
}

func TestMetadataBatchKeepsMissingItems(t *testing.T) {
	c := newTestClient(t, func(r *http.Request) (*http.Response, error) {
		if r.URL.Query().Get("sid") == "11" {
			return &http.Response{StatusCode: 404, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(""))}, nil
		}
		return response(t, map[string]any{"id": 10, "title": "Audio", "duration": 10}), nil
	})
	ids := []string{"audio:10", "audio:11", "audio:10"}
	batch, err := c.MetadataBatch(context.Background(), ids)
	if err != nil {
		t.Fatal(err)
	}
	if batch.Items[1].Track != nil || *batch.Items[1].ErrorCode != "NOT_FOUND" || len(batch.Items) != 3 {
		t.Fatal("missing or duplicate batch item lost")
	}
	if err := online.ValidateTrackBatch("bilibili", batch, ids); err != nil {
		t.Fatal(err)
	}
}

func TestUnavailableZeroTypeStaysMissingWithoutNetwork(t *testing.T) {
	c := newTestClient(t, func(*http.Request) (*http.Response, error) {
		t.Fatal("unavailable placeholder contacted upstream")
		return nil, nil
	})
	_, err := c.Metadata(context.Background(), "unavailable:0:500")
	errorCode(t, err, "NOT_FOUND")
	ids := []string{"unavailable:0:500"}
	batch, err := c.MetadataBatch(context.Background(), ids)
	if err != nil {
		t.Fatal(err)
	}
	if len(batch.Items) != 1 || batch.Items[0].Track != nil || batch.Items[0].ErrorCode == nil || *batch.Items[0].ErrorCode != "NOT_FOUND" {
		t.Fatal("zero-type placeholder was not preserved")
	}
	if err := online.ValidateTrackBatch("bilibili", batch, ids); err != nil {
		t.Fatal(err)
	}
}

func TestDestinationFiltering(t *testing.T) {
	for _, value := range []string{"127.0.0.1", "10.0.0.1", "169.254.169.254", "100.64.0.1", "::1", "::ffff:127.0.0.1", "2001:db8::1", "64:ff9b::7f00:1"} {
		if publicAddress(netip.MustParseAddr(value)) {
			t.Errorf("unsafe destination accepted: %s", value)
		}
	}
	if !publicAddress(netip.MustParseAddr("1.1.1.1")) || allowedHost("api.bilibili.com.evil") || !allowedHost("upos.bilivideo.com") {
		t.Fatal("destination policy mismatch")
	}
	if _, err := publicDial(context.Background(), "tcp", "127.0.0.1:443"); err == nil {
		t.Fatal("direct IP destination accepted")
	}
}
