package mediastore

import (
	"context"
	"io"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/domain"
)

type Service struct {
	repo  Repository
	files *Files
}

func New(repo Repository, files *Files) (*Service, error) {
	if repo == nil || files == nil {
		return nil, domain.Fail("INVALID_ARGUMENT", "Media store requires a repository and files")
	}
	return &Service{repo: repo, files: files}, nil
}

// Publish preserves the old reference until validated bytes are durable. Any
// failure leaves a journal entry for recovery; file presence alone proves nothing.
func (s *Service) Publish(ctx context.Context, owner, kind, mime string, target Target, input io.Reader, limit int64, validate Validator, guard Guard) (Publication, error) {
	if guard == nil {
		return Publication{}, domain.Fail("INVALID_ARGUMENT", "Media publication requires a reference guard")
	}
	now := time.Now().UTC()
	id := domain.NewID()
	p := Publication{ID: domain.NewID(), Owner: owner, Kind: kind, Status: "prepared", Object: Object{ID: id, Kind: kind, MIME: mime, RelativePath: "objects/" + id}, Target: target, CreatedAt: now, UpdatedAt: now}
	if err := ctx.Err(); err != nil {
		return p, err
	}
	if err := s.repo.BeginPublication(ctx, p); err != nil {
		return p, err
	}
	obj, err := s.files.Write(ctx, id, kind, mime, input, limit, validate)
	if err != nil {
		return p, err
	}
	if err = s.repo.WrittenPublication(ctx, owner, p.ID, obj); err != nil {
		return p, err
	}
	p.Object, p.Status = obj, "written"
	if err = ctx.Err(); err != nil {
		return p, err
	}
	if err = s.files.Publish(obj); err != nil {
		return p, err
	}
	if err = s.repo.SetPublicationStage(ctx, owner, p.ID, "written", "published"); err != nil {
		return p, err
	}
	p.Status = "published"
	if err = ctx.Err(); err != nil {
		return p, err
	}
	if err = guard(func() error { return s.repo.ReferencePublication(ctx, owner, p.ID) }); err != nil {
		return p, err
	}
	p.Status = "referenced"
	if err = s.repo.SetPublicationStage(ctx, owner, p.ID, "referenced", "finished"); err != nil {
		return p, err
	}
	p.Status = "finished"
	return p, nil
}

// Recover content before restarting workers. A previous process's source guard
// or upload body no longer exists, so incomplete results are not made current.
// Published orphan files remain available to the later reference-aware collector.
func (s *Service) Recover(ctx context.Context) error {
	after := ""
	for {
		items, err := s.repo.PendingPublications(ctx, after, 128)
		if err != nil {
			return err
		}
		if len(items) == 0 {
			return nil
		}
		for _, p := range items {
			if err = ctx.Err(); err != nil {
				return err
			}
			if p.Target.Purpose != "fingerprint" && p.Target.Purpose != "content" {
				return domain.Fail("UNSUPPORTED", "Media publication recovery handler is unavailable")
			}
			switch p.Status {
			case "prepared", "written", "published":
				if err = s.repo.SetPublicationStage(ctx, p.Owner, p.ID, p.Status, "aborted"); err != nil {
					return err
				}
				if err = s.files.RemoveStaging(p.Object.ID); err != nil {
					return err
				}
			case "referenced":
				in, err := s.files.Open(p.Object)
				if err != nil {
					if err = s.repo.InvalidatePublication(ctx, p.Owner, p.ID, "RESOURCE_CHANGED"); err != nil {
						return err
					}
					after = p.ID
					continue
				}
				if err = in.Close(); err != nil {
					return err
				}
				if err = s.repo.SetPublicationStage(ctx, p.Owner, p.ID, "referenced", "finished"); err != nil {
					return err
				}
			default:
				return domain.Fail("CONFLICT", "Invalid pending media publication phase")
			}
			after = p.ID
		}
	}
}

// Open is an internal operation; an API must authorize a current reference first.
func (s *Service) Open(o Object) (io.ReadCloser, error) { return s.files.Open(o) }
