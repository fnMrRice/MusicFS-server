package plugins_test

import (
	"bytes"
	"context"
	"os/exec"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/decoder/ffmpeg"
)

func TestCatalogMetadataActualProbeTagsRateAndUnknownValues(t *testing.T) {
	ffmpeg, ffprobe := audioTools(t)
	cmd := exec.Command(ffmpeg, "-v", "error", "-i", "pipe:0", "-c:a", "flac", "-metadata", "title=Track", "-metadata", "artist=Artist", "-metadata", "album=Album", "-metadata", "genre=Rock", "-metadata", "date=2024-10-10", "-f", "flac", "pipe:1")
	cmd.Stdin = bytes.NewReader(toneWave(1))
	var encoded, stderr bytes.Buffer
	cmd.Stdout = &encoded
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatal(err, stderr.String())
	}
	data := encoded.Bytes()
	m, err := audioffmpeg.ProbeMetadata(context.Background(), ffprobe, bytes.NewReader(data), int64(len(data)))
	if err != nil || m.Album == nil || *m.Album != "Album" || m.Genre == nil || *m.Genre != "Rock" || m.Year == nil || *m.Year != 2024 || m.SampleRateHz == nil || *m.SampleRateHz != 48000 {
		t.Fatal(m, err)
	}
	if m.ProbeAlgorithm != "ffprobe" || m.ProbeVersion == "" {
		t.Fatalf("metadata probe lost its algorithm identity: %+v", m)
	}
	wave := toneWave(1)
	unknown, err := audioffmpeg.ProbeMetadata(context.Background(), ffprobe, bytes.NewReader(wave), int64(len(wave)))
	if err != nil || unknown.Album != nil || unknown.Genre != nil || unknown.Year != nil || unknown.SampleRateHz == nil || *unknown.SampleRateHz != 48000 {
		t.Fatal(unknown, err)
	}
}
