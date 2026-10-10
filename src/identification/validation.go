package identification

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/domain"
)

var evidenceIdentifier = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,128}$`)

func Invalid() error { return domain.Fail("INVALID_ARGUMENT", "Invalid asset evidence") }

// AlgorithmVersionID preserves canonical tool versions. Other version strings
// (such as distribution suffixes) use a collision-resistant opaque identity,
// never lossy character stripping or raw tool text in public evidence fields.
func AlgorithmVersionID(version string) string {
	if evidenceIdentifier.MatchString(version) {
		return version
	}
	sum := sha256.Sum256([]byte(version))
	return "sha256-" + hex.EncodeToString(sum[:])
}

func (s Spec) Validate(size *int64) error {
	if !evidenceIdentifier.MatchString(s.Algorithm) || !evidenceIdentifier.MatchString(s.AlgorithmVersion) || !evidenceIdentifier.MatchString(s.ConfigRevision) {
		return Invalid()
	}
	if s.Range.Start < 0 || s.Range.End != nil && *s.Range.End < s.Range.Start {
		return Invalid()
	}
	if s.Range.Unit != "bytes" && s.Range.Unit != "milliseconds" {
		return Invalid()
	}
	if s.Range.Unit == "bytes" && size != nil && (s.Range.Start > *size || s.Range.End != nil && *s.Range.End > *size) {
		return Invalid()
	}
	switch s.Kind {
	case "bitrate":
		if s.Origin != "provider" && s.Origin != "probe" && s.Origin != "estimate" && s.Origin != "manual" {
			return Invalid()
		}
	case "providerSha512":
		if s.Origin != "provider" || s.Algorithm != "sha512" {
			return Invalid()
		}
	case "fileSha512":
		if s.Origin != "probe" || s.Algorithm != "sha512" {
			return Invalid()
		}
	default:
		return Invalid()
	}
	if s.Kind != "bitrate" && (s.Range.Unit != "bytes" || s.Range.Start != 0 || size == nil || s.Range.End == nil || *s.Range.End != *size) {
		return Invalid()
	}
	return nil
}

func ValidDigest(value string) bool {
	b, err := hex.DecodeString(value)
	return err == nil && len(b) == 64 && value == strings.ToLower(value)
}

func (r Result) Validate(s Spec) error {
	if r.BitrateMode != "unknown" && r.BitrateMode != "cbr" && r.BitrateMode != "vbr" {
		return Invalid()
	}
	if r.BitrateBps != nil && *r.BitrateBps <= 0 {
		return Invalid()
	}
	if r.SHA512 != nil && !ValidDigest(*r.SHA512) {
		return Invalid()
	}
	if r.ComputedAt != nil && (r.ComputedAt.IsZero() || r.ComputedAt.After(time.Now().Add(time.Minute))) {
		return Invalid()
	}
	if r.Status == "failed" || r.Status == "unavailable" {
		if r.FailureCode == nil || !evidenceIdentifier.MatchString(*r.FailureCode) || r.BitrateBps != nil || r.BitrateMode != "unknown" || r.SHA512 != nil || r.ComputedAt != nil {
			return Invalid()
		}
		return nil
	}
	if r.FailureCode != nil {
		return Invalid()
	}
	switch s.Kind {
	case "bitrate":
		if r.Status != "reported" || r.SHA512 != nil || r.BitrateBps == nil || r.ComputedAt == nil {
			return Invalid()
		}
	case "providerSha512":
		if r.Status != "reported" || r.SHA512 == nil || r.BitrateBps != nil || r.BitrateMode != "unknown" || r.ComputedAt == nil {
			return Invalid()
		}
	case "fileSha512":
		if r.Status != "verified" || r.SHA512 == nil || r.BitrateBps != nil || r.BitrateMode != "unknown" || r.ComputedAt == nil {
			return Invalid()
		}
	default:
		return Invalid()
	}
	return nil
}
