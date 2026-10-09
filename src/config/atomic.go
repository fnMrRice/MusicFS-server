package config

import (
	"os"
	"path/filepath"

	"github.com/fnMrRice/MusicFS-server/src/domain"
)

func canWriteConfig(path string) bool {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0200 == 0 {
		return false
	}
	f, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return false
	}
	f.Close()
	dir, err := os.Stat(filepath.Dir(path))
	if err != nil || dir.Mode().Perm()&0200 == 0 {
		return false
	}
	f, err = os.CreateTemp(filepath.Dir(path), ".musicfs-write-check-")
	if err != nil {
		return false
	}
	name := f.Name()
	f.Close()
	return os.Remove(name) == nil
}

func atomicWriteConfig(path string, raw []byte, verify func() error) error {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0200 == 0 {
		return domain.Fail("CONFIG_READ_ONLY", "Configuration is not a writable regular file")
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".musicfs-config-")
	if err != nil {
		return domain.Fail("CONFIG_READ_ONLY", "Cannot create configuration replacement")
	}
	name := f.Name()
	defer os.Remove(name)
	defer f.Close()
	if err = restrictReplacement(path, name, info); err != nil {
		return domain.Fail("CONFIG_READ_ONLY", "Cannot preserve configuration permissions")
	}
	if _, err = f.Write(raw); err != nil {
		return domain.Fail("TEMPORARY_FAILURE", "Cannot write configuration replacement")
	}
	if err = f.Sync(); err != nil {
		return domain.Fail("TEMPORARY_FAILURE", "Cannot persist configuration replacement")
	}
	if err = f.Close(); err != nil {
		return domain.Fail("TEMPORARY_FAILURE", "Cannot close configuration replacement")
	}
	if err = verify(); err != nil {
		return err
	}
	if err = replaceConfig(name, path); err != nil {
		return domain.Fail("TEMPORARY_FAILURE", "Cannot confirm atomic configuration replacement; reload required")
	}
	return nil
}

func lockConfig(path string) (func(), error) {
	name := path + ".lock"
	if info, err := os.Lstat(name); err == nil && !info.Mode().IsRegular() {
		return nil, domain.Fail("CONFIG_READ_ONLY", "Invalid configuration lock file")
	}
	f, err := os.OpenFile(name, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := lockConfigFile(f); err != nil {
		f.Close()
		return nil, err
	}
	// Keep the lock inode: unlinking could let another writer lock a different file.
	return func() { f.Close() }, nil
}
