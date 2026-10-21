package dspworker

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"

	"github.com/fnMrRice/MusicFS-server/plugins/dsp/builtin"
)

type Config struct {
	Builtin    bool     `json:"builtin,omitempty" yaml:"builtin,omitempty"`
	Name       string   `json:"name" yaml:"name"`
	Executable string   `json:"executable" yaml:"executable"`
	SHA256     string   `json:"sha256" yaml:"sha256"`
	Args       []string `json:"args,omitempty" yaml:"args,omitempty"`
	TimeoutMs  int      `json:"timeoutMs,omitempty" yaml:"timeoutMs,omitempty"`
}

var validName = regexp.MustCompile(`^[a-z][a-z0-9_.-]{0,63}$`)
var validSHA = regexp.MustCompile(`^[0-9a-f]{64}$`)

func (c Config) Validate() error {
	if !validName.MatchString(c.Name) || c.Name == "gain" || c.Name == "bypass" || builtin.EffectName(c.Name) != "" || len(c.Args) > 32 || c.TimeoutMs != 0 && (c.TimeoutMs < 10 || c.TimeoutMs > 30000) {
		return fmt.Errorf("invalid external DSP configuration")
	}
	if c.Builtin {
		if c.Name != "musicfs.dsp.gain" {
			return fmt.Errorf("unsupported builtin DSP plugin")
		}
	} else if !filepath.IsAbs(c.Executable) || !validSHA.MatchString(c.SHA256) {
		return fmt.Errorf("invalid external DSP executable identity")
	}
	for _, arg := range c.Args {
		if len(arg) > 4096 {
			return fmt.Errorf("DSP argument exceeds size limit")
		}
	}
	return nil
}

func (c Config) Clone() Config {
	c.Args = slices.Clone(c.Args)
	return c
}

func (c Config) VerifyExecutable() error {
	if err := c.Validate(); err != nil {
		return err
	}
	if c.Builtin {
		return nil
	}
	file, err := os.Open(c.Executable)
	if err != nil {
		return fmt.Errorf("ACCESS_DENIED: DSP executable unavailable")
	}
	defer file.Close()
	hash := sha256.New()
	if _, err = io.Copy(hash, file); err != nil || hex.EncodeToString(hash.Sum(nil)) != c.SHA256 {
		return fmt.Errorf("ACCESS_DENIED: DSP executable checksum mismatch")
	}
	return nil
}
