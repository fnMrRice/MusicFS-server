package scanwatch

import "time"

// pending stores one coalesced hint, never one record per filesystem event.
type pending struct {
	dirty, full        bool
	first, due         time.Time
	debounce, maxDelay time.Duration
}

func (p *pending) hint(now time.Time) {
	if !p.dirty {
		p.first = now
	}
	p.dirty = true
	if p.full {
		return
	}
	p.due = now.Add(p.debounce)
	if deadline := p.first.Add(p.maxDelay); deadline.Before(p.due) {
		p.due = deadline
	}
}

func (p *pending) reconcile(now time.Time) {
	p.dirty, p.full, p.first, p.due = true, true, now, now
}

func (p *pending) take(now time.Time) string {
	if !p.dirty || now.Before(p.due) {
		return ""
	}
	mode := "incremental"
	if p.full {
		mode = "full"
	}
	p.dirty, p.full = false, false
	return mode
}
