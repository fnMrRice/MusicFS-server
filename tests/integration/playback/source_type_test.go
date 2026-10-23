package playback_test

import (
	"context"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
)

func TestPlaybackRejectsUndeclaredProviderSourceType(t *testing.T) {
	service, repository, who, request, _ := playbackService(t)
	runtime, ok := repository.library.Runtime("root")
	if !ok {
		t.Fatal("fixture provider missing")
	}
	runtime.Worker.Manifest.SourceType = ""
	if err := repository.library.UpdateRuntime(runtime); err != nil {
		t.Fatal(err)
	}
	_, err := service.Create(context.Background(), who, request, "unknown-source-type")
	if pluginproto.ErrorCode(err) != "UNSUPPORTED" {
		t.Fatal("undeclared source type fell back to local file playback", err)
	}
}
