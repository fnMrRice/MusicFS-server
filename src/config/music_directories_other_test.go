//go:build !windows

package config

import "testing"

func TestDiscoverMusicDirectoriesSkipsNonWindowsDrives(t *testing.T) {
	if got := discoverMusicDirectories(); len(got) != 0 {
		t.Fatalf("non-Windows drive discovery returned directories: %v", got)
	}
}
