package resources_test

import (
	"crypto/sha256"
	"fmt"
	"io"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/playback"
)

func completeOutput(t *testing.T, c *client, track string) (int64, string) {
	t.Helper()
	input := playback.CreateRequest{SourceTrackID: track, Capabilities: playback.Capabilities{HTMLAudio: true, Formats: []playback.Format{{MIME: "audio/mpeg", CanPlay: "probably"}}}, Preferences: playback.Preferences{ForceProcessing: true, GainDB: -6}}
	var description playback.Description
	check(t, c.request("POST", "/playback/sessions", input, "resource-complete-output", &description))
	if description.DeliveryMode != "processed" {
		t.Fatal("complete output did not use processing")
	}
	var size int64
	var digest string
	for attempt := 0; attempt < 2; attempt++ {
		response, err := c.http.Get(c.base + description.StreamURL)
		check(t, err)
		if response.StatusCode != 200 {
			response.Body.Close()
			t.Fatal("complete stream failed", response.StatusCode)
		}
		h := sha256.New()
		n, err := io.Copy(h, io.LimitReader(response.Body, 64<<20))
		check(t, err)
		check(t, response.Body.Close())
		if n < 128<<10 || n >= 64<<20 {
			t.Fatal("unexpected complete output size", n)
		}
		actual := fmt.Sprintf("%x", h.Sum(nil))
		if attempt == 0 {
			size, digest = n, actual
		} else if size != n || digest != actual {
			t.Fatal("cached output changed bytes")
		}
	}
	check(t, c.request("DELETE", "/playback/sessions/"+description.SessionID, nil, "", nil))
	return size, digest
}
