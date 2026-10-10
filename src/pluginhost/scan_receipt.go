package pluginhost

import (
	"fmt"
	"slices"

	"github.com/fnMrRice/MusicFS-server/src/pluginproto"
)

type scanReceipt struct {
	page     pluginproto.EntriesPage
	excluded []string
}

func (w *Worker) validateScanReceipt(grant string, page *pluginproto.ScanPage) error {
	w.mu.Lock()
	receipt := w.scanGrants[grant].receipt
	w.mu.Unlock()
	if receipt == nil || page.RootIdentity != receipt.page.RootIdentity || page.NextCursor != receipt.page.NextCursor || len(page.Items) != len(receipt.page.Items) || !slices.Equal(page.Issues, receipt.page.Issues) || page.Complete != (receipt.page.NextCursor == "" && len(receipt.page.Issues) == 0) {
		return fmt.Errorf("PROTOCOL_MISMATCH: scan result does not match host snapshot")
	}
	entries := make(map[string]pluginproto.Entry, len(receipt.page.Items))
	for _, entry := range receipt.page.Items {
		entries[entry.RelativePath] = entry
	}
	for _, observation := range page.Items {
		entry, ok := entries[observation.RelativePath]
		if !ok || observation.RootID != entry.RootID || observation.ObservedVersion != entry.ObservedVersion || observation.SizeBytes != entry.SizeBytes {
			return fmt.Errorf("ACCESS_DENIED: observation is outside host snapshot")
		}
		delete(entries, observation.RelativePath)
	}
	page.ExcludedPaths = append([]string{}, receipt.excluded...)
	return nil
}
