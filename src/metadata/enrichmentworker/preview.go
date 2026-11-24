package enrichmentworker

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"reflect"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/metadata/enrichment"
	"github.com/fnMrRice/MusicFS-server/src/storage/mediastore"
	"github.com/fnMrRice/MusicFS-server/src/storage/mediastore/content"
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

func (s *Service) preview(ctx context.Context, w enrichment.Work, instance string, input *library.ScrapeInput, session enrichment.Session, c enrichment.Candidate) (enrichment.PreviewCandidate, error) {
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
	body := previewWriter{limit: c.SizeBytes}
	fetched, err := session.Fetch(ctx, c.CandidateID, &body)
	if err != nil {
		return failedPreview(p, failureCode(err)), nil
	}
	validated, err := content.Validate(c.Field, body.Bytes())
	if err != nil || !reflect.DeepEqual(fetched, c) || int64(body.Len()) != c.SizeBytes || validated.SHA256 != c.SHA256 || int64(len(validated.Bytes)) != c.SizeBytes || validated.Format != c.Format || validated.MIME != c.MIME {
		return failedPreview(p, "PROTOCOL_MISMATCH"), nil
	}
	return s.storePreview(ctx, w, input, p, validated.Bytes)
}

func (s *Service) storePreview(ctx context.Context, w enrichment.Work, input *library.ScrapeInput, p enrichment.PreviewCandidate, body []byte) (enrichment.PreviewCandidate, error) {
	metadata := mediastore.ContentMetadata{Target: enrichment.ContentTarget(w.Target), Field: p.Field, Language: p.Language, Format: p.Format, Origin: mediastore.Origin{Kind: "scraper", InstanceID: p.InstanceID, CandidateID: p.CandidateID}}
	digest := sha256.Sum256([]byte(fmt.Sprintf("%s/%s/%d/%s/%s", w.JobID, w.Target.SourceTrackID, w.Generation, p.InstanceID, p.CandidateID)))
	guard := func(publish func() error) error {
		return input.Publish(func() error {
			if err := s.repo.CurrentEnrichment(ctx, w); err != nil {
				return err
			}
			return publish()
		})
	}
	object, err := s.content.UploadGuarded(ctx, w.Owner, metadata, p.MIME, fmt.Sprintf("scrape-%x", digest), bytes.NewReader(body), guard)
	if err != nil {
		return p, err
	}
	p.PreviewObjectID = &object.ID
	return p, nil
}

// previewWriter 将正文限制在已预留预算内，防止替换插件绕过宿主配额。
type previewWriter struct {
	buffer bytes.Buffer
	limit  int64
}

func (w *previewWriter) Len() int      { return w.buffer.Len() }
func (w *previewWriter) Bytes() []byte { return w.buffer.Bytes() }

func (w *previewWriter) Write(p []byte) (int, error) {
	if int64(len(p)) > w.limit-int64(w.Len()) {
		return 0, fmt.Errorf("PROTOCOL_MISMATCH: scraper content exceeds declared size")
	}
	return w.buffer.Write(p)
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
