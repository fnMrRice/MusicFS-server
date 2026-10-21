package plugins_test

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/transform/thumbnail"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginlog"
)

func TestThumbnailLogsPluginScopeAndDiagnosticLevels(t *testing.T) {
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, &slog.HandlerOptions{Level: pluginlog.LevelTrace})))
	t.Cleanup(func() { slog.SetDefault(previous) })
	var input bytes.Buffer
	if err := png.Encode(&input, image.NewNRGBA(image.Rect(0, 0, 4, 2))); err != nil {
		t.Fatal(err)
	}
	if _, err := thumbnail.New().Thumbnail(context.Background(), input.Bytes()); err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(&output)
	var started, completed, dimensions bool
	for {
		var record map[string]any
		if err := decoder.Decode(&record); err == io.EOF {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		if record["pluginId"] != "musicfs.plugin.transform.thumbnail" {
			t.Fatalf("wrong plugin identity: %v", record)
		}
		started = started || record["event"] == "plugin.operation.started"
		completed = completed || record["event"] == "plugin.operation.completed" && record["level"] == "DEBUG"
		dimensions = dimensions || record["width"] == float64(4) && record["height"] == float64(2)
		for key := range record {
			if strings.Contains(strings.ToLower(key), "body") || strings.Contains(strings.ToLower(key), "payload") {
				t.Fatalf("media payload field logged: %s", key)
			}
		}
	}
	if !started || !completed || !dimensions {
		t.Fatalf("missing diagnostic records: started=%v completed=%v dimensions=%v", started, completed, dimensions)
	}
}
