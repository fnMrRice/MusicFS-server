package enrichment

import (
	"encoding/hex"
	"fmt"
	"strings"
	"unicode/utf8"
)

func validText(value string, limit int, required bool) bool {
	return (!required || value != "") && len(value) <= limit && utf8.ValidString(value)
}

func ValidRef(value string) bool {
	b, err := hex.DecodeString(value)
	return err == nil && len(b) == 24 && value == strings.ToLower(value)
}

func (t Target) Validate() error {
	if !validText(t.SourceTrackID, 128, true) || !validText(t.AssetID, 128, true) || !validText(t.AssetVersion, 1024, true) || t.Revision < 0 {
		return fmt.Errorf("INVALID_ARGUMENT: invalid metadata target")
	}
	return nil
}

func (r SearchRequest) Validate() error {
	if err := r.Target.Validate(); err != nil {
		return err
	}
	if !validText(r.Context.UserID, 128, true) || !validText(r.Context.InstanceID, 128, true) || !validText(r.Context.TaskID, 128, true) || r.Context.AccountRef != "" || r.Context.TimeoutMs < 1 || r.Context.TimeoutMs > 30000 || len(r.Resources) > MaxResources || len(r.Fields) < 1 || len(r.Fields) > 2 || len(r.Languages) > 16 {
		return fmt.Errorf("INVALID_ARGUMENT: invalid local scraper scope")
	}
	fields := map[string]bool{}
	for _, field := range r.Fields {
		if (field != "lyrics" && field != "artwork") || fields[field] {
			return fmt.Errorf("INVALID_ARGUMENT: invalid metadata fields")
		}
		fields[field] = true
	}
	for _, language := range r.Languages {
		if !validText(language, 35, true) {
			return fmt.Errorf("INVALID_ARGUMENT: invalid metadata language")
		}
	}
	refs := map[string]bool{}
	var attachmentBytes int64
	audioCount := 0
	for _, resource := range r.Resources {
		if !ValidRef(resource.ResourceRef) || refs[resource.ResourceRef] || !validText(resource.Version, 1024, true) || resource.SizeBytes <= 0 || resource.SizeBytes > 8<<40 {
			return fmt.Errorf("INVALID_ARGUMENT: invalid metadata resource")
		}
		refs[resource.ResourceRef] = true
		if resource.Kind == "audio" {
			audioCount++
			if resource.Match != "embedded" || audioCount > 1 {
				return fmt.Errorf("INVALID_ARGUMENT: invalid embedded resource")
			}
		} else {
			if !fields[resource.Kind] || (resource.Match != "exactStem" && !(resource.Kind == "artwork" && (resource.Match == "directoryCover" || resource.Match == "containerEmbedded"))) {
				return fmt.Errorf("ACCESS_DENIED: unauthorized attachment resource")
			}
			attachmentBytes += resource.SizeBytes
			if attachmentBytes > 32<<20 {
				return fmt.Errorf("RATE_LIMITED: attachment input budget exceeded")
			}
		}
	}
	if r.Query != nil {
		if !validText(r.Query.Title, 4096, false) || len(r.Query.Artists) > 32 {
			return fmt.Errorf("INVALID_ARGUMENT: invalid scraper query")
		}
		for _, artist := range r.Query.Artists {
			if !validText(artist, 1024, false) {
				return fmt.Errorf("INVALID_ARGUMENT: invalid scraper artist")
			}
		}
	}
	return nil
}
