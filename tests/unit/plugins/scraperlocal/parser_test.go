package scraperlocal_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/scraper/local"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/metadata/enrichment"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func search(t *testing.T, kind string, body []byte) (*scraperlocal.Engine, enrichment.SearchResult) {
	t.Helper()
	match := "exactStem"
	if kind == "audio" {
		match = "embedded"
	}
	request := enrichment.SearchRequest{Context: pluginproto.Context{UserID: "user", InstanceID: "scraper", TaskID: "task", TimeoutMs: 30000}, Target: enrichment.Target{SourceTrackID: "track", AssetID: "asset", AssetVersion: "version", Revision: 3}, Fields: []string{"lyrics", "artwork"}, Languages: []string{}, Resources: []enrichment.Resource{{ResourceRef: strings.Repeat("a", 48), Version: "v1", Kind: kind, SizeBytes: int64(len(body)), Match: match}}}
	engine, err := scraperlocal.NewEngine(request, func(context.Context, enrichment.Resource) (io.ReaderAt, error) { return bytes.NewReader(body), nil })
	if err != nil {
		t.Fatal(err)
	}
	result, err := engine.Search(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return engine, result
}

func TestEmbeddedID3LyricsPicturesAndUnsupportedFrames(t *testing.T) {
	lyric := []byte("[00:01.20]中文歌词\n[00:02.30]English")
	for _, version := range []byte{3, 4} {
		t.Run(fmt.Sprint(version), func(t *testing.T) {
			encoding := byte(3)
			text := lyric
			if version == 3 {
				encoding = 0
				text = []byte("[00:01.20]English")
			}
			uslt := append([]byte{encoding, 'e', 'n', 'g', 0}, text...)
			apic := append([]byte{0}, []byte("image/png\x00\x03\x00")...)
			apic = append(apic, testkit.ScraperPNG()...)
			linked := append([]byte{0}, []byte("-->\x00\x03\x00https://external.invalid/image")...)
			body := testkit.ScraperID3(version, testkit.ScraperFrame{ID: "USLT", Body: uslt}, testkit.ScraperFrame{ID: "APIC", Body: apic}, testkit.ScraperFrame{ID: "APIC", Body: linked})
			before := append([]byte(nil), body...)
			engine, result := search(t, "audio", body)
			if len(result.Candidates) != 3 || result.Candidates[0].Status != "available" || result.Candidates[1].MIME != "image/png" || result.Candidates[2].Status != "unavailable" {
				t.Fatalf("unexpected candidates %+v", result)
			}
			var output bytes.Buffer
			candidate, err := engine.Fetch(context.Background(), result.Candidates[0].CandidateID, &output)
			if err != nil || !bytes.Equal(output.Bytes(), text) || candidate.Target.Revision != 3 || candidate.Format != "lrc" {
				t.Fatal("embedded lyrics body or binding changed")
			}
			if _, err = engine.Fetch(context.Background(), result.Candidates[2].CandidateID, &output); err == nil {
				t.Fatal("linked picture was fetched")
			}
			if !bytes.Equal(body, before) {
				t.Fatal("audio input was modified")
			}
		})
	}
	utf16 := []byte{1, 'z', 'h', 'o', 0xff, 0xfe, 0, 0, 0xff, 0xfe, 0x2d, 0x4e, 0x87, 0x65, 0, 0}
	engine, result := search(t, "audio", testkit.ScraperID3(3, testkit.ScraperFrame{ID: "USLT", Body: utf16}))
	var out bytes.Buffer
	if len(result.Candidates) != 1 {
		t.Fatal(result)
	}
	_, err := engine.Fetch(context.Background(), result.Candidates[0].CandidateID, &out)
	if err != nil || out.String() != "中文" {
		t.Fatalf("UTF16 lyrics: %q %v", out.String(), err)
	}
}

func TestFLACEmbeddedAndSidecarContent(t *testing.T) {
	lyrics := "[00:01]中文\n[00:02]translation"
	body := testkit.ScraperFLAC(testkit.ScraperBlock{Kind: 4, Body: testkit.ScraperVorbis("TITLE=Not a candidate", "LYRICS="+lyrics)}, testkit.ScraperBlock{Kind: 6, Body: testkit.ScraperFLACPicture(testkit.ScraperPNG(), "image/png")})
	engine, result := search(t, "audio", body)
	if len(result.Candidates) != 2 || result.Candidates[0].Format != "lrc" || result.Candidates[1].Format != "png" {
		t.Fatalf("FLAC content missing %+v", result)
	}
	var out bytes.Buffer
	if _, err := engine.Fetch(context.Background(), result.Candidates[0].CandidateID, &out); err != nil || out.String() != lyrics {
		t.Fatal("FLAC lyrics changed")
	}
	_, result = search(t, "lyrics", []byte(lyrics))
	if len(result.Candidates) != 1 || result.Candidates[0].Origin != "sidecar" {
		t.Fatal("sidecar provenance missing")
	}
	_, result = search(t, "lyrics", []byte{0xff, 0xaa})
	if len(result.Candidates) != 1 || result.Candidates[0].Status != "unavailable" {
		t.Fatal("unknown encoding silently accepted")
	}
	_, result = search(t, "artwork", []byte("not a PNG"))
	if len(result.Candidates) != 1 || result.Candidates[0].Status != "unavailable" {
		t.Fatal("damaged image accepted")
	}
}

func TestMalformedEmbeddedLengthsAreBounded(t *testing.T) {
	valid := testkit.ScraperID3(4, testkit.ScraperFrame{ID: "USLT", Body: []byte("\x03eng\x00text")})
	for i := 1; i < len(valid); i++ {
		_, result := search(t, "audio", valid[:i])
		if len(result.Candidates) != 0 && result.Candidates[0].Status == "available" {
			t.Fatal("truncated frame became available")
		}
	}
	bad := append([]byte(nil), valid...)
	bad[6] = 0x7f
	bad[7] = 0x7f
	bad[8] = 0x7f
	bad[9] = 0x7f
	_, result := search(t, "audio", bad)
	if len(result.Issues) != 1 {
		t.Fatal("unbounded tag length not rejected")
	}
	_, result = search(t, "audio", testkit.ScraperFLAC(testkit.ScraperBlock{Kind: 4, Body: []byte{255, 255, 255, 255}}))
	if len(result.Issues) != 1 {
		t.Fatal("unbounded FLAC field accepted")
	}
}
