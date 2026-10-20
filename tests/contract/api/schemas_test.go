package api_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/pluginconfig"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestAllModelsCompile(t *testing.T) {
	for _, family := range []string{"http", "plugin"} {
		path := "docs/contracts/http.schema.json"
		if family == "plugin" {
			path = "docs/contracts/plugins/v1/protocol.schema.json"
		}
		doc := testkit.ReadJSON(t, path).(map[string]any)
		for name := range doc["$defs"].(map[string]any) {
			t.Run(family+"/"+name, func(t *testing.T) { testkit.ContractSchema(t, family, name) })
		}
	}
}

func TestContractSamples(t *testing.T) {
	for _, family := range []string{"api", "plugins"} {
		files, err := filepath.Glob(filepath.Join(testkit.RepositoryRoot(), "tests/testdata/contracts", family, "*.json"))
		if err != nil || len(files) == 0 {
			t.Fatalf("missing %s contract samples: %v", family, err)
		}
		for _, file := range files {
			var samples []struct {
				Name   string          `json:"name"`
				Schema string          `json:"schema"`
				Valid  bool            `json:"valid"`
				Value  json.RawMessage `json:"value"`
			}
			b, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(b, &samples); err != nil {
				t.Fatal(err)
			}
			for _, sample := range samples {
				t.Run(family+"/"+sample.Name, func(t *testing.T) {
					kind := "http"
					if family == "plugins" {
						kind = "plugin"
					}
					err := testkit.ContractSchema(t, kind, sample.Schema).Validate(testkit.DecodeJSON(t, sample.Value))
					if (err == nil) != sample.Valid {
						t.Fatalf("valid=%v, schema returned %v", sample.Valid, err)
					}
				})
			}
		}
	}
}

func TestPluginDTOsMatchContract(t *testing.T) {
	manifest := pluginproto.Manifest{
		PluginID: "musicfs.plugin.source.localfiles", Plugin: pluginconfig.Identity("musicfs.plugin.source.localfiles"), PluginVersion: "0.1.0", ProtocolVersion: pluginproto.ProtocolVersion,
		Roles: []string{"provider"}, Capabilities: []string{"library.scan", "metadata.read", "stream.resolve"},
		Systems: []string{"windows", "linux"}, Architectures: []string{"amd64"},
		MaxMessageBytes: pluginproto.MaxMessageBytes, MaxPageItems: pluginproto.MaxPageItems,
	}
	testkit.ValidatePlugin(t, "Manifest", manifest)
	testkit.ValidatePlugin(t, "Hello", pluginproto.Hello{ProtocolVersion: "1.0", InstanceID: "b1548e96-3081-5c9c-8376-bf290e369d51", Nonce: "nonce", ConfigRevision: "1"})
	testkit.ValidatePlugin(t, "Ready", pluginproto.Ready{Manifest: manifest, InstanceID: "b1548e96-3081-5c9c-8376-bf290e369d51", Nonce: "nonce"})
	testkit.ValidatePlugin(t, "ScanRequest", pluginproto.ScanRequest{RootID: "root", ScanID: "scan", Generation: 1, Limit: 200, Scope: pluginproto.ScanScope{RootID: "root", RootIdentity: "root-version", Recursive: true, MaxDepth: 64}, PlanDigest: strings.Repeat("a", 64), GrantID: "grant-1"})
	testkit.ValidatePlugin(t, "ScanPage", pluginproto.ScanPage{PlanDigest: strings.Repeat("a", 64), ScanID: "scan", Generation: 1, Page: 0, Items: []pluginproto.Observation{}, Complete: true, RootIdentity: "root-version", Issues: []string{}})
	testkit.ValidatePlugin(t, "ResolveRequest", pluginproto.ResolveRequest{ExternalID: "track", ExpectedVersion: "asset-version"})
	testkit.ValidatePlugin(t, "Resource", pluginproto.Resource{ResourceRef: "resource", RootID: "root", RelativePath: "中文 曲目.wav", ObservedVersion: "version", SizeBytes: 4096, MIME: "audio/wav", Range: true})
}

