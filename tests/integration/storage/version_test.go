package storage_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/plugins/pluginrpc"
	"github.com/fnMrRice/MusicFS-server/src/storage"
)

func TestPersistenceHandshakeRequiresReleaseVersion(t *testing.T) {
	options := storageWorker(t, "./tests/testapps/persistence-version-worker")
	for _, mode := range []string{"missing", "blank", "invalid", "leading-zero", "oversize"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			process, err := pluginrpc.Start(context.Background(), pluginrpc.ProcessConfig{Executable: options.Executable, SHA256: options.SHA256, WorkDir: root, Args: []string{mode}}, reflect.TypeFor[storage.Backend](), "backend", storage.Options{DataRoot: root})
			if process != nil {
				process.Close()
			}
			matchingError(t, err, "PROTOCOL_MISMATCH")
		})
	}
}

func TestPersistenceServeRequiresReleaseVersion(t *testing.T) {
	for _, version := range []string{"", " ", "1.2", "1.2.3-01"} {
		if err := pluginrpc.Serve(t.Context(), nil, nil, version, nil, nil); err == nil {
			t.Fatalf("invalid persistence release was accepted: %q", version)
		}
	}
}
