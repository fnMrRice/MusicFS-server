package playback_test

import (
	"context"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/playback"
	"github.com/fnMrRice/MusicFS-server/src/plugins/audioffmpeg"
)

func TestRequiredDSPDoesNotFallBackToOriginalWhenProcessorUnavailable(t *testing.T) {
	for _, paused := range []bool{false, true} {
		name := "unavailable"
		if paused {
			name = "paused"
		}
		t.Run(name, func(t *testing.T) {
			old, repo, who, request, _ := playbackService(t)
			old.Close()
			var processor *audioffmpeg.Processor
			if paused {
				processor = &audioffmpeg.Processor{}
			}
			player, err := playback.New(context.Background(), repo.Store, repo.library, processor, playback.Config{
				CacheRoot: t.TempDir(), MaxSessions: 8, MaxCacheBytes: 16 << 20, RequireDSP: true,
			})
			if err != nil {
				t.Fatal(err)
			}
			defer player.Close()
			if paused {
				player.UpdateAudio(nil, nil)
			}
			_, err = player.Create(context.Background(), who, request, "required-dsp-create")
			recoveryError(t, err, "UNSUPPORTED")
		})
	}
}
