package enrichmentworker

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/enrichment"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/mediastore"
	"github.com/fnMrRice/MusicFS-server/src/plugins/scraperlocal"
)

// ID3 declares ISO 639-2. Only known exact aliases are normalized; no language
// is inferred from lyrics text and unknown declarations stay explicit.
func canonicalLanguage(language string) string {
	return mediastore.CanonicalLanguage("lyrics", language)
}

func languageSelected(language string, requested []string) bool {
	if len(requested) == 0 {
		return true
	}
	language = canonicalLanguage(language)
	for _, value := range requested {
		if canonicalLanguage(value) == language {
			return true
		}
	}
	return false
}

func (s *Service) preview(ctx context.Context, w enrichment.Work, instance string, input *library.ScrapeInput, session *scraperlocal.Session, c enrichment.Candidate) (enrichment.PreviewCandidate, error) {
	p := enrichment.PreviewCandidate{CandidateID: c.CandidateID, Target: w.Target, InstanceID: instance, Field: c.Field, Language: canonicalLanguage(c.Language), Format: c.Format, MIME: c.MIME, SizeBytes: c.SizeBytes, SHA256: c.SHA256, Origin: c.Origin, Basis: c.Basis, Status: c.Status, FailureCode: c.FailureCode, ExpiresAt: time.Now().UTC().Add(enrichment.CandidateLifetime)}
	if p.Field == "artwork" {
		p.Language = ""
	}
	if err := s.repo.ReserveEnrichmentPreview(ctx, w, p.SizeBytes); err != nil {
		return p, err
	}
	if c.Status != "available" {
		return p, nil
	}
	var body bytes.Buffer
	if _, err := session.Fetch(ctx, c.CandidateID, &body); err != nil {
		return failedPreview(p, failureCode(err)), nil
	}
	metadata := mediastore.ContentMetadata{Target: enrichment.ContentTarget(w.Target), Field: p.Field, Language: p.Language, Format: p.Format, Origin: mediastore.Origin{Kind: "scraper", InstanceID: instance, CandidateID: p.CandidateID}}
	digest := sha256.Sum256([]byte(fmt.Sprintf("%s/%s/%d/%s/%s", w.JobID, w.Target.SourceTrackID, w.Generation, instance, p.CandidateID)))
	guard := func(publish func() error) error {
		return input.Publish(func() error {
			if err := s.repo.CurrentEnrichment(ctx, w); err != nil {
				return err
			}
			return publish()
		})
	}
	object, err := s.content.UploadGuarded(ctx, w.Owner, metadata, p.MIME, fmt.Sprintf("scrape-%x", digest), bytes.NewReader(body.Bytes()), guard)
	if err != nil {
		return p, err
	}
	p.PreviewObjectID = &object.ID
	return p, nil
}

func failedPreview(p enrichment.PreviewCandidate, code string) enrichment.PreviewCandidate {
	p.Status = "unavailable"
	p.FailureCode = &code
	p.PreviewObjectID = nil
	p.SizeBytes = 0
	p.SHA256 = ""
	p.Format = ""
	p.MIME = ""
	return p
}
