package candidates

import (
	"context"
	"fmt"
	"math/bits"
)

const maxOffsetItems = 24
const minimumOverlapItems = 50

func compareFingerprint(ctx context.Context, a, b []uint32, operations *int) (*FingerprintComparison, error) {
	minimum := max(minimumOverlapItems, (min(len(a), len(b))*4+4)/5)
	var best *FingerprintComparison
	for offset := -maxOffsetItems; offset <= maxOffsetItems; offset++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		first, last := max(0, -offset), min(len(a), len(b)-offset)
		count := last - first
		if count < minimum {
			continue
		}
		if count > MaxPopcounts-*operations {
			return nil, fmt.Errorf("RATE_LIMITED: candidate comparison budget exhausted")
		}
		*operations += count
		distance := 0
		for i := first; i < last; i++ {
			distance += bits.OnesCount32(a[i] ^ b[i+offset])
		}
		item := FingerprintComparison{BitErrors: distance, ComparedBits: count * 32, ComparedItems: count, OffsetItems: offset, BER: float64(distance) / float64(count*32)}
		// Compare ratios exactly and break ties by coverage, then smaller offset.
		if best == nil || item.BitErrors*best.ComparedBits < best.BitErrors*item.ComparedBits ||
			(item.BitErrors*best.ComparedBits == best.BitErrors*item.ComparedBits && (item.ComparedItems > best.ComparedItems ||
				(item.ComparedItems == best.ComparedItems && absolute(item.OffsetItems) < absolute(best.OffsetItems)))) {
			copy := item
			best = &copy
		}
	}
	return best, nil
}

func absolute(value int) int {
	if value < 0 {
		return -value
	}
	return value
}
