package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"math"
	"slices"
	"strings"

	"github.com/fnMrRice/MusicFS-server/sdk/scraper"
)

type scrapedAlbumMember struct {
	id, title             string
	titles                []scraper.LocalizedTitle
	credits               [][]string
	sourced, disposable   bool
	shortestMS, longestMS int64
	pending               []pendingAlbumSource
	revision              int64
}

type scrapedAlbumRef struct{ slot, track, external string }

// 清理占位前保护跨专辑关系、手动合并、歌单及录音设置。m、x 分别表示曲目和专辑成员。
const disposableAlbumPlaceholder = `m.scraper_placeholder=1
 AND NOT EXISTS(SELECT 1 FROM music_sources WHERE owner=m.owner AND track_id=m.id)
 AND NOT EXISTS(SELECT 1 FROM music_manual_album_tracks WHERE track_id=m.id AND album_id<>x.album_id)
 AND NOT EXISTS(SELECT 1 FROM music_album_scraped_track_refs WHERE track_id=m.id AND album_id<>x.album_id)
 AND NOT EXISTS(SELECT 1 FROM music_album_merged_slots WHERE track_id=m.id)
 AND NOT EXISTS(SELECT 1 FROM music_playlist_entries WHERE owner=m.owner AND track_id=m.id)
 AND NOT EXISTS(SELECT 1 FROM recording_source_links WHERE recording_id=m.id)
 AND NOT EXISTS(SELECT 1 FROM recording_default_sources WHERE recording_id=m.id)
 AND NOT EXISTS(SELECT 1 FROM recording_cover_preferences WHERE recording_id=m.id)`

func albumMatchingMembers(ctx context.Context, tx *sql.Tx, owner, album string) (map[string]*scrapedAlbumMember, error) {
	members := map[string]*scrapedAlbumMember{}
	err := musicRows(ctx, tx, `SELECT m.id,m.title,m.localized_titles,
 EXISTS(SELECT 1 FROM music_sources WHERE track_id=m.id),
 `+disposableAlbumPlaceholder+`
 FROM music_album_tracks x JOIN music_tracks m ON m.id=x.track_id WHERE x.album_id=? AND m.owner=?
 AND (m.origin='manual' OR (m.origin='recording' AND m.metadata_locked=1) OR EXISTS(SELECT 1 FROM music_sources WHERE track_id=m.id))`, func(r *sql.Rows) error {
		m := &scrapedAlbumMember{}
		var titles []byte
		if err := r.Scan(&m.id, &m.title, &titles, &m.sourced, &m.disposable); err != nil {
			return err
		}
		members[m.id] = m
		return json.Unmarshal(titles, &m.titles)
	}, album, owner)
	if err != nil {
		return nil, err
	}
	err = musicRows(ctx, tx, `SELECT x.track_id,a.name,(SELECT json_group_array(n.name) FROM music_artist_localized_names n WHERE n.artist_id=a.id)
 FROM music_album_tracks x JOIN music_track_artists c ON c.track_id=x.track_id JOIN music_artists a ON a.id=c.artist_id
 WHERE x.album_id=? AND a.owner=? ORDER BY x.track_id,c.position`, func(r *sql.Rows) error {
		var id, name string
		var raw []byte
		if err := r.Scan(&id, &name, &raw); err != nil {
			return err
		}
		m := members[id]
		if m == nil {
			return nil
		}
		names := scrapedArtistNames(name)
		if len(names) == 1 {
			var aliases []string
			if err := json.Unmarshal(raw, &aliases); err != nil {
				return err
			}
			m.credits = append(m.credits, append(names, aliases...))
		} else {
			for _, one := range names {
				m.credits = append(m.credits, []string{one})
			}
		}
		return nil
	}, album, owner)
	if err != nil {
		return nil, err
	}
	err = musicRows(ctx, tx, `SELECT x.track_id,min(CAST(json_extract(t.document,'$.durationMs') AS BIGINT)),max(CAST(json_extract(t.document,'$.durationMs') AS BIGINT))
 FROM music_album_tracks x JOIN music_sources s ON s.track_id=x.track_id JOIN tracks t ON t.id=s.id
 WHERE x.album_id=? AND s.owner=? AND json_valid(t.document) AND json_type(t.document,'$.durationMs')='integer'
 AND CAST(json_extract(t.document,'$.durationMs') AS BIGINT)>0 GROUP BY x.track_id`, func(r *sql.Rows) error {
		var id string
		var shortest, longest int64
		if err := r.Scan(&id, &shortest, &longest); err != nil {
			return err
		}
		members[id].shortestMS, members[id].longestMS = shortest, longest
		return nil
	}, album, owner)
	return members, err
}

