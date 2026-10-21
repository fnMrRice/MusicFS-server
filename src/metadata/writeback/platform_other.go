//go:build !windows

package writeback

import "os"

func openDisk(Root, Target) (disk, error) {
	return nil, unsupported("Recoverable writeback is only verified on local Windows NTFS")
}

func inspectCapability(Root) error {
	return unsupported("Recoverable writeback is only verified on local Windows NTFS")
}
func publishJournal(root *os.Root, from, to string, replace bool) error {
	if !replace {
		if err := root.Link(from, to); err != nil {
			return err
		}
		if err := root.Remove(from); err != nil {
			return err
		}
	} else if err := root.Rename(from, to); err != nil {
		return err
	}
	dir, err := root.Open(".")
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
