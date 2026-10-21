package mediastore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"mime"
	"regexp"
	"strings"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/storage/mediastore/content"
)

type ContentService struct {
	ContentRepository
	journal Repository
	files   *Files
	gate    chan struct{}
	derived *DerivedService
}

func NewContent(repo ContentRepository, journal Repository, files *Files) *ContentService {
	s := &ContentService{ContentRepository: repo, journal: journal, files: files, gate: make(chan struct{}, 2)}
	if derived, ok := repo.(DerivedRepository); ok {
		media, err := New(journal, files)
		if err == nil {
			s.derived = &DerivedService{DerivedRepository: derived, media: media, content: s, gate: make(chan struct{}, 2)}
		}
	}
	return s
}

var languageTag = regexp.MustCompile(`^[A-Za-z]{2,8}(?:-[A-Za-z0-9]{1,8})*$`)

func (m ContentMetadata) Validate() error {
	for _, value := range []string{m.Target.SourceTrackID, m.Target.AssetID, m.Target.AssetVersion} {
		if value == "" || len(value) > 256 || strings.ContainsAny(value, "\x00\r\n") {
			return domain.Fail("INVALID_ARGUMENT", "Invalid content target")
		}
	}
	if len(m.Language) > 64 || (m.Language != "" && !languageTag.MatchString(m.Language)) {
		return domain.Fail("INVALID_ARGUMENT", "Invalid content language")
	}
	if m.Field != "lyrics" && m.Field != "artwork" {
		return domain.Fail("UNSUPPORTED", "Unsupported media field")
	}
	if m.Field == "artwork" && m.Language != "" {
		return domain.Fail("INVALID_ARGUMENT", "Artwork does not have a lyrics language")
	}
	if (m.Field == "lyrics" && m.Format != "text" && m.Format != "lrc") || (m.Field == "artwork" && m.Format != "jpeg" && m.Format != "png") {
		return domain.Fail("UNSUPPORTED", "Unsupported content format")
	}
	if m.Origin.Kind != "upload" && m.Origin.Kind != "edit" && m.Origin.Kind != "scraper" && m.Origin.Kind != "import" {
		return domain.Fail("INVALID_ARGUMENT", "Invalid content provenance")
	}
	if len(m.Origin.InstanceID) > 128 || len(m.Origin.CandidateID) > 128 || strings.ContainsAny(m.Origin.InstanceID+m.Origin.CandidateID, "/\\\x00\r\n") {
		return domain.Fail("INVALID_ARGUMENT", "Invalid content provenance")
	}
	return nil
}

// Upload stores a preview object; only SelectContent changes the chosen content.
// The service derives format, MIME, size and digest from bounded actual bytes.
func (s *ContentService) Upload(ctx context.Context, owner string, metadata ContentMetadata, declaredMIME, key string, input io.Reader) (ContentObject, error) {
	return s.upload(ctx, owner, metadata, declaredMIME, key, input, func(publish func() error) error { return publish() })
}

// UploadGuarded keeps a scraper's source-version protection through reference
// publication. Manual uploads do not claim to have read the original audio.
func (s *ContentService) UploadGuarded(ctx context.Context, owner string, metadata ContentMetadata, declaredMIME, key string, input io.Reader, guard Guard) (ContentObject, error) {
	if guard == nil {
		return ContentObject{}, domain.Fail("INVALID_ARGUMENT", "Content publication requires a source guard")
	}
	return s.upload(ctx, owner, metadata, declaredMIME, key, input, guard)
}

