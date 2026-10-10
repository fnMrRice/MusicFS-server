package storage_test

import (
	"context"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/domain"
)

func TestRemotePlaylistReconfigurationDoesNotCombineRemovalEvidence(t *testing.T) {
	s := evidenceStore(t)
	ctx := context.Background()
	p := remoteImport(t, s, "owner", remoteBinding())
	remotePublish(t, s, p, remoteObservation(1000, "a", "b"))
	firstAbsence := remotePublish(t, s, p, remoteObservation(1001, "a"))
	b := remoteBinding()
	b.ConfigRevision = "config-v2"
	if _, _, err := s.QueueRemoteSync(ctx, "owner", p.ID, 1, b, domain.NewID()); err != nil {
		t.Fatal(err)
	}
	j, err := s.ClaimRemoteSync(ctx)
	if err != nil {
		t.Fatal(err)
	}
	next, err := s.FinishRemoteSync(ctx, j, remoteObservation(1002, "a"))
	if err != nil || len(next.Entries) != 2 || len(next.Removals) != 0 || next.Entries[1].FirstMissingObservation == nil || *next.Entries[1].FirstMissingObservation == firstAbsence.ObservationID {
		t.Fatal("different binding revisions combined removal proof", err)
	}
	p, err = s.RemotePlaylist(ctx, "owner", p.ID)
	if err != nil {
		t.Fatal(err)
	}
	confirmed := remotePublish(t, s, p, remoteObservation(1003, "a"))
	if len(confirmed.Entries) != 1 || len(confirmed.Removals) != 1 || confirmed.Removals[0].FirstMissingObservation != next.ObservationID {
		t.Fatal("same-binding confirmation could not finish removal")
	}
}
