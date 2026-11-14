package playlists

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

func (s SourceRef) Key() string {
	if s.TrackID != nil {
		if s.ForcedSourceID != nil {
			return "track:" + *s.TrackID + "\x00forced:" + *s.ForcedSourceID
		}
		return "track:" + *s.TrackID
	}
	if s.SourceTrackID != nil {
		return "local:" + *s.SourceTrackID
	}
	if s.RemoteSourceID != nil {
		return "remote:" + *s.RemoteSourceID
	}
	return ""
}
func (s SourceRef) Validate() error {
	if s.TrackID != nil {
		if !ValidID(*s.TrackID) || s.SourceTrackID != nil || s.RemoteSourceID != nil ||
			s.ForcedSourceID != nil && !ValidID(*s.ForcedSourceID) {
			return Invalid()
		}
		return nil
	}
	if s.ForcedSourceID != nil {
		return Invalid()
	}
	if (s.SourceTrackID == nil) == (s.RemoteSourceID == nil) {
		return Invalid()
	}
	if s.SourceTrackID != nil && !ValidID(*s.SourceTrackID) || s.RemoteSourceID != nil && !ValidID(*s.RemoteSourceID) {
		return Invalid()
	}
	return nil
}
func PlaylistName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || !utf8.ValidString(name) || utf8.RuneCountInString(name) > 256 || strings.ContainsFunc(name, unicode.IsControl) {
		return "", Invalid()
	}
	return name, nil
}
func ValidateBindings(bindings []PlaylistBinding) error {
	if len(bindings) > MaxBindings {
		return Invalid()
	}
	seen := map[string]bool{}
	entries := map[string]bool{}
	count := 0
	for _, b := range bindings {
		if b.ID != "" {
			if !ValidID(b.ID) || seen[b.ID] {
				return Invalid()
			}
			seen[b.ID] = true
		}
		switch b.Kind {
		case "local":
			if b.TargetID != nil {
				return Invalid()
			}
			for _, e := range b.Entries {
				count++
				if count > MaxEntries {
					return Invalid()
				}
				if e.ID != "" {
					if !ValidID(e.ID) || entries[e.ID] {
						return Invalid()
					}
					entries[e.ID] = true
				}
				if err := e.Source.Validate(); err != nil {
					return err
				}
			}
		case "remote", "playlist":
			if b.TargetID == nil || !ValidID(*b.TargetID) || len(b.Entries) != 0 {
				return Invalid()
			}
		default:
			return Invalid()
		}
	}
	return nil
}
func (r HideRequest) Validate() error {
	if r.ExpectedRevision < 0 {
		return Invalid()
	}
	if err := r.Source.Validate(); err != nil {
		return err
	}
	if len(r.BindingPath) > MaxGraphDepth {
		return Invalid()
	}
	for _, id := range r.BindingPath {
		if !ValidID(id) {
			return Invalid()
		}
	}
	switch r.Scope {
	case "allSources":
		if len(r.BindingPath) != 0 || r.EntryID != nil {
			return Invalid()
		}
	case "binding":
		if len(r.BindingPath) == 0 || r.EntryID != nil {
			return Invalid()
		}
	case "entry":
		if len(r.BindingPath) == 0 || r.EntryID == nil || !ValidID(*r.EntryID) {
			return Invalid()
		}
	default:
		return Invalid()
	}
	return nil
}
func AggregateSnapshotPage(s AggregateSnapshot, offset, limit int) (AggregatePage, error) {
	visible := []AggregateEntry{}
	for _, e := range s.Entries {
		if !e.Hidden {
			visible = append(visible, e)
		}
	}
	if offset < 0 || offset > len(visible) || limit < 1 || limit > MaxPageSize {
		return AggregatePage{}, Invalid()
	}
	end := min(offset+limit, len(visible))
	p := AggregatePage{PlaylistID: s.PlaylistID, Revision: s.Revision, RuleRevision: s.RuleRevision, HideRevision: s.HideRevision, MatchingRevision: s.MatchingRevision, Total: len(visible), Items: append([]AggregateEntry{}, visible[offset:end]...), NeedsConfirmation: append([]string{}, s.NeedsConfirmation...)}
	if end < len(visible) {
		p.NextOffset = &end
	}
	return p, nil
}
