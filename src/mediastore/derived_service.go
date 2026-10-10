package mediastore

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"

	"github.com/fnMrRice/MusicFS-server/src/domain"
)

type DerivedService struct {
	DerivedRepository
	media   *Service
	content *ContentService
	gate    chan struct{}
}

func (s *ContentService) Derived() *DerivedService { return s.derived }

func (s *DerivedService) Open(ctx context.Context, owner, id string) (DerivedObject, io.ReadCloser, error) {
	o, err := s.DerivedObject(ctx, owner, id)
	if err != nil {
		return o, nil, err
	}
	if o.State != "available" {
		return o, nil, domain.Fail("RESOURCE_CHANGED", "Derived media is unavailable")
	}
	if err = ctx.Err(); err != nil {
		return o, nil, err
	}
	in, err := s.media.Open(o.Object)
	if err != nil {
		if e := s.InvalidateDerived(ctx, owner, id); e != nil {
			return o, nil, e
		}
		return o, nil, domain.Fail("RESOURCE_CHANGED", "Derived media is missing or damaged")
	}
	return o, in, nil
}

func (s *DerivedService) existing(ctx context.Context, owner, key string) (DerivedObject, bool, error) {
	o, err := s.FindDerived(ctx, owner, key)
	var de *domain.Error
	if errors.As(err, &de) && de.Code == "NOT_FOUND" {
		return o, false, nil
	}
	if err != nil {
		return o, false, err
	}
	o, in, err := s.Open(ctx, owner, o.ID)
	if err != nil {
		return o, false, err
	}
	return o, true, in.Close()
}

func (s *DerivedService) publish(ctx context.Context, owner, kind, mime string, metadata DerivedMetadata, body []byte) (DerivedObject, error) {
	key, err := metadata.Key()
	if err != nil {
		return DerivedObject{}, err
	}
	if o, exists, err := s.existing(ctx, owner, key); err != nil || exists {
		return o, err
	}
	raw, err := json.Marshal(metadata)
	if err != nil {
		return DerivedObject{}, err
	}
	id := metadata.ParentObjectID
	if kind == "scrape-document" {
		id = metadata.JobID
	}
	// Body is already bounded and validated by the generator. Files.Write also
	// verifies exactly these bytes before making its durable reference visible.
	validate := func(in io.ReadSeeker) error {
		actual, err := io.ReadAll(io.LimitReader(in, int64(len(body))+1))
		if err != nil {
			return err
		}
		if !bytes.Equal(actual, body) {
			return domain.Fail("RESOURCE_CHANGED", "Derived bytes changed")
		}
		return nil
	}
	p, err := s.media.Publish(ctx, owner, kind, mime, Target{Purpose: "derived", ID: id, Generation: max(1, metadata.Generation), Metadata: raw}, bytes.NewReader(body), MaxObjectBytes, validate, func(fn func() error) error { return fn() })
	if err != nil {
		// A concurrent generator may have published the same immutable relation.
		var de *domain.Error
		if errors.As(err, &de) && de.Code == "CONFLICT" {
			if existing, found, e := s.existing(ctx, owner, key); e == nil && found {
				return existing, nil
			}
		}
		return DerivedObject{}, err
	}
	return s.DerivedObject(ctx, owner, p.Object.ID)
}

// PublishDocument accepts an internally constructed, complete attempt record.
// HTTP callers cannot submit arbitrary JSON to this storage path.
func (s *DerivedService) PublishDocument(ctx context.Context, owner string, metadata DerivedMetadata, body []byte) (DerivedObject, error) {
	if metadata.Transform != "scrape-document/v1" || len(body) == 0 || int64(len(body)) > MaxObjectBytes || !json.Valid(body) {
		return DerivedObject{}, domain.Fail("INVALID_ARGUMENT", "Invalid scrape document")
	}
	var envelope struct {
		Schema string `json:"schema"`
	}
	if json.Unmarshal(body, &envelope) != nil || envelope.Schema != metadata.Transform {
		return DerivedObject{}, domain.Fail("INVALID_ARGUMENT", "Invalid scrape document schema")
	}
	return s.publish(ctx, owner, "scrape-document", "application/json", metadata, body)
}
