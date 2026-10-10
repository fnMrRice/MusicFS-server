package mediastore

import "os"

func publishManifest(root *os.Root) error {
	return publishFile(root, "manifest.pending", "manifest.json")
}
