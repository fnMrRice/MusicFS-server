package playback_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	audioffmpeg "github.com/fnMrRice/MusicFS-server/plugins/decoder/ffmpeg"
	ncmdecoder "github.com/fnMrRice/MusicFS-server/plugins/decoder/ncm"
	"github.com/fnMrRice/MusicFS-server/src/playback"
	streamhttp "github.com/fnMrRice/MusicFS-server/src/playback/stream"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestOriginalFLACNegotiatesClientCapabilitiesAndPreservesBytes(t *testing.T) {
	processor := flacTestProcessor(t, "stereo")
	decoder, err := ncmdecoder.NewProcessor(ncmdecoder.Config{Builtin: true})
	if err != nil {
		t.Fatal(err)
	}
	payload := flacTestPayload(t)
	for _, container := range []string{"flac", "ncm"} {
		for _, test := range []struct {
			name, canPlay, mode string
			force               bool
		}{
			{"supported", "probably", "proxy", false},
			{"maybe", "maybe", "proxy", false},
			{"unsupported", "", "processed", false},
			{"forced", "probably", "processed", true},
		} {
			t.Run(container+"-"+test.name, func(t *testing.T) {
				data := payload
				if container == "ncm" {
					data, err = testkit.BuildNCMWithOptions(payload, map[string]any{"format": "mp3", "duration": 1}, testkit.NCMOptions{CoverPadding: 128 << 10})
					if err != nil {
						t.Fatal(err)
					}
				}
				f := metadataPlaybackFixture(t, "source."+container, data, processor, decoder)
				f.request.Capabilities.Formats = []playback.Format{{MIME: "audio/mpeg", CanPlay: "probably"}, {MIME: "audio/flac", CanPlay: test.canPlay}}
				f.request.Preferences.ForceProcessing = test.force
				description := f.create(t, "flac-client-capabilities")
				if description.DeliveryMode != test.mode {
					t.Fatal("FLAC did not follow client capabilities and processing preference", description)
				}
				raw, _ := json.Marshal(description)
				testkit.ValidateHTTP(t, "Playback", raw)
				profile := description.OutputProfile
				if test.mode == "processed" {
					if profile.MIME != "audio/mpeg" || profile.Codec == nil || *profile.Codec != "mp3" || !strings.HasPrefix(profile.ID, "mp3-") {
						t.Fatal("unsupported or forced FLAC did not use the negotiated MP3 profile", profile)
					}
					return
				}
				if description.SeekMode != "byteRange" || profile.ID != "" || profile.MIME != "audio/flac" || profile.Codec == nil || *profile.Codec != "flac" || profile.Container == nil || *profile.Container != "flac" || profile.Channels != 2 || profile.SampleRate != 48000 || profile.Layout != "stereo" || profile.BitrateBps != 116162 || description.SeekableRange == nil || description.SeekableRange.EndMs != 6000 {
					t.Fatal("original FLAC metadata mismatch", description)
				}
				if container == "ncm" {
					if len(description.EffectiveProcessing) != 1 || description.EffectiveProcessing[0].Type != "ncmDecrypt" {
						t.Fatal("native NCM FLAC should only report decryption", description.EffectiveProcessing)
					}
				} else if len(description.EffectiveProcessing) != 0 {
					t.Fatal("native FLAC incorrectly reported processing", description.EffectiveProcessing)
				}
				assertOriginalFLACRanges(t, f, description, payload)
			})
		}
	}
}

func TestOriginalFLACMetadataWithoutProbeAndInvalidStructure(t *testing.T) {
	payload := flacTestPayload(t)
	profile := createOriginalMetadataPlayback(t, "source.flac", payload, nil, nil)
	if profile.MIME != "audio/flac" || profile.Codec == nil || *profile.Codec != "flac" || profile.Container == nil || *profile.Container != "flac" || profile.Channels != 2 || profile.SampleRate != 48000 || profile.Layout != "stereo" || profile.BitrateBps != 0 {
		t.Fatal("FLAC structure metadata should remain available without inventing bitrate", profile)
	}
	for _, test := range []struct {
		name string
		edit func([]byte) []byte
	}{
		{"magic-only", func(data []byte) []byte { return data[:4] }},
		{"truncated-streaminfo", func(data []byte) []byte { return data[:40] }},
		{"wrong-streaminfo-size", func(data []byte) []byte { data[7] = 33; return data }},
		{"zero-sample-rate", func(data []byte) []byte { data[18], data[19] = 0, 0; data[20] &= 15; return data }},
		{"missing-audio", func(data []byte) []byte { data[4] |= 128; return data[:42] }},
		{"invalid-audio-boundary", func(data []byte) []byte { data[4] |= 128; return data }},
		{"oversized-metadata", func(data []byte) []byte { data[45] = 255; return data }},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := metadataPlaybackFixture(t, "broken.flac", test.edit(bytes.Clone(payload)), nil, nil)
			_, err := f.player.Create(t.Context(), f.who, f.request, "flac-invalid-structure")
			recoveryError(t, err, "UNSUPPORTED")
		})
	}
}

