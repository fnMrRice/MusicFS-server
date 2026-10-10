package processedcache

import (
	"context"
	"sort"
)

// MakeRoom prefers a new input reservation over unreferenced reusable output.
// Active attempts and open readers are never evicted, even under pressure.
func (m *Manager) MakeRoom(ctx context.Context, required int64) {
	m.Collect(ctx)
	if required <= 0 || required > m.budget.Limit() {
		return
	}
	m.mu.Lock()
	available := m.budget.Limit() - m.budget.Used()
	if available < required {
		unused := make([]*entry, 0, len(m.entries))
		for _, e := range m.entries {
			if e.refs == 0 {
				unused = append(unused, e)
			}
		}
		sort.Slice(unused, func(i, j int) bool { return unused[i].lastRelease.Before(unused[j].lastRelease) })
		for _, e := range unused {
			e.invalid = true
			available += e.record.Size
			if available >= required {
				break
			}
		}
	}
	m.mu.Unlock()
	m.Collect(ctx)
}
