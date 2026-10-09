package api_test

import (
	"crypto/sha512"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestAudioFixtureHashes(t *testing.T) {
	root := filepath.Join(testkit.RepositoryRoot(), "tests/testdata/audio")
	var manifest struct {
		License string `json:"license"`
		Files   []struct {
			Name  string `json:"name"`
			Size  int64  `json:"sizeBytes"`
			Hash  string `json:"sha512"`
			Valid bool   `json:"expectedValid"`
		} `json:"files"`
	}
	b, err := os.ReadFile(filepath.Join(root, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.License != "CC0-1.0" || len(manifest.Files) != 8 {
		t.Fatal("incomplete fixture manifest")
	}
	for _, file := range manifest.Files {
		t.Run(file.Name, func(t *testing.T) {
			b, err := os.ReadFile(filepath.Join(root, file.Name))
			if err != nil {
				t.Fatal(err)
			}
			digest := sha512.Sum512(b)
			if int64(len(b)) != file.Size || hex.EncodeToString(digest[:]) != file.Hash {
				t.Fatal("fixture content does not match manifest")
			}
		})
	}
}

func TestWaveMarkersAndSilence(t *testing.T) {
	for _, test := range []struct {
		name   string
		prefix int
		factor float64
	}{{"markers.wav", 0, 1}, {"silence-prefix.wav", 2, 1}, {"中文 空格曲目.wav", 0, 1.5}} {
		t.Run(test.name, func(t *testing.T) {
			b, err := os.ReadFile(filepath.Join(testkit.RepositoryRoot(), "tests/testdata/audio", test.name))
			if err != nil {
				t.Fatal(err)
			}
			if len(b) != 44+(6+test.prefix)*48000*4 || string(b[:4]) != "RIFF" || string(b[8:12]) != "WAVE" || binary.LittleEndian.Uint16(b[20:22]) != 1 || binary.LittleEndian.Uint16(b[22:24]) != 2 || binary.LittleEndian.Uint32(b[24:28]) != 48000 {
				t.Fatal("unexpected wave format or length")
			}
			pcm := b[44:]
			for _, sample := range pcm[:test.prefix*48000*4] {
				if sample != 0 {
					t.Fatal("nonzero sample in silence prefix")
				}
			}
			for segment := 0; segment < 3; segment++ {
				for channel := 0; channel < 2; channel++ {
					expectSilent := segment == 0 && channel == 1 || segment == 1 && channel == 0
					crossings := 0
					previous := int16(0)
					peak := int16(0)
					start := (test.prefix+segment*2)*48000 + 24000
					for i := start; i < start+48000; i++ {
						value := int16(binary.LittleEndian.Uint16(pcm[i*4+channel*2:]))
						if value > peak {
							peak = value
						}
						if value > 0 && previous <= 0 {
							crossings++
						}
						previous = value
					}
					if expectSilent {
						if peak != 0 || crossings != 0 {
							t.Fatalf("segment %d channel %d is not silent", segment, channel)
						}
					} else if math.Abs(float64(crossings)-220*float64(segment+1)*test.factor) > 1 || peak < 4090 || peak > 4096 {
						t.Fatalf("segment %d channel %d: unexpected frequency %d or peak %d", segment, channel, crossings, peak)
					}
				}
			}
		})
	}
}
