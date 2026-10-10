package mediastore_test

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/mediastore/content"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestLyricsStrictEncodingAndFormats(t *testing.T) {
	for _, body := range [][]byte{[]byte("[00:01.00]中文歌词\n[00:02.00]<00:02.10>逐<00:02.20>字"), []byte("plain text\nEnglish lyrics"), {0xff, 0xfe, 0x2d, 0x4e, 0x87, 0x65}, {0xfe, 0xff, 0x4e, 0x2d, 0x65, 0x87}} {
		got, err := content.Validate("lyrics", body)
		if err != nil || len(got.SHA256) != 64 || got.MIME != "text/plain; charset=utf-8" {
			t.Fatalf("valid lyrics rejected: %v", err)
		}
		if bytes.Contains(body, []byte("[00:")) && got.Format != "lrc" {
			t.Fatal("timed lyrics not identified")
		}
	}
	for _, body := range [][]byte{nil, []byte(" \n"), {0xff, 0xff}, {0xff, 0xfe, 0}, {0xfe, 0xff, 0xd8, 0}, {0}, {'a', 0, 'b'}, make([]byte, content.MaxLyricsBytes+1)} {
		if _, err := content.Validate("lyrics", body); err == nil {
			t.Fatal("invalid or unknown lyrics encoding accepted")
		}
	}
}

func TestArtworkContentAndDecodeLimits(t *testing.T) {
	jpeg, err := content.Validate("artwork", testkit.ScraperJPEG())
	if err != nil || jpeg.MIME != "image/jpeg" {
		t.Fatal("JPEG content was not detected")
	}
	picture := testkit.ScraperPNG()
	got, err := content.Validate("artwork", picture)
	if err != nil || got.Format != "png" || got.MIME != "image/png" || got.Width != 2 || got.Height != 2 {
		t.Fatalf("valid artwork rejected: %v", err)
	}
	oversize := append([]byte(nil), picture...)
	binary.BigEndian.PutUint32(oversize[16:20], 8193)
	binary.BigEndian.PutUint32(oversize[29:33], crc32.ChecksumIEEE(oversize[12:29]))
	for _, body := range [][]byte{[]byte("<svg>unsafe</svg>"), picture[:len(picture)/2], oversize, make([]byte, content.MaxArtworkBytes+1)} {
		if _, err := content.Validate("artwork", body); err == nil {
			t.Fatal("invalid artwork accepted")
		}
	}
}
