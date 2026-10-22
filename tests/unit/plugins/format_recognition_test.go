package plugins_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/audioformat"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/plugins/sourceaccess"
)

func TestScanUsesContentForRenamedContainerAndPluginExtensionsForCandidates(t *testing.T) {
	root := t.TempDir()
	for name, body := range map[string]string{"renamed.wav": "CTENFDAM", "disguised.mp3": "CTENFDAM", "plain.ncm": "ID3\x04", "unknown.wav": "unknown", "ignored.txt": "text", "ignored.bin": "CTENFDAM", "source.wav.backup": "ID3\x04"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	access, err := sourceaccess.New([]sourceaccess.RootConfig{{ID: "root", Path: root}})
	if err != nil {
		t.Fatal(err)
	}
	entries, _, _, err := access.Entries(context.Background(), pluginproto.ScanScope{RootID: "root", RootIdentity: access.Roots()[0].ExpectedIdentity, Recursive: true, MaxDepth: 64})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"renamed.wav": "audio/x-ncm", "disguised.mp3": "audio/x-ncm", "plain.ncm": "audio/mpeg", "unknown.wav": "application/octet-stream"}
	if len(entries) != len(want) {
		t.Fatalf("unexpected entries: %+v", entries)
	}
	for _, entry := range entries {
		if want[entry.RelativePath] != entry.MIME {
			t.Fatalf("wrong content recognition: %+v", entry)
		}
	}
}

func TestScanAcceptsAdditionalPluginWithoutHostFormatChanges(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"custom.data", "candidate.music", "ordinary.wav"} {
		body := "unknown"
		if name == "custom.data" {
			body = "custom"
		}
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	registry, err := audioformat.New(audioformat.Detector{PluginID: "custom", Priority: 200, Extensions: []string{".music", ".data"}, Probe: func(header []byte, _ int64) (audioformat.Match, bool) {
		return audioformat.Match{Format: "custom", MIME: "audio/custom"}, string(header) == "custom"
	}})
	if err != nil {
		t.Fatal(err)
	}
	access, err := sourceaccess.NewWithFormats([]sourceaccess.RootConfig{{ID: "root", Path: root}}, registry)
	if err != nil {
		t.Fatal(err)
	}
	entries, _, _, err := access.Entries(context.Background(), pluginproto.ScanScope{RootID: "root", RootIdentity: access.Roots()[0].ExpectedIdentity, Recursive: true, MaxDepth: 64})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("host format list interfered: %+v", entries)
	}
	for _, entry := range entries {
		if entry.RelativePath == "ordinary.wav" {
			t.Fatal("host retained an undeclared extension")
		}
		if entry.RelativePath == "custom.data" && entry.MIME != "audio/custom" {
			t.Fatalf("plugin result lost: %+v", entry)
		}
	}
}
