package playback_test

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	audioffmpeg "github.com/fnMrRice/MusicFS-server/plugins/decoder/ffmpeg"
	ncmdecoder "github.com/fnMrRice/MusicFS-server/plugins/decoder/ncm"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/playback"
	"github.com/fnMrRice/MusicFS-server/src/plugins/sourceaccess"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestOriginalWaveReportsMonoSampleRateWithoutProbe(t *testing.T) {
	data := make([]byte, 44+22050*2)
	copy(data, "RIFF")
	binary.LittleEndian.PutUint32(data[4:], uint32(len(data)-8))
	copy(data[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(data[16:], 16)
	binary.LittleEndian.PutUint16(data[20:], 1)
	binary.LittleEndian.PutUint16(data[22:], 1)
	binary.LittleEndian.PutUint32(data[24:], 22050)
	binary.LittleEndian.PutUint32(data[28:], 44100)
	binary.LittleEndian.PutUint16(data[32:], 2)
	binary.LittleEndian.PutUint16(data[34:], 16)
	copy(data[36:], "data")
	binary.LittleEndian.PutUint32(data[40:], uint32(len(data)-44))
	profile := createOriginalMetadataPlayback(t, "mono.wav", data, nil, nil)
	if profile.Channels != 1 || profile.SampleRate != 22050 || profile.Layout != "mono" || profile.BitrateBps != 352800 {
		t.Fatalf("mono WAV metadata was not preserved: %+v", profile)
	}
}

func TestOriginalMP3AndNCMProbeFailureDoesNotInventMetadata(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(testkit.RepositoryRoot(), "tests/testdata/audio/vbr-untagged.mp3"))
	if err != nil {
		t.Fatal(err)
	}
	decoder, err := ncmdecoder.NewProcessor(ncmdecoder.Config{Builtin: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, container := range []string{"mp3", "ncm"} {
		t.Run(container, func(t *testing.T) {
			payload := data
			if container == "ncm" {
				payload, err = testkit.BuildNCM(data, nil)
				if err != nil {
					t.Fatal(err)
				}
			}
			profile := createOriginalMetadataPlayback(t, "unknown."+container, payload, nil, decoder)
			if profile.Channels != 0 || profile.SampleRate != 0 || profile.Layout != "" || profile.BitrateBps != 0 {
				t.Fatalf("unavailable probe invented output metadata: %+v", profile)
			}
		})
	}
}

func TestOriginalMP3AndNCMReportActualMetadata(t *testing.T) {
	ffmpeg, ffprobe := os.Getenv("MUSICFS_FFMPEG"), os.Getenv("MUSICFS_FFPROBE")
	if ffmpeg == "" || ffprobe == "" {
		t.Skip("set MUSICFS_FFMPEG and MUSICFS_FFPROBE for original stream metadata validation")
	}
	processor, err := audioffmpeg.NewProcessor(t.Context(), audioffmpeg.Config{Builtin: true, FFmpegPath: ffmpeg, FFprobePath: ffprobe})
	if err != nil {
		t.Fatal(err)
	}
	decoder, err := ncmdecoder.NewProcessor(ncmdecoder.Config{Builtin: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, channels := range []int{1, 2} {
		path := filepath.Join(t.TempDir(), "source.mp3")
		command := exec.CommandContext(t.Context(), ffmpeg, "-v", "error", "-i", filepath.Join(testkit.RepositoryRoot(), "tests/testdata/audio/markers.wav"), "-ac", fmt.Sprint(channels), "-ar", "44100", "-b:a", "192k", "-id3v2_version", "0", "-write_id3v1", "0", path)
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("MP3 fixture encoding failed: %v %s", err, output)
		}
		payload, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, container := range []string{"mp3", "ncm"} {
			t.Run(fmt.Sprintf("%s-%d", container, channels), func(t *testing.T) {
				data := payload
				if container == "ncm" {
					data, err = testkit.BuildNCM(payload, nil)
					if err != nil {
						t.Fatal(err)
					}
				}
				profile := createOriginalMetadataPlayback(t, "source."+container, data, processor, decoder)
				layout := "stereo"
				if channels == 1 {
					layout = "mono"
				}
				if profile.Channels != channels || profile.SampleRate != 44100 || profile.Layout != layout || profile.Codec == nil || *profile.Codec != "mp3" || profile.BitrateBps != 192000 {
					t.Fatalf("original stream metadata mismatch: %+v", profile)
				}
			})
		}
	}
}

func createOriginalMetadataPlayback(t *testing.T, name string, payload []byte, processor *audioffmpeg.Processor, decoder *ncmdecoder.Processor) playback.OutputProfile {
	t.Helper()
	f := metadataPlaybackFixture(t, name, payload, processor, decoder)
	description, err := f.player.Create(t.Context(), f.who, f.request, "original-metadata-create")
	if err != nil || description.DeliveryMode != "proxy" {
		t.Fatalf("metadata probing changed original playback: %+v %v", description, err)
	}
	raw, err := json.Marshal(description)
	if err != nil {
		t.Fatal(err)
	}
	testkit.ValidateHTTP(t, "Playback", raw)
	stream, err := f.player.OpenStream(t.Context(), f.who, description.SessionID, description.AttemptID, false)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	actual, err := io.ReadAll(stream.File)
	if err != nil || len(actual) == 0 || filepath.Ext(name) != ".ncm" && !bytes.Equal(actual, payload) {
		t.Fatal("metadata probing changed original bytes", err)
	}
	return description.OutputProfile
}

func metadataPlaybackFixture(t *testing.T, name string, payload []byte, processor *audioffmpeg.Processor, decoder *ncmdecoder.Processor) recoveryFixture {
	t.Helper()
	old, repo, who, request, oldPath := playbackService(t)
	old.Close()
	root := filepath.Dir(oldPath)
	if err := os.WriteFile(filepath.Join(root, name), payload, 0600); err != nil {
		t.Fatal(err)
	}
	access, err := sourceaccess.New([]sourceaccess.RootConfig{{ID: "root", Path: root}})
	if err != nil {
		t.Fatal(err)
	}
	entry, err := access.Inspect("root", name)
	if err != nil {
		t.Fatal(err)
	}
	codec, container := "mp3", "mp3"
	if filepath.Ext(name) == ".wav" {
		codec, container = "pcm_s16le", "wav"
	}
	if filepath.Ext(name) == ".ncm" {
		container = "ncm"
	}
	if _, _, err := repo.Enqueue(t.Context(), who.User.ID, "root", testkit.ScanPlan("root"), "metadata-fixture-scan"); err != nil {
		t.Fatal(err)
	}
	job, err := repo.Claim(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	declaredBitrate := int64(9999999)
	observed := library.Observation{Observation: pluginproto.Observation{ExternalID: "metadata", RootID: "root", RelativePath: name, ObservedVersion: entry.ObservedVersion,
		DisplayName: name, MIME: entry.MIME, SizeBytes: entry.SizeBytes, Codec: &codec, BitrateBps: &declaredBitrate}, Container: &container, Artists: []string{}}
	if err := repo.Publish(t.Context(), job, testkit.ScanPublication(job, []library.Observation{observed})); err != nil {
		t.Fatal(err)
	}
	if err := repo.Finish(t.Context(), job, 1, nil); err != nil {
		t.Fatal(err)
	}
	page, err := repo.ListTracks(t.Context(), who.User.ID, "root", "", 50)
	if err != nil {
		t.Fatal("metadata source not published", err)
	}
	request.SourceTrackID = ""
	for _, track := range page.Items {
		if track.DisplayName == name {
			request.SourceTrackID = track.ID
		}
	}
	if request.SourceTrackID == "" {
		t.Fatal("metadata source not listed")
	}
	request.Preferences.DSPExecution = "client"
	request.Capabilities.Formats = append(request.Capabilities.Formats, playback.Format{MIME: "audio/flac", CanPlay: "probably"})
	player, err := playback.New(t.Context(), repo.Store, repo.library, processor, playback.Config{NCMDecoder: decoder, CacheRoot: t.TempDir(), MaxSessions: 8, MaxCacheBytes: 16 << 20})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(player.Close)
	return recoveryFixture{player: player, repo: repo, who: who, request: request, path: filepath.Join(root, name)}
}
