package mediastore

import (
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

func syncDirectory(_ *os.Root, _ string) error { return nil }

func publishFile(root *os.Root, from, to string) error {
	// The private media root is service-owned. No replace/copy fallback is allowed;
	// MOVEFILE_WRITE_THROUGH confirms the same-volume publication before DB use.
	src, err := windows.UTF16PtrFromString(filepath.Join(root.Name(), filepath.FromSlash(from)))
	if err != nil {
		return err
	}
	dst, err := windows.UTF16PtrFromString(filepath.Join(root.Name(), filepath.FromSlash(to)))
	if err != nil {
		return err
	}
	return windows.MoveFileEx(src, dst, windows.MOVEFILE_WRITE_THROUGH)
}
