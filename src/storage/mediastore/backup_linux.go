package mediastore

import "os"

func publishManifest(root *os.Root) error {
	if err := root.Link("manifest.pending", "manifest.json"); err != nil {
		return err
	}
	if err := syncDirectory(root, "."); err != nil {
		return err
	}
	if err := root.Remove("manifest.pending"); err != nil {
		return err
	}
	return syncDirectory(root, ".")
}
