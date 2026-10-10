package playback_test

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/auth"
	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/matching"
	"github.com/fnMrRice/MusicFS-server/src/playback"
	"github.com/fnMrRice/MusicFS-server/src/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/plugins/audioffmpeg"
	"github.com/fnMrRice/MusicFS-server/src/sourceaccess"
	"github.com/fnMrRice/MusicFS-server/src/timeline"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

type recoveryFixture struct {
	player    *playback.Service
	repo      *gatedRepository
	who       auth.Session
	request   playback.CreateRequest
	tracks    []library.Track
	recording string
	path      string
}

func recoverySetup(t *testing.T, processor *audioffmpeg.Processor, maxBytes int64) recoveryFixture {
	t.Helper()
	old, repo, who, request, path := playbackService(t)
	old.Close()
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for i := range 4 {
		if err = os.WriteFile(filepath.Join(filepath.Dir(path), fmt.Sprintf("target-%d.wav", i)), shiftWave(t, payload, (i+1)*1000), 0600); err != nil {
			t.Fatal(err)
		}
	}
	access, err := sourceaccess.New([]sourceaccess.RootConfig{{ID: "root", Path: filepath.Dir(path)}})
	if err != nil {
		t.Fatal(err)
	}
	observations := []library.Observation{}
	for i := range 5 {
		name, external := "track.wav", "track"
		if i > 0 {
			name = fmt.Sprintf("target-%d.wav", i-1)
			external = fmt.Sprintf("target-%d", i-1)
		}
		entry, err := access.Inspect("root", name)
		if err != nil {
			t.Fatal(err)
		}
		observations = append(observations, library.Observation{Observation: pluginproto.Observation{ExternalID: external, RootID: "root", RelativePath: name, ObservedVersion: entry.ObservedVersion, DisplayName: name, MIME: entry.MIME, DurationMs: entry.DurationMs, SizeBytes: entry.SizeBytes}, Artists: []string{}})
	}
	ctx := context.Background()
	if _, _, err = repo.Enqueue(ctx, "admin", "root", testkit.ScanPlan("root"), "recovery-fixture-scan"); err != nil {
		t.Fatal(err)
	}
	j, err := repo.Claim(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = repo.Publish(ctx, j, testkit.ScanPublication(j, observations)); err != nil {
		t.Fatal(err)
	}
	if err = repo.Finish(ctx, j, len(observations), nil); err != nil {
		t.Fatal(err)
	}
	page, err := repo.ListTracks(ctx, "admin", "root", "", 50)
	if err != nil {
		t.Fatal(err)
	}
	tracks := make([]library.Track, 5)
	for _, public := range page.Items {
		track, err := repo.FindTrack(ctx, "admin", public.ID)
		if err != nil {
			t.Fatal(err)
		}
		if track.ExternalID == "track" {
			tracks[0] = track
		} else {
			var index int
			fmt.Sscanf(track.ExternalID, "target-%d", &index)
			tracks[index+1] = track
		}
	}
	ids := []string{}
	for _, track := range tracks {
		if track.AssetID == "" {
			t.Fatal("missing actual scanned asset")
		}
		ids = append(ids, track.ID)
	}
	linked, err := repo.ManualLink(ctx, "admin", matching.LinkRequest{SourceTrackIDs: ids}, "recovery-fixture-link")
	if err != nil {
		t.Fatal(err)
	}
	if processor == nil {
		processor = &audioffmpeg.Processor{Capabilities: audioffmpeg.Capabilities{FFmpegSHA256: strings.Repeat("a", 64)}}
	}
	if maxBytes == 0 {
		maxBytes = 16 << 20
	}
	player, err := playback.New(ctx, repo.Store, repo.library, processor, playback.Config{CacheRoot: filepath.Join(filepath.Dir(path), "cache-recovery"), MaxSessions: 8, MaxCacheBytes: maxBytes})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(player.Close)
	request.Preferences.ForceProcessing = true
	return recoveryFixture{player, repo, who, request, tracks, *linked.RecordingID, path}
}
func shiftWave(t *testing.T, input []byte, ms int) []byte {
	t.Helper()
	var rate int
	var data int
	for offset := 12; offset+8 <= len(input); {
		size := int(binary.LittleEndian.Uint32(input[offset+4:]))
		if string(input[offset:offset+4]) == "fmt " {
			rate = int(binary.LittleEndian.Uint32(input[offset+16:]))
		}
		if string(input[offset:offset+4]) == "data" {
			data = offset
			break
		}
		offset += 8 + size + (size % 2)
	}
	if data == 0 || rate == 0 {
		t.Fatal("invalid generated wave")
	}
	padding := make([]byte, rate*ms/1000)
	out := append([]byte{}, input[:data+8]...)
	out = append(out, padding...)
	out = append(out, input[data+8:]...)
	binary.LittleEndian.PutUint32(out[4:], uint32(len(out)-8))
	binary.LittleEndian.PutUint32(out[data+4:], binary.LittleEndian.Uint32(input[data+4:])+uint32(len(padding)))
	return out
}
func timelineTarget(t library.Track) timeline.Target {
	return timeline.Target{SourceTrackID: t.ID, AssetID: t.AssetID, Version: t.Version}
}
func (f recoveryFixture) evidence(t *testing.T, index int) timeline.Evidence {
	t.Helper()
	profile, err := f.player.RecoveryProfile()
	if err != nil {
		t.Fatal(err)
	}
	e, err := f.player.AddTimeline(context.Background(), f.who.User.ID, timeline.Request{RecordingID: f.recording, From: timelineTarget(f.tracks[0]), To: timelineTarget(f.tracks[index]), Range: timeline.Range{EndMs: 6000}, OffsetMs: int64(index * 1000), Profile: profile}, fmt.Sprintf("manual-align-%d", index))
	if err != nil {
		t.Fatal(err)
	}
	return e
}
func (f recoveryFixture) create(t *testing.T, key string) playback.Description {
	t.Helper()
	d, err := f.player.Create(context.Background(), f.who, f.request, key)
	if err != nil {
		t.Fatal(err)
	}
	return d
}
func recoveryRequest(d playback.Description, target string, e *timeline.Evidence) playback.RecoverSourceRequest {
	r := playback.RecoverSourceRequest{AttemptID: d.AttemptID, TargetSourceTrackID: target, PositionMs: 4000, ObservedError: &playback.ObservedError{Code: "NETWORK", Message: "Synthetic interruption"}}
	if e != nil {
		r.EvidenceID = &e.ID
	}
	return r
}
func recoveryError(t *testing.T, err error, code string) {
	t.Helper()
	var failure *domain.Error
	if !errors.As(err, &failure) || failure.Code != code {
		t.Fatalf("expected %s, got %v", code, err)
	}
}
