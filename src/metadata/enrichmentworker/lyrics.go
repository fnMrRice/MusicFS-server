package enrichmentworker

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/sdk/scraper"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/metadata/enrichment"
	"github.com/fnMrRice/MusicFS-server/src/metadata/onlinescrape"
	"github.com/fnMrRice/MusicFS-server/src/storage/mediastore/content"
)

// collectRefresh 按来源顺序选择歌词。轮到本地时一次读出歌词和封面；
// 其他来源先命中或用户移除本地歌词时，本地插件只处理封面。
func (s *Service) collectRefresh(ctx context.Context, w enrichment.Work, task string, input *library.ScrapeInput, out *enrichment.Outcome, trace *documentTrace) error {
	track, err := s.library.Track(ctx, w.Owner, w.Target.SourceTrackID)
	if err != nil {
		return err
	}
	query := scraper.Query{Artist: strings.Join(track.Artists, ", ")}
	if track.Title != nil {
		query.Title = *track.Title
	}
	if track.Album != nil {
		query.Album = *track.Album
	}
	if track.DurationMs != nil {
		query.Duration = float64(*track.DurationMs) / 1000
	}

	readArtwork := false
	readLocal := func(ctx context.Context, fields []string) error {
		err := s.collectInstances(ctx, w, task, input, fields, out, trace)
		if err != nil {
			switch code := failureCode(err); code {
			case "SOURCE_OFFLINE", "UNSUPPORTED":
				addIssue(out, "", code)
				return nil
			}
		}
		return err
	}
	visitErr := s.LyricsCatalog.VisitLyricsSources(ctx, w.Owner, func(ctx context.Context, plugin string) (bool, error) {
		if plugin == onlinescrape.LocalLyricsPluginID {
			readArtwork = true
			err := readLocal(ctx, w.Request.Fields)
			return slices.ContainsFunc(out.Candidates, func(c enrichment.PreviewCandidate) bool {
				return c.Field == "lyrics" && c.PreviewObjectID != nil && enrichment.TrackSpecificCandidate(c)
			}), err
		}
		// 为下一个来源、封面和最终发布预留时间。
		searchCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		matches, err := s.LyricsCatalog.SearchMatchedLyrics(searchCtx, w.Owner, plugin, query)
		cancel()
		if err != nil {
			if ctx.Err() == nil && searchCtx.Err() == context.DeadlineExceeded {
				return false, domain.Fail("SOURCE_UNAVAILABLE", "Lyrics search timed out")
			}
			return false, err
		}
		if len(matches) == 0 {
			return false, nil
		}
		attempt := trace.attempt(plugin, plugin)
		for _, item := range matches {
			if len(out.Candidates) >= enrichment.MaxCandidates {
				return false, domain.Fail("RATE_LIMITED", "Metadata candidate limit reached")
			}
			p, err := s.catalogLyricsPreview(ctx, w, input, plugin, item)
			if err != nil {
				finishAttempt(attempt, err)
				return false, err
			}
			out.Candidates = append(out.Candidates, p)
			attempt.Fetches = append(attempt.Fetches, enrichment.DocumentFetch{CandidateID: p.CandidateID, State: "completed", Preview: &p})
		}
		finishAttempt(attempt, nil)
		return true, nil
	})
	if err := ctx.Err(); err != nil {
		return err
	}
	if visitErr != nil {
		code := failureCode(visitErr)
		switch code {
		case "SOURCE_UNAVAILABLE", "RATE_LIMITED", "TEMPORARY_FAILURE", "UNSUPPORTED", "SOURCE_OFFLINE":
			addIssue(out, "", code)
		default:
			return visitErr
		}
	}
	if !readArtwork && slices.Contains(w.Request.Fields, "artwork") {
		return readLocal(ctx, []string{"artwork"})
	}
	return nil
}

func (s *Service) catalogLyricsPreview(ctx context.Context, w enrichment.Work, input *library.ScrapeInput, plugin string, value onlinescrape.Candidate) (enrichment.PreviewCandidate, error) {
	body, err := content.Validate("lyrics", []byte(value.Lyrics))
	if err != nil {
		return enrichment.PreviewCandidate{}, err
	}
	p := enrichment.PreviewCandidate{CandidateID: domain.NewID(), Target: w.Target, InstanceID: plugin, Field: "lyrics", Language: "und", Format: body.Format, MIME: body.MIME, SizeBytes: int64(len(body.Bytes)), SHA256: body.SHA256, Origin: "catalog", Basis: "matchedTrack", Status: "available", ExpiresAt: time.Now().UTC().Add(enrichment.CandidateLifetime)}
	if err := s.repo.ReserveEnrichmentPreview(ctx, w, p.SizeBytes); err != nil {
		return p, err
	}
	return s.storePreview(ctx, w, input, p, body.Bytes)
}

// LocalLyrics 为手动预览使用已启用的本地插件，取出的歌词始终绑定原曲目版本。
func (s *Service) LocalLyrics(ctx context.Context, owner, source string, _ scraper.Query) (onlinescrape.LocalLyricsResult, error) {
	var result onlinescrape.LocalLyricsResult
	for _, r := range s.runtimeSnapshot() {
		if r.Owner != owner || r.Descriptor.PluginID != onlinescrape.LocalLyricsPluginID || r.Host == nil {
			continue
		}
		lease, err := r.Gate.AcquireEpoch(ctx, r.Epoch)
		if err != nil {
			return result, err
		}
		defer lease.Close()
		if lease != nil {
			ctx = lease.Context()
		}
		task := domain.NewID()
		fields := []string{"lyrics"}
		input, err := s.library.OpenScrape(ctx, owner, task, source, fields)
		if err != nil {
			return result, err
		}
		defer input.Close()
		if err := input.DecodeAudio(ctx, s.decoder()); err != nil {
			return result, err
		}
		scope := pluginproto.Context{UserID: owner, InstanceID: r.ID, TaskID: task, TimeoutMs: 30000}
		session, err := r.Host.Open(ctx, scope, input.Target, input.Inputs)
		if err != nil {
			return result, domain.Fail(failureCode(err), "Local lyrics scraper is unavailable")
		}
		defer session.Close()
		found, err := session.Search(ctx, fields, nil, nil)
		if err != nil {
			return result, err
		}
		if err := enrichment.ValidateSearchResult(found, input.Target, fields); err != nil {
			return result, err
		}
		result.AssetID, result.AssetVersion = input.Target.AssetID, input.Target.AssetVersion
		for _, c := range found.Candidates {
			if c.Status != "available" {
				continue
			}
			body := previewWriter{limit: c.SizeBytes}
			if _, err := session.Fetch(ctx, c.CandidateID, &body); err != nil {
				return result, err
			}
			title := input.Query.Title
			if title == "" {
				title = c.Basis
			}
			result.Items = append(result.Items, scraper.Item{ID: c.CandidateID, Title: title, Artist: strings.Join(input.Query.Artists, ", "), Lyrics: string(body.Bytes()), Format: c.Format})
		}
		return result, input.Validate()
	}
	return result, nil
}
