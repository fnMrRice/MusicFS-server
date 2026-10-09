package ncmdecoder_test

import (
	"bytes"
	"errors"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/plugins/ncmdecoder"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func payloadFor(format string, size int) []byte {
	payload := make([]byte, size)
	_, _ = rand.New(rand.NewSource(71)).Read(payload)
	if format == "flac" {
		copy(payload, "fLaC")
	} else {
		copy(payload, []byte{0xff, 0xfb, 0x90, 0})
	}
	return payload
}

func fixture(t testing.TB, payload []byte, metadata any) []byte {
	t.Helper()
	data, err := testkit.BuildNCM(payload, metadata)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func reader(t *testing.T, data []byte) *ncmdecoder.Reader {
	t.Helper()
	r, err := ncmdecoder.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestRandomAccessAndSectionReader(t *testing.T) {
	for _, format := range []string{"mp3", "flac"} {
		t.Run(format, func(t *testing.T) {
			payload := payloadFor(format, 120123)
			data, err := testkit.BuildNCMWithOptions(payload, nil, testkit.NCMOptions{Cover: []byte{1, 2, 3}, CoverPadding: 37, Key: []byte{13, 17, 0, 255}})
			if err != nil {
				t.Fatal(err)
			}
			r := reader(t, data)
			if r.Size() != int64(len(payload)) || r.Metadata().Format != format {
				t.Fatalf("unexpected size or format: %d %+v", r.Size(), r.Metadata())
			}
			random := rand.New(rand.NewSource(81))
			for i := 0; i < 1500; i++ {
				offset := random.Intn(len(payload))
				count := random.Intn(3000) + 1
				got := make([]byte, count)
				n, err := r.ReadAt(got, int64(offset))
				want := min(count, len(payload)-offset)
				if n != want || !bytes.Equal(got[:n], payload[offset:offset+want]) {
					t.Fatalf("mismatch at offset %d count %d", offset, count)
				}
				if want < count && !errors.Is(err, io.EOF) || want == count && err != nil {
					t.Fatalf("unexpected read error: %v", err)
				}
			}
			section := io.NewSectionReader(r, 0, r.Size())
			var result bytes.Buffer
			if _, err := io.CopyBuffer(&result, section, make([]byte, 777)); err != nil || !bytes.Equal(result.Bytes(), payload) {
				t.Fatalf("sequential mismatch: %v", err)
			}
			if _, err := section.Seek(257, io.SeekStart); err != nil {
				t.Fatal(err)
			}
			chunk := make([]byte, 511)
			if _, err := io.ReadFull(section, chunk); err != nil || !bytes.Equal(chunk, payload[257:768]) {
				t.Fatalf("seek mismatch: %v", err)
			}
		})
	}
}

func TestConcurrentReadAndMetadataCopy(t *testing.T) {
	payload := payloadFor("mp3", 14001)
	metadata := map[string]any{"musicName": "中文标题", "album": "测试专辑", "artist": [][]any{{"艺术家", 42}}, "duration": "6543", "bitrate": 128000, "format": "flac"}
	r := reader(t, fixture(t, payload, metadata))
	m := r.Metadata()
	if m.Format != "mp3" || m.MIME != "audio/mpeg" || m.Title != "中文标题" || m.Album != "测试专辑" || len(m.Artists) != 1 || m.Artists[0] != "艺术家" || m.DurationMs == nil || *m.DurationMs != 6543 || m.BitrateBps == nil || *m.BitrateBps != 128000 {
		t.Fatalf("metadata mismatch: %+v", m)
	}
	m.Artists[0] = "mutated"
	*m.DurationMs = 1
	*m.BitrateBps = 2
	var tasks sync.WaitGroup
	for index := 0; index < 16; index++ {
		tasks.Add(1)
		go func(index int) {
			defer tasks.Done()
			for i := 0; i < 100; i++ {
				offset := (index*100 + i*127) % (len(payload) - 512)
				var chunk [512]byte
				if n, err := r.ReadAt(chunk[:], int64(offset)); n != len(chunk) || err != nil || !bytes.Equal(chunk[:], payload[offset:offset+512]) {
					t.Errorf("concurrent read mismatch: %v", err)
					return
				}
				m := r.Metadata()
				if m.Artists[0] != "艺术家" || *m.DurationMs != 6543 || *m.BitrateBps != 128000 {
					t.Error("metadata leaked mutation")
					return
				}
				m.Artists[0] = "private copy"
			}
		}(index)
	}
	tasks.Wait()
}

func TestOptionalMetadataAndEOF(t *testing.T) {
	for _, metadata := range []any{nil, map[string]any{}, map[string]any{"musicName": false, "album": 3, "artist": false, "duration": -1, "bitrate": "overflow999999999999999999999"}} {
		r := reader(t, fixture(t, payloadFor("flac", 300), metadata))
		m := r.Metadata()
		if m.Title != "" || m.Album != "" || len(m.Artists) != 0 || m.DurationMs != nil || m.BitrateBps != nil {
			t.Fatalf("invalid optional field retained: %+v", m)
		}
		if n, err := r.ReadAt(nil, 1000); n != 0 || err != nil {
			t.Fatalf("zero read: %d %v", n, err)
		}
		if n, err := r.ReadAt(make([]byte, 1), r.Size()); n != 0 || !errors.Is(err, io.EOF) {
			t.Fatalf("end read: %d %v", n, err)
		}
		if _, err := r.ReadAt(make([]byte, 1), -1); err == nil {
			t.Fatal("negative offset accepted")
		}
	}
}

func TestTaggedMP3(t *testing.T) {
	for _, version := range []byte{2, 3, 4} {
		payload := append([]byte{'I', 'D', '3', version, 0, 0, 0, 0, 0, 5}, make([]byte, 5)...)
		payload = append(payload, payloadFor("mp3", 900)...)
		r := reader(t, fixture(t, payload, nil))
		if r.Metadata().Format != "mp3" {
			t.Fatal("ID3 MP3 not identified")
		}
	}
}

func TestCommittedAudioPayloads(t *testing.T) {
	for _, name := range []string{"vbr-untagged.mp3", "lossless.flac"} {
		t.Run(name, func(t *testing.T) {
			payload, err := os.ReadFile(filepath.Join("..", "..", "testdata", "audio", name))
			if err != nil {
				t.Fatal(err)
			}
			r := reader(t, fixture(t, payload, map[string]any{"musicName": "真实音频载荷的合成容器"}))
			result, err := io.ReadAll(io.NewSectionReader(r, 0, r.Size()))
			if err != nil || !bytes.Equal(result, payload) {
				t.Fatalf("audio fixture mismatch: %v", err)
			}
		})
	}
}
