// Package sourcedisplay resolves display metadata for owned playlist references.
package sourcedisplay

import (
	"context"
	"errors"
	"sort"

	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/online"
	"github.com/fnMrRice/MusicFS-server/src/onlinehost"
	"github.com/fnMrRice/MusicFS-server/src/playlists"
)

const MaxItems = 50

type Item struct {
	Source       playlists.SourceRef     `json:"source"`
	Local        *library.Track          `json:"local"`
	RemoteSource *playlists.RemoteSource `json:"remoteSource"`
	RemoteTrack  *online.Track           `json:"remoteTrack"`
	ErrorCode    *string                 `json:"errorCode"`
}
type Page struct {
	Items []Item `json:"items"`
}
type Repository interface {
	SourceDisplay(context.Context, string, []playlists.SourceRef) (Page, error)
}
type Service struct {
	repo   Repository
	online *onlinehost.Service
}

func New(repo Repository, service *onlinehost.Service) *Service { return &Service{repo, service} }

func Validate(refs []playlists.SourceRef) error {
	if len(refs) < 1 || len(refs) > MaxItems {
		return domain.Fail("INVALID_ARGUMENT", "Source lookup must contain 1 to 50 references")
	}
	for _, ref := range refs {
		if err := ref.Validate(); err != nil {
			return err
		}
	}
	return nil
}
func (s *Service) Lookup(ctx context.Context, owner string, refs []playlists.SourceRef) (Page, error) {
	if err := Validate(refs); err != nil {
		return Page{}, err
	}
	page, err := s.repo.SourceDisplay(ctx, owner, refs)
	if err != nil {
		return Page{}, err
	}
	groups := map[string][]int{}
	for i, item := range page.Items {
		if item.RemoteSource != nil && item.ErrorCode == nil {
			key := item.RemoteSource.InstanceID + "\x00" + item.RemoteSource.AccountID
			groups[key] = append(groups[key], i)
		}
	}
	instances := []string{}
	for id := range groups {
		instances = append(instances, id)
	}
	sort.Strings(instances)
	for _, id := range instances {
		indices := groups[id]
		ids := []string{}
		for _, i := range indices {
			ids = append(ids, page.Items[i].RemoteSource.ExternalID)
		}
		var batch online.TrackBatch
		var e error
		if s.online == nil {
			e = domain.Fail("SOURCE_OFFLINE", "Online service is unavailable")
		} else {
			ref := page.Items[indices[0]].RemoteSource
			batch, e = s.online.MetadataBatchForAccount(ctx, owner, ref.InstanceID, ref.AccountID, ids)
		}
		if e != nil {
			if ctx.Err() != nil {
				return Page{}, ctx.Err()
			}
			code := "TEMPORARY_FAILURE"
			var failure *domain.Error
			if errors.As(e, &failure) {
				code = failure.Code
			}
			for _, i := range indices {
				page.Items[i].ErrorCode = &code
			}
			continue
		}
		if len(batch.Items) != len(indices) {
			return Page{}, domain.Fail("PROTOCOL_MISMATCH", "Source lookup result count mismatch")
		}
		for j, i := range indices {
			page.Items[i].RemoteTrack = batch.Items[j].Track
			page.Items[i].ErrorCode = batch.Items[j].ErrorCode
		}
	}
	return page, nil
}
