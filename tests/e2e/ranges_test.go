package e2e_test

import (
	"bytes"
	"github.com/fnMrRice/MusicFS-server/src/playback"
	"io"
	"net/http"
	"testing"
)

func verifyRanges(t *testing.T, c *client, d playback.Description, fixture []byte) {
	t.Helper()
	for _, tc := range []struct {
		rangeValue string
		status     int
		expected   []byte
	}{{"bytes=44-99", 206, fixture[44:100]}, {"bytes=-32", 206, fixture[len(fixture)-32:]}, {"bytes=999999999-", 416, nil}, {"", 200, fixture}} {
		r, _ := http.NewRequest("GET", c.base+d.StreamURL, nil)
		r.Header.Set("Range", tc.rangeValue)
		response, err := c.http.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := io.ReadAll(response.Body)
		response.Body.Close()
		if response.StatusCode != tc.status {
			t.Fatalf("range %s: %d", tc.rangeValue, response.StatusCode)
		}
		if tc.expected != nil && !bytes.Equal(raw, tc.expected) {
			t.Fatal("range bytes differ")
		}
	}
	r, _ := http.NewRequest("HEAD", c.base+d.StreamURL, nil)
	response, err := c.http.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 200 || response.Header.Get("ETag") == "" || response.Header.Get("Accept-Ranges") != "bytes" {
		t.Fatal("missing HEAD metadata")
	}
	r, _ = http.NewRequest("GET", c.base+d.StreamURL, nil)
	r.Header.Set("Range", "bytes=44-99")
	r.Header.Set("If-Range", "\"wrong\"")
	response, err = c.http.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatal("If-Range mismatch should return full representation")
	}
}
