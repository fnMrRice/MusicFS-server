package processedcache

import (
	"os"
	"sync"
	"sync/atomic"
)

type Pin struct {
	manager *Manager
	entry   *entry
	once    sync.Once
}

func (p *Pin) Close() {
	if p == nil {
		return
	}
	p.once.Do(func() {
		p.manager.mu.Lock()
		defer p.manager.mu.Unlock()
		p.entry.refs--
		if p.entry.refs == 0 {
			p.entry.lastRelease = p.manager.now()
		}
	})
}

type Lease struct {
	File   *os.File
	Record Record
	pin    *Pin
	once   sync.Once
	closed atomic.Bool
}

func (l *Lease) Close() {
	if l == nil {
		return
	}
	l.once.Do(func() { l.closed.Store(true); l.File.Close(); l.pin.Close() })
}
func (l *Lease) Pin() *Pin {
	l.pin.manager.mu.Lock()
	defer l.pin.manager.mu.Unlock()
	if l.closed.Load() {
		return nil
	}
	l.pin.entry.refs++
	return &Pin{manager: l.pin.manager, entry: l.pin.entry}
}

// Acquire supplies bytes, not authorization. The caller must recheck source,
// authentication and current attempt before serving any returned data.
func (m *Manager) Acquire(identity Identity) *Lease {
	key, err := identity.Key()
	if err != nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	e := m.entries[key]
	if m.ctx.Err() != nil || e == nil || e.invalid || m.now().Sub(e.record.CreatedAt) >= m.config.HardTTL {
		return nil
	}
	info, err := os.Lstat(m.path(e.record.ID, e.record.Identity.Profile))
	if err != nil || !info.Mode().IsRegular() || info.Size() != e.record.Size || !os.SameFile(info, e.info) || !info.ModTime().Equal(e.info.ModTime()) {
		e.invalid = true
		return nil
	}
	f, err := os.Open(m.path(e.record.ID, e.record.Identity.Profile))
	if err != nil {
		return nil
	}
	opened, err := f.Stat()
	if err != nil || !os.SameFile(info, opened) || opened.Size() != e.record.Size || !opened.ModTime().Equal(info.ModTime()) {
		f.Close()
		e.invalid = true
		return nil
	}
	e.refs++
	e.record.LastUsedAt = m.now()
	return &Lease{File: f, Record: e.record, pin: &Pin{manager: m, entry: e}}
}
