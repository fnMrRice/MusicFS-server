// Package logging 统一管理宿主和插件的结构化日志、动态等级及文件保留。
package logging

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

const LevelTrace = slog.Level(-8)
const RetentionDays = 30

type Settings struct {
	Level   string            `json:"level"`
	Plugins map[string]string `json:"plugins"`
}

type Options struct {
	Directory    string
	Level        string
	PluginLevels map[string]string
	Console      io.Writer
}

type Manager struct {
	mu        sync.RWMutex
	directory string
	settings  Settings
	levels    map[string]slog.Level
	floor     slog.Level
	sinks     map[string]*fileSink
	console   slog.Handler
	archive   chan struct{}
	stop      chan struct{}
	done      chan struct{}
	closed    bool
}

var pluginPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
var reservedName = regexp.MustCompile(`(?i)^(CON|PRN|AUX|NUL|COM[0-9]|LPT[0-9])(?:\.|$)`)

func ValidPluginID(id string) bool {
	return pluginPattern.MatchString(id) && !reservedName.MatchString(id) && !strings.HasSuffix(id, ".")
}

func parseLevel(value string) (slog.Level, error) {
	switch value {
	case "trace":
		return LevelTrace, nil
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, errors.New("log level must be trace, debug, info, warn or error")
	}
}

func ValidateLevel(value string) error { _, err := parseLevel(value); return err }

func New(options Options) (*Manager, error) {
	if options.Directory == "" {
		return nil, errors.New("log directory is required")
	}
	if options.Level == "" {
		options.Level = "info"
	}
	m := &Manager{directory: options.Directory, sinks: map[string]*fileSink{}, archive: make(chan struct{}, 1), stop: make(chan struct{}), done: make(chan struct{})}
	if err := m.Configure(Settings{Level: options.Level, Plugins: options.PluginLevels}); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(m.directory, "plugins"), 0700); err != nil {
		return nil, errors.New("cannot create log directory")
	}
	if options.Console != nil {
		m.console = slog.NewJSONHandler(&lockedWriter{output: options.Console}, handlerOptions())
	}
	if _, err := m.sink(""); err != nil {
		return nil, err
	}
	if err := m.cleanup(time.Now()); err != nil {
		m.closeSinks()
		return nil, err
	}
	go m.maintain()
	return m, nil
}

func (m *Manager) Logger() *slog.Logger { return slog.New(&handler{manager: m}) }

// RegisterPlugins 提前创建独立文件，使尚未产生事件的插件也有确定的日志位置。
func (m *Manager) RegisterPlugins(ids ...string) error {
	for _, id := range ids {
		if !ValidPluginID(id) {
			return errors.New("invalid logging plugin identifier")
		}
		if _, err := m.sink(id); err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) Settings() Settings {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return Settings{Level: m.settings.Level, Plugins: maps.Clone(m.settings.Plugins)}
}

// Configure 在完整校验后一次替换等级，已有 Logger 会立即使用新的阈值。
func (m *Manager) Configure(settings Settings) error {
	level, err := parseLevel(settings.Level)
	if err != nil {
		return err
	}
	if len(settings.Plugins) > 128 {
		return errors.New("too many plugin log level overrides")
	}
	levels := map[string]slog.Level{"": level}
	floor := level
	for id, value := range settings.Plugins {
		if !ValidPluginID(id) {
			return errors.New("invalid logging plugin identifier")
		}
		parsed, err := parseLevel(value)
		if err != nil {
			return err
		}
		levels[id] = parsed
		if parsed < floor {
			floor = parsed
		}
	}
	if settings.Plugins == nil {
		settings.Plugins = map[string]string{}
	}
	settings.Plugins = maps.Clone(settings.Plugins)
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return errors.New("logging manager is closed")
	}
	m.settings, m.levels, m.floor = settings, levels, floor
	return nil
}

func (m *Manager) sink(id string) (*fileSink, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil, errors.New("logging manager is closed")
	}
	if s := m.sinks[id]; s != nil {
		return s, nil
	}
	if len(m.sinks) >= 257 {
		return nil, errors.New("too many plugin log files")
	}
	name := filepath.Join(m.directory, "server.log")
	if id != "" {
		name = filepath.Join(m.directory, "plugins", id+".log")
	}
	s, err := openSink(name, m.archiveReady)
	if err != nil {
		return nil, errors.New("cannot open log file")
	}
	m.sinks[id] = s
	return s, nil
}

func (m *Manager) enabled(id string, level slog.Level) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	threshold, ok := m.levels[id]
	if !ok {
		threshold = m.levels[""]
	}
	return !m.closed && level >= threshold
}

// Rotate 可供宿主运维显式轮转；正常写入还会按 UTC 日期和文件大小自动轮转。
func (m *Manager) Rotate() error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.closed {
		return errors.New("logging manager is closed")
	}
	var result error
	for _, s := range m.sinks {
		result = errors.Join(result, s.rotate())
	}
	return result
}

func (m *Manager) maintain() {
	defer close(m.done)
	timer := time.NewTicker(time.Hour)
	defer timer.Stop()
	for {
		select {
		case <-m.stop:
			return
		case <-m.archive:
			if err := m.cleanup(time.Now()); err != nil {
				m.Logger().Error("Log archive maintenance failed", "errorCode", "LOG_MAINTENANCE_FAILED")
			}
		case now := <-timer.C:
			m.mu.RLock()
			var err error
			for _, s := range m.sinks {
				err = errors.Join(err, s.rotateDay(now))
			}
			m.mu.RUnlock()
			err = errors.Join(err, m.cleanup(now))
			if err != nil {
				m.Logger().Error("Log archive maintenance failed", "errorCode", "LOG_MAINTENANCE_FAILED")
			}
		}
	}
}

func (m *Manager) Close() error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil
	}
	m.closed = true
	close(m.stop)
	m.mu.Unlock()
	<-m.done
	return errors.Join(m.closeSinks(), m.cleanup(time.Now()))
}

func (m *Manager) archiveReady() {
	select {
	case m.archive <- struct{}{}:
	default:
	}
}

func (m *Manager) closeSinks() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	var result error
	for _, s := range m.sinks {
		result = errors.Join(result, s.close())
	}
	return result
}

type lockedWriter struct {
	mu     sync.Mutex
	output io.Writer
}

func (w *lockedWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.output.Write(p)
}

func (m *Manager) reportFailure() {
	// 不再次走文件 Handler，避免磁盘写入失败时递归记录。
	fmt.Fprintln(os.Stderr, "MusicFS log file write failed")
}
