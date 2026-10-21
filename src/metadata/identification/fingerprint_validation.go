package identification

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/playback/audio"
)

func (r FingerprintRequest) Validate() error {
	if _, _, err := (audio.NormalizeRequest{TaskID: "fingerprint", AssetID: "fingerprint", StartMs: r.StartMs, DurationMs: r.DurationMs}).Samples(); err != nil {
		return Invalid()
	}
	return r.ExtractorIdentity.Validate()
}

func (t FingerprintTarget) Validate() error {
	if t.SourceTrackID == "" || t.AssetID == "" || t.AssetVersion == "" || t.FileSizeBytes < 0 {
		return Invalid()
	}
	return t.FingerprintRequest.Validate()
}

func (t FingerprintTarget) Digest() (string, error) {
	if err := t.Validate(); err != nil {
		return "", err
	}
	raw, err := json.Marshal(struct {
		Target FingerprintTarget
		Size   int64
	}{t, t.FileSizeBytes})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(append([]byte("musicfs-fingerprint-job-v1\x00"), raw...))
	return hex.EncodeToString(sum[:]), nil
}

func (s FingerprintSummary) Validate(t FingerprintTarget) error {
	if err := t.Validate(); err != nil {
		return err
	}
	start, count, err := (audio.NormalizeRequest{TaskID: "fingerprint", AssetID: t.AssetID, StartMs: t.StartMs, DurationMs: t.DurationMs}).Samples()
	q := s.Quality
	if err != nil || s.ExtractorIdentity != t.ExtractorIdentity || s.ActualStartSample != start || s.SampleCount < 1 || s.SampleCount > count || s.ValueCount < 0 || s.ValueCount > 1000 || s.ComputedAt.IsZero() || s.ComputedAt.After(time.Now().Add(5*time.Minute)) || q.UniqueValues < 0 || q.UniqueValues > s.ValueCount || q.DominantValues < 0 || q.DominantValues > s.ValueCount {
		return Invalid()
	}
	if (s.ValueCount == 0 && (q.UniqueValues != 0 || q.DominantValues != 0)) || (s.ValueCount > 0 && (q.UniqueValues < 1 || q.DominantValues < 1)) {
		return Invalid()
	}
	status := "available"
	if s.SampleCount < 110250 {
		status = "short"
	} else if q.UniqueValues < 8 || s.ValueCount == 0 || q.DominantValues*10 >= s.ValueCount*9 {
		status = "lowInformation"
	}
	if q.Status != status {
		return Invalid()
	}
	return nil
}
