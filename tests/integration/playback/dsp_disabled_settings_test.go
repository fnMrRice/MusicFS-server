package playback_test

import (
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/playback"
	"github.com/fnMrRice/MusicFS-server/src/playback/audio"
	"github.com/fnMrRice/MusicFS-server/src/plugins/dspsettings"
	"github.com/fnMrRice/MusicFS-server/src/plugins/settings"
)

func TestDisabledProcessorAllowsEmptyStereoDSPButPreservesProcessingRequirements(t *testing.T) {
	for _, scope := range []string{"personal", "global"} {
		for _, scenario := range []string{"original", "required", "forced", "gain", "effect", "surround"} {
			t.Run(scope+"/"+scenario, func(t *testing.T) {
				original, repo, who, request, _ := playbackService(t)
				original.Close()
				options := dspsettings.Options{Chain: []audio.NodeSpec{}, OutputLayout: "stereo", BufferFrames: 64}
				switch scenario {
				case "forced":
					request.Preferences.ForceProcessing = true
				case "gain":
					request.Preferences.GainDB = -6
				case "effect":
					options.Chain = []audio.NodeSpec{{Name: "reverb", Version: 1}}
				case "surround":
					options.OutputLayout = "7.1"
				}
				owner, id := who.User.ID, dspsettings.StorageID
				if scope == "global" {
					owner, id = dspsettings.GlobalOwner, dspsettings.GlobalStorageID
				}
				if _, err := repo.PutPluginSettings(t.Context(), owner, id, 0, settings.Values{"schemaRevision": dspsettings.SchemaRevision, "settings": options}); err != nil {
					t.Fatal(err)
				}
				player, err := playback.New(t.Context(), repo.Store, repo.library, nil, playback.Config{CacheRoot: t.TempDir(), MaxSessions: 8, MaxCacheBytes: 16 << 20, RequireDSP: scenario == "required"})
				if err != nil {
					t.Fatal(err)
				}
				defer player.Close()
				description, err := player.Create(t.Context(), who, request, "disabled-dsp-create")
				if scenario != "original" {
					recoveryError(t, err, "UNSUPPORTED")
					return
				}
				if err != nil || description.DeliveryMode != "proxy" || len(description.EffectiveProcessing) != 0 {
					t.Fatal("empty stereo settings blocked original audio", description, err)
				}
			})
		}
	}
}