func TestOpenAPIReferencesAndOperations(t *testing.T) {
	doc := testkit.ReadJSON(t, "docs/contracts/openapi.yaml").(map[string]any)
	if doc["openapi"] != "3.1.0" {
		t.Fatal("expected OpenAPI 3.1.0")
	}
	models := testkit.ReadJSON(t, "docs/contracts/http.schema.json").(map[string]any)["$defs"].(map[string]any)
	accountModels := testkit.ReadJSON(t, "docs/contracts/platform-account.schema.json").(map[string]any)["$defs"].(map[string]any)
	var walk func(any)
	walk = func(value any) {
		switch v := value.(type) {
		case []any:
			for _, child := range v {
				walk(child)
			}
		case map[string]any:
			if ref, ok := v["$ref"].(string); ok {
				if strings.HasPrefix(ref, "./http.schema.json#/$defs/") {
					if models[strings.TrimPrefix(ref, "./http.schema.json#/$defs/")] == nil {
						t.Errorf("unresolved model reference: %s", ref)
					}
				} else if strings.HasPrefix(ref, "./platform-account.schema.json#/$defs/") {
					if accountModels[strings.TrimPrefix(ref, "./platform-account.schema.json#/$defs/")] == nil {
						t.Errorf("unresolved platform account reference: %s", ref)
					}
				} else if strings.HasPrefix(ref, "#/") {
					var current any = doc
					for _, segment := range strings.Split(strings.TrimPrefix(ref, "#/"), "/") {
						object, ok := current.(map[string]any)
						if !ok || object[segment] == nil {
							t.Errorf("unresolved OpenAPI reference: %s", ref)
							break
						}
						current = object[segment]
					}
				} else {
					t.Errorf("unexpected external reference: %s", ref)
				}
			}
			for _, child := range v {
				walk(child)
			}
		}
	}
	walk(doc)
	ids := map[string]bool{}
	paths := doc["paths"].(map[string]any)
	for path, value := range paths {
		for method, operation := range value.(map[string]any) {
			if method == "parameters" {
				continue
			}
			op := operation.(map[string]any)
			id, ok := op["operationId"].(string)
			if !ok || ids[id] || op["responses"] == nil {
				t.Errorf("invalid operation: %s %s", method, path)
			}
			ids[id] = true
			if (method == "post" || method == "put" || method == "delete") && path != "/auth/session" || method == "delete" {
				encoded, _ := json.Marshal(op["parameters"])
				if !strings.Contains(string(encoded), "/CSRF") {
					t.Errorf("missing CSRF contract: %s %s", method, path)
				}
				if (method == "post" || method == "put") && op["x-idempotency"] != "natural" && op["x-idempotency"] != "read-only" && path != "/online-instances/{id}/account" && path != "/lifecycle/leases/{id}/renew" && path != "/lifecycle/leases/{id}/release" && path != "/remote-playlist-sync-jobs/{id}/cancel" && path != "/online-instances/{id}/tracks/{externalId}/resolve" && path != "/media/{id}/thumbnail" && path != "/writeback-jobs/{id}/cancel" && path != "/writeback-jobs/{id}/recover" && path != "/metadata-jobs/{id}/cancel" && path != "/hash-jobs/{id}/cancel" && path != "/fingerprint-jobs/{id}/cancel" && path != "/source-tracks/{id}/match-candidates/refresh" && !strings.Contains(string(encoded), "/Idempotency") {
					t.Errorf("missing idempotency contract: %s %s", method, path)
				}
			}
		}
	}
	for _, id := range []string{"healthReady", "login", "currentSession", "logout", "listProviders", "createScan", "getScan", "listSourceTracks", "createPlayback", "stopPlayback", "seekPlayback", "recoverPlayback", "getStream", "headStream", "createHash", "getHash", "cancelHash", "createFingerprint", "getFingerprint", "cancelFingerprint", "listFingerprints", "getFingerprintDetail", "getMatchingRevision", "refreshMatchCandidates", "listMatchCandidates", "getRecordingLink", "getRecording", "manualLink", "manualUnlink", "manualSplit", "setMatchExclusion", "listMatchingChanges", "uploadMedia", "getMedia", "getMediaInfo", "getMediaSelection", "selectMedia", "unlockMediaSelection", "listScraperInstances", "createMetadataJob", "getMetadataJob", "cancelMetadataJob", "applyMetadataCandidate", "getScanMetadataResults", "createWriteBack", "getWriteBack", "cancelWriteBack", "recoverWriteBack"} {
		if !ids[id] {
			t.Errorf("missing operation %s", id)
		}
	}
}
