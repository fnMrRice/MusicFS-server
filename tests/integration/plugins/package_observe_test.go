package plugins_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/library/jobs"
	"github.com/fnMrRice/MusicFS-server/src/plugins/plugincontrol"
	"github.com/fnMrRice/MusicFS-server/src/plugins/pluginhost"
	"github.com/fnMrRice/MusicFS-server/src/plugins/sourceaccess"
)

const packageProvider = "4bac928d-f959-5df3-9077-25730ec700e1"

func packageLibrary(t *testing.T, probe library.Probe) (*library.Service, *pluginhost.Worker, string) {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "RJ123", "音轨"), 0700); err != nil {
		t.Fatal(err)
	}
	wave(t, filepath.Join(root, "RJ123", "音轨", "01.wav"))
	if err := os.WriteFile(filepath.Join(root, "RJ123", "说明.txt"), []byte("package notes"), 0600); err != nil {
		t.Fatal(err)
	}
	w, err := pluginhost.Start(context.Background(), pluginhost.Config{Builtin: true, InstanceID: packageProvider, UserID: "owner", Roots: []sourceaccess.RootConfig{{ID: "music", Path: root}}, StateDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { w.Close() })
	s := library.New(nil, jobs.New(nil), []library.Runtime{{ID: packageProvider, Owner: "owner", Worker: w, ConfigRevision: "revision-1", ImportDefaults: pluginproto.ImportOptions{Recursive: false}}}, probe)
	return s, w, root
}

func packageBridgeCode(t *testing.T, err error, code string) {
	t.Helper()
	var failure *domain.Error
	if !errors.As(err, &failure) || failure.Code != code {
		t.Fatalf("expected %s, got %v", code, err)
	}
}

