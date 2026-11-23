package onlinecache

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
)

var cacheID = regexp.MustCompile("^[a-f0-9]{64}$")

type Manager struct {
	root    string
	repo    Repository
	mu      sync.Mutex
	records map[string]Record
	byKey   map[string]string
	pins    map[string]int
	garbage map[string]bool
	pending map[string]*downloadRequest
}

type downloadRequest struct {
	owner, source string
	done          chan struct{}
	deleted       map[string]bool
}

func New(ctx context.Context, root string, repo Repository) (*Manager, error) {
	if err := os.MkdirAll(root, 0700); err != nil {
		return nil, err
	}
	m := &Manager{root: root, repo: repo, records: map[string]Record{}, byKey: map[string]string{}, pins: map[string]int{}, garbage: map[string]bool{}, pending: map[string]*downloadRequest{}}
	records, err := repo.ListOnlineDownloads(ctx)
	if err != nil {
		return nil, err
	}
	valid := map[string]bool{}
	for _, r := range records {
		if !cacheID.MatchString(r.ID) || !cacheID.MatchString(r.Key) {
			return nil, errors.New("invalid online audio cache record")
		}
		name := r.ID + ".audio"
		info, err := os.Lstat(filepath.Join(root, name))
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		if err == nil && info.Mode().IsRegular() && info.Size() == r.SizeBytes && r.SizeBytes > 0 {
			valid[name] = true
			m.records[r.ID] = r
			m.byKey[r.Key] = r.ID
		} else if err := repo.ForgetOnlineDownload(ctx, r.target()); err != nil {
			return nil, err
		}
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		name := e.Name()
		if !valid[name] && !e.IsDir() && (strings.HasPrefix(name, "partial-") || strings.HasSuffix(name, ".audio")) {
			if err := os.Remove(filepath.Join(root, name)); err != nil {
				return nil, err
			}
		}
	}
	if err = m.trimDue(ctx, time.Now()); err != nil {
		return nil, err
	}
	return m, nil
}

// Trim 按用户和插件分别计算所有实例占用。正在被读取的副本留到下次清理。
func (m *Manager) Trim(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id := range m.garbage {
		m.collect(id)
	}
	type group struct{ owner, plugin string }
	groups := map[group][]Record{}
	for _, r := range m.records {
		g := group{r.Owner, r.Plugin}
		groups[g] = append(groups[g], r)
	}
	for g, rs := range groups {
		p, err := ReadPreferences(ctx, m.repo, g.owner, g.plugin)
		if err != nil {
			return err
		}
		sort.Slice(rs, func(i, j int) bool {
			if rs[i].DownloadedAt.Equal(rs[j].DownloadedAt) {
				return rs[i].ID < rs[j].ID
			}
			return rs[i].DownloadedAt.Before(rs[j].DownloadedAt)
		})
		var total int64
		for _, r := range rs {
			total += r.SizeBytes
		}
		for _, r := range rs {
			if total <= p.LimitBytes {
				break
			}
			if m.pins[r.ID] > 0 {
				continue
			}
			if err = m.remove(ctx, r); err != nil {
				return err
			}
			total -= r.SizeBytes
		}
	}
	return nil
}

func (m *Manager) Run(ctx context.Context) {
	for {
		now := time.Now()
		next := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, now.Location())
		timer := time.NewTimer(time.Until(next))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case now = <-timer.C:
			if err := m.trimDue(ctx, now); err != nil && ctx.Err() == nil {
				slog.ErrorContext(ctx, "Online audio cache cleanup failed", "error", err)
			}
		}
	}
}

// 记录服务器日期，使重启补做错过的清理，而且同一天只执行一次。
func (m *Manager) trimDue(ctx context.Context, now time.Time) error {
	path := filepath.Join(m.root, "last-cleanup")
	raw, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	today := now.Format(time.DateOnly)
	if string(raw) == today {
		return nil
	}
	if err = m.Trim(ctx); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(today), 0600)
}

func (m *Manager) remove(ctx context.Context, r Record) error {
	if err := m.repo.ForgetOnlineDownload(ctx, r.target()); err != nil {
		return err
	}
	delete(m.records, r.ID)
	if m.byKey[r.Key] == r.ID {
		delete(m.byKey, r.Key)
	}
	m.garbage[r.ID] = true
	m.collect(r.ID)
	return nil
}

// Delete 撤销指定用户来源下的单份版本。读者可消费已打开的副本，退出后才回收文件。
func (m *Manager) Delete(ctx context.Context, owner, source, id string) error {
	if owner == "" || source == "" || len(source) > 128 || !cacheID.MatchString(id) {
		return domain.Fail("INVALID_ARGUMENT", "Invalid online download identity")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.records[id]
	if !ok || r.Owner != owner || r.Source != source {
		return domain.Fail("NOT_FOUND", "Online download not found")
	}
	if err := m.remove(ctx, r); err != nil {
		return err
	}
	// 已在传输的相同正文继续供播放，不能在删除成功后重新登记。
	for _, request := range m.pending {
		if request.owner == owner && request.source == source {
			if request.deleted == nil {
				request.deleted = map[string]bool{}
			}
			request.deleted[id] = true
		}
	}
	return nil
}

func (m *Manager) collect(id string) {
	if !m.garbage[id] || m.pins[id] > 0 {
		return
	}
	if err := os.Remove(filepath.Join(m.root, id+".audio")); err != nil && !os.IsNotExist(err) {
		slog.Warn("Online audio cache file cleanup deferred", "versionId", id, "error", err)
		return
	}
	delete(m.garbage, id)
}

type readHandle struct {
	io.ReadCloser
	once    sync.Once
	release func()
}

// 调用方持有 mu，丢失或截断的副本视为未命中，允许重新下载。
func (m *Manager) lookup(ctx context.Context, key string) (io.ReadCloser, error) {
	r, ok := m.records[m.byKey[key]]
	if !ok {
		return nil, nil
	}
	body, err := m.pin(r)
	if err == nil || !os.IsNotExist(err) && !errors.Is(err, errDamaged) {
		return body, err
	}
	return nil, m.remove(ctx, r)
}

func (r *readHandle) Close() (err error) {
	r.once.Do(func() { err = r.ReadCloser.Close(); r.release() })
	return err
}

// 调用方持有 mu。打开的文件被清理任务计为占用中，关闭后才可回收。
func (m *Manager) pin(r Record) (io.ReadCloser, error) {
	if err := m.checkFile(r); err != nil {
		return nil, err
	}
	f, err := os.Open(filepath.Join(m.root, r.ID+".audio"))
	if err != nil {
		return nil, err
	}
	m.pins[r.ID]++
	return &readHandle{ReadCloser: f, release: func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		m.pins[r.ID]--
		if m.pins[r.ID] == 0 {
			delete(m.pins, r.ID)
			m.collect(r.ID)
		}
	}}, nil
}

var errDamaged = errors.New("online audio cache file changed")

func (m *Manager) checkFile(r Record) error {
	info, err := os.Lstat(filepath.Join(m.root, r.ID+".audio"))
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() != r.SizeBytes {
		return errDamaged
	}
	return nil
}
