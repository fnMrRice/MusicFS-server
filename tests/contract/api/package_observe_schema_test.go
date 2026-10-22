package api_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestPackageObservationProtocolContract(t *testing.T) {
	entry := pluginproto.Entry{RootID: "music", RelativePath: "RJ123/音轨/01.wav", FileIdentity: "file-identity", LinkCount: 1, ObservedVersion: "version", SizeBytes: 48, MIME: "audio/wav"}
	request := pluginproto.PackageObserveRequest{Entries: []pluginproto.Entry{entry}}
	testkit.ValidatePlugin(t, "PackageObserveRequest", request)
	testkit.ValidatePlugin(t, "Request", map[string]any{"jsonrpc": "2.0", "id": "h-package", "method": "provider.observePackage", "params": request})
	externalID := strings.Repeat("a", 32)
	result := pluginproto.PackageObserveResult{Items: []pluginproto.Observation{{ExternalID: externalID, RootID: entry.RootID, RelativePath: entry.RelativePath, SourceRef: externalID, ObservedVersion: entry.ObservedVersion, DisplayName: "01.wav", SizeBytes: entry.SizeBytes, MIME: entry.MIME}}}
	testkit.ValidatePlugin(t, "PackageObserveResult", result)
	testkit.ValidatePlugin(t, "Success", map[string]any{"jsonrpc": "2.0", "id": "h-package", "result": result})
	for name, value := range map[string]any{
		"empty":    pluginproto.PackageObserveRequest{Entries: []pluginproto.Entry{}},
		"too-many": pluginproto.PackageObserveRequest{Entries: make([]pluginproto.Entry, 201)},
		"unknown":  map[string]any{"entries": request.Entries, "scanId": "not-a-scan"},
	} {
		t.Run(name, func(t *testing.T) {
			raw, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			if testkit.ContractSchema(t, "plugin", "PackageObserveRequest").Validate(testkit.DecodeJSON(t, raw)) == nil {
				t.Fatal("accepted invalid package request")
			}
		})
	}
	for _, path := range []string{".", "../escape.wav", "/absolute.wav", `a\b.wav`, "a:stream.wav"} {
		entry.RelativePath = path
		raw, err := json.Marshal(pluginproto.PackageObserveRequest{Entries: []pluginproto.Entry{entry}})
		if err != nil {
			t.Fatal(err)
		}
		if testkit.ContractSchema(t, "plugin", "PackageObserveRequest").Validate(testkit.DecodeJSON(t, raw)) == nil {
			t.Fatalf("accepted invalid path %q", path)
		}
	}
}
