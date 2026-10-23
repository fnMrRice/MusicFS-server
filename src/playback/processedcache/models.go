// Package processedcache stores only completed, scoped audio processing output.
// It never grants source access: callers must authorize publication and reuse.
package processedcache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/fnMrRice/MusicFS-server/src/playback/timeline"
)

const MaxEntries = 128

type Identity struct {
	Owner, ProviderID, SourceTrackID, AssetID, Version string
	InputSHA256, WorkerSHA256, DecoderSHA256           string
	PluginID, PluginVersion, PipelineRevision, Profile string
	GainDB                                             float64
	PositionMs                                         int64
}

func (i Identity) Key() (string, error) {
	for _, value := range []string{i.Owner, i.ProviderID, i.SourceTrackID, i.AssetID, i.Version, i.PluginID, i.PluginVersion} {
		if value == "" || len(value) > 1024 || !utf8.ValidString(value) || strings.IndexFunc(value, unicode.IsControl) >= 0 {
			return "", errors.New("invalid processed cache identity")
		}
	}
	if !validHex(i.InputSHA256, 64) || !validHex(i.WorkerSHA256, 64) || !validHex(i.DecoderSHA256, 64) || !timeline.ValidProcessingRevision(i.PipelineRevision) || !timeline.ValidOutputProfile(i.Profile) || math.IsNaN(i.GainDB) || i.GainDB < -60 || i.GainDB > 0 || i.PositionMs < 0 || i.PositionMs > 86400000 {
		return "", errors.New("invalid processed cache configuration")
	}
	raw, err := json.Marshal(i)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

type Record struct {
	ID, Key, SHA256       string
	Identity              Identity
	Size                  int64
	CreatedAt, LastUsedAt time.Time
}

func (r Record) Validate() error {
	key, err := r.Identity.Key()
	if err != nil || key != r.Key || !validHex(r.ID, 48) || !validHex(r.SHA256, 64) || r.Size <= 0 || r.CreatedAt.IsZero() || r.LastUsedAt.Before(r.CreatedAt) {
		return errors.New("invalid completed cache record")
	}
	return nil
}

type Authorization struct {
	Owner, AuthID, SessionID, AttemptID string
}

type Repository interface {
	ListProcessedCache(context.Context) ([]Record, error)
	PublishProcessedCache(context.Context, Record, Authorization) error
	CheckProcessedCache(context.Context, Record, Authorization) error
	ForgetProcessedCache(context.Context, string) error
}

func validHex(value string, length int) bool {
	if len(value) != length || strings.ToLower(value) != value {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
