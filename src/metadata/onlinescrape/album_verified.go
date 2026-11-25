package onlinescrape

import (
	"context"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/scraper"
	"github.com/fnMrRice/MusicFS-server/src/library/music"
	"github.com/fnMrRice/MusicFS-server/src/storage/mediastore"
)

type albumVerification struct {
	evidence []music.AlbumTrackEvidence
	ranked   []albumMatch
}

const albumTracksTimeout = 30 * time.Second

func (s *Service) prepareAlbumVerification(ctx context.Context, repo music.Repository, owner, albumID, provider string, target music.AlbumScrapeTarget) (albumVerification, error) {
	evidence, err := repo.MusicAlbumTrackEvidence(ctx, owner, albumID, provider)
	if err != nil || len(evidence) == 0 {
		return albumVerification{}, err
	}
	ranked, err := s.rankAlbums(ctx, owner, target, representativeAlbumArtist(evidence))
	return albumVerification{evidence: evidence, ranked: ranked}, err
}

func (s *Service) verifiedAlbumTracks(ctx context.Context, owner string, match albumMatch, evidence []music.AlbumTrackEvidence, timeout time.Duration) (*searchItem, error) {
	read, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	query := scraper.Query{ExternalID: match.item.ID, SourceURL: match.item.SourceURL}
	page, err := s.lookup(read, owner, match.plugin, "albumTracks", query, false)
	if err != nil {
		return nil, err
	}
	// 读取曲目表时平台可能返回其他发行版，须同时核对发行版编号和原专辑曲目。
	for i := range page.items {
		tracks := &page.items[i]
		if tracks.ID == match.item.ID && music.MatchesAlbumTracks(evidence, tracks.Tracks) {
			return tracks, nil
		}
	}
	return nil, nil
}

// SearchVerifiedAlbum 只给已通过原曲目核验的同一发行版签发资料和曲目表令牌。
func (s *Service) SearchVerifiedAlbum(ctx context.Context, repo music.Repository, owner, albumID, provider string) ([]AutoAlbumCandidate, error) {
	target, err := repo.MusicAlbumScrapeTarget(ctx, owner, albumID, provider)
	if err != nil {
		return nil, err
	}
	search, err := s.prepareAlbumVerification(ctx, repo, owner, albumID, provider, target)
	if err != nil {
		return nil, err
	}
	result := make([]AutoAlbumCandidate, 0, len(search.ranked))
	var firstFailure error
	for i, match := range search.ranked {
		timeout := albumTracksTimeout
		if deadline, ok := ctx.Deadline(); ok {
			// 各候选分享剩余查询时间，并给返回已核验结果预留最多一秒。
			left := time.Until(deadline)
			timeout = min(timeout, (left-min(time.Second, left/10))/time.Duration(len(search.ranked)-i))
		}
		tracks, err := s.verifiedAlbumTracks(ctx, owner, match, search.evidence, timeout)
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if err != nil && firstFailure == nil {
			firstFailure = err
		}
		if tracks == nil {
			continue
		}
		details := s.issueAlbumCandidate(owner, match.plugin, "albumDetails", match.item)
		list := s.issueAlbumCandidate(owner, match.plugin, "albumTracks", *tracks)
		result = append(result, AutoAlbumCandidate{Candidate: details, PluginID: match.plugin, AlbumTracksToken: list.Token})
	}
	if len(result) == 0 && firstFailure != nil {
		return nil, firstFailure
	}
	return result, nil
}

func (s *Service) issueAlbumCandidate(owner, plugin, field string, item searchItem) Candidate {
	page := searchPage{items: []searchItem{item}, size: item.size}
	return s.issue(owner, plugin, field, page, mediastore.ContentTarget{})[0]
}
