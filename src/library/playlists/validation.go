package playlists

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
)

func validText(s string, limit int) bool {
	return s != "" && len(s) <= limit && utf8.ValidString(s) && strings.TrimSpace(s) == s && !strings.ContainsFunc(s, unicode.IsControl)
}
func ValidID(s string) bool { return validText(s, 128) && !strings.ContainsFunc(s, unicode.IsSpace) }
func Invalid() error        { return domain.Fail("INVALID_ARGUMENT", "Invalid remote playlist parameters") }
func (r RemoteRef) Validate() error {
	if !ValidID(r.InstanceID) || !ValidID(r.ExternalID) || (r.AccountID != "" && !ValidID(r.AccountID)) {
		return Invalid()
	}
	return nil
}
func (b Binding) Validate() error {
	if err := b.RemoteRef.Validate(); err != nil {
		return err
	}
	if !ValidID(b.Platform) || !ValidID(b.ConfigRevision) {
		return Invalid()
	}
	return nil
}
func Digest(v any) (string, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func (o Observation) Validate(externalID string) error {
	if !o.Complete {
		return domain.Fail("TEMPORARY_FAILURE", "Remote playlist observation is incomplete; previous snapshot retained")
	}
	if o.Playlist.ExternalID != externalID || !validText(o.Playlist.Title, 4096) || o.Playlist.TrackCount == nil || *o.Playlist.TrackCount != int64(len(o.Entries)) || len(o.Entries) > MaxEntries || o.ObservedAt.IsZero() || o.ObservedAt.After(time.Now().Add(5*time.Minute)) {
		return Invalid()
	}
	if o.Playlist.SourceURL != "" {
		u, e := url.Parse(o.Playlist.SourceURL)
		if len(o.Playlist.SourceURL) > 4096 || e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Fragment != "" {
			return Invalid()
		}
	}
	if o.Consistency != online.Unversioned && o.Consistency != "versioned" {
		return Invalid()
	}
	if o.Consistency == "versioned" && (o.Version == nil || !validText(*o.Version, 256)) || o.Consistency == online.Unversioned && o.Version != nil {
		return Invalid()
	}
	for i, e := range o.Entries {
		if e.Position != i || !ValidID(e.ExternalTrackID) {
			return Invalid()
		}
	}
	return nil
}

// The host computes its own digest from the ordered metadata observation. It
// is never an audio asset version or a full-file hash.
func (o Observation) Digest() (string, error) {
	return Digest(struct {
		Playlist online.Playlist
		Entries  []online.PlaylistEntry
	}{o.Playlist, o.Entries})
}

func Page(snapshot Snapshot, offset, limit int) (SnapshotPage, error) {
	if offset < 0 || offset > len(snapshot.Entries) || limit < 1 || limit > MaxPageSize {
		return SnapshotPage{}, Invalid()
	}
	end := min(offset+limit, len(snapshot.Entries))
	page := SnapshotPage{PlaylistID: snapshot.PlaylistID, Revision: snapshot.Revision, BindingRevision: snapshot.BindingRevision, ObservationID: snapshot.ObservationID, Consistency: snapshot.Consistency, RemoteVersion: snapshot.RemoteVersion, ObservedAt: snapshot.ObservedAt, Total: len(snapshot.Entries), Items: append([]Entry{}, snapshot.Entries[offset:end]...)}
	if end < len(snapshot.Entries) {
		page.NextOffset = &end
	}
	return page, nil
}
