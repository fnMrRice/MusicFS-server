package writeback_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/tags/flac"
	"github.com/fnMrRice/MusicFS-server/src/metadata/writeback"
)

type block struct {
	kind byte
	body []byte
}

func fixture(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile("../../testdata/audio/lossless.flac")
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func parse(t *testing.T, b []byte) ([]block, []byte) {
	t.Helper()
	if len(b) < 4 || string(b[:4]) != "fLaC" {
		t.Fatal("invalid fixture")
	}
	b = b[4:]
	var blocks []block
	for {
		if len(b) < 4 {
			t.Fatal("truncated metadata")
		}
		last, kind, n := b[0]&128 != 0, b[0]&127, int(b[1])<<16|int(b[2])<<8|int(b[3])
		b = b[4:]
		if n > len(b) {
			t.Fatal("truncated block")
		}
		blocks = append(blocks, block{kind, append([]byte(nil), b[:n]...)})
		b = b[n:]
		if last {
			return blocks, b
		}
	}
}
func assemble(blocks []block, audio []byte) []byte {
	b := []byte("fLaC")
	for i, block := range blocks {
		k := block.kind
		if i == len(blocks)-1 {
			k |= 128
		}
		n := len(block.body)
		b = append(b, k, byte(n>>16), byte(n>>8), byte(n))
		b = append(b, block.body...)
	}
	return append(b, audio...)
}
func comments(vendor string, values ...string) []byte {
	var b bytes.Buffer
	put := func(s string) { binary.Write(&b, binary.LittleEndian, uint32(len(s))); b.WriteString(s) }
	put(vendor)
	binary.Write(&b, binary.LittleEndian, uint32(len(values)))
	for _, s := range values {
		put(s)
	}
	return b.Bytes()
}
func picture(t *testing.T) []byte {
	t.Helper()
	im := image.NewNRGBA(image.Rect(0, 0, 2, 3))
	im.Set(1, 1, color.NRGBA{R: 255, A: 255})
	var b bytes.Buffer
	if err := png.Encode(&b, im); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
func rewrite(t *testing.T, input []byte, p writeback.Patch) []byte {
	t.Helper()
	var out bytes.Buffer
	_, err := flac.New().Rewrite(context.Background(), bytes.NewReader(input), int64(len(input)), &out, p)
	if err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}
func TestFLACPreservesAudioAndUnselectedMetadata(t *testing.T) {
	blocks, audio := parse(t, fixture(t))
	custom := []block{blocks[0], {4, comments("private encoder", "tItLe=Existing", "X-PRIVATE=Opaque value", "X-PRIVATE=Duplicate", "LYRICS=Existing lyrics")}, {2, []byte("TESTopaque application payload")}, {42, []byte{0, 255, 0, 1}}}
	input := assemble(custom, audio)
	output := rewrite(t, input, writeback.Patch{Comments: map[string][]string{"title": {"Ignored"}, "artists": {"One", "Two"}}, Artwork: picture(t)})
	got, gotAudio := parse(t, output)
	if !bytes.Equal(audio, gotAudio) || !bytes.Equal(got[0].body, custom[0].body) {
		t.Fatal("audio or streaminfo changed")
	}
	for _, wanted := range custom[2:] {
		found := false
		for _, b := range got {
			if b.kind == wanted.kind && bytes.Equal(b.body, wanted.body) {
				found = true
			}
		}
		if !found {
			t.Fatal("unknown block lost")
		}
	}
	var tags []byte
	for _, b := range got {
		if b.kind == 4 {
			tags = b.body
		}
	}
	for _, s := range []string{"private encoder", "tItLe=Existing", "X-PRIVATE=Opaque value", "X-PRIVATE=Duplicate", "LYRICS=Existing lyrics", "ARTIST=One", "ARTIST=Two"} {
		if !bytes.Contains(tags, []byte(s)) {
			t.Fatalf("lost field %s", s)
		}
	}
	if bytes.Contains(tags, []byte("Ignored")) {
		t.Fatal("default overwrote existing value")
	}
	overwritten := rewrite(t, output, writeback.Patch{Comments: map[string][]string{"title": {"Replacement"}}, Overwrite: true})
	got, gotAudio = parse(t, overwritten)
	if !bytes.Equal(audio, gotAudio) {
		t.Fatal("audio changed")
	}
	for _, b := range got {
		if b.kind == 4 && (bytes.Contains(b.body, []byte("tItLe=Existing")) || !bytes.Contains(b.body, []byte("TITLE=Replacement"))) {
			t.Fatal("explicit title replace failed")
		}
	}
	if bytes.Equal(output, overwritten) {
		t.Fatal("edit missing")
	}
}
func TestFLACRefusesMalformedAndClearOperations(t *testing.T) {
	input := fixture(t)
	valid := writeback.Patch{Comments: map[string][]string{"title": {"Test"}}}
	cases := [][]byte{nil, []byte("fLaC"), input[:42], append([]byte("OggS"), input[4:]...)}
	for _, b := range cases {
		var out bytes.Buffer
		if _, err := flac.New().Rewrite(context.Background(), bytes.NewReader(b), int64(len(b)), &out, valid); err == nil {
			t.Fatal("accepted malformed input")
		}
	}
	for _, p := range []writeback.Patch{{}, {Comments: map[string][]string{"title": {""}}}, {Comments: map[string][]string{"arbitrary": {"x"}}}, {Artwork: []byte("https://image.invalid")}} {
		var out bytes.Buffer
		if _, err := flac.New().Rewrite(context.Background(), bytes.NewReader(input), int64(len(input)), &out, p); err == nil {
			t.Fatal("accepted invalid patch")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := flac.New().Rewrite(ctx, bytes.NewReader(input), int64(len(input)), io.Discard, valid); err != context.Canceled {
		t.Fatalf("cancel: %v", err)
	}
	if _, err := flac.New().Rewrite(context.Background(), bytes.NewReader(input), int64(len(input)), shortWriter{}, valid); err != io.ErrShortWrite {
		t.Fatalf("short write: %v", err)
	}
}

type shortWriter struct{}

func (shortWriter) Write(p []byte) (int, error) { return len(p) / 2, nil }
