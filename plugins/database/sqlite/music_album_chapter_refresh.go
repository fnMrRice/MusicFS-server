package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"

	"github.com/fnMrRice/MusicFS-server/sdk/scraper"
	"github.com/fnMrRice/MusicFS-server/src/library/works"
)

// 同一发行的上一次快照可证明旧的混合曲名来自 Bangumi。缺少唯一章节编号时保留曲库显示名。
func previousBangumiChapters(ctx context.Context, tx *sql.Tx, album string, field works.ScrapedField) (map[string]*scraper.Track, error) {
	if field.PluginID != "musicfs.scraper.bangumi" {
		return nil, nil
	}
	var raw []byte
	err := tx.QueryRowContext(ctx, `SELECT document FROM music_album_scraped_fields WHERE album_id=? AND field='albumTracks'
 AND json_extract(document,'$.pluginId')=? AND json_extract(document,'$.item.id')=?`, album, field.PluginID, field.Item.ID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var saved works.ScrapedField
	if err := json.Unmarshal(raw, &saved); err != nil {
		return nil, err
	}
	chapters := make(map[string]*scraper.Track, len(saved.Item.Tracks))
	for i := range saved.Item.Tracks {
		track := &saved.Item.Tracks[i]
		if _, found := chapters[track.ExternalID]; found {
			chapters[track.ExternalID] = nil
		} else {
			chapters[track.ExternalID] = track
		}
	}
	return chapters, nil
}

func updateScrapedAlbumMember(ctx context.Context, tx *sql.Tx, owner string, member *scrapedAlbumMember, track albumScrapeTrack) error {
	display := member.title
	base := member.titles
	addCredits := false
	old := track.previous
	if old != nil && member.disposable && member.id == track.prior && member.title == old.Title {
		display = track.input.Title
		base = slices.DeleteFunc(slices.Clone(base), func(name scraper.LocalizedTitle) bool {
			return slices.Contains(old.LocalizedTitles, name) && !slices.Contains(track.input.LocalizedTitles, name)
		})
		// 旧章节只有标题，没有艺人。只补齐仍为空的曲库署名。
		addCredits = old.Artist == "" && len(member.credits) == 0 && track.track.Artist != ""
	}
	titles, err := mergedScrapedTitles(display, base, track.input)
	if err != nil {
		return err
	}
	if member.title == display && slices.Equal(member.titles, titles) && !addCredits {
		return nil
	}
	if addCredits {
		names, err := scrapedCreditNames(ctx, tx, owner, track.input.ArtistNames)
		if err != nil {
			return err
		}
		if err := writeMusicCredits(ctx, tx, owner, member.id, names); err != nil {
			return err
		}
	}
	raw, err := json.Marshal(titles)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE music_tracks SET title=?,localized_titles=?,revision=revision+1 WHERE id=?", display, string(raw), member.id); err != nil {
		return err
	}
	member.title, member.titles = display, titles
	return nil
}
