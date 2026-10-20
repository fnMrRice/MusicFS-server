package processedcache_test

import (
	"strings"
	"testing"
)

func TestDSPChainIdentitySeparatesCompletedOutput(t *testing.T) {
	m, _, repo, _, _ := setup(t, 1024)
	first := identity()
	first.PipelineRevision = "dsp-v1:" + strings.Repeat("a", 64)
	pin := complete(t, m, repo, first, "processed-dsp-output")
	defer pin.Close()
	lease := m.Acquire(first)
	if lease == nil {
		t.Fatal("verified DSP chain output was not reusable")
	}
	lease.Close()
	for _, revision := range []string{"gain-v1", "dsp-v1:" + strings.Repeat("b", 64)} {
		candidate := first
		candidate.PipelineRevision = revision
		if got := m.Acquire(candidate); got != nil {
			got.Close()
			t.Fatal("different DSP chain reused completed output")
		}
	}
	for _, revision := range []string{"", "dsp-v1:", "dsp-v1:" + strings.Repeat("A", 64), "dsp-v1:" + strings.Repeat("g", 64), "dsp-v2:" + strings.Repeat("a", 64)} {
		candidate := first
		candidate.PipelineRevision = revision
		if _, err := candidate.Key(); err == nil {
			t.Fatalf("invalid chain identity accepted: %q", revision)
		}
	}
}
