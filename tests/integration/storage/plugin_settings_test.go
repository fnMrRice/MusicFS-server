package storage_test

import (
	"github.com/fnMrRice/MusicFS-server/sdk/pluginsettings"
	"github.com/fnMrRice/MusicFS-server/src/metadata/matching"
	"github.com/fnMrRice/MusicFS-server/src/plugins/settings"
	"github.com/fnMrRice/MusicFS-server/src/storage"
	"testing"
)

func TestIndependentPluginSettingsDeclarations(t *testing.T) {
	s := evidenceStore(t)
	provider := settings.Provider{ID: "example.settings", Schema: pluginsettings.Schema{Revision: "v1", Title: "例子", Fields: []pluginsettings.Field{
		{Key: "caption", Type: "text", DefaultValue: "默认标题"},
		{Key: "choice", Type: "select", DefaultValue: "one", Options: []pluginsettings.Option{{Value: "one", Label: "选项一"}, {Value: "two", Label: "选项二"}}},
	}, Actions: []pluginsettings.Action{{ID: "test", Label: "按钮"}}}}
	service := settings.New(s, provider)
	page, err := service.Read(t.Context(), "admin", provider.ID)
	if err != nil || page.Job.Status != "idle" {
		t.Fatal("settings without a background job failed", err)
	}
	revision := int64(0)
	page, err = service.Update(t.Context(), "admin", provider.ID, settings.Request{ExpectedRevision: &revision, SchemaRevision: "v1", Values: settings.Values{"caption": "<script>plain text</script>", "choice": "two"}})
	if err != nil || page.Values["caption"] != "<script>plain text</script>" || page.Values["choice"] != "two" {
		t.Fatal("plugin-declared controls not persisted", err)
	}
	revision = 1
	_, err = service.Update(t.Context(), "admin", provider.ID, settings.Request{ExpectedRevision: &revision, SchemaRevision: "v1", Values: settings.Values{"choice": "unlisted"}})
	matchingError(t, err, "INVALID_ARGUMENT")
	_, err = service.Execute(t.Context(), "admin", provider.ID, "test", "generic-action")
	matchingError(t, err, "UNSUPPORTED")
}

func TestPersistenceWorkerPluginSettingsAndMatchingJobs(t *testing.T) {
	options := storageWorker(t)
	options.DataRoot = t.TempDir()
	s, err := storage.Open(t.Context(), "plugin", options)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	stored, err := s.PutPluginSettings(t.Context(), "admin", "example.settings", 0, settings.Values{"automatic": true, "threshold": float64(35), "text": "示例"})
	if err != nil || stored.Revision != 1 {
		t.Fatal("remote settings save failed", err)
	}
	read, err := s.GetPluginSettings(t.Context(), "admin", "example.settings")
	if err != nil || read.Values["threshold"] != float64(35) || read.Values["automatic"] != true {
		t.Fatal("remote settings types changed", err)
	}
	id := "matching-rpc-job"
	job := settings.Job{ID: &id, Status: "queued"}
	if err = s.SaveMatchingRun(t.Context(), matching.Run{Owner: "admin", Key: "rpc-action-key", Job: job}); err != nil {
		t.Fatal(err)
	}
	run, err := s.MatchingRun(t.Context(), "admin")
	if err != nil || run.Job.ID == nil || *run.Job.ID != id {
		t.Fatal("remote task lost", err)
	}
	receipt, err := s.MatchingAction(t.Context(), "admin", "rpc-action-key")
	if err != nil || receipt == nil || receipt.ID == nil || *receipt.ID != id {
		t.Fatal("remote action receipt lost", err)
	}
	private, err := s.MatchingAction(t.Context(), "other", "rpc-action-key")
	if err != nil || private != nil {
		t.Fatal("remote action crossed user boundary", err)
	}
	library, err := s.MatchingLibrary(t.Context(), "admin")
	if err != nil || library.Revision == "" || len(library.Sources) != 0 {
		t.Fatal("remote matching snapshot failed", err)
	}
	grouped, err := s.ListGroupedTracks(t.Context(), "admin", "", "", 20)
	if err != nil || grouped.MatchingRevision == nil || *grouped.MatchingRevision != 0 {
		t.Fatal("remote grouped snapshot failed", err)
	}
}