func TestOriginalFLACMultichannelMetadataAndRequiredConversion(t *testing.T) {
	processor := flacTestProcessor(t, "stereo")
	for _, channels := range []int{3, 4, 5, 6, 7, 8} {
		t.Run(fmt.Sprint(channels), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "multichannel.flac")
			command := exec.CommandContext(t.Context(), os.Getenv("MUSICFS_FFMPEG"), "-v", "error", "-i", filepath.Join(testkit.RepositoryRoot(), "tests/testdata/audio/markers.wav"), "-ac", fmt.Sprint(channels), "-ar", "44100", path)
			if output, err := command.CombinedOutput(); err != nil {
				t.Fatalf("FLAC fixture encoding failed: %v %s", err, output)
			}
			payload, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			profile := createOriginalMetadataPlayback(t, "multichannel.flac", payload, processor, nil)
			if profile.Channels != channels || profile.SampleRate != 44100 || profile.Layout == "" || profile.BitrateBps <= 0 {
				t.Fatal("FLAC multichannel metadata was lost", profile)
			}
		})
	}
	processor = flacTestProcessor(t, "5.1")
	f := metadataPlaybackFixture(t, "source.flac", flacTestPayload(t), processor, nil)
	f.request.Capabilities.Formats = append(f.request.Capabilities.Formats, playback.Format{MIME: `audio/webm; codecs="opus"`, CanPlay: "probably"})
	description := f.create(t, "flac-required-channel-conversion")
	if description.DeliveryMode != "processed" || description.OutputProfile.Channels != 6 || description.OutputProfile.Codec == nil || *description.OutputProfile.Codec != "opus" {
		t.Fatal("client FLAC support bypassed required channel conversion", description)
	}
}

func flacTestProcessor(t *testing.T, layout string) *audioffmpeg.Processor {
	t.Helper()
	ffmpeg, ffprobe := os.Getenv("MUSICFS_FFMPEG"), os.Getenv("MUSICFS_FFPROBE")
	if ffmpeg == "" || ffprobe == "" {
		t.Skip("set MUSICFS_FFMPEG and MUSICFS_FFPROBE for original FLAC validation")
	}
	processor, err := audioffmpeg.NewProcessor(t.Context(), audioffmpeg.Config{Builtin: true, FFmpegPath: ffmpeg, FFprobePath: ffprobe, OutputLayout: layout})
	if err != nil {
		t.Fatal(err)
	}
	return processor
}

func flacTestPayload(t *testing.T) []byte {
	t.Helper()
	payload, err := os.ReadFile(filepath.Join(testkit.RepositoryRoot(), "tests/testdata/audio/lossless.flac"))
	if err != nil {
		t.Fatal(err)
	}
	return payload
}

func assertOriginalFLACRanges(t *testing.T, f recoveryFixture, description playback.Description, expected []byte) {
	t.Helper()
	for _, ranged := range []bool{false, true} {
		stream, err := f.player.OpenStream(t.Context(), f.who, description.SessionID, description.AttemptID, false)
		if err != nil {
			t.Fatal(err)
		}
		request := httptest.NewRequest("GET", description.StreamURL, nil)
		status, wanted := http.StatusOK, expected
		if ranged {
			request.Header.Set("Range", "bytes=10-99")
			status, wanted = http.StatusPartialContent, expected[10:100]
		}
		response := httptest.NewRecorder()
		if err := streamhttp.Serve(response, request, stream); err != nil || response.Code != status || response.Header().Get("Content-Type") != "audio/flac" || !bytes.Equal(response.Body.Bytes(), wanted) {
			t.Fatal("original FLAC stream did not preserve HTTP type, range, or bytes", err, response.Code, response.Header())
		}
	}
}
