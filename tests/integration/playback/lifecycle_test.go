package playback_test

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite"
	"github.com/fnMrRice/MusicFS-server/plugins/decoder/ffmpeg"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/core/auth"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/library/jobs"
	"github.com/fnMrRice/MusicFS-server/src/playback"
	"github.com/fnMrRice/MusicFS-server/src/plugins/pluginhost"
	"github.com/fnMrRice/MusicFS-server/src/plugins/sourceaccess"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

type gatedRepository struct {
	*sqlite.Store
	gate    atomic.Bool
	entered chan struct{}
	proceed chan struct{}
	library *library.Service
}

func (r *gatedRepository) FindTrack(ctx context.Context, user, id string) (library.Track, error) {
	if r.gate.Load() {
		close(r.entered)
		select {
		case <-r.proceed:
		case <-ctx.Done():
			return library.Track{}, ctx.Err()
		}
	}
	return r.Store.FindTrack(ctx, user, id)
}

func playbackService(t *testing.T) (*playback.Service, *gatedRepository, auth.Session, playback.CreateRequest, string) {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	root := filepath.Join(dir, "music")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	wave, err := os.ReadFile(filepath.Join(testkit.RepositoryRoot(), "tests/testdata/audio/markers.wav"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "track.wav")
	if err := os.WriteFile(path, wave, 0600); err != nil {
		t.Fatal(err)
	}
	access, err := sourceaccess.New([]sourceaccess.RootConfig{{ID: "root", Path: root}})
	if err != nil {
		t.Fatal(err)
	}
	entry, err := access.Inspect("root", "track.wav")
	if err != nil {
		t.Fatal(err)
	}
	store, err := sqlite.Open(ctx, filepath.Join(dir, "data.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	if _, _, err := store.Enqueue(ctx, "admin", "root", testkit.ScanPlan("root"), "fixture-scan"); err != nil {
		t.Fatal(err)
	}
	job, err := store.Claim(ctx)
	if err != nil {
		t.Fatal(err)
	}
	observation := library.Observation{Observation: pluginproto.Observation{ExternalID: "track", RootID: "root", RelativePath: "track.wav", ObservedVersion: entry.ObservedVersion, DisplayName: "track.wav", MIME: entry.MIME, DurationMs: entry.DurationMs, SizeBytes: entry.SizeBytes}, Artists: []string{}}
	if err := store.Publish(ctx, job, testkit.ScanPublication(job, []library.Observation{observation})); err != nil {
		t.Fatal(err)
	}
	if err := store.Finish(ctx, job, 1, nil); err != nil {
		t.Fatal(err)
	}
	page, err := store.ListTracks(ctx, "admin", "root", "", 50)
	if err != nil || len(page.Items) != 1 {
		t.Fatal("fixture not published", err)
	}
	repo := &gatedRepository{Store: store, entered: make(chan struct{}), proceed: make(chan struct{})}
	lib := library.New(repo, jobs.New(store), []library.Runtime{{ID: "root", Name: "Fixture", Owner: "admin", Worker: &pluginhost.Worker{Access: access, Manifest: pluginproto.Manifest{SourceType: pluginproto.SourceLocal}}}}, nil)
	repo.library = lib
	service, err := playback.New(ctx, store, lib, &audioffmpeg.Processor{Capabilities: syntheticAudioCapabilities("")}, playback.Config{CacheRoot: filepath.Join(dir, "cache"), MaxSessions: 8, MaxCacheBytes: 16 << 20})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(service.Close)
	who := auth.Session{ID: "auth-session-1", User: auth.User{ID: "admin", Role: "admin"}, ExpiresAt: time.Now().UTC().Add(time.Hour)}
	if err := store.SaveAuth(ctx, who); err != nil {
		t.Fatal(err)
	}
	request := playback.CreateRequest{SourceTrackID: page.Items[0].ID, Capabilities: playback.Capabilities{HTMLAudio: true, Formats: []playback.Format{{MIME: "audio/wav", CanPlay: "probably"}, {MIME: "audio/mpeg", CanPlay: "probably"}}}}
	return service, repo, who, request, path
}

func TestOriginalPlaybackChecksFormatOwnershipAndImmutableBytes(t *testing.T) {
	service, _, who, request, path := playbackService(t)
	d, err := service.Create(context.Background(), who, request, "create-once")
	if err != nil {
		t.Fatal(err)
	}
	if d.OutputProfile.Transport != "htmlAudio" {
		t.Fatalf("unexpected browser transport: %s", d.OutputProfile.Transport)
	}
	if d.OutputProfile.Codec == nil || *d.OutputProfile.Codec != "pcm_s16le" || d.SeekableRange == nil || d.SeekableRange.EndMs != 6000 {
		t.Fatal("WAV format was not validated without FFprobe")
	}
	if d.OutputProfile.Channels != 2 || d.OutputProfile.SampleRate != 48000 || d.OutputProfile.Layout != "stereo" || d.OutputProfile.BitrateBps != 1536000 {
		t.Fatalf("WAV playback lost verified output metadata: %+v", d.OutputProfile)
	}
	retry, err := service.Create(context.Background(), who, request, "create-once")
	if err != nil || retry.AttemptID != d.AttemptID {
		t.Fatal("create retry generated another attempt", err)
	}
	other := who
	other.ID = "different-auth-session"
	if stream, err := service.OpenStream(context.Background(), other, d.SessionID, d.AttemptID, false); err == nil {
		stream.Close()
		t.Fatal("another login session accessed a known stream URL")
	}
	original, _ := os.ReadFile(path)
	if err := os.WriteFile(path, []byte("replacement"), 0600); err != nil {
		t.Fatal(err)
	}
	stream, err := service.OpenStream(context.Background(), who, d.SessionID, d.AttemptID, false)
	if err != nil {
		t.Fatal(err)
	}
	actual, err := io.ReadAll(stream.File)
	if err != nil || !bytes.Equal(actual, original) {
		t.Fatal("cached attempt changed with the original file", err)
	}
	if err := service.Stop(context.Background(), who, d.SessionID); err != nil {
		t.Fatal(err)
	}
	if stream.Context.Err() == nil {
		t.Fatal("stop did not cancel an active stream")
	}
	stream.Close()
	if err := service.Stop(context.Background(), who, d.SessionID); err != nil {
		t.Fatal("repeated stop is not idempotent", err)
	}
}

func TestProcessedHEADAndOldAttemptDoNotBlockNewAttempt(t *testing.T) {
	service, _, who, request, _ := playbackService(t)
	request.Preferences.ForceProcessing = true
	d, err := service.Create(context.Background(), who, request, "processed-create")
	if err != nil {
		t.Fatal(err)
	}
	head, err := service.OpenStream(context.Background(), who, d.SessionID, d.AttemptID, true)
	if err != nil {
		t.Fatal(err)
	}
	defer head.Close()
	old, err := service.OpenStream(context.Background(), who, d.SessionID, d.AttemptID, false)
	if err != nil {
		t.Fatal("HEAD prevented the real audio request", err)
	}
	defer old.Close()
	control := playback.ControlRequest{AttemptID: d.AttemptID, PositionMs: 2000}
	next, err := service.Control(context.Background(), who, d.SessionID, "seek", control, "seek-once")
	if err != nil {
		t.Fatal(err)
	}
	if old.Context.Err() == nil {
		t.Fatal("old attempt was not cancelled")
	}
	current, err := service.OpenStream(context.Background(), who, d.SessionID, next.AttemptID, false)
	if err != nil {
		t.Fatal("old attempt cleanup blocked the new attempt", err)
	}
	defer current.Close()
	retry, err := service.Control(context.Background(), who, d.SessionID, "seek", control, "seek-once")
	if err != nil || retry.AttemptID != next.AttemptID {
		t.Fatal("idempotent seek was checked as a stale attempt", err)
	}
	control.PositionMs = 3000
	if _, err := service.Control(context.Background(), who, d.SessionID, "seek", control, "seek-once"); err == nil {
		t.Fatal("same key with changed parameters was accepted")
	}
}

func TestPendingCreateDoesNotBlockStopAndIsRevocable(t *testing.T) {
	service, repo, who, request, _ := playbackService(t)
	d, err := service.Create(context.Background(), who, request, "initial-create")
	if err != nil {
		t.Fatal(err)
	}
	repo.gate.Store(true)
	created := make(chan error, 1)
	go func() { _, err := service.Create(context.Background(), who, request, "pending-create"); created <- err }()
	<-repo.entered
	stopped := make(chan error, 1)
	go func() { stopped <- service.Stop(context.Background(), who, d.SessionID) }()
	select {
	case err := <-stopped:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		close(repo.proceed)
		t.Fatal("pending create blocked unrelated stop")
	}
	service.Revoke(who.ID)
	select {
	case err := <-created:
		if err == nil {
			t.Fatal("revoked authentication still published a pending create")
		}
	case <-time.After(time.Second):
		close(repo.proceed)
		t.Fatal("revocation did not cancel pending create")
	}
}

func TestQueuedCreateChecksRevocationAfterAcquiringCreationSlot(t *testing.T) {
	service, repo, who, request, _ := playbackService(t)
	repo.gate.Store(true)
	first := make(chan error, 1)
	second := make(chan error, 1)
	go func() { _, err := service.Create(context.Background(), who, request, "first-create"); first <- err }()
	<-repo.entered
	queued := make(chan struct{})
	go func() {
		close(queued)
		_, err := service.Create(context.Background(), who, request, "second-create")
		second <- err
	}()
	<-queued
	if err := repo.DeleteAuth(context.Background(), who.ID); err != nil {
		t.Fatal(err)
	}
	service.Revoke(who.ID)
	for _, result := range []chan error{first, second} {
		select {
		case err := <-result:
			if err == nil {
				t.Fatal("revoked authentication published a queued playback")
			}
		case <-time.After(time.Second):
			close(repo.proceed)
			t.Fatal("revoked creation queue did not drain")
		}
	}
}
