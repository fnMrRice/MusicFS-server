package domain_test

import (
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"testing"
)

func TestIdentityIsOpaqueAndIndependent(t *testing.T) {
	seen := map[string]bool{}
	for range 1000 {
		id := domain.NewID()
		if len(id) != 48 || seen[id] {
			t.Fatalf("invalid or repeated identity: %q", id)
		}
		seen[id] = true
	}
}
