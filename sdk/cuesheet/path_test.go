package cuesheet_test

import (
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/cuesheet"
)

func TestUnsafeReferences(t *testing.T) {
	for _, reference := range []string{
		"", "/music/track.flac", `\music\track.flac`, `C:\music\track.flac`, `C:track.flac`,
		`\\server\share\track.flac`, `\\?\C:\track.flac`, "../track.flac", "sub/../../track.flac",
		`..\track.flac`, "sub/../..", ".", "..", "sub/", "track.flac:stream", "https://example.test/track.flac",
		"sub/.. /track.flac", "track.flac.", "track.flac ", "NUL", "nul.flac", "sub/CON.txt", "COM1.wav", "LPT9.mp3",
		"track?.flac", "track*.flac", "<track>.flac", "a|b.flac",
	} {
		t.Run(reference, func(t *testing.T) {
			assertError(t, "FILE \""+reference+"\" WAVE\nTRACK 01 AUDIO\nINDEX 01 00:00:00", cuesheet.CodeInvalid)
		})
	}
}

func FuzzParse(f *testing.F) {
	f.Add("FILE track.flac WAVE\nTRACK 01 AUDIO\nINDEX 01 00:00:00")
	f.Add("FILE ../track.flac WAVE")
	f.Add("\ufeffTITLE \"中文\"\r\n")
	f.Fuzz(func(t *testing.T, input string) {
		sheet, err := cuesheet.Parse(input)
		if err != nil {
			return
		}
		if len(sheet.Files) == 0 || len(sheet.Files) > cuesheet.MaxFiles || len(sheet.Tracks) == 0 || len(sheet.Tracks) > cuesheet.MaxTracks {
			t.Fatalf("invalid parsed collection sizes: %+v", sheet)
		}
		for _, file := range sheet.Files {
			if strings.HasPrefix(file.Path, "/") || strings.HasPrefix(file.Path, "../") || strings.ContainsAny(file.Path, "\\:") {
				t.Fatalf("unsafe parsed reference: %+v", file)
			}
		}
		for _, track := range sheet.Tracks {
			if track.FileIndex < 0 || track.FileIndex >= len(sheet.Files) || track.StartFrames < 0 || track.EndFrames != nil && *track.EndFrames <= track.StartFrames {
				t.Fatalf("invalid parsed track: %+v", track)
			}
		}
	})
}
