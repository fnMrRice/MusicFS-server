package playback_test

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/plugins/decoder/ffmpeg"
	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/plugincontrol"
)

func TestPlaybackPluginLeaseSurvivesRequestAndDrainsUntilStop(t *testing.T) {
	gate := plugincontrol.NewGate(true)
	processor := &audioffmpeg.Processor{Capabilities: audioffmpeg.Capabilities{FFmpegSHA256: strings.Repeat("a", 64)}}
	processor.SetGate(gate)
	f := recoverySetup(t, processor, 0)
	ctx, cancel := context.WithCancel(context.Background())
	d, err := f.player.Create(ctx, f.who, f.request, "admitted-playback")
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	if gate.Active() != 1 {
		t.Fatal("session did not retain processing lease", gate.Active())
	}
	gate.SetAccepting(false, false)
	if _, err = f.player.Create(context.Background(), f.who, f.request, "disabled-new-playback"); err == nil {
		t.Fatal("disabled processor accepted new session")
	}
	stream, err := f.player.OpenStream(context.Background(), f.who, d.SessionID, d.AttemptID, false)
	if err != nil {
		t.Fatal("graceful drain interrupted admitted session", err)
	}
	lease, err := processor.AcquireLease(stream.Context)
	if err != nil {
		t.Fatal("admitted stream lost nested processing permission", err)
	}
	lease.Close()
	stream.Close()
	if err = f.player.Stop(context.Background(), f.who, d.SessionID); err != nil {
		t.Fatal(err)
	}
	if gate.Active() != 0 {
		t.Fatal("stopped playback retained processing lease", gate.Active())
	}
}

func TestPlaybackPluginForceCancelsStreamAndReleasesAfterCleanup(t *testing.T) {
	gate := plugincontrol.NewGate(true)
	processor := &audioffmpeg.Processor{Capabilities: audioffmpeg.Capabilities{FFmpegSHA256: strings.Repeat("a", 64)}}
	processor.SetGate(gate)
	f := recoverySetup(t, processor, 0)
	d, err := f.player.Create(context.Background(), f.who, f.request, "force-playback")
	if err != nil {
		t.Fatal(err)
	}
	stream, err := f.player.OpenStream(context.Background(), f.who, d.SessionID, d.AttemptID, false)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	gate.SetAccepting(false, true)
	select {
	case <-stream.Context.Done():
	case <-time.After(time.Second):
		t.Fatal("forced stop did not cancel stream")
	}
	deadline := time.Now().Add(3 * time.Second)
	for gate.Active() != 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if gate.Active() != 0 {
		t.Fatal("forced session did not release processing lease")
	}
	if _, err = f.player.OpenStream(context.Background(), f.who, d.SessionID, d.AttemptID, false); err == nil {
		t.Fatal("forced session remained usable")
	}
}

func TestPlaybackSessionKeepsItsProcessorWhenNewSessionsAreReconfigured(t *testing.T) {
	oldGate := plugincontrol.NewGate(true)
	old := &audioffmpeg.Processor{Capabilities: audioffmpeg.Capabilities{FFmpegSHA256: strings.Repeat("a", 64)}}
	old.SetGate(oldGate)
	f := recoverySetup(t, old, 0)
	d, err := f.player.Create(context.Background(), f.who, f.request, "old-generation")
	if err != nil {
		t.Fatal(err)
	}
	next := &audioffmpeg.Processor{Capabilities: audioffmpeg.Capabilities{FFmpegSHA256: strings.Repeat("b", 64)}}
	next.SetGate(plugincontrol.NewGate(false))
	f.player.UpdateAudio(next, nil)
	oldGate.SetAccepting(false, false)
	stream, err := f.player.OpenStream(context.Background(), f.who, d.SessionID, d.AttemptID, false)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	// The test processor deliberately has no execution slots. Reaching its
	// rate limiter proves stream execution retained the admitted old processor;
	// accidentally using next instead would return PLUGIN_DISABLED first.
	err = stream.Process(io.Discard)
	var failure *domain.Error
	if errors.As(err, &failure) && failure.Code == "PLUGIN_DISABLED" {
		t.Fatal("active session switched to replacement processor")
	}
	if err == nil || !strings.Contains(err.Error(), "RATE_LIMITED") {
		t.Fatal("unexpected synthetic processor response", err)
	}
}

func TestPlaybackSourcePluginLeaseSurvivesRequestAndForceCancelsProxy(t *testing.T) {
	player, repo, who, request, _ := playbackService(t)
	gate := plugincontrol.NewGate(true)
	if err := repo.library.SetGate("root", gate); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	d, err := player.Create(ctx, who, request, "proxy-source-lease")
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	if gate.Active() != 1 {
		t.Fatal("proxy did not retain source plugin lease", gate.Active())
	}
	gate.SetAccepting(false, false)
	if _, err = player.Create(context.Background(), who, request, "proxy-disabled-source"); err == nil {
		t.Fatal("disabled source created new playback")
	}
	stream, err := player.OpenStream(context.Background(), who, d.SessionID, d.AttemptID, false)
	if err != nil {
		t.Fatal("source drain interrupted proxy playback", err)
	}
	defer stream.Close()
	gate.SetAccepting(false, true)
	select {
	case <-stream.Context.Done():
	case <-time.After(time.Second):
		t.Fatal("forced source stop did not cancel proxy playback")
	}
	if err = player.Stop(context.Background(), who, d.SessionID); err != nil {
		t.Fatal(err)
	}
	if gate.Active() != 0 {
		t.Fatal("proxy stop retained source plugin lease", gate.Active())
	}
}
