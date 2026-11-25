package plugins_test

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/decoder/ffmpeg"
)

func TestCatalogMetadataActualProbeTagsRateAndUnknownValues(t *testing.T) {
	ffmpeg, ffprobe := audioTools(t)
	// 合成网易云注释只包含字符串形式的 musicId，覆盖超过浮点精度的编号。
	const tag = "163 key(Don't modify):L64FU3W4YxX3ZFTmbZ+8/TOHd4SXCEvHq+p31uxStfVzr4HOpt2vBiNQAl/OEu9T"
	cmd := exec.Command(ffmpeg, "-v", "error", "-i", "pipe:0", "-c:a", "flac", "-metadata", "title=Track", "-metadata", "artist=Artist", "-metadata", "album=Album", "-metadata", "genre=Rock", "-metadata", "date=2024-10-10", "-metadata", "comment="+tag, "-f", "flac", "pipe:1")
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
	if m.NetEaseID != "9007199254740993" {
		t.Fatalf("missing embedded NetEase track ID: %q", m.NetEaseID)
	}
	if m.BitDepth != 16 {
		t.Fatalf("unexpected FLAC bit depth: %d", m.BitDepth)
	}
	wave := toneWave(1)
	unknown, err := audioffmpeg.ProbeMetadata(context.Background(), ffprobe, bytes.NewReader(wave), int64(len(wave)))
	if err != nil || unknown.Album != nil || unknown.Genre != nil || unknown.Year != nil || unknown.SampleRateHz == nil || *unknown.SampleRateHz != 48000 {
		t.Fatal(unknown, err)
	}
	if unknown.BitDepth != 16 {
		t.Fatalf("unexpected PCM bit depth: %d", unknown.BitDepth)
	}
	mp3, err := os.ReadFile(filepath.Join("..", "..", "testdata", "audio", "vbr-untagged.mp3"))
	if err != nil {
		t.Fatal(err)
	}
	lossy, err := audioffmpeg.ProbeMetadata(t.Context(), ffprobe, bytes.NewReader(mp3), int64(len(mp3)))
	if err != nil || lossy.BitDepth != 0 {
		t.Fatalf("unexpected MP3 metadata: %+v, %v", lossy, err)
	}
}

func TestProbeNativeAlbumRequiresVisibleAlbumTrackCredit(t *testing.T) {
	ffmpeg, ffprobe := audioTools(t)
	// 合成原生标签含歌曲编号、发行编号、曲名 Track、署名 Artist 和专辑 Album。
	const tag = "163 key(Don't modify):L64FU3W4YxX3ZFTmbZ+8/TOHd4SXCEvHq+p31uxStfXNwUaBs3EL2NfXhoJD93k0UsC2ygPUMB2Bmkd/04It8OLyX7/OVYWQQjsUQ/qEK5lqhtIE6KSra9D5LqEgmk1K/23SaxLka9Y4hs1DZGgTmAUoNGImfI84YaaLd0aOpZk="
	for _, tc := range []struct {
		title, artist, album string
		match                bool
	}{
		{"Track", "Guest / Artist", "Ａlbum", true},
		{"Track", "Artist", "Album Disc 2", false},
		{"Track", "Artist", "Best Of", false},
		{"Track", "Another Artist", "Album", false},
		{"Track (Live)", "Artist", "Album", false},
	} {
		cmd := exec.Command(ffmpeg, "-v", "error", "-i", "pipe:0", "-c:a", "flac",
			"-metadata", "title="+tc.title, "-metadata", "artist="+tc.artist, "-metadata", "album="+tc.album,
			"-metadata", "comment="+tag, "-f", "flac", "pipe:1")
		cmd.Stdin = bytes.NewReader(toneWave(1))
		data, err := cmd.Output()
		if err != nil {
			t.Fatal(err)
		}
		m, err := audioffmpeg.ProbeMetadata(t.Context(), ffprobe, bytes.NewReader(data), int64(len(data)))
		if err != nil || m.NetEaseID != "9007199254740993" {
			t.Fatalf("Missing track identity for %v: %v, %v", tc, m, err)
		}
		if (m.AlbumRelease != nil) != tc.match || tc.match && (m.AlbumRelease.Namespace != "netease" || m.AlbumRelease.ID != "9007199254740995") {
			t.Fatalf("Unexpected release for %v: %+v", tc, m.AlbumRelease)
		}
	}
}
