package timeline

import (
	"encoding/hex"
	"math"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/fnMrRice/MusicFS-server/src/domain"
)

const MaxPositionMs int64 = 24 * 60 * 60 * 1000

func identity(value string, limit int) bool {
	return value != "" && len(value) <= limit && utf8.ValidString(value) && !strings.ContainsFunc(value, unicode.IsControl)
}
func (t Target) Validate() error {
	if !identity(t.SourceTrackID, 128) || !identity(t.AssetID, 128) || !identity(t.Version, 1024) {
		return domain.Fail("INVALID_ARGUMENT", "Invalid timeline asset target")
	}
	return nil
}
func (p Profile) Validate() error {
	decoded, err := hex.DecodeString(p.DecoderSHA256)
	if p.OutputProfile != "mp3-128" || p.ProcessingRevision != "gain-v1" || err != nil || len(decoded) != 32 || p.DecoderSHA256 != strings.ToLower(p.DecoderSHA256) || math.IsNaN(p.GainDB) || math.IsInf(p.GainDB, 0) || p.GainDB < -60 || p.GainDB > 0 {
		return domain.Fail("INVALID_ARGUMENT", "Unsupported timeline processing profile")
	}
	return nil
}
func (r Request) Validate() error {
	if !identity(r.RecordingID, 128) || r.From.SourceTrackID == r.To.SourceTrackID || r.Range.StartMs < 0 || r.Range.EndMs <= r.Range.StartMs || r.Range.EndMs > MaxPositionMs || r.OffsetMs < -MaxPositionMs || r.OffsetMs > MaxPositionMs || r.Range.StartMs+r.OffsetMs < 0 || r.Range.EndMs+r.OffsetMs > MaxPositionMs {
		return domain.Fail("INVALID_ARGUMENT", "Invalid manual timeline alignment")
	}
	if err := r.From.Validate(); err != nil {
		return err
	}
	if err := r.To.Validate(); err != nil {
		return err
	}
	return r.Profile.Validate()
}
func (e Evidence) Map(c Check) (int64, error) {
	if e.RevokedAt != nil || e.Origin != "manualAlignment" || e.Revision != 1 || e.From != c.From || e.To != c.To || e.Profile != c.Profile || c.PositionMs < e.Range.StartMs || c.PositionMs >= e.Range.EndMs {
		return 0, domain.Fail("RESTART_REQUIRED", "No applicable timeline evidence covers this position")
	}
	if err := e.Request.Validate(); err != nil {
		return 0, err
	}
	return c.PositionMs + e.OffsetMs, nil
}
