package timeline_test

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/playback/timeline"
)

func TestDirectedManualAlignmentAndStrictProfile(t *testing.T) {
	r := timeline.Request{RecordingID: "recording", From: timeline.Target{SourceTrackID: "a", AssetID: "asset-a", Version: "v1"}, To: timeline.Target{SourceTrackID: "b", AssetID: "asset-b", Version: "v2"}, Range: timeline.Range{StartMs: 0, EndMs: 6000}, OffsetMs: 1000, Profile: timeline.Profile{OutputProfile: "mp3-128", ProcessingRevision: "gain-v1", DecoderSHA256: strings.Repeat("a", 64)}}
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
	beyond := r
	beyond.OffsetMs = 0
	beyond.Range.EndMs = 24*60*60*1000 + 1
	if err := beyond.Validate(); err == nil {
		t.Fatal("alignment exceeded the actual processing seek limit")
	}
	e := timeline.Evidence{ID: "evidence", Request: r, Origin: "manualAlignment", Revision: 1}
	c := timeline.Check{From: r.From, To: r.To, PositionMs: 4000, Profile: r.Profile}
	if got, err := e.Map(c); err != nil || got != 5000 {
		t.Fatalf("mapped position: %d %v", got, err)
	}
	for _, mutate := range []func(*timeline.Check){func(c *timeline.Check) { c.PositionMs = 6000 }, func(c *timeline.Check) { c.From, c.To = c.To, c.From }, func(c *timeline.Check) { c.To.Version = "changed" }, func(c *timeline.Check) { c.Profile.GainDB = -6 }, func(c *timeline.Check) { c.Profile.DecoderSHA256 = strings.Repeat("b", 64) }} {
		bad := c
		mutate(&bad)
		if _, err := e.Map(bad); err == nil {
			t.Fatal("uncovered or different input/profile accepted")
		}
	}
	now := time.Now()
	e.RevokedAt = &now
	if _, err := e.Map(c); err == nil {
		t.Fatal("revoked evidence accepted")
	}
	for _, mutate := range []func(*timeline.Request){func(r *timeline.Request) { r.OffsetMs = -1 }, func(r *timeline.Request) { r.Range.EndMs = math.MaxInt64 }, func(r *timeline.Request) { r.OffsetMs = math.MaxInt64 }, func(r *timeline.Request) { r.Profile.GainDB = math.NaN() }, func(r *timeline.Request) { r.Profile.DecoderSHA256 = "" }} {
		bad := r
		mutate(&bad)
		if err := bad.Validate(); err == nil {
			t.Fatal("invalid alignment accepted")
		}
	}
}
func TestRecoveryBudgetCountsFailuresAndDoesNotReset(t *testing.T) {
	var b timeline.Budget
	now := time.Now()
	for _, id := range []string{"a", "b", "c"} {
		if err := b.Take(now, id, false); err != nil {
			t.Fatal(err)
		}
	}
	if err := b.Take(now, "a", false); err == nil {
		t.Fatal("duplicate target retried")
	}
	if err := b.Take(now, "d", false); err == nil {
		t.Fatal("fourth target admitted")
	}
	if err := b.Take(now, "current", true); err != nil {
		t.Fatal(err)
	}
	if err := b.Take(now, "current", true); err != nil {
		t.Fatal(err)
	}
	if err := b.Take(now, "current", true); err == nil {
		t.Fatal("third same-source retry admitted")
	}
	var timed timeline.Budget
	if err := timed.Take(now, "a", false); err != nil {
		t.Fatal(err)
	}
	if err := timed.Take(now.Add(time.Minute), "b", false); err == nil {
		t.Fatal("chain deadline reset")
	}
}
