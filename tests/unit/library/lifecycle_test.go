package library_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/jobs"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/plugincontrol"
	"github.com/fnMrRice/MusicFS-server/src/pluginhost"
	"github.com/fnMrRice/MusicFS-server/src/sourceaccess"
)

func controlledLibrary() (*library.Service, *plugincontrol.Gate) {
	gate := plugincontrol.NewGate(true)
	return library.New(nil, jobs.New(nil), []library.Runtime{{ID: "local", Owner: "admin", Name: "Local", Gate: gate}}, nil), gate
}
func assertLibraryCode(t *testing.T, err error, code string) {
	t.Helper()
	var de *domain.Error
	if !errors.As(err, &de) || de.Code != code {
		t.Fatalf("expected %s, got %v", code, err)
	}
}

func TestLibraryDrainKeepsSessionLifetimeAndRejectsNewWork(t *testing.T) {
	service, gate := controlledLibrary()
	ctx := context.Background()
	request, cancelRequest := context.WithCancel(ctx)
	session, err := service.HoldReadLifetime(request, ctx, "admin", "local")
	if err != nil {
		t.Fatal(err)
	}
	cancelRequest()
	if session.Context().Err() != nil {
		t.Fatal("session incorrectly inherited completed request cancellation")
	}
	gate.SetAccepting(false, false)
	_, err = service.HoldRead(ctx, "admin", "local")
	assertLibraryCode(t, err, "PLUGIN_DISABLED")
	// A seek uses a new request and lifetime, but must inherit the old grant.
	continuation, err := service.HoldReadLifetime(session.WithContext(ctx), ctx, "admin", "local")
	if err != nil {
		t.Fatal("existing session continuation denied", err)
	}
	if gate.Active() != 2 {
		t.Fatal("missing admitted leases", gate.Active())
	}
	gate.SetAccepting(false, true)
	if session.Context().Err() == nil || continuation.Context().Err() == nil {
		t.Fatal("force did not cancel old operations")
	}
	if gate.Active() != 2 {
		t.Fatal("force cancellation released resources before cleanup")
	}
	wait, cancel := context.WithTimeout(ctx, 10*time.Millisecond)
	defer cancel()
	if err = service.WaitIdle(wait, "admin", "local"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("reported idle before cleanup", err)
	}
	continuation.Close()
	session.Close()
	session.Close()
	if err = service.WaitIdle(ctx, "admin", "local"); err != nil || gate.Active() != 0 {
		t.Fatal("cleanup left leases", err, gate.Active())
	}
}

func TestLibraryWriterWaitCancellationAndRecoveryBarrier(t *testing.T) {
	service, gate := controlledLibrary()
	ctx := context.Background()
	writer, err := service.TryExclusive(ctx, "admin", "local", "writeback")
	if err != nil {
		t.Fatal(err)
	}
	request, cancel := context.WithCancel(ctx)
	cancel()
	_, err = service.HoldReadLifetime(request, ctx, "admin", "local")
	if !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled wait succeeded", err)
	}
	if gate.Active() != 1 {
		t.Fatal("cancelled wait leaked admission", gate.Active())
	}
	if err = writer.Block(); err != nil {
		t.Fatal(err)
	}
	gate.SetAccepting(false, true)
	if err = writer.Check(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal("forced writer not cancelled", err)
	}
	writer.Close()
	if err = service.WaitIdle(ctx, "admin", "local"); err != nil {
		t.Fatal("persistent barrier blocked worker stop", err)
	}
	if !service.WritebackPending("admin", "local", "writeback") {
		t.Fatal("stop removed durable recovery barrier")
	}
	gate.SetAccepting(true, false)
	_, err = service.HoldRead(ctx, "admin", "local")
	assertLibraryCode(t, err, "CONFLICT")
	recovery, err := service.TryExclusive(ctx, "admin", "local", "writeback")
	if err != nil {
		t.Fatal(err)
	}
	recovery.ClearBarrier()
	recovery.Close()
	read, err := service.HoldRead(ctx, "admin", "local")
	if err != nil {
		t.Fatal(err)
	}
	read.Close()
}

func TestLibraryConcurrentRuntimeAndProbeReplacement(t *testing.T) {
	service, _ := controlledLibrary()
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				r, ok := service.Runtime("local")
				if !ok {
					t.Error("configured identity lost")
					return
				}
				r.Name = "Updated"
				if err := service.UpdateRuntime(r); err != nil {
					t.Error(err)
				}
				service.SetProbe(nil)
				_ = service.Runtimes()
			}
		}()
	}
	wg.Wait()
	if err := service.UpdateRuntime(library.Runtime{ID: "other", Owner: "admin"}); err == nil {
		t.Fatal("unconfigured runtime added")
	}
	if err := service.UpdateRuntime(library.Runtime{ID: "local", Owner: "other"}); err == nil {
		t.Fatal("runtime ownership changed")
	}
}

func TestLibraryAdmittedTaskContinuesDuringDrainAndForceClosesSource(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "tone.wav"), []byte("audio"), 0600); err != nil {
		t.Fatal(err)
	}
	access, err := sourceaccess.New([]sourceaccess.RootConfig{{ID: "root", Path: root}})
	if err != nil {
		t.Fatal(err)
	}
	entry, err := access.Inspect("root", "tone.wav")
	if err != nil {
		t.Fatal(err)
	}
	gate := plugincontrol.NewGate(true)
	service := library.New(nil, jobs.New(nil), []library.Runtime{{ID: "local", Owner: "admin", Gate: gate, Worker: &pluginhost.Worker{Access: access}}}, nil)
	track := library.Track{ID: "track", Owner: "admin", ProviderID: "local", RootID: "root", Path: "tone.wav", Version: entry.ObservedVersion, AssetID: "asset"}
	first, err := service.Open(context.Background(), track, "existing-task")
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	gate.SetAccepting(false, false)
	second, err := service.Open(context.Background(), track, "existing-task")
	if err != nil {
		t.Fatal("admitted task rejected", err)
	}
	defer second.Close()
	_, err = service.Open(context.Background(), track, "new-task")
	assertLibraryCode(t, err, "PLUGIN_DISABLED")
	gate.SetAccepting(false, true)
	deadline := time.After(time.Second)
	for {
		if _, err = first.Stat(); err != nil {
			break
		}
		select {
		case <-deadline:
			t.Fatal("force left source handle open")
		case <-time.After(time.Millisecond):
		}
	}
	if gate.Active() != 1 {
		t.Fatal("source cleanup was not accounted", gate.Active())
	}
	service.Revoke("local", "existing-task")
	if err = service.WaitIdle(context.Background(), "admin", "local"); err != nil || gate.Active() != 0 {
		t.Fatal(err, gate.Active())
	}
}
