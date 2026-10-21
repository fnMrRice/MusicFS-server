package playlists

import (
	"time"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
)

// Reconcile retains each occurrence and records the independent observations
// used to confirm a disappearance. Failure never mutates the prior snapshot.
func Reconcile(previous Snapshot, o Observation, next Snapshot, sources map[string]RemoteSource) (Snapshot, error) {
	if err := o.Validate(o.Playlist.ExternalID); err != nil {
		return Snapshot{}, err
	}
	if previous.Revision > 0 && !o.ObservedAt.After(previous.ObservedAt) {
		return Snapshot{}, domain.Fail("CONFLICT", "Remote observation is not newer than the published snapshot")
	}
	digest, err := o.Digest()
	if err != nil {
		return Snapshot{}, err
	}
	if previous.RemoteVersion != nil && o.Version != nil && *previous.RemoteVersion == *o.Version && previous.MetadataDigest != digest {
		return Snapshot{}, domain.Fail("RESOURCE_CHANGED", "A fixed remote snapshot changed during observation")
	}
	next.Consistency, next.RemoteVersion, next.MetadataDigest = o.Consistency, o.Version, digest
	next.Metadata = o.Playlist
	next.ObservedAt, next.CreatedAt = o.ObservedAt.UTC(), time.Now().UTC()
	next.Entries, next.Removals = []Entry{}, []Removal{}
	available := map[string][]Entry{}
	for _, entry := range previous.Entries {
		available[entry.Source.ExternalID] = append(available[entry.Source.ExternalID], entry)
	}
	used := map[string]bool{}
	for _, observed := range o.Entries {
		source, ok := sources[observed.ExternalTrackID]
		if !ok {
			return Snapshot{}, Invalid()
		}
		entry := Entry{ID: domain.NewID(), Source: source}
		prior := available[observed.ExternalTrackID]
		if len(prior) > 0 {
			entry.ID = prior[0].ID
			available[observed.ExternalTrackID] = prior[1:]
		}
		entry.Position, entry.LastObservedPosition, entry.State = observed.Position, observed.Position, "present"
		next.Entries = append(next.Entries, entry)
		used[entry.ID] = true
	}
	for _, entry := range previous.Entries {
		if used[entry.ID] {
			continue
		}
		if previous.BindingRevision != next.BindingRevision {
			entry.FirstMissingObservation = nil
		}
		if o.Consistency == "versioned" || entry.FirstMissingObservation != nil {
			first := next.ObservationID
			if entry.FirstMissingObservation != nil {
				first = *entry.FirstMissingObservation
			}
			next.Removals = append(next.Removals, Removal{EntryID: entry.ID, RemoteSourceID: entry.Source.ID, FirstMissingObservation: first, ConfirmedObservation: next.ObservationID})
			continue
		}
		entry.State = "suspectedRemoved"
		entry.Position = len(next.Entries)
		first := next.ObservationID
		entry.FirstMissingObservation = &first
		next.Entries = append(next.Entries, entry)
	}
	if len(next.Entries) > MaxSnapshotEntries {
		return Snapshot{}, domain.Fail("RATE_LIMITED", "Remote snapshot exceeds its limit")
	}
	return next, nil
}
