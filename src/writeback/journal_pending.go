package writeback

import (
	"context"
	"io"
	"sort"
	"strings"
)

// Pending performs a bounded-memory keyset listing. Unknown directory entries
// are never interpreted as writable sources or recovery objects.
func (j *FileJournal) Pending(ctx context.Context, after string, limit int) ([]Record, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if (after != "" && !jobID.MatchString(after)) || limit < 1 || limit > 128 {
		return nil, invalid("Invalid writeback recovery page")
	}
	directory, err := j.root.Open(".")
	if err != nil {
		return nil, err
	}
	defer directory.Close()
	result := []Record{}
	seen := 0
	for {
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		entries, readError := directory.ReadDir(128)
		for _, entry := range entries {
			seen++
			if seen > 100000 {
				return nil, unsupported("Writeback journal directory exceeds the recovery limit")
			}
			id, ok := strings.CutSuffix(entry.Name(), ".json")
			if !ok || entry.IsDir() || !jobID.MatchString(id) || id <= after {
				continue
			}
			r, err := j.load(ctx, id)
			if err != nil {
				return nil, err
			}
			if r.Stage == "committed" || r.Stage == "aborted" {
				continue
			}
			result = append(result, r)
			sort.Slice(result, func(i, k int) bool { return result[i].ID < result[k].ID })
			if len(result) > limit {
				result = result[:limit]
			}
		}
		if readError == io.EOF {
			return result, nil
		}
		if readError != nil {
			return nil, readError
		}
	}
}
