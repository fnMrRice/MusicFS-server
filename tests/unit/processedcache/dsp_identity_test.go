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

func TestChannelProfilesCannotReuseAnotherLayoutsCompletedOutput(t *testing.T) {
	m, _, repo, _, _ := setup(t, 4096)
	for _, profile := range []string{"mp3-128", "opus-5.1", "opus-7.1"} {
		current := identity()
		current.Profile = profile
		pin := complete(t, m, repo, current, "output-"+profile)
		defer pin.Close()
		for _, other := range []string{"mp3-128", "opus-5.1", "opus-7.1"} {
			if other == profile {
				continue
			}
			candidate := current
			candidate.Profile = other
			key, err := candidate.Key()
			currentKey, currentErr := current.Key()
			if err != nil || currentErr != nil || key == currentKey {
				t.Fatal("output layouts share a cache identity", profile, other)
			}
		}
	}
	invalid := identity()
	invalid.Profile = "opus-unknown"
	if _, err := invalid.Key(); err == nil {
		t.Fatal("undefined cache output profile accepted")
	}
}
