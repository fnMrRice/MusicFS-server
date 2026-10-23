package e2e_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/core/config"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/library/jobs"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func providerScanRequest(t *testing.T, c *client, provider string) map[string]any {
	t.Helper()
	code, raw, _ := c.request(t, "GET", "/api/v1/provider-instances", nil, "")
	if code != 200 {
		t.Fatalf("read provider declaration: %d %s", code, raw)
	}
	testkit.ValidateHTTP(t, "Providers", raw)
	var response struct {
		Items []library.Provider `json:"items"`
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		t.Fatal(err)
	}
	for _, item := range response.Items {
		if item.ID != provider {
			continue
		}
		if item.Configuration == nil {
			t.Fatal("provider omitted import configuration")
		}
		ids := make([]string, 0, len(item.Roots))
		for _, root := range item.Roots {
			ids = append(ids, root.ID)
		}
		return map[string]any{"mode": "full", "schemaRevision": item.Configuration.SchemaRevision, "rootIds": ids, "options": map[string]any{}}
	}
	t.Fatal("provider missing")
	return nil
}

func waitScanOptions(t *testing.T, c *client, id string) jobs.Job {
	t.Helper()
	for deadline := time.Now().Add(15 * time.Second); ; {
		code, raw, _ := c.request(t, "GET", "/api/v1/scan-jobs/"+id, nil, "")
		if code != 200 {
			t.Fatalf("read scan: %d %s", code, raw)
		}
		testkit.ValidateHTTP(t, "ScanJob", raw)
		var job jobs.Job
		if err := json.Unmarshal(raw, &job); err != nil {
			t.Fatal(err)
		}
		if job.Status == "succeeded" {
			return job
		}
		if job.Status == "failed" || job.Status == "cancelled" || time.Now().After(deadline) {
			t.Fatalf("scan failed: %s", raw)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestYAMLPluginImportOptionsHTTPAndScopedLibrary(t *testing.T) {
	provider := build(t, "musicfs-plugin-local")
	payload, err := os.ReadFile(filepath.Join("..", "testdata", "audio", "markers.wav"))
	if err != nil {
		t.Fatal(err)
	}
	first, second := t.TempDir(), t.TempDir()
	for _, item := range []struct{ root, path string }{{first, "root.wav"}, {first, "sub/child.wav"}, {first, "sub/deeper/deep.wav"}, {first, strings.Repeat("d/", 70) + "unlimited.wav"}, {second, "other.wav"}} {
		path := filepath.Join(item.root, filepath.FromSlash(item.path))
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, payload, 0600); err != nil {
			t.Fatal(err)
		}
	}
	disabled := false
	cfg, base := yamlServiceConfig(t)
	cfg.Plugins = []config.Plugin{{PluginID: "musicfs.plugin.source.localfiles", Executable: provider, Enabled: true, Instances: []config.Instance{{InstanceID: "b1548e96-3081-5c9c-8376-bf290e369d51", Name: "Safe display name", Enabled: true, ImportDefaults: &pluginproto.ImportOverrides{Recursive: &disabled, ImportLyrics: &disabled}, Config: config.InstanceConfig{MusicRoots: []config.MusicRoot{{ID: "one", Name: "第一目录", Path: first}, {ID: "two", Name: "第二目录", Path: second}}}}}}}
	startNCMServer(t, cfg, base)
	c := login(t, base, cfg.AdminToken)
	request := providerScanRequest(t, c, "b1548e96-3081-5c9c-8376-bf290e369d51")
	_, raw, _ := c.request(t, "GET", "/api/v1/provider-instances", nil, "")
	for _, secret := range []string{first, second, provider, cfg.AdminToken, "rootIdentity", "configRevision", "stateDir"} {
		encoded, _ := json.Marshal(secret)
		if strings.Contains(string(raw), string(encoded[1:len(encoded)-1])) {
			t.Fatal("provider descriptor exposed private detail")
		}
	}
	var descriptor struct {
		Items []library.Provider `json:"items"`
	}
	_ = json.Unmarshal(raw, &descriptor)
	if descriptor.Items[0].Configuration.ImportDefaults.Recursive || descriptor.Items[0].Configuration.ImportDefaults.MaxDepth != 0 || !descriptor.Items[0].Configuration.ImportDefaults.ImportArtwork || descriptor.Items[0].Configuration.SchemaRevision != "local-import-v7" || len(descriptor.Items[0].Configuration.UnavailableOptions) != 0 {
		t.Fatalf("wrong loaded defaults: %s", raw)
	}
	sequence := 0
	submit := func(body map[string]any) jobs.Job {
		sequence++
		code, bodyRaw, _ := c.request(t, "POST", "/api/v1/provider-instances/b1548e96-3081-5c9c-8376-bf290e369d51/scan-jobs", body, fmt.Sprintf("scope-scan-%04d", sequence))
		if code != 202 {
			t.Fatalf("submit scope: %d %s", code, bodyRaw)
		}
		testkit.ValidateHTTP(t, "ScanAccepted", bodyRaw)
		var result struct {
			JobID string `json:"jobId"`
		}
		_ = json.Unmarshal(bodyRaw, &result)
		return waitScanOptions(t, c, result.JobID)
	}
	list := func() map[string]library.Track {
		code, body, _ := c.request(t, "GET", "/api/v1/source-tracks", nil, "")
		if code != 200 {
			t.Fatal(code)
		}
		testkit.ValidateHTTP(t, "SourceTracks", body)
		var page library.Page
		_ = json.Unmarshal(body, &page)
		result := map[string]library.Track{}
		for _, track := range page.Items {
			result[track.DisplayName] = track
		}
		return result
	}
	job := submit(request)
	if job.EffectiveOptions.Recursive || job.EffectiveOptions.MaxDepth != 0 || len(list()) != 2 {
		t.Fatal("instance root-only defaults ignored")
	}
	request["options"] = map[string]any{"recursive": true}
	job = submit(request)
	if !job.EffectiveOptions.Recursive || job.EffectiveOptions.MaxDepth != 0 || len(list()) != 5 {
		t.Fatal("enabling recursion did not restore default depth")
	}
	before := list()
	if err := os.Remove(filepath.Join(first, "sub", "child.wav")); err != nil {
		t.Fatal(err)
	}
	request["rootIds"] = []string{"one"}
	request["options"] = map[string]any{"recursive": false}
	submit(request)
	after := list()
	for name, track := range before {
		if after[name].ID != track.ID || after[name].Availability != "available" {
			t.Fatalf("shallow scan damaged retained track %s", name)
		}
	}
	request["options"] = map[string]any{"recursive": true, "maxDepth": 1}
	submit(request)
	after = list()
	if after["child.wav"].Availability != "missing" || after["deep.wav"].Availability != "available" || after["unlimited.wav"].Availability != "available" || after["other.wav"].Availability != "available" {
		t.Fatal("scoped missing reconciliation escaped selected depth/root")
	}
	for i, tc := range []struct {
		patch  map[string]any
		status int
		code   string
	}{
		{map[string]any{"schemaRevision": "stale"}, 409, "CONFLICT"},
		{map[string]any{"rootIds": []string{"private"}}, 403, "ACCESS_DENIED"},
		{map[string]any{"rootIds": []string{"one", "one"}}, 400, "INVALID_ARGUMENT"},
		{map[string]any{"options": map[string]any{"recursive": false, "maxDepth": 2}}, 400, "INVALID_ARGUMENT"},
		{map[string]any{"options": map[string]any{"recursive": true, "maxDepth": -1}}, 400, "INVALID_ARGUMENT"},
		{map[string]any{"options": map[string]any{"recursive": nil}}, 400, "INVALID_ARGUMENT"},
		{map[string]any{"options": map[string]any{"privatePath": "private"}}, 400, "INVALID_ARGUMENT"},
		{map[string]any{"options": nil}, 400, "INVALID_ARGUMENT"},
	} {
		body := providerScanRequest(t, c, "b1548e96-3081-5c9c-8376-bf290e369d51")
		for k, v := range tc.patch {
			body[k] = v
		}
		status, bodyRaw, _ := c.request(t, "POST", "/api/v1/provider-instances/b1548e96-3081-5c9c-8376-bf290e369d51/scan-jobs", body, fmt.Sprintf("invalid-scope-%04d", i))
		if status != tc.status || !strings.Contains(string(bodyRaw), `"code":"`+tc.code+`"`) {
			t.Fatalf("invalid case %d: %d %s", i, status, bodyRaw)
		}
		testkit.ValidateHTTP(t, "Error", bodyRaw)
	}
	request = providerScanRequest(t, c, "b1548e96-3081-5c9c-8376-bf290e369d51")
	status, firstRaw, _ := c.request(t, "POST", "/api/v1/provider-instances/b1548e96-3081-5c9c-8376-bf290e369d51/scan-jobs", request, "scope-idempotency-0001")
	if status != 202 {
		t.Fatal(status)
	}
	status, replayed, _ := c.request(t, "POST", "/api/v1/provider-instances/b1548e96-3081-5c9c-8376-bf290e369d51/scan-jobs", request, "scope-idempotency-0001")
	var firstAccepted, replayAccepted struct {
		JobID string `json:"jobId"`
	}
	_ = json.Unmarshal(firstRaw, &firstAccepted)
	_ = json.Unmarshal(replayed, &replayAccepted)
	if status != 202 || replayAccepted.JobID == "" || replayAccepted.JobID != firstAccepted.JobID {
		t.Fatalf("same effective plan was not replayed: %d %s", status, replayed)
	}
	request["options"] = map[string]any{"recursive": true}
	status, raw, _ = c.request(t, "POST", "/api/v1/provider-instances/b1548e96-3081-5c9c-8376-bf290e369d51/scan-jobs", request, "scope-idempotency-0001")
	if status != 409 {
		t.Fatalf("changed plan reused key: %d %s", status, raw)
	}
	t.Log("Real YAML, plugin-owned declaration, instance and request normalization, safe roots, scoped missing reconciliation, versioned lyrics capability and plan idempotency verified")
}
