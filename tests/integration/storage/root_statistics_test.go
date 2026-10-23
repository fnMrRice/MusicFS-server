package storage_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestRootStatisticsCountsPublishedAvailableFilesWithinOwnerAndProvider(t *testing.T) {
	ctx := context.Background()
	store := evidenceStore(t)
	assertStatistics := func(owner, provider string, want []library.RootStatistic) {
		t.Helper()
		result, err := store.RootStatistics(ctx, owner, provider)
		if err != nil || !reflect.DeepEqual(result.Roots, want) {
			t.Fatalf("root statistics = %+v, want %+v: %v", result.Roots, want, err)
		}
	}
	assertStatistics("admin", "local", []library.RootStatistic{})
	observation := func(id, root, mime string, size int64) library.Observation {
		return library.Observation{Observation: pluginproto.Observation{ExternalID: id, RootID: root,
			RelativePath: "private/" + id, ObservedVersion: "v1", DisplayName: "Same recording",
			MIME: mime, SizeBytes: size}, Artists: []string{}}
	}
	a := observation("first.ncm", "a", "application/octet-stream", 3<<30)
	b := observation("second.flac", "a", "audio/flac", 18)
	c := observation("third.wav", "b", "audio/wav", 25)
	plan := testkit.ScanPlan("a", "b")
	publishScope(t, store, plan, a, b, c)
	want := []library.RootStatistic{{RootID: "a", TrackCount: 2, SizeBytes: (3 << 30) + 18}, {RootID: "b", TrackCount: 1, SizeBytes: 25}}
	assertStatistics("admin", "local", want)
	// 相同根编号的其他用户和其他来源不得混入当前统计。
	for _, scope := range [][2]string{{"other", "local"}, {"admin", "remote"}} {
		if _, _, err := store.Enqueue(ctx, scope[0], scope[1], testkit.ScanPlan("a"), domain.NewID()); err != nil {
			t.Fatal(err)
		}
		job, err := store.Claim(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err := store.Publish(ctx, job, testkit.ScanPublication(job, []library.Observation{a})); err != nil {
			t.Fatal(err)
		}
		if err := store.Finish(ctx, job, 1, nil); err != nil {
			t.Fatal(err)
		}
	}
	assertStatistics("admin", "local", want)
	assertStatistics("other", "local", []library.RootStatistic{{RootID: "a", TrackCount: 1, SizeBytes: 3 << 30}})
	assertStatistics("admin", "remote", []library.RootStatistic{{RootID: "a", TrackCount: 1, SizeBytes: 3 << 30}})
	// 未发布的新一轮进度和扫描失败都保留旧统计。
	if _, _, err := store.Enqueue(ctx, "admin", "local", plan, domain.NewID()); err != nil {
		t.Fatal(err)
	}
	active, err := store.Claim(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateProgress(ctx, active, 100); err != nil {
		t.Fatal(err)
	}
	assertStatistics("admin", "local", want)
	if err := store.Finish(ctx, active, 100, errors.New("Scan interrupted")); err != nil {
		t.Fatal(err)
	}
	assertStatistics("admin", "local", want)
	// 新发布把遗漏来源标记为 missing，统计不再计入它们。
	publishScope(t, store, plan, a)
	assertStatistics("admin", "local", []library.RootStatistic{{RootID: "a", TrackCount: 1, SizeBytes: 3 << 30}})
	publishScope(t, store, plan)
	assertStatistics("admin", "local", []library.RootStatistic{})
}
