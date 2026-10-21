//go:build !windows

package pluginhost

import (
	"errors"
	"os"
	"path/filepath"
)

func publishState(from, to string) error {
	if err := os.Rename(from, to); err != nil {
		return err
	}
	for _, name := range []string{filepath.Dir(to), filepath.Dir(filepath.Dir(to))} {
		dir, err := os.Open(name)
		if err != nil {
			return err
		}
		if err = errors.Join(dir.Sync(), dir.Close()); err != nil {
			return err
		}
	}
	return nil
}
