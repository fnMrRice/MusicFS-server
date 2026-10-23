package ncmdecoder_test

import (
	"bytes"
	"errors"
	"io"
	"testing"

	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestContainerArtworkUsesEffectiveLength(t *testing.T) {
	payload := payloadFor("mp3", 4096)
	cover := []byte("opaque container artwork")
	data, err := testkit.BuildNCMWithOptions(payload, nil, testkit.NCMOptions{Cover: cover, CoverPadding: 3 << 20})
	if err != nil {
		t.Fatal(err)
	}
	r := reader(t, data)
	artwork, size := r.Artwork()
	if artwork == nil || size != int64(len(cover)) {
		t.Fatalf("unexpected artwork descriptor: %v %d", artwork, size)
	}
	buffer := make([]byte, len(cover)+20)
	n, err := artwork.ReadAt(buffer, 0)
	if n != len(cover) || !errors.Is(err, io.EOF) || !bytes.Equal(buffer[:n], cover) {
		t.Fatalf("artwork leaked padding or changed bytes: %d %v", n, err)
	}
	if n, err := artwork.ReadAt(buffer, size); n != 0 || !errors.Is(err, io.EOF) {
		t.Fatalf("artwork range escaped effective length: %d %v", n, err)
	}
	if _, err := artwork.ReadAt(buffer, -1); err == nil {
		t.Fatal("negative artwork offset accepted")
	}
	got, err := io.ReadAll(io.NewSectionReader(r, 0, r.Size()))
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("artwork extraction changed audio bytes: %v", err)
	}
}

func TestEmptyContainerArtworkIsAbsent(t *testing.T) {
	for _, padding := range []int{0, 2048} {
		data, err := testkit.BuildNCMWithOptions(payloadFor("flac", 128), nil, testkit.NCMOptions{CoverPadding: padding})
		if err != nil {
			t.Fatal(err)
		}
		artwork, size := reader(t, data).Artwork()
		if artwork != nil || size != 0 {
			t.Fatalf("padding-only cover exposed artwork: %d", size)
		}
	}
}
