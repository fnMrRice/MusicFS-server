package directoryidentity_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/directoryidentity"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
)

func TestRebindPlanRejectsJournalCorruptionAndKeepsOpaqueReference(t *testing.T) {
	r := directoryidentity.Rebind{JobID: strings.Repeat("a", 48), RootID: "music", RootIdentity: "root-identity", RelativePath: "song.wav", ExternalID: "independent-track", OldIdentity: "old", OldVersion: "v1", NewIdentity: "new", NewVersion: "v2"}
	stored := map[string]pluginproto.StateValue{}
	put := func(key string, value any) {
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		stored[key] = pluginproto.StateValue{Revision: 1, Value: raw}
	}
	put(directoryidentity.AliasKey(r.RootID, r.OldIdentity), directoryidentity.Aliases{Paths: map[string]string{r.RelativePath: r.ExternalID}})
	put(directoryidentity.TrackKey(r.ExternalID), pluginproto.Observation{ExternalID: r.ExternalID, SourceRef: "opaque:asset/7", RootID: r.RootID, RelativePath: r.RelativePath, ObservedVersion: r.OldVersion, SizeBytes: 10, MIME: "audio/wav"})
	read := func(key string) (pluginproto.StateValue, error) { return stored[key], nil }
	observation, changes, err := directoryidentity.Prepare(read, r, pluginproto.Entry{SizeBytes: 20, MIME: "audio/wav"})
	if err != nil || observation.SourceRef != "opaque:asset/7" {
		t.Fatal(observation, err)
	}
	if err := directoryidentity.ValidatePlan(r, observation, changes); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func([]directoryidentity.Change){
		func(c []directoryidentity.Change) { c[0].Key = "private-config" },
		func(c []directoryidentity.Change) { c[0].After.Revision++ },
		func(c []directoryidentity.Change) { c[1].After.Value = json.RawMessage(`{"externalId":"foreign"}`) },
		func(c []directoryidentity.Change) { c[0], c[2] = c[2], c[0] },
	} {
		invalid := append([]directoryidentity.Change(nil), changes...)
		mutate(invalid)
		if directoryidentity.ValidatePlan(r, observation, invalid) == nil {
			t.Fatal("corrupt rebind journal accepted")
		}
	}
	for _, change := range changes {
		stored[change.Key] = change.After
	}
	if err := directoryidentity.CheckCompleted(read, r); err != nil {
		t.Fatal(err)
	}
}