// 署名按完整名单比对。每个上游署名须唯一对应本曲一位歌手的主名或已保存别名。
func sameScrapedCredits(credits [][]string, incoming []string) bool {
	if len(credits) == 0 || len(credits) != len(incoming) {
		return false
	}
	used := make([]bool, len(credits))
	for _, name := range incoming {
		index := -1
		for i, choices := range credits {
			if slices.ContainsFunc(choices, func(s string) bool { return strings.EqualFold(s, name) }) {
				if index != -1 {
					return false
				}
				index = i
			}
		}
		if index < 0 || used[index] {
			return false
		}
		used[index] = true
	}
	return true
}

func (m *scrapedAlbumMember) matchesTitle(value string) bool {
	return strings.EqualFold(m.title, value) || slices.ContainsFunc(m.titles, func(a scraper.LocalizedTitle) bool { return strings.EqualFold(a.Title, value) })
}

func (m *scrapedAlbumMember) evidence(t albumScrapeTrack) int {
	equalName := m.matchesTitle(t.input.Title) || slices.ContainsFunc(t.input.LocalizedTitles, func(a scraper.LocalizedTitle) bool { return m.matchesTitle(a.Title) })
	hasTrackCredits := len(t.creditVariants) > 0
	artists := slices.ContainsFunc(t.creditVariants, func(names []string) bool { return sameScrapedCredits(m.credits, names) })
	if !hasTrackCredits {
		artists = sameScrapedCredits(m.credits, t.input.ArtistNames)
	}
	near := nearScrapedDuration(t.track.Duration, m.shortestMS, m.longestMS)
	if equalName {
		// 同名曲优先按艺人和时长区分；两个已知且不同的署名不算同一首。
		if !artists && len(m.credits) > 0 && hasTrackCredits {
			return 0
		}
		score := 3
		if artists {
			score++
		}
		if near {
			score++
		}
		if m.disposable {
			return 1
		}
		return score
	}
	if m.sourced && artists && near && hasTrackCredits {
		return 2
	}
	return 0
}

func nearScrapedDuration(seconds float64, shortest, longest int64) bool {
	duration := seconds * 1000
	return duration > 0 && shortest > 0 && math.Abs(float64(shortest)-duration) <= 1000 && math.Abs(float64(longest)-duration) <= 1000
}

type bestAlbumMatch struct {
	id           string
	score, count int
}

func (b *bestAlbumMatch) consider(id string, score int) {
	if score > b.score {
		b.id = id
		b.score = score
		b.count = 1
	} else if score == b.score {
		b.count++
	}
}

// 同一发行版的已确认来源与人工编辑身份优先。其余候选必须在两个方向上均有唯一最佳证据。
func chooseAlbumMembers(tracks []albumScrapeTrack, members map[string]*scrapedAlbumMember) []string {
	chosen := make([]string, len(tracks))
	used := map[string]bool{}
	for i, t := range tracks {
		if members[t.override.track] != nil {
			chosen[i] = t.override.track
			used[chosen[i]] = true
		}
	}
	for i, t := range tracks {
		if chosen[i] != "" {
			continue
		}
		if prior := members[t.prior]; prior != nil && !prior.disposable && !used[prior.id] {
			chosen[i] = prior.id
			used[prior.id] = true
		}
	}
	best := make([]bestAlbumMatch, len(tracks))
	byMember := map[string]*bestAlbumMatch{}
	for i, t := range tracks {
		if chosen[i] != "" {
			continue
		}
		for id, m := range members {
			if used[id] {
				continue
			}
			var score int
			if len(m.pending) > 0 {
				prior := members[t.prior]
				if prior == nil || !prior.disposable || !slices.ContainsFunc(m.pending, func(s pendingAlbumSource) bool { return s.slot == t.slot }) {
					continue
				}
				// 跨出暂存卡时只接受双向唯一的完整署名和时长，不用同名来打破并列。
				score = 2
			} else {
				score = m.evidence(t)
			}
			if score == 0 {
				continue
			}
			best[i].consider(id, score)
			if byMember[id] == nil {
				byMember[id] = &bestAlbumMatch{}
			}
			byMember[id].consider(t.slot, score)
		}
	}
	for i, b := range best {
		if b.count == 1 && byMember[b.id].count == 1 && byMember[b.id].id == tracks[i].slot {
			chosen[i] = b.id
			used[b.id] = true
		}
	}
	for i, t := range tracks {
		if chosen[i] == "" && members[t.prior] != nil && !used[t.prior] {
			chosen[i] = t.prior
			used[t.prior] = true
		}
	}
	return chosen
}

func scrapedArtistNames(value string) []string {
	names := []string{}
	for _, part := range strings.FieldsFunc(value, func(r rune) bool { return r == '/' || r == '／' || r == '×' || r == '、' }) {
		name := strings.TrimSpace(part)
		if name != "" && !slices.ContainsFunc(names, func(n string) bool { return strings.EqualFold(n, name) }) {
			names = append(names, name)
		}
	}
	return names
}
