//go:build !windows

package musicdirs_test

import (
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/library/musicdirs"
)

func TestDiscoverMusicDirectoriesSkipsNonWindowsDrives(t *testing.T) {
	if got := musicdirs.Discover(); len(got) != 0 {
		t.Fatalf("non-Windows drive discovery returned directories: %v", got)
	}
}
