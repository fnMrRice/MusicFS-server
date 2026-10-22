package playback_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"testing"

	ncmdecoder "github.com/fnMrRice/MusicFS-server/plugins/decoder/ncm"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/playback"
	"github.com/fnMrRice/MusicFS-server/src/playback/timeline"
	"github.com/fnMrRice/MusicFS-server/src/plugins/codecsettings"
	"github.com/fnMrRice/MusicFS-server/src/plugins/settings"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestTranscodeBitrateUsesInnerSourceReferenceBeforePCMConversion(t *testing.T) {
	processor := flacTestProcessor(t, "stereo")
	decoder, err := ncmdecoder.NewProcessor(ncmdecoder.Config{Builtin: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, pcm := range []bool{false, true} {
		t.Run(fmt.Sprint(pcm), func(t *testing.T) {
			payload, err := testkit.BuildNCMWithOptions(flacTestPayload(t), map[string]any{"format": "flac"}, testkit.NCMOptions{CoverPadding: 1 << 20})
			if err != nil {
				t.Fatal(err)
			}
			f := metadataPlaybackFixture(t, "source.ncm", payload, processor, decoder)
			publishFixtureBitrate(t, f, 116162)
			f.request.Preferences.ForceProcessing = true
			if pcm {
				service := settings.New(f.repo, codecsettings.Providers()...)
				page, err := service.Read(t.Context(), f.who.User.ID, codecsettings.FFmpegID)
				if err != nil {
					t.Fatal(err)
				}
				if _, err = service.Update(t.Context(), f.who.User.ID, codecsettings.FFmpegID, settings.Request{ExpectedRevision: &page.Revision, SchemaRevision: page.Schema.Revision, Values: settings.Values{"decodeOutput": "pcm_s16le"}}); err != nil {
					t.Fatal(err)
				}
			}
			d := f.create(t, "nearest-ncm-bitrate")
			if d.OutputProfile.ID != "mp3-112" || d.OutputProfile.BitrateBps != 112000 {
				t.Fatal("NCM wrapper or decoded PCM replaced the original audio bitrate reference", d.OutputProfile)
			}
			encoded, err := processedBytes(t, f, d)
			if err != nil {
				t.Fatal(err)
			}
			assertEncodedMP3Bitrate(t, encoded, 112000)
		})
	}
}

func TestSelectedBitratePersistsAcrossCacheSeekAndRecovery(t *testing.T) {
	processor := flacTestProcessor(t, "stereo")
	f := recoverySetup(t, processor, 0)
	publishFixtureBitrate(t, f, 192000)
	f.request.Preferences.ForceProcessing = false
	original := f.create(t, "selected-original-create")
	if original.DeliveryMode != "proxy" {
		t.Fatal("bitrate selection changed compatible original playback", original)
	}
	f.request.Preferences.ForceProcessing = true
	first := f.create(t, "selected-192-create")
	firstBytes, err := processedBytes(t, f, first)
	if err != nil {
		t.Fatal(err)
	}
	assertEncodedMP3Bitrate(t, firstBytes, 192000)
	publishFixtureBitrate(t, f, 320000)
	second := f.create(t, "selected-320-create")
	secondBytes, err := processedBytes(t, f, second)
	if err != nil {
		t.Fatal(err)
	}
	assertEncodedMP3Bitrate(t, secondBytes, 320000)
	if first.AssetID != second.AssetID || first.OutputProfile.ID != "mp3-192" || second.OutputProfile.ID != "mp3-320" || bytes.Equal(firstBytes, secondBytes) {
		t.Fatal("source bitrate did not independently select each session representation", first.OutputProfile, second.OutputProfile)
	}
	records, err := f.repo.ListProcessedCache(t.Context())
	if err != nil || len(records) != 2 || records[0].Identity.Profile == records[1].Identity.Profile || records[0].Key == records[1].Key {
		t.Fatal("different target bitrates shared a cache identity", records, err)
	}
	cached, err := processedBytes(t, f, first)
	if err != nil || !bytes.Equal(firstBytes, cached) {
		t.Fatal("old session did not retain its bitrate cache", err)
	}
	seek, err := f.player.Control(t.Context(), f.who, first.SessionID, "seek", playback.ControlRequest{AttemptID: first.AttemptID, PositionMs: 1000}, "selected-bitrate-seek")
	if err != nil || seek.OutputProfile.ID != first.OutputProfile.ID {
		t.Fatal("seek changed the active session bitrate", seek, err)
	}
	tail, err := processedBytes(t, f, seek)
	if err != nil {
		t.Fatal(err)
	}
	assertEncodedMP3Bitrate(t, tail, 192000)
	profile, err := f.player.RecoveryProfile()
	if err != nil {
		t.Fatal(err)
	}
	profile.OutputProfile = first.OutputProfile.ID
	evidence, err := f.player.AddTimeline(t.Context(), f.who.User.ID, timeline.Request{RecordingID: f.recording, From: timelineTarget(f.tracks[0]), To: timelineTarget(f.tracks[1]), Range: timeline.Range{EndMs: 6000}, OffsetMs: 1000, Profile: profile}, "selected-bitrate-evidence")
	if err != nil {
		t.Fatal(err)
	}
	result, err := f.player.RecoverSource(t.Context(), f.who, seek.SessionID, recoveryRequest(seek, f.tracks[1].ID, &evidence), "selected-bitrate-recovery")
	if err != nil || result.Decision != "continue" || result.Playback == nil || result.Playback.OutputProfile.ID != first.OutputProfile.ID {
		t.Fatal("cross-source recovery changed the selected session bitrate", result, err)
	}
	recovered, err := processedBytes(t, f, *result.Playback)
	if err != nil {
		t.Fatal(err)
	}
	assertEncodedMP3Bitrate(t, recovered, 192000)
	result, err = f.player.RecoverSource(t.Context(), f.who, original.SessionID, recoveryRequest(original, f.tracks[1].ID, &evidence), "selected-original-first-transcode")
	if err != nil || result.Decision != "continue" || result.Playback == nil || result.Playback.OutputProfile.ID != first.OutputProfile.ID {
		t.Fatal("first transcode during original playback recovery lost the source bitrate reference", result, err)
	}
	recovered, err = processedBytes(t, f, *result.Playback)
	if err != nil {
		t.Fatal(err)
	}
	assertEncodedMP3Bitrate(t, recovered, 192000)
}

func publishFixtureBitrate(t *testing.T, f recoveryFixture, bitrate int64) {
	t.Helper()
	page, err := f.repo.ListTracks(t.Context(), f.who.User.ID, "root", "", 50)
	if err != nil {
		t.Fatal(err)
	}
	observations := []library.Observation{}
	for _, public := range page.Items {
		track, err := f.repo.FindTrack(t.Context(), f.who.User.ID, public.ID)
		if err != nil {
			t.Fatal(err)
		}
		observations = append(observations, library.Observation{Observation: pluginproto.Observation{ExternalID: track.ExternalID, RootID: track.RootID, RelativePath: track.Path, ObservedVersion: track.Version,
			DisplayName: track.DisplayName, MIME: track.MIME, SizeBytes: track.Size, Codec: track.Codec, DurationMs: track.DurationMs, BitrateBps: &bitrate}, Container: track.Container, Artists: track.Artists})
	}
	if _, _, err = f.repo.Enqueue(t.Context(), f.who.User.ID, "root", testkit.ScanPlan("root"), domain.NewID()); err != nil {
		t.Fatal(err)
	}
	job, err := f.repo.Claim(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err = f.repo.Publish(t.Context(), job, testkit.ScanPublication(job, observations)); err != nil {
		t.Fatal(err)
	}
	if err = f.repo.Finish(t.Context(), job, len(observations), nil); err != nil {
		t.Fatal(err)
	}
}

func assertEncodedMP3Bitrate(t *testing.T, payload []byte, expected int) {
	t.Helper()
	command := exec.CommandContext(t.Context(), os.Getenv("MUSICFS_FFPROBE"), "-v", "error", "-select_streams", "a:0", "-show_entries", "stream=codec_name,bit_rate", "-of", "json", "pipe:0")
	command.Stdin = bytes.NewReader(payload)
	raw, err := command.Output()
	var metadata struct {
		Streams []struct {
			Codec   string `json:"codec_name"`
			Bitrate string `json:"bit_rate"`
		} `json:"streams"`
	}
	if err != nil || json.Unmarshal(raw, &metadata) != nil || len(metadata.Streams) != 1 || metadata.Streams[0].Codec != "mp3" || metadata.Streams[0].Bitrate != fmt.Sprint(expected) {
		t.Fatalf("encoded MP3 bitrate differs from advertised target: %s %v", raw, err)
	}
}
