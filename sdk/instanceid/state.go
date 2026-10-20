package instanceid

import (
	"errors"
	"os"
	"path/filepath"
)

// StateDirectory reuses a legacy directory only when its identity maps exactly
// to this UUID. Keeping the directory avoids moving credentials or root proofs.
func StateDirectory(base, id string) (string, error) {
	if !Valid(id) {
		return "", errors.New("instance state requires a UUID")
	}
	entries, err := os.ReadDir(base)
	if errors.Is(err, os.ErrNotExist) {
		return filepath.Join(base, id), nil
	}
	if err != nil {
		return "", err
	}
	found := ""
	for _, entry := range entries {
		if Canonical(entry.Name()) != id {
			continue
		}
		info, err := os.Lstat(filepath.Join(base, entry.Name()))
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("instance state directory is invalid")
		}
		if found != "" {
			return "", errors.New("multiple state directories map to the same instance UUID")
		}
		found = filepath.Join(base, entry.Name())
	}
	if found == "" {
		found = filepath.Join(base, id)
	}
	return found, nil
}
