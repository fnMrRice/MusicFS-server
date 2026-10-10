package timeline

import (
	"time"

	"github.com/fnMrRice/MusicFS-server/src/domain"
)

// Budget lives as long as the playback session. Taking a slot before work
// begins makes failed preparation count too. Seeking never resets this state.
type Budget struct {
	StartedAt  time.Time
	SameSource int
	Targets    map[string]bool
}

func (b *Budget) Take(now time.Time, target string, sameSource bool) error {
	if !b.StartedAt.IsZero() && !now.Before(b.StartedAt.Add(time.Minute)) {
		return domain.Fail("RECOVERY_EXHAUSTED", "Recovery chain deadline elapsed; restart playback")
	}
	if sameSource {
		if b.SameSource >= 2 {
			return domain.Fail("RECOVERY_EXHAUSTED", "Same-source recovery budget exhausted; restart playback")
		}
	} else if b.Targets[target] || len(b.Targets) >= 3 {
		return domain.Fail("RECOVERY_EXHAUSTED", "Recovery target budget exhausted; restart playback")
	}
	if b.StartedAt.IsZero() {
		b.StartedAt = now
	}
	if sameSource {
		b.SameSource++
	} else {
		if b.Targets == nil {
			b.Targets = map[string]bool{}
		}
		b.Targets[target] = true
	}
	return nil
}
func (b Budget) Deadline() time.Time { return b.StartedAt.Add(time.Minute) }
