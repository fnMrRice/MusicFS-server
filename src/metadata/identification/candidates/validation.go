package candidates

import (
	"encoding/hex"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/fnMrRice/MusicFS-server/src/metadata/identification/fingerprint"
)

func textValid(value string, limit int, required bool) bool {
	return (!required || value != "") && len(value) <= limit && utf8.ValidString(value)
}

func validateAsset(a AssetRef) bool {
	return textValid(a.SourceTrackID, 128, true) && textValid(a.AssetID, 128, true) && textValid(a.AssetVersion, 1024, true)
}

func validateInput(in Input) error {
	if !validateAsset(in.Asset) || !textValid(in.Title, 4096, false) || len(in.Artists) > 32 || (in.DurationMs != nil && *in.DurationMs < 0) {
		return fmt.Errorf("INVALID_ARGUMENT: invalid candidate input")
	}
	for _, artist := range in.Artists {
		if !textValid(artist, 1024, false) {
			return fmt.Errorf("INVALID_ARGUMENT: invalid candidate artist")
		}
	}
	return nil
}

func currentHash(in Input) (*VerifiedHash, error) {
	h := in.FileHash
	if h == nil || h.Asset != in.Asset {
		return nil, nil
	}
	if !textValid(h.EvidenceID, 128, true) || h.FileSizeBytes < 0 || len(h.SHA512) != 128 || strings.ToLower(h.SHA512) != h.SHA512 {
		return nil, fmt.Errorf("INVALID_ARGUMENT: invalid verified file hash evidence")
	}
	if decoded, err := hex.DecodeString(h.SHA512); err != nil || len(decoded) != 64 {
		return nil, fmt.Errorf("INVALID_ARGUMENT: invalid verified file hash evidence")
	}
	return h, nil
}

func currentFingerprint(in Input) (*FingerprintEvidence, error) {
	f := in.Fingerprint
	if f == nil || f.Asset != in.Asset {
		return nil, nil
	}
	if !textValid(f.EvidenceID, 128, true) {
		return nil, fmt.Errorf("INVALID_ARGUMENT: invalid fingerprint evidence identity")
	}
	if _, err := f.Detail.Encode(); err != nil {
		return nil, err
	}
	if f.Detail.Quality.Status != "available" {
		return nil, nil
	}
	return f, nil
}

func compatible(a, b fingerprint.Detail) bool {
	if a.Identity != b.Identity || a.RequestedStartMs != b.RequestedStartMs || a.RequestedDurationMs != b.RequestedDurationMs {
		return false
	}
	// Different tails may overlap; a much shorter excerpt needs its own request.
	return min(a.PCM.SampleCount, b.PCM.SampleCount)*5 >= max(a.PCM.SampleCount, b.PCM.SampleCount)*4
}

func normalizeLabel(value string) string {
	// Keep version words, punctuation, and parenthesized qualifiers intact.
	return strings.ToLower(strings.Join(strings.Fields(value), " "))
}

func metadata(a, b Input) (title, artist bool, delta *int64) {
	label := normalizeLabel(a.Title)
	title = label != "" && label == normalizeLabel(b.Title)
	artists := make(map[string]bool, len(a.Artists))
	for _, value := range a.Artists {
		if v := normalizeLabel(value); v != "" {
			artists[v] = true
		}
	}
	for _, value := range b.Artists {
		if artists[normalizeLabel(value)] {
			artist = true
			break
		}
	}
	if a.DurationMs != nil && b.DurationMs != nil && *a.DurationMs > 0 && *b.DurationMs > 0 {
		difference := max(*a.DurationMs, *b.DurationMs) - min(*a.DurationMs, *b.DurationMs)
		delta = &difference
	}
	return
}
