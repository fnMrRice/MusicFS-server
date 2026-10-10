//go:build !windows

package config

func discoverMusicDirectories() []string {
	return nil
}
