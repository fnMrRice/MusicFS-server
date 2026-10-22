package cuesheet_test

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/cuesheet"
)

func TestParsePreservesFramesMetadataAndFileBoundaries(t *testing.T) {
	input := "\ufeff" + strings.ReplaceAll(`REM COMMENT "opaque text
TITLE "中文作品"
PERFORMER "社团"
CATALOG "0123456789012"
CDTEXTFILE "ignored.cdtext"
FILE "CD1\中文 音轨.flac" WAVE
  TRACK 01 AUDIO
    TITLE "第一章"
    PERFORMER "声优甲"
    SONGWRITER "作者"
    ISRC "XX0000000001"
    INDEX 00 00:00:00
    INDEX 01 00:02:00
  TRACK 02 AUDIO
    TITLE "第二章"
    INDEX 00 03:59:74
    INDEX 01 04:01:01
FILE "CD2/音轨.flac" WAVE
  TRACK 03 AUDIO
    INDEX 01 00:00:00
`, "\n", "\r\n")
	sheet, err := cuesheet.Parse(input)
	if err != nil {
		t.Fatal(err)
	}
	if sheet.Title != "中文作品" || sheet.Performer != "社团" || len(sheet.Files) != 2 || len(sheet.Tracks) != 3 {
		t.Fatalf("unexpected sheet: %+v", sheet)
	}
	if sheet.Files[0].Path != "CD1/中文 音轨.flac" || sheet.Files[0].Type != "WAVE" {
		t.Fatalf("unexpected file: %+v", sheet.Files[0])
	}
	first, second, third := sheet.Tracks[0], sheet.Tracks[1], sheet.Tracks[2]
	if first.Title != "第一章" || first.Performer != "声优甲" || first.FileIndex != 0 || first.StartFrames != 150 || *first.Index00Frames != 0 {
		t.Fatalf("unexpected first track: %+v", first)
	}
	if first.EndFrames == nil || *first.EndFrames != 18076 || second.StartFrames != 18076 || *second.Index00Frames != 17999 {
		t.Fatalf("incorrect pregap boundary: %+v %+v", first, second)
	}
	if second.EndFrames != nil || third.EndFrames != nil || third.FileIndex != 1 || third.StartFrames != 0 {
		t.Fatalf("file timelines were combined: %+v %+v", second, third)
	}
	if second.Performer != "" {
		t.Fatalf("track metadata must not silently inherit sheet metadata: %+v", second)
	}
}

func TestValidVariants(t *testing.T) {
	cases := []struct {
		name, text, path string
		frames           int64
	}{
		{"case and tabs", "file\ttrack.mp3\tmp3\ntrack\t01\taudio\nindex\t01\t00:00:01", "track.mp3", 1},
		{"normalized reference", "FILE ./CD1/../track.flac WAVE\nTRACK 01 AUDIO\nINDEX 01 00:59:74", "track.flac", 4499},
		{"zero gap", "FILE track.aiff AIFF\nTRACK 05 AUDIO\nINDEX 00 00:00:00\nINDEX 01 00:00:00", "track.aiff", 0},
		{"long work", "FILE track.flac WAVE\nTRACK 01 AUDIO\nINDEX 01 120:00:00", "track.flac", 540000},
		{"maximum frame", "FILE track.flac WAVE\nTRACK 01 AUDIO\nINDEX 01 2049638230412172:24:07", "track.flac", math.MaxInt64},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sheet, err := cuesheet.Parse(tc.text)
			if err != nil {
				t.Fatal(err)
			}
			if sheet.Files[0].Path != tc.path || sheet.Tracks[0].StartFrames != tc.frames || sheet.Tracks[0].EndFrames != nil {
				t.Fatalf("unexpected sheet: %+v", sheet)
			}
		})
	}
}

