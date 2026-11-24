package enrichment

import (
	"encoding/hex"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/fnMrRice/MusicFS-server/src/storage/mediastore/content"
)

var resultCode = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,63}$`)

// ValidateSearchResult 在插件返回后复核公共范围与预算；来源专属证据由插件校验。
func ValidateSearchResult(result SearchResult, target Target, fields []string) error {
	if result.Candidates == nil || result.Issues == nil || len(result.Candidates) > MaxCandidates || len(result.Issues) > MaxResources {
		return fmt.Errorf("PROTOCOL_MISMATCH: invalid scraper result limits")
	}
	seen := map[string]bool{}
	var total int64
	for _, c := range result.Candidates {
		if c.Origin == "catalog" && c.Basis == "matchedTrack" {
			return fmt.Errorf("PROTOCOL_MISMATCH: reserved host candidate provenance")
		}
		if c.Target != target || !slices.Contains(fields, c.Field) || !ValidRef(c.CandidateID) || seen[c.CandidateID] || !validText(c.Language, 35, false) || !ValidProvenance(c.Origin, c.Basis, c.Field) {
			return fmt.Errorf("PROTOCOL_MISMATCH: scraper candidate scope mismatch")
		}
		seen[c.CandidateID] = true
		if c.Status == "unavailable" {
			if c.FailureCode == nil || !resultCode.MatchString(*c.FailureCode) || c.SizeBytes != 0 || c.SHA256 != "" || c.Format != "" || c.MIME != "" {
				return fmt.Errorf("PROTOCOL_MISMATCH: invalid unavailable scraper candidate")
			}
			continue
		}
		limit := int64(content.MaxLyricsBytes)
		if c.Field == "artwork" {
			limit = content.MaxArtworkBytes
		}
		hash, err := hex.DecodeString(c.SHA256)
		format := c.Field == "lyrics" && (c.Format == "text" || c.Format == "lrc") && c.MIME == "text/plain; charset=utf-8" || c.Field == "artwork" && (c.Format == "png" && c.MIME == "image/png" || c.Format == "jpeg" && c.MIME == "image/jpeg")
		if c.Status != "available" || c.FailureCode != nil || c.SizeBytes <= 0 || c.SizeBytes > limit || err != nil || len(hash) != 32 || c.SHA256 != strings.ToLower(c.SHA256) || !format {
			return fmt.Errorf("PROTOCOL_MISMATCH: invalid scraper candidate content")
		}
		total += c.SizeBytes
		if total > MaxJobPreviewBytes {
			return fmt.Errorf("PROTOCOL_MISMATCH: scraper content budget exceeded")
		}
	}
	for _, issue := range result.Issues {
		if !resultCode.MatchString(issue.Code) || issue.ResourceRef != "" && !ValidRef(issue.ResourceRef) {
			return fmt.Errorf("PROTOCOL_MISMATCH: invalid scraper issue")
		}
	}
	return nil
}

// ValidProvenance 限制插件来源描述的大小；标签和远程平台专属值由插件解释。
func ValidProvenance(origin, basis, field string) bool {
	if origin == "container" && (field != "artwork" || basis != "containerEmbedded") || basis == "containerEmbedded" && origin != "container" {
		return false
	}
	return validText(origin, 64, true) && validText(basis, 64, true) && !strings.ContainsAny(origin+basis, "\x00\r\n")
}
