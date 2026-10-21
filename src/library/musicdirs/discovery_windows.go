package musicdirs

import "golang.org/x/sys/windows"

// Discover probes conventional directories on the system's logical drives.
func Discover() []string {
	drives, err := windows.GetLogicalDrives()
	if err != nil {
		return nil
	}
	return Find(DriveRoots(drives))
}

// DriveRoots translates the Windows logical-drive mask into absolute roots.
func DriveRoots(drives uint32) []string {
	var roots []string
	for i := uint32(0); i < 26; i++ {
		if drives&(1<<i) != 0 {
			roots = append(roots, string(rune('A'+i))+":\\")
		}
	}
	return roots
}