func TestInvalidStructure(t *testing.T) {
	file := "FILE track.flac WAVE\n"
	track := file + "TRACK 01 AUDIO\n"
	valid := track + "INDEX 01 00:00:00\n"
	cases := []struct{ name, text, code string }{
		{"empty", "", cuesheet.CodeInvalid},
		{"metadata only", "TITLE album", cuesheet.CodeInvalid},
		{"invalid encoding", string([]byte{0xff}), cuesheet.CodeInvalid},
		{"embedded control", "REM unsafe\x00", cuesheet.CodeInvalid},
		{"bare carriage return", "REM one\rREM two", cuesheet.CodeInvalid},
		{"missing file type", "FILE track.flac", cuesheet.CodeInvalid},
		{"missing file", "TRACK 01 AUDIO", cuesheet.CodeInvalid},
		{"missing track", file, cuesheet.CodeInvalid},
		{"missing index", track, cuesheet.CodeInvalid},
		{"missing index before track", track + "TRACK 02 AUDIO\nINDEX 01 00:00:00", cuesheet.CodeInvalid},
		{"missing index before file", track + valid, cuesheet.CodeInvalid},
		{"index zero only", track + "INDEX 00 00:00:00", cuesheet.CodeInvalid},
		{"index without track", file + "INDEX 01 00:00:00", cuesheet.CodeInvalid},
		{"empty file section", file + valid, cuesheet.CodeInvalid},
		{"empty last file", valid + "FILE other.flac WAVE", cuesheet.CodeInvalid},
		{"duplicate file", valid + valid, cuesheet.CodeInvalid},
		{"normalized duplicate file", valid + "FILE ./track.flac WAVE", cuesheet.CodeInvalid},
		{"duplicate track", valid + "TRACK 01 AUDIO\nINDEX 01 00:01:00", cuesheet.CodeInvalid},
		{"descending track", file + "TRACK 02 AUDIO\nINDEX 01 00:00:00\nTRACK 01 AUDIO", cuesheet.CodeInvalid},
		{"zero track", file + "TRACK 00 AUDIO", cuesheet.CodeInvalid},
		{"out of range track", file + "TRACK 100 AUDIO", cuesheet.CodeInvalid},
		{"signed track", file + "TRACK +1 AUDIO", cuesheet.CodeInvalid},
		{"non audio", file + "TRACK 01 MODE1/2352", cuesheet.CodeUnsupported},
		{"raw disc data", "FILE track.bin BINARY", cuesheet.CodeUnsupported},
		{"unknown directive", valid + "MYSTERY 1", cuesheet.CodeUnsupported},
		{"pregap", track + "PREGAP 00:02:00", cuesheet.CodeUnsupported},
		{"postgap", valid + "POSTGAP 00:02:00", cuesheet.CodeUnsupported},
		{"preemphasis", track + "FLAGS PRE", cuesheet.CodeUnsupported},
		{"unsupported index", valid + "INDEX 02 00:02:00", cuesheet.CodeUnsupported},
		{"duplicate index one", valid + "INDEX 01 00:01:00", cuesheet.CodeInvalid},
		{"duplicate index zero", track + "INDEX 00 00:00:00\nINDEX 00 00:01:00", cuesheet.CodeInvalid},
		{"late index zero", valid + "INDEX 00 00:01:00", cuesheet.CodeInvalid},
		{"reversed gap", track + "INDEX 00 00:02:00\nINDEX 01 00:01:00", cuesheet.CodeInvalid},
		{"equal track starts", valid + "TRACK 02 AUDIO\nINDEX 01 00:00:00", cuesheet.CodeInvalid},
		{"reversed file index", track + "INDEX 01 00:03:00\nTRACK 02 AUDIO\nINDEX 00 00:02:00", cuesheet.CodeInvalid},
		{"unclosed quote", "TITLE \"title", cuesheet.CodeInvalid},
		{"adjacent quoted value", "TITLE \"title\"extra", cuesheet.CodeInvalid},
		{"unquoted space", "TITLE long title", cuesheet.CodeInvalid},
		{"missing metadata", valid + "TITLE", cuesheet.CodeInvalid},
		{"invalid ignored metadata", valid + "ISRC", cuesheet.CodeInvalid},
		{"ambiguous metadata", valid + "FILE other.flac WAVE\nTITLE ambiguous", cuesheet.CodeInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) { assertError(t, tc.text, tc.code) })
	}
}

func TestInvalidPositions(t *testing.T) {
	for _, value := range []string{
		"00:60:00", "00:00:75", "-1:00:00", "+1:00:00", "00:01", "00:00:00:00",
		"0:00:00", "00:0:00", "00:00:0", "00:00:1a", "00:00:01.0", "9223372036854775808:00:00",
		"2049638230412172:24:08", "2049638230412173:00:00",
	} {
		t.Run(value, func(t *testing.T) {
			assertError(t, "FILE track.flac WAVE\nTRACK 01 AUDIO\nINDEX 01 "+value, cuesheet.CodeInvalid)
		})
	}
}

func TestResourceLimits(t *testing.T) {
	assertError(t, strings.Repeat(" ", cuesheet.MaxSheetBytes+1), cuesheet.CodeLimit)
	assertError(t, "REM "+strings.Repeat("a", cuesheet.MaxLineBytes), cuesheet.CodeLimit)
	var input strings.Builder
	input.WriteString("FILE track.flac WAVE\n")
	for i := 1; i <= cuesheet.MaxTracks; i++ {
		fmt.Fprintf(&input, "TRACK %02d AUDIO\nINDEX 01 %02d:00:00\n", i, i)
	}
	sheet, err := cuesheet.Parse(input.String())
	if err != nil || len(sheet.Tracks) != cuesheet.MaxTracks {
		t.Fatalf("track limit should be inclusive: tracks=%d err=%v", len(sheet.Tracks), err)
	}
	input.WriteString("TRACK 100 AUDIO\nINDEX 01 100:00:00")
	assertError(t, input.String(), cuesheet.CodeLimit)
}

func TestErrorsDoNotExposeInputPaths(t *testing.T) {
	_, err := cuesheet.Parse("FILE \"C:\\private\\sensitive.flac\" WAVE")
	var cueErr *cuesheet.Error
	if !errors.As(err, &cueErr) || cueErr.Line != 1 || strings.Contains(err.Error(), "private") || strings.Contains(err.Error(), "sensitive") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func assertError(t *testing.T, input, code string) {
	t.Helper()
	sheet, err := cuesheet.Parse(input)
	var cueErr *cuesheet.Error
	if !errors.As(err, &cueErr) || cueErr.Code != code {
		t.Fatalf("wanted error %s, got %v", code, err)
	}
	if len(sheet.Tracks) != 0 || len(sheet.Files) != 0 {
		t.Fatalf("failed parsing must not return partial results: %+v", sheet)
	}
}