func TestPackageObservationReusesScanIdentityAndPlaybackSource(t *testing.T) {
	probes := 0
	s, w, _ := packageLibrary(t, func(ctx context.Context, input io.ReadSeeker, size int64) (library.Observation, error) {
		probes++
		header := make([]byte, 4)
		if _, err := io.ReadFull(input, header); err != nil || string(header) != "RIFF" {
			t.Fatalf("probe did not receive authorized audio: %q %v", header, err)
		}
		title, codec := "Detected title", "pcm_s16le"
		return library.Observation{Observation: pluginproto.Observation{MIME: "audio/wav", Codec: &codec}, Title: &title, Artists: []string{"Voice"}}, nil
	})
	ctx := context.Background()
	original := scanAll(t, w, pluginproto.ScanRequest{RootID: "music", ScanID: "before-package", Generation: 1, Limit: 200})
	if len(original) != 1 {
		t.Fatalf("unexpected ordinary scan: %+v", original)
	}
	snapshot, err := s.PackageSourceSnapshot(ctx, "owner", packageProvider, "music", "RJ123")
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.ConfigRevision != "revision-1" || snapshot.ProviderID != packageProvider || len(snapshot.Entries) != 4 {
		t.Fatalf("incomplete package snapshot: %+v", snapshot)
	}
	items, err := s.PackageSourceObservations(ctx, "owner", snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ExternalID != original[0].ExternalID || items[0].ObservedVersion != original[0].ObservedVersion || items[0].Title == nil || *items[0].Title != "Detected title" || items[0].ProbeStatus != "succeeded" || probes != 1 {
		t.Fatalf("package observation diverged: %+v probes=%d", items, probes)
	}
	resource, err := w.Resolve(ctx, pluginproto.ResolveRequest{ExternalID: items[0].ExternalID, ExpectedVersion: items[0].ObservedVersion})
	if err != nil {
		t.Fatal(err)
	}
	h, err := w.Access.Open(ctx, sourceaccess.Scope{UserID: "owner", InstanceID: packageProvider, TaskID: "provider-resolve"}, resource.ResourceRef)
	if err != nil {
		t.Fatal("package track did not resolve through ordinary playback", err)
	}
	h.Close()
	after := scanAll(t, w, pluginproto.ScanRequest{RootID: "music", ScanID: "after-package", Generation: 2, Limit: 200})
	if len(after) != 1 || after[0].ExternalID != original[0].ExternalID {
		t.Fatal("ordinary scan lost package identity")
	}
}

func TestPackageBridgeRejectsOtherOwnerTamperingAndChangedSources(t *testing.T) {
	s, _, root := packageLibrary(t, nil)
	ctx := context.Background()
	_, err := s.PackageSourceSnapshot(ctx, "another-owner", packageProvider, "music", "RJ123")
	packageBridgeCode(t, err, "NOT_FOUND")
	_, err = s.PackageSourceSnapshot(ctx, "owner", packageProvider, "music", "../escape")
	packageBridgeCode(t, err, "INVALID_ARGUMENT")
	if strings.Contains(err.Error(), root) {
		t.Fatal("error leaked physical root")
	}
	snapshot, err := s.PackageSourceSnapshot(ctx, "owner", packageProvider, "music", "RJ123")
	if err != nil {
		t.Fatal(err)
	}
	tampered := snapshot
	tampered.Entries = append([]library.PackageEntry(nil), snapshot.Entries...)
	tampered.Entries[0].Name = "altered"
	_, err = s.PackageSourceObservations(ctx, "owner", tampered)
	packageBridgeCode(t, err, "RESOURCE_CHANGED")
	if err := os.WriteFile(filepath.Join(root, "RJ123", "说明.txt"), []byte("changed notes"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err = s.PackageSourceObservations(ctx, "owner", snapshot)
	packageBridgeCode(t, err, "RESOURCE_CHANGED")
}

func TestPackageBridgeOpenRetainsLeaseAndRejectsOfflineProvider(t *testing.T) {
	s, w, _ := packageLibrary(t, nil)
	ctx := context.Background()
	snapshot, err := s.PackageSourceSnapshot(ctx, "owner", packageProvider, "music", "RJ123")
	if err != nil {
		t.Fatal(err)
	}
	var attachment library.PackageEntry
	for _, entry := range snapshot.Entries {
		if entry.Kind == "attachment" {
			attachment = entry
		}
	}
	h, cleanup, err := s.PackageSourceOpen(ctx, "owner", packageProvider, "music", attachment.SourcePath, attachment.Version)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.TryExclusive(ctx, "owner", packageProvider, "write-operation"); err == nil {
		cleanup()
		t.Fatal("open attachment did not retain read lease")
	}
	body, err := io.ReadAll(io.NewSectionReader(h, 0, attachment.SizeBytes))
	if err != nil || string(body) != "package notes" {
		cleanup()
		t.Fatalf("unexpected attachment: %q %v", body, err)
	}
	cleanup()
	cleanup()
	exclusive, err := s.TryExclusive(ctx, "owner", packageProvider, "write-operation")
	if err != nil {
		t.Fatal("attachment cleanup did not release read lease", err)
	}
	exclusive.Close()
	w.Close()
	_, err = s.PackageSourceSnapshot(ctx, "owner", packageProvider, "music", "RJ123")
	packageBridgeCode(t, err, "SOURCE_OFFLINE")
}

func TestPackageAttachmentStopsWhenProviderLeaseIsCanceled(t *testing.T) {
	s, _, _ := packageLibrary(t, nil)
	gate := plugincontrol.NewGate(true)
	if err := s.SetGate(packageProvider, gate); err != nil {
		t.Fatal(err)
	}
	snapshot, err := s.PackageSourceSnapshot(context.Background(), "owner", packageProvider, "music", "RJ123")
	if err != nil {
		t.Fatal(err)
	}
	var attachment library.PackageEntry
	for _, entry := range snapshot.Entries {
		if entry.Kind == "attachment" {
			attachment = entry
		}
	}
	reader, cleanup, err := s.PackageSourceOpen(context.Background(), "owner", packageProvider, "music", attachment.SourcePath, attachment.Version)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	gate.SetAccepting(false, true)
	deadline := time.Now().Add(time.Second)
	for reader.Validate() == nil && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if _, err := reader.ReadAt(make([]byte, 1), 0); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled provider lease still permits attachment read: %v", err)
	}
	cleanup()
	if gate.Active() != 0 {
		t.Fatal("attachment cleanup retained provider lease")
	}
}

func TestPackageObservationRejectsMutationDuringProbe(t *testing.T) {
	var root string
	s, _, folder := packageLibrary(t, func(context.Context, io.ReadSeeker, int64) (library.Observation, error) {
		if err := os.Mkdir(filepath.Join(root, "RJ123", "new-folder"), 0700); err != nil {
			t.Fatal(err)
		}
		return library.Observation{}, nil
	})
	root = folder
	snapshot, err := s.PackageSourceSnapshot(context.Background(), "owner", packageProvider, "music", "RJ123")
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.PackageSourceObservations(context.Background(), "owner", snapshot)
	packageBridgeCode(t, err, "RESOURCE_CHANGED")
}

func TestPackageObservationBatchesLongEscapedPaths(t *testing.T) {
	s, _, root := packageLibrary(t, nil)
	deep := filepath.Join(root, "RJ123")
	for i := 0; i < 16; i++ {
		deep = filepath.Join(deep, strings.Repeat("&", 95))
	}
	if err := os.MkdirAll(deep, 0700); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 120; i++ {
		wave(t, filepath.Join(deep, fmt.Sprintf("%03d.wav", i)))
	}
	snapshot, err := s.PackageSourceSnapshot(context.Background(), "owner", packageProvider, "music", "RJ123")
	if err != nil {
		t.Fatal(err)
	}
	items, err := s.PackageSourceObservations(context.Background(), "owner", snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 121 {
		t.Fatalf("long paths lost observations: %d", len(items))
	}
}
