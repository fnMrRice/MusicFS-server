package config

import (
	"encoding/hex"
	"errors"
	"strings"
)

func (c Config) validateFingerprinting() error {
	f := c.Fingerprinting
	if f.MaxConcurrent < 1 || f.MaxConcurrent > 2 || f.TimeoutSeconds < 1 || f.TimeoutSeconds > 120 {
		return errors.New("invalid fingerprinting resource limits")
	}
	if strings.ContainsRune(f.FpcalcPath, 0) {
		return errors.New("invalid fingerprint executable path")
	}
	if f.FpcalcSHA256 != "" {
		hash, err := hex.DecodeString(f.FpcalcSHA256)
		if err != nil || len(hash) != 32 {
			return errors.New("fpcalcSha256 must contain 64 hexadecimal characters")
		}
	}
	if !f.Enabled {
		return nil
	}
	if f.FpcalcPath == "" || f.FpcalcSHA256 == "" {
		return errors.New("enabled fingerprinting requires fpcalcPath and fpcalcSha256")
	}
	for _, plugin := range c.Plugins {
		if plugin.PluginID != "musicfs.audio.ffmpeg" {
			continue
		}
		for _, instance := range plugin.Instances {
			if instance.Role == "audio" {
				return nil
			}
		}
	}
	return errors.New("enabled fingerprinting requires a configured audio FFmpeg instance")
}
