package onlinecache

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
)

const (
	customFolder = "MusicFS-downloads"
	ownerMarker  = ".musicfs-owner"
	locationFile = "download-directory.json"
)

type DirectorySettings struct {
	Path          string `json:"path"`
	EffectivePath string `json:"effectivePath"`
}

// 当前目录可切换，历史目录保留用于重启后回收未完成的下载。
type directoryState struct {
	Owner string   `json:"owner"`
	Path  string   `json:"path"`
	Known []string `json:"known,omitempty"`
}

func (m *Manager) loadDirectories() error {
	raw, err := os.ReadFile(filepath.Join(m.root, locationFile))
	if os.IsNotExist(err) {
		m.ownerMarker = rand.Text()
		return nil
	}
	if err != nil {
		return err
	}
	if err = json.Unmarshal(raw, &m.directories); err != nil {
		return err
	}
	s := m.directories
	if len(s.Owner) < 16 || len(s.Owner) > 128 || s.Path != "" && !slices.Contains(s.Known, s.Path) {
		return errors.New("invalid online download directory configuration")
	}
	for _, path := range s.Known {
		if !validDirectory(path) {
			return errors.New("invalid stored online download directory")
		}
	}
	m.ownerMarker = s.Owner
	return nil
}

func validDirectory(path string) bool {
	return filepath.IsAbs(path) && path == filepath.Clean(path) && !strings.ContainsRune(path, 0) &&
		!strings.HasPrefix(path, `\\.\`) && !strings.HasPrefix(path, `\\?\`)
}

func (m *Manager) effectiveDirectory(path string) string {
	if path == "" {
		return m.root
	}
	return filepath.Join(path, customFolder)
}

func (m *Manager) DownloadDirectory() DirectorySettings {
	m.directoryMu.RLock()
	defer m.directoryMu.RUnlock()
	return DirectorySettings{m.directories.Path, m.effectiveDirectory(m.directories.Path)}
}

func (m *Manager) SetDownloadDirectory(path string) (DirectorySettings, error) {
	m.directoryMu.Lock()
	defer m.directoryMu.Unlock()
	path = strings.TrimSpace(path)
	if path != "" {
		path = filepath.Clean(path)
		if !validDirectory(path) {
			return DirectorySettings{}, domain.Fail("INVALID_ARGUMENT", "The download directory must be an absolute server path")
		}
		if rel, err := filepath.Rel(m.root, path); err == nil && rel == "." {
			path = ""
		}
	}
	if path != "" {
		if m.validateDirectory != nil {
			if err := m.validateDirectory(m.effectiveDirectory(path)); err != nil {
				return DirectorySettings{}, err
			}
		}
		if m.directories.Owner == "" {
			initial := directoryState{Owner: m.ownerMarker}
			if err := m.saveDirectories(initial); err != nil {
				return DirectorySettings{}, domain.Fail("CONFIG_READ_ONLY", "Could not save the online download directory")
			}
			m.directories = initial
		}
		var err error
		path, err = m.resolveDirectory(path)
		if err != nil {
			return DirectorySettings{}, err
		}
		if m.validateDirectory != nil {
			if err := m.validateDirectory(m.effectiveDirectory(path)); err != nil {
				return DirectorySettings{}, err
			}
		}
		if err := m.prepareDirectory(path); err != nil {
			return DirectorySettings{}, err
		}
	}
	state := m.directories
	if path != state.Path {
		state.Path, state.Owner = path, m.ownerMarker
		if path != "" && !slices.Contains(state.Known, path) {
			state.Known = append(slices.Clone(state.Known), path)
		}
		if err := m.saveDirectories(state); err != nil {
			return DirectorySettings{}, domain.Fail("CONFIG_READ_ONLY", "Could not save the online download directory")
		}
		m.directories = state
	}
	return DirectorySettings{path, m.effectiveDirectory(path)}, nil
}

// 同一路径的大小写、符号链接和挂载别名归并到原有目录身份，避免误删仍在播放的副本。
func (m *Manager) resolveDirectory(path string) (string, error) {
	for _, known := range m.directories.Known {
		if rel, err := filepath.Rel(path, known); err == nil && rel == "." {
			return known, nil
		}
	}
	if err := os.MkdirAll(path, 0700); err != nil {
		return "", domain.Fail("INVALID_ARGUMENT", "The server cannot create the selected download directory")
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", domain.Fail("INVALID_ARGUMENT", "Could not resolve the download directory")
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.IsDir() {
		return "", domain.Fail("INVALID_ARGUMENT", "Could not access the download directory")
	}
	for _, known := range m.directories.Known {
		if other, err := os.Stat(known); err == nil && os.SameFile(other, info) {
			return known, nil
		}
	}
	return resolved, nil
}

func (m *Manager) saveDirectories(state directoryState) error {
	f, err := os.CreateTemp(m.root, "directory-")
	if err != nil {
		return err
	}
	defer func() { f.Close(); os.Remove(f.Name()) }()
	if err = json.NewEncoder(f).Encode(state); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), filepath.Join(m.root, locationFile))
}

// 只接管本次创建的专属子目录，或归属同一安装的已登记目录。
func (m *Manager) prepareDirectory(path string) error {
	dir := m.effectiveDirectory(path)
	unwritable := domain.Fail("INVALID_ARGUMENT", "The server cannot write to the selected download directory")
	if slices.Contains(m.directories.Known, path) && !m.ownsDirectory(path) {
		return domain.Fail("INVALID_ARGUMENT", "The previously selected download directory is unavailable")
	}
	err := os.Mkdir(dir, 0700)
	switch {
	case err == nil:
		if err = os.WriteFile(filepath.Join(dir, ownerMarker), []byte(m.directoryMarker(path)), 0600); err != nil {
			os.Remove(dir)
			return unwritable
		}
	case !os.IsExist(err):
		return unwritable
	case !m.ownsDirectory(path):
		return domain.Fail("INVALID_ARGUMENT", "The MusicFS-downloads subdirectory is already in use or cannot be verified")
	}
	check, err := os.CreateTemp(dir, "partial-")
	if err != nil {
		return unwritable
	}
	if err = check.Close(); err != nil {
		os.Remove(check.Name())
		return unwritable
	}
	if err = os.Remove(check.Name()); err != nil {
		return unwritable
	}
	return nil
}

func (m *Manager) directoryMarker(path string) string {
	sum := sha256.Sum256([]byte(m.ownerMarker + "\x00" + path))
	return hex.EncodeToString(sum[:])
}

func (m *Manager) ownsDirectory(base string) bool {
	dir := m.effectiveDirectory(base)
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return false
	}
	path := filepath.Join(dir, ownerMarker)
	info, err = os.Lstat(path)
	expected := m.directoryMarker(base)
	if err != nil || !info.Mode().IsRegular() || info.Size() != int64(len(expected)) {
		return false
	}
	raw, err := os.ReadFile(path)
	return err == nil && string(raw) == expected
}

// 移动硬盘可能在启动或播放期间离线。与单个文件消失区分，保留整盘的下载记录。
func (m *Manager) availableDirectory(path string) (string, error) {
	dir := m.effectiveDirectory(path)
	if m.validateDirectory != nil {
		if err := m.validateDirectory(dir); err != nil {
			return "", err
		}
	}
	if path != "" && !m.ownsDirectory(path) {
		return "", domain.Fail("TEMPORARY_FAILURE", "The online download directory is unavailable")
	}
	return dir, nil
}
