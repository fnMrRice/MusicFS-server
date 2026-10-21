package mediastore

import "os"

func syncDirectory(root *os.Root, name string) error {
	f, err := root.Open(name)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}

func publishFile(root *os.Root, from, to string) error {
	// A hard-link publication is atomic and fails if the immutable ID exists.
	// Both directories are created beneath the same configured media filesystem.
	if err := root.Link(from, to); err != nil {
		return err
	}
	if err := syncDirectory(root, "objects"); err != nil {
		return err
	}
	if err := root.Remove(from); err != nil {
		return err
	}
	return syncDirectory(root, "staging")
}
