package config

import "golang.org/x/sys/windows"

func discoverMusicDirectories() []string {
	drives, err := windows.GetLogicalDrives()
	if err != nil {
		return nil
	}
	return findMusicDirectories(logicalDriveRoots(drives))
}

func logicalDriveRoots(drives uint32) []string {
	var roots []string
	for i := uint32(0); i < 26; i++ {
		if drives&(1<<i) != 0 {
			roots = append(roots, string(rune('A'+i))+":\\")
		}
	}
	return roots
}
