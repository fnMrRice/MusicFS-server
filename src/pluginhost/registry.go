package pluginhost

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/sourceaccess"
)

type Config struct {
	Executable          string
	Args                []string
	SHA256              string
	InstanceID          string
	UserID              string
	Roots               []sourceaccess.RootConfig
	StateDir            string
	Disabled            bool
	SnapshotIdleTimeout time.Duration
	SnapshotMaxLifetime time.Duration
}

func (c Config) snapshotTimeouts() (time.Duration, time.Duration) {
	idle, lifetime := c.SnapshotIdleTimeout, c.SnapshotMaxLifetime
	if idle == 0 {
		idle = 5 * time.Minute
	}
	if lifetime == 0 {
		lifetime = time.Hour
	}
	return idle, lifetime
}

func (c Config) Validate() error {
	idle, lifetime := c.snapshotTimeouts()
	if idle <= 0 || lifetime <= 0 || idle > lifetime || lifetime > time.Hour {
		return fmt.Errorf("ACCESS_DENIED: invalid scan snapshot timeouts")
	}
	if c.Disabled {
		return fmt.Errorf("UNSUPPORTED: instance disabled")
	}
	if c.InstanceID == "" || c.UserID == "" || !filepath.IsAbs(c.Executable) || !filepath.IsAbs(c.StateDir) {
		return fmt.Errorf("ACCESS_DENIED: invalid worker configuration")
	}
	f, err := os.Open(c.Executable)
	if err != nil {
		return fmt.Errorf("UNSUPPORTED: worker executable unavailable")
	}
	defer f.Close()
	i, err := f.Stat()
	if err != nil || !i.Mode().IsRegular() {
		return fmt.Errorf("UNSUPPORTED: invalid worker executable")
	}
	if c.SHA256 == "" {
		return fmt.Errorf("ACCESS_DENIED: worker checksum required")
	}
	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil {
		return err
	}
	if hex.EncodeToString(h.Sum(nil)) != c.SHA256 {
		return fmt.Errorf("ACCESS_DENIED: worker checksum mismatch")
	}
	return nil
}
