package bootstrap

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/fnMrRice/MusicFS-server/sdk/instanceid"
	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/writeback"
)

// A writeback can be committed in the database while its final journal update
// is still pending. Check the journal before any identity migration, so these
// interrupted writes remain recoverable by the previous server.
func checkLegacyWritebackJournals(ctx context.Context, dataRoot string) error {
	path := filepath.Join(dataRoot, "writeback")
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return domain.Fail("CONFLICT", "Writeback journal directory is invalid")
	}
	// The directory already exists; OpenJournal and Pending do not publish or
	// alter journal records. A missing journal directory is never created here.
	journal, err := writeback.OpenJournal(path)
	if err != nil {
		return err
	}
	defer journal.Close()
	after := ""
	for {
		records, err := journal.Pending(ctx, after, 128)
		if err != nil {
			return err
		}
		for _, record := range records {
			if instanceid.Canonical(record.Target.InstanceID) != record.Target.InstanceID {
				return domain.Fail("CONFLICT", "Legacy plugin instances have unfinished file writes; finish writeback recovery with the previous server before upgrading")
			}
		}
		if len(records) < 128 {
			return nil
		}
		after = records[len(records)-1].ID
	}
}
