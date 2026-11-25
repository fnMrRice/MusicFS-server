package onlinescrape

import (
	"context"
	"errors"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library/music"
	"github.com/fnMrRice/MusicFS-server/src/library/works"
)

type AlbumBatchProgress struct {
	Total, Processed, Saved, Skipped, NotFound, Failed int
}

type batchAlbum struct {
	id       string
	revision int64
}

// AutoScrapeAlbums 固定本次筛选中尚未刮削的专辑及修订，逐张选用第一份完整发行版。
func (s *Service) AutoScrapeAlbums(ctx context.Context, repo music.Repository, owner, providerID, query string, progress func(AlbumBatchProgress) error) (AlbumBatchProgress, error) {
	var result AlbumBatchProgress
	var albums []batchAlbum
	filter := music.Query{ProviderID: providerID, Q: query, UnscrapedOnly: true, Limit: 200}
	for {
		page, err := repo.MusicAlbums(ctx, owner, filter)
		if err != nil {
			return result, err
		}
		for _, album := range page.Items {
			albums = append(albums, batchAlbum{album.ID, album.Revision})
		}
		if page.NextAfter == nil {
			break
		}
		filter.After = *page.NextAfter
	}
	result.Total = len(albums)
	if progress != nil {
		if err := progress(result); err != nil {
			return result, err
		}
	}
	var firstFailure error
	for _, album := range albums {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		outcome, err := s.scrapeBatchAlbum(ctx, repo, owner, providerID, album)
		if err != nil && ctx.Err() != nil {
			return result, ctx.Err()
		}
		switch {
		case err != nil:
			result.Failed++
			if firstFailure == nil {
				firstFailure = err
			}
		case outcome == albumSaved:
			result.Saved++
		case outcome == albumChanged:
			result.Skipped++
		default:
			result.NotFound++
		}
		result.Processed++
		if progress != nil {
			if err := progress(result); err != nil {
				return result, err
			}
		}
	}
	return result, firstFailure
}

type albumOutcome int

const (
	albumUnmatched albumOutcome = iota
	albumSaved
	albumChanged
)

func (s *Service) scrapeBatchAlbum(ctx context.Context, repo music.Repository, owner, provider string, album batchAlbum) (albumOutcome, error) {
	target, err := repo.MusicAlbumScrapeTarget(ctx, owner, album.id, provider)
	if err != nil {
		var failure *domain.Error
		if errors.As(err, &failure) && failure.Code == "NOT_FOUND" {
			return albumChanged, nil
		}
		return albumUnmatched, err
	}
	search, err := s.prepareAlbumVerification(ctx, repo, owner, album.id, provider, target)
	if err != nil {
		return albumUnmatched, err
	}
	var firstFailure error
	for _, match := range search.ranked {
		tracks, err := s.verifiedAlbumTracks(ctx, owner, match, search.evidence, albumTracksTimeout)
		if ctx.Err() != nil {
			return albumUnmatched, ctx.Err()
		}
		if tracks != nil {
			fields := []works.ScrapedField{
				{Field: "albumDetails", PluginID: match.plugin, CandidateToken: domain.NewID(), Item: cloneItem(match.item.Item)},
				{Field: "albumTracks", PluginID: match.plugin, CandidateToken: domain.NewID(), Item: cloneItem(tracks.Item)},
			}
			_, err = repo.SelectAutoMusicAlbumScrapedFields(ctx, owner, album.id, provider, album.revision, fields, domain.NewID())
			if err == nil {
				return albumSaved, nil
			}
			var failure *domain.Error
			if !errors.As(err, &failure) {
				return albumUnmatched, err
			}
			switch failure.Code {
			case "NOT_FOUND", "CONFLICT":
				return albumChanged, nil
			case "ALBUM_TRACK_MISMATCH":
				continue
			case "INVALID_ARGUMENT":
				// 当前候选曲目资料无法发布时，继续下一份发行版。
			default:
				return albumUnmatched, err
			}
		}
		if err != nil && firstFailure == nil {
			firstFailure = err
		}
	}
	return albumUnmatched, firstFailure
}
