package library_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/jobs"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/pluginconfig"
	"github.com/fnMrRice/MusicFS-server/src/pluginhost"
	"github.com/fnMrRice/MusicFS-server/src/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/sourceaccess"
	"github.com/fnMrRice/MusicFS-server/src/storage/sqlite"
)

type scanFailureCase struct {
	name, code, status string
	retry              bool
	ms                 int64
	seconds            *int
}

func retrySeconds(value int) *int { return &value }
func scanFailureCases() []scanFailureCase {
	return []scanFailureCase{
		{"explicit-transient", "TEMPORARY_FAILURE", "retry_wait", true, 1501, retrySeconds(2)},
		{"unmarked-transient", "TEMPORARY_FAILURE", "failed", false, 1000, nil},
		{"credentials", "AUTH_REQUIRED", "paused", true, 1000, retrySeconds(1)},
		{"changed-resource", "RESOURCE_CHANGED", "failed", true, 1000, retrySeconds(1)},
		{"excessive-wait", "RATE_LIMITED", "paused", true, 300001, nil},
		{"negative-wait", "RATE_LIMITED", "paused", true, -1, nil},
		{"transport-close", "TEMPORARY_FAILURE", "failed", false, 0, nil},
	}
}

// This helper is an actual child process using the same public framing as a
// Provider. It never calls library internals or writes to the authorized root.
func TestScanFailureWorkerProcess(t *testing.T) {
	index := -1
	for _, arg := range os.Args {
		if strings.HasPrefix(arg, "musicfs-scan-failure=") {
			var err error
			index, err = strconv.Atoi(strings.TrimPrefix(arg, "musicfs-scan-failure="))
			if err != nil {
				os.Exit(2)
			}
		}
	}
	if index < 0 {
		return
	}
	cases := scanFailureCases()
	if index >= len(cases) {
		os.Exit(2)
	}
	test := cases[index]
	framer := pluginproto.NewFramer(os.Stdin, os.Stdout)
	request, err := framer.Read()
	if err != nil || request.Method != "host.hello" {
		os.Exit(3)
	}
	var hello pluginproto.Hello
	if json.Unmarshal(request.Params, &hello) != nil {
		os.Exit(4)
	}
	ready := pluginproto.Ready{InstanceID: hello.InstanceID, Nonce: hello.Nonce, Manifest: pluginproto.Manifest{
		PluginID: "musicfs.localfiles", PluginVersion: "retry-test-v1", ProtocolVersion: pluginproto.ProtocolVersion,
		Roles: []string{"provider"}, Capabilities: []string{"library.scan"}, MaxMessageBytes: pluginproto.MaxMessageBytes, MaxPageItems: pluginproto.MaxPageItems, Configuration: pluginconfig.LocalFiles(),
	}}
	body, err := json.Marshal(ready)
	if err != nil || framer.Write(pluginproto.Envelope{ID: request.ID, Result: body}) != nil {
		os.Exit(5)
	}
	for scans := 0; ; {
		request, err = framer.Read()
		if err != nil {
			os.Exit(0)
		}
		if request.Method != "provider.scan" {
			os.Exit(6)
		}
		var scan pluginproto.ScanRequest
		if json.Unmarshal(request.Params, &scan) != nil {
			os.Exit(7)
		}
		scans++
		if scans == 1 {
			page := pluginproto.ScanPage{ScanID: scan.ScanID, Generation: scan.Generation, PlanDigest: scan.PlanDigest, Page: 0, Complete: true, RootIdentity: scan.Scope.RootIdentity, Issues: []string{}, Items: []pluginproto.Observation{{ExternalID: "stable-track", RootID: scan.RootID, RelativePath: "existing.wav", ObservedVersion: "fixture-v1", DisplayName: "Existing track", MIME: "audio/wav", SizeBytes: 0}}}
			body, err = json.Marshal(page)
			if err != nil || framer.Write(pluginproto.Envelope{ID: request.ID, Result: body}) != nil {
				os.Exit(8)
			}
			continue
		}
		if test.name == "transport-close" {
			os.Exit(23)
		}
		failure := &pluginproto.RPCError{Code: -32000, Message: "private path and credential", Data: pluginproto.ErrorData{Code: test.code, Retryable: test.retry, RetryAfterMs: &test.ms}}
		if framer.Write(pluginproto.Envelope{ID: request.ID, Error: failure}) != nil {
			os.Exit(9)
		}
	}
}

