package processedcache

import (
	"errors"
	"math"
	"sync"
)

// Budget includes reservations, complete files and files whose deletion failed.
// It has no callbacks and may be held below the playback or cache mutex.
type Budget struct {
	mu          sync.Mutex
	limit, used int64
}

func NewBudget(limit int64) (*Budget, error) {
	if limit <= 0 {
		return nil, errors.New("cache budget must be positive")
	}
	return &Budget{limit: limit}, nil
}
func (b *Budget) TryAcquire(size int64) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if size < 0 || b.used > b.limit || size > b.limit-b.used {
		return false
	}
	b.used += size
	return true
}
func (b *Budget) Release(size int64) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if size < 0 || size > b.used {
		return false
	}
	b.used -= size
	return true
}
func (b *Budget) Used() int64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.used
}
func (b *Budget) Limit() int64 { return b.limit }

// Adopt accounts for existing files before startup cleanup, even if a reduced
// configured limit makes the old cache temporarily exceed its new budget.
func (b *Budget) adopt(size int64) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if size < 0 || size > math.MaxInt64-b.used {
		return errors.New("cache accounting overflow")
	}
	b.used += size
	return nil
}
