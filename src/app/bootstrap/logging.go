package bootstrap

import (
	"errors"
	"fmt"
	"io"
	stdlog "log"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"github.com/fnMrRice/MusicFS-server/src/core/config"
	"github.com/fnMrRice/MusicFS-server/src/core/logging"
)

var managedLoggingLease sync.Mutex

// 日志目录独立加锁，避免两个不同数据目录的服务误用同一组轮转文件。
func startLogging(c config.Config, output io.Writer) (*logging.Manager, func(), error) {
	directory := c.Logging.Directory
	if directory == "" {
		directory = filepath.Join(c.DataRoot, "logs")
	}
	if err := os.MkdirAll(directory, 0700); err != nil {
		return nil, nil, errors.New("cannot create log directory")
	}
	lock, err := os.OpenFile(filepath.Join(directory, ".log.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, nil, errors.New("cannot open log directory lock")
	}
	if err = lockFile(lock); err != nil {
		lock.Close()
		return nil, nil, errors.New("log directory is already in use")
	}
	if !managedLoggingLease.TryLock() {
		lock.Close()
		return nil, nil, errors.New("managed logging is already active in this process")
	}
	transferred := false
	defer func() {
		if !transferred {
			managedLoggingLease.Unlock()
		}
	}()
	m, err := logging.New(logging.Options{Directory: directory, Level: c.Logging.Level, PluginLevels: c.Logging.Plugins, Console: output})
	if err != nil {
		lock.Close()
		return nil, nil, err
	}
	ids := []string{
		"musicfs.plugin.source.localfiles", "musicfs.plugin.source.netease", "musicfs.plugin.source.qqmusic", "musicfs.plugin.source.applemusic", "musicfs.plugin.source.bilibili",
		"musicfs.plugin.decoder.ffmpeg", "musicfs.plugin.decoder.ncm", "musicfs.scraper.local", "musicfs.dsp.gain", "musicfs.plugin.storage.sqlite",
		"musicfs.plugin.analysis.chromaprint", "musicfs.plugin.tags.flac", "musicfs.plugin.matching.standard", "musicfs.plugin.transform.thumbnail",
		"musicfs.dsp.equalizer", "musicfs.dsp.compressor", "musicfs.dsp.reverb", "musicfs.plugin.decoder.channelmix",
	}
	for _, p := range c.Plugins {
		ids = append(ids, p.PluginID)
	}
	for _, p := range c.AudioDSP.Plugins {
		ids = append(ids, p.Name)
	}
	if c.Storage.Backend == "plugin" {
		ids = append(ids, "musicfs.plugin.storage.plugin")
	}
	if err = m.RegisterPlugins(ids...); err != nil {
		m.Close()
		lock.Close()
		return nil, nil, err
	}
	previous := slog.Default()
	previousOutput, previousFlags, previousPrefix := stdlog.Writer(), stdlog.Flags(), stdlog.Prefix()
	slog.SetDefault(m.Logger())
	transferred = true
	return m, sync.OnceFunc(func() {
		slog.SetDefault(previous)
		stdlog.SetOutput(previousOutput)
		stdlog.SetFlags(previousFlags)
		stdlog.SetPrefix(previousPrefix)
		if err := m.Close(); err != nil {
			fmt.Fprintln(os.Stderr, "MusicFS log archive shutdown failed")
		}
		_ = lock.Close()
		managedLoggingLease.Unlock()
	}), nil
}
