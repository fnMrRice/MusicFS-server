package logging_test

import (
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/core/logging"
)

func TestExternalDiagnosticsAreBoundedAndDoNotTrustPayload(t *testing.T) {
	dir := t.TempDir()
	m := newManager(t, dir, io.Discard)
	if err := m.Configure(logging.Settings{Level: "trace"}); err != nil {
		t.Fatal(err)
	}
	previous := slog.Default()
	slog.SetDefault(m.Logger())
	t.Cleanup(func() { slog.SetDefault(previous) })
	w := logging.PluginStderr(pluginID, "trusted-instance")
	chunks := []string{
		`{"event":"plugin.operation.completed","level":"DEBUG","pluginId":"evil","instanceId":"evil",`,
		`"msg":"secret=super-sensitive","errorCode":"secret=super-sensitive","durationMs":15}` + "\n",
		"Authorization: Bearer super-sensitive\n",
		strings.Repeat("super-sensitive", 5000) + "\n",
		"trailing-secret",
	}
	for _, chunk := range chunks {
		if _, err := io.WriteString(w, chunk); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	data := contents(t, filepath.Join(dir, "plugins", pluginID+".log"))
	if strings.Contains(data, "super-sensitive") || strings.Contains(data, "trailing-secret") || strings.Contains(data, "evil") {
		t.Fatal("untrusted stderr fields were exposed")
	}
	if !strings.Contains(data, "Plugin operation completed") || !strings.Contains(data, "trusted-instance") || len(data) > 4096 {
		t.Fatal("diagnostics were dropped or unbounded")
	}
}
