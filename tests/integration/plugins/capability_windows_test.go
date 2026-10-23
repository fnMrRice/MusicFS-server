package plugins_test

import (
	"github.com/fnMrRice/MusicFS-server/plugins/tags/flac"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/metadata/writeback"
)

func TestWritebackCapabilityCoexistsWithLocalWorker(t *testing.T) {
	w, cfg, r := rebindFixture(t)
	root := writeback.Root{Owner: cfg.UserID, InstanceID: cfg.InstanceID, RootID: r.RootID, Path: cfg.Roots[0].Path, Identity: w.Access.Roots()[0].ExpectedIdentity}
	for i := range 20 {
		capability, err := writeback.InspectCapability(root, flac.New())
		if err != nil || !capability.Available {
			t.Fatalf("capability check %d with an active scanned worker: %+v %v", i, capability, err)
		}
	}
}