func (s *ContentService) upload(ctx context.Context, owner string, metadata ContentMetadata, declaredMIME, key string, input io.Reader, guard Guard) (ContentObject, error) {
	select {
	case s.gate <- struct{}{}:
		defer func() { <-s.gate }()
	default:
		return ContentObject{}, domain.Fail("RATE_LIMITED", "Media upload is busy")
	}
	limit := int64(content.MaxLyricsBytes)
	if metadata.Field == "artwork" {
		limit = content.MaxArtworkBytes
	}
	if metadata.Field != "lyrics" && metadata.Field != "artwork" {
		return ContentObject{}, domain.Fail("UNSUPPORTED", "Unsupported media field")
	}
	if input == nil {
		return ContentObject{}, domain.Fail("INVALID_ARGUMENT", "Media content is required")
	}
	body, err := io.ReadAll(io.LimitReader(&contextReader{ctx: ctx, r: input}, limit+1))
	if err != nil {
		return ContentObject{}, err
	}
	validated, err := content.Validate(metadata.Field, body)
	if err != nil {
		return ContentObject{}, domain.Fail("UNSUPPORTED", "Invalid or oversized media content")
	}
	actual, _, _ := mime.ParseMediaType(validated.MIME)
	if declaredMIME != "" {
		declared, _, e := mime.ParseMediaType(declaredMIME)
		if e != nil || declared != actual {
			return ContentObject{}, domain.Fail("UNSUPPORTED", "Media type does not match its content")
		}
	}
	metadata.Format = validated.Format
	metadata.Language = CanonicalLanguage(metadata.Field, metadata.Language)
	if err = metadata.Validate(); err != nil {
		return ContentObject{}, err
	}
	request := UploadInput{Metadata: metadata, MIME: validated.MIME, Size: int64(len(validated.Bytes)), SHA256: validated.SHA256}
	p, existing, err := s.ContentRepository.PrepareContent(ctx, owner, request, key)
	if err != nil {
		return ContentObject{}, err
	}
	if existing {
		if p.Status == "finished" || p.Status == "referenced" {
			return s.ContentRepository.ContentObject(ctx, owner, p.Object.ID)
		}
		return ContentObject{}, domain.Fail("CONFLICT", "Media upload is unfinished or unavailable; use a new operation")
	}
	verify := func(file io.ReadSeeker) error {
		hash := sha256.New()
		n, err := io.Copy(hash, io.LimitReader(contextReader{ctx: ctx, r: file}, request.Size+1))
		if err != nil {
			return err
		}
		if n != request.Size || hex.EncodeToString(hash.Sum(nil)) != request.SHA256 {
			return domain.Fail("RESOURCE_CHANGED", "Media bytes changed")
		}
		return nil
	}
	object, err := s.files.Write(ctx, p.Object.ID, metadata.Field, validated.MIME, bytes.NewReader(validated.Bytes), limit, verify)
	if err != nil {
		return ContentObject{}, err
	}
	if object.Size != request.Size || object.SHA256 != request.SHA256 {
		return ContentObject{}, domain.Fail("RESOURCE_CHANGED", "Media bytes changed")
	}
	if err = s.journal.WrittenPublication(ctx, owner, p.ID, object); err != nil {
		return ContentObject{}, err
	}
	if err = ctx.Err(); err != nil {
		return ContentObject{}, err
	}
	if err = s.files.Publish(object); err != nil {
		return ContentObject{}, err
	}
	if err = s.journal.SetPublicationStage(ctx, owner, p.ID, "written", "published"); err != nil {
		return ContentObject{}, err
	}
	if err = ctx.Err(); err != nil {
		return ContentObject{}, err
	}
	if err = guard(func() error { return s.journal.ReferencePublication(ctx, owner, p.ID) }); err != nil {
		return ContentObject{}, err
	}
	if err = s.journal.SetPublicationStage(ctx, owner, p.ID, "referenced", "finished"); err != nil {
		return ContentObject{}, err
	}
	return s.ContentRepository.ContentObject(ctx, owner, object.ID)
}

func (s *ContentService) Open(ctx context.Context, owner, id string) (ContentObject, io.ReadCloser, error) {
	object, err := s.ContentRepository.ContentObject(ctx, owner, id)
	if err != nil {
		return object, nil, err
	}
	if object.State != "available" {
		return object, nil, domain.Fail("RESOURCE_CHANGED", "Media content is unavailable")
	}
	if err = ctx.Err(); err != nil {
		return object, nil, err
	}
	file, err := s.files.Open(object.Object)
	if err != nil {
		if e := s.ContentRepository.InvalidateContent(ctx, owner, id); e != nil {
			return object, nil, e
		}
		return object, nil, domain.Fail("RESOURCE_CHANGED", "Media content is missing or damaged")
	}
	return object, file, nil
}