func TestLibraryScanFailurePreservesOnlyExplicitSafeRetryMetadata(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	binary, err := os.Open(executable)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.New()
	_, readErr := io.Copy(hash, binary)
	closeErr := binary.Close()
	if readErr != nil || closeErr != nil {
		t.Fatal(readErr, closeErr)
	}
	digest := hex.EncodeToString(hash.Sum(nil))
	for index, test := range scanFailureCases() {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			store, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "scan.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			root := t.TempDir()
			if err = os.WriteFile(filepath.Join(root, "existing.wav"), nil, 0600); err != nil {
				t.Fatal(err)
			}
			worker, err := pluginhost.Start(ctx, pluginhost.Config{Executable: executable, Args: []string{"-test.run=^TestScanFailureWorkerProcess$", "--", "musicfs-scan-failure=" + strconv.Itoa(index)}, SHA256: digest, InstanceID: "local", UserID: "admin", StateDir: t.TempDir(), Roots: []sourceaccess.RootConfig{{ID: "music", Path: root}}})
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := worker.Close(); err != nil {
					t.Error("worker cleanup failed", err)
				}
			}()
			declaration := worker.Manifest.Configuration
			queue := jobs.New(store)
			lib := library.New(store, queue, []library.Runtime{{ID: "local", Name: "Local", Owner: "admin", Worker: worker, ConfigRevision: "retry-test", Configuration: declaration, ImportDefaults: declaration.ImportDefaults, RootNames: map[string]string{"music": "Music"}}}, nil)
			if err = queue.Start(ctx); err != nil {
				t.Fatal(err)
			}
			defer queue.Close()
			input := library.ScanInput{Mode: "full", SchemaRevision: declaration.SchemaRevision, RootIDs: []string{"music"}}
			first, _, err := lib.Scan(ctx, "admin", "local", input, "first-success")
			if err != nil {
				t.Fatal(err)
			}
			first = waitScanResult(t, ctx, lib, first.ID)
			if first.Status != "succeeded" {
				t.Fatal("initial scan did not publish", first)
			}
			before, err := lib.List(ctx, "admin", "local", "", 20)
			if err != nil || len(before.Items) != 1 {
				t.Fatal("initial library missing", err)
			}
			second, _, err := lib.Scan(ctx, "admin", "local", input, "failure-request")
			if err != nil {
				t.Fatal(err)
			}
			result := waitScanResult(t, ctx, lib, second.ID)
			if result.Status != test.status || result.Error == nil || result.Error.Code != test.code || strings.Contains(result.Error.Message, "private") || result.AttemptCount != 1 || result.Complete {
				t.Fatalf("unsafe public scan result: %+v", result)
			}
			if (result.Error.RetryAfter == nil) != (test.seconds == nil) || (test.seconds != nil && *result.Error.RetryAfter != *test.seconds) {
				t.Fatal("retry wait conversion changed", result.Error.RetryAfter, test.seconds)
			}
			if test.status == "retry_wait" {
				if result.NextAttemptAt == nil || result.NextAttemptAt.Sub(result.UpdatedAt) < 2*time.Second || result.NextAttemptAt.Sub(result.UpdatedAt) > 2500*time.Millisecond {
					t.Fatal("worker retry deadline was not persisted")
				}
			} else if result.NextAttemptAt != nil {
				t.Fatal("non-retry failure was scheduled")
			}
			after, err := lib.List(ctx, "admin", "local", "", 20)
			if err != nil || len(after.Items) != 1 || after.Items[0].ID != before.Items[0].ID || after.LibraryRevision != before.LibraryRevision || result.LastSuccessAt == nil {
				t.Fatal("failed scan discarded previous library", err)
			}
		})
	}
}

func waitScanResult(t *testing.T, ctx context.Context, lib *library.Service, id string) jobs.Job {
	t.Helper()
	for {
		j, err := lib.Job(ctx, "admin", id)
		if err != nil {
			t.Fatal(err)
		}
		if j.Status != "queued" && j.Status != "running" {
			return j
		}
		select {
		case <-ctx.Done():
			t.Fatal("scan result timed out", ctx.Err())
		case <-time.After(10 * time.Millisecond):
		}
	}
}
