//go:build windows

package resources_test

import (
	"testing"
	"time"
)

func TestResourceProcessTreeRejectsReusedParentPID(t *testing.T) {
	start := time.Now()
	input := map[uint32]processSample{
		1: {PID: 1, CreatedAt: start},
		2: {PID: 2, ParentPID: 1, CreatedAt: start.Add(time.Second)},
		3: {PID: 3, ParentPID: 2, CreatedAt: start.Add(-time.Hour)},
		4: {PID: 4, ParentPID: 3, CreatedAt: start.Add(2 * time.Second)},
		5: {PID: 5, ParentPID: 2, CreatedAt: start.Add(2 * time.Second)},
	}
	actual := verifiedTree(1, input)
	if len(actual) != 3 {
		t.Fatalf("included an unrelated orphan: %+v", actual)
	}
	for _, process := range actual {
		if process.PID == 3 || process.PID == 4 {
			t.Fatal("reused PID was treated as a parent")
		}
	}
}
