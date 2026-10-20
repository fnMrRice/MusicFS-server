package storage_test

import (
	"context"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/jobs"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/pluginconfig"
	"github.com/fnMrRice/MusicFS-server/src/pluginhost"
	"github.com/fnMrRice/MusicFS-server/src/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/sourceaccess"
)

// A declaration-only worker is intentional: revision validation must terminate
// the recovered task before attempting any plugin RPC or reading source bytes.
func TestQueuedScanRejectsChangedRuntimeBeforeOpeningWorker(t *testing.T) {
	for name, change := range map[string]func(*library.Runtime, *pluginproto.ScanPlan){
		"config":        func(r *library.Runtime, _ *pluginproto.ScanPlan) { r.ConfigRevision = "new-config" },
		"plugin":        func(r *library.Runtime, _ *pluginproto.ScanPlan) { r.Worker.Manifest.PluginVersion = "new-plugin" },
		"schema":        func(r *library.Runtime, _ *pluginproto.ScanPlan) { r.Configuration.SchemaRevision = "new-schema" },
		"root-identity": func(_ *library.Runtime, p *pluginproto.ScanPlan) { p.Roots[0].RootIdentity = "old-root-identity" },
	} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			s := evidenceStore(t)
			access, err := sourceaccess.New([]sourceaccess.RootConfig{{ID: "music", Path: t.TempDir()}})
			if err != nil {
				t.Fatal(err)
			}
			declaration := pluginconfig.LocalFiles()
			r := library.Runtime{ID: "local", Name: "Local", Owner: "admin", ConfigRevision: "config-v1", Configuration: declaration, ImportDefaults: declaration.ImportDefaults, RootNames: map[string]string{"music": "Music"}, Worker: &pluginhost.Worker{Access: access, Manifest: pluginproto.Manifest{PluginID: "musicfs.plugin.source.localfiles", PluginVersion: "0.2.0"}}}
			queue := jobs.New(s)
			planner := library.New(s, queue, []library.Runtime{r}, nil)
			plan, err := planner.BuildScanPlan(ctx, "admin", "local", library.ScanInput{Mode: "full", SchemaRevision: declaration.SchemaRevision, RootIDs: []string{"music"}})
			if err != nil {
				t.Fatal(err)
			}
			change(&r, &plan)
			job, _, err := s.Enqueue(ctx, "admin", "local", plan, "queued-version")
			if err != nil {
				t.Fatal(err)
			}
			library.New(s, queue, []library.Runtime{r}, nil)
			if err = queue.Start(ctx); err != nil {
				t.Fatal(err)
			}
			defer queue.Close()
			deadline := time.Now().Add(3 * time.Second)
			for time.Now().Before(deadline) {
				current, err := s.GetJob(ctx, "admin", job.ID)
				if err != nil {
					t.Fatal(err)
				}
				if current.Status == "failed" {
					if current.Error == nil || current.Error.Code != "RESOURCE_CHANGED" || current.Progress.Observed != 0 {
						t.Fatalf("wrong rejection: %+v", current)
					}
					page, err := s.ListTracks(ctx, "admin", "local", "", 50)
					if err != nil || page.LibraryRevision != "0" || len(page.Items) != 0 {
						t.Fatal("obsolete task changed library", err)
					}
					return
				}
				time.Sleep(10 * time.Millisecond)
			}
			t.Fatal("obsolete task did not fail before RPC")
		})
	}
}
