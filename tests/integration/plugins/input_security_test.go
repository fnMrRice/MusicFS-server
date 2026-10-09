package plugins_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/plugins/audioffmpeg"
)

func TestAudioInputsCannotFollowDisguisedPlaylists(t *testing.T) {
	_, ffprobe := audioTools(t)
	var accessed atomic.Int32
	untrusted := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { accessed.Add(1); w.WriteHeader(http.StatusForbidden) }))
	defer untrusted.Close()
	input := []byte(fmt.Sprintf("#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-TARGETDURATION:4\n#EXT-X-MEDIA-SEQUENCE:0\n#EXTINF:4.0,\n%s/segment.aac\n#EXT-X-ENDLIST\n", untrusted.URL))
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if _, err := audioffmpeg.ProbeMetadata(ctx, ffprobe, bytes.NewReader(input), int64(len(input))); err == nil {
		t.Fatal("ffprobe accepted an HLS playlist as audio")
	}
	if accessed.Load() != 0 {
		t.Fatal("ffprobe followed a playlist URL")
	}
	p := newProcessor(t)
	if err := p.Process(ctx, audioffmpeg.Plan{TaskID: "playlist", AssetID: "disguised-mp3", Profile: "mp3-128"}, bytes.NewReader(input), int64(len(input)), io.Discard); err == nil {
		t.Fatal("decoder accepted an HLS playlist as audio")
	}
	if accessed.Load() != 0 {
		t.Fatal("decoder followed a playlist URL")
	}
}
