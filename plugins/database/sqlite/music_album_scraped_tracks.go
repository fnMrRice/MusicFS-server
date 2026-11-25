package sqlite

import (
	"cmp"
	"context"
	"database/sql"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/scraper"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library/music"
	"github.com/fnMrRice/MusicFS-server/src/library/works"
)

type albumScrapeTrack struct {
	track          scraper.Track
	previous       *scraper.Track
	input          music.TrackInput
	slot, prior    string
	override       albumSlotOverride
	creditVariants [][]string
}

func albumReleaseTracks(ctx context.Context, tx *sql.Tx, album string, field works.ScrapedField) ([]albumScrapeTrack, error) {
	bySlot := map[string]scrapedAlbumRef{}
	byID := map[string]string{}
	err := musicRows(ctx, tx, `SELECT slot,track_id,external_id FROM music_album_scraped_track_refs
 WHERE album_id=? AND plugin_id=? AND release_id=?`, func(row *sql.Rows) error {
		var r scrapedAlbumRef
		if err := row.Scan(&r.slot, &r.track, &r.external); err != nil {
			return err
		}
		bySlot[r.slot] = r
		if _, seen := byID[r.external]; seen {
			byID[r.external] = ""
		} else {
			byID[r.external] = r.track
		}
		return nil
	}, album, field.PluginID, field.Item.ID)
	if err != nil {
		return nil, err
	}
	overrides, err := albumSlotOverrides(ctx, tx, album, field.PluginID, field.Item.ID)
	if err != nil {
		return nil, err
	}
	previous, err := previousBangumiChapters(ctx, tx, album, field)
	if err != nil {
		return nil, err
	}
	incoming := make([]albumScrapeTrack, 0, len(field.Item.Tracks))
	slots := map[string]int{}
	idCounts := map[string]int{}
	for _, track := range field.Item.Tracks {
		idCounts[track.ExternalID]++
	}
	for pos, track := range field.Item.Tracks {
		credits := scrapedArtistNames(cmp.Or(track.Artist, field.Item.Artist))
		input, err := (music.TrackInput{TrackFields: music.TrackFields{Title: track.Title, LocalizedTitles: track.LocalizedTitles}, ArtistNames: credits}).Normalize()
		if err != nil {
			return nil, err
		}
		slot := fmt.Sprintf("%d:%d", track.Disc, track.Number)
		if track.Number == 0 {
			slot = fmt.Sprintf("index:%d", pos)
		}
		slots[slot]++
		if slots[slot] > 1 {
			slot = fmt.Sprintf("%s:%d", slot, slots[slot])
		}
		prior := ""
		var chapter *scraper.Track
		if track.ExternalID != "" && idCounts[track.ExternalID] == 1 {
			prior = byID[track.ExternalID]
			if prior != "" {
				chapter = previous[track.ExternalID]
			}
		}
		if r := bySlot[slot]; prior == "" && (r.external == "" || track.ExternalID == "" || r.external == track.ExternalID) {
			prior = r.track
		}
		override := overrides["slot:"+slot]
		if track.ExternalID != "" && idCounts[track.ExternalID] == 1 {
			if stable, ok := overrides["id:"+track.ExternalID]; ok {
				override = stable
			}
		}
		// 各语言给出的都是完整署名。带空格的 & 也可能是组合名，只增加候选拆法。
		var variants [][]string
		appendCredit := func(name string) {
			if strings.TrimSpace(name) == "" {
				return
			}
			variants = append(variants, scrapedArtistNames(name))
			if strings.Contains(name, " & ") {
				variants = append(variants, scrapedArtistNames(strings.ReplaceAll(name, " & ", "/")))
			}
		}
		appendCredit(track.Artist)
		for _, name := range track.LocalizedArtistNames {
			appendCredit(name.Title)
		}
		incoming = append(incoming, albumScrapeTrack{track: track, previous: chapter, input: input, slot: slot, prior: prior, override: override, creditVariants: variants})
	}
	return incoming, nil
}

// 将上游别名追加到已有曲目。上游未声明曲名语言时保留 und，不根据字形推测语言。
func mergedScrapedTitles(display string, existing []scraper.LocalizedTitle, t music.TrackInput) ([]scraper.LocalizedTitle, error) {
	titles := slices.Clone(existing)
	for _, name := range t.LocalizedTitles {
		if !slices.ContainsFunc(titles, func(old scraper.LocalizedTitle) bool {
			return old.Language == name.Language && strings.EqualFold(old.Title, name.Title)
		}) {
			titles = append(titles, name)
		}
	}
	if !strings.EqualFold(display, t.Title) && !slices.ContainsFunc(titles, func(name scraper.LocalizedTitle) bool { return strings.EqualFold(name.Title, t.Title) }) {
		titles = append(titles, scraper.LocalizedTitle{Language: "und", Title: t.Title})
	}
	if len(titles) > scraper.MaxLocalizedTitles {
		return nil, domain.Fail("INVALID_ARGUMENT", "Too many localized track titles")
	}
	return titles, nil
}

func createScrapedAlbumTrack(ctx context.Context, tx *sql.Tx, owner string, in music.TrackInput) (string, error) {
	id := domain.NewID()
	if _, err := tx.ExecContext(ctx, `INSERT INTO music_tracks(id,owner,title,origin,metadata_locked,scraper_placeholder) VALUES(?,?,?,'manual',1,1)`, id, owner, in.Title); err != nil {
		return "", err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := tx.ExecContext(ctx, "INSERT INTO recordings(id,owner,created_at,updated_at) VALUES(?,?,?,?)", id, owner, now, now); err != nil {
		return "", err
	}
	credits, err := scrapedCreditNames(ctx, tx, owner, in.ArtistNames)
	if err != nil {
		return "", err
	}
	return id, writeMusicCredits(ctx, tx, owner, id, credits)
}

// 保留本地曲名和所有来源，只用确认的曲目替换无人使用的自动占位。
func syncMusicAlbumScrapedTracks(ctx context.Context, tx *sql.Tx, owner, album string, field works.ScrapedField, manual bool) error {
	members, err := albumMatchingMembers(ctx, tx, owner, album)
	if err != nil {
		return err
	}
	tracks, err := albumReleaseTracks(ctx, tx, album, field)
	if err != nil {
		return err
	}
	if manual && slices.ContainsFunc(tracks, func(t albumScrapeTrack) bool { return members[t.prior] != nil && members[t.prior].disposable }) {
		if err := addPendingAlbumMembers(ctx, tx, owner, album, tracks, members); err != nil {
			return err
		}
	}
	chosen := chooseAlbumMembers(tracks, members)
	if _, err := tx.ExecContext(ctx, "DELETE FROM music_album_scraped_track_refs WHERE album_id=?", album); err != nil {
		return err
	}
	used := map[string]bool{}
	positions := map[string]int{}
	changedPending := map[string]bool{}
	for i, id := range chosen {
		if id != "" {
			if _, ok := positions[id]; !ok || tracks[i].override.preferred {
				positions[id] = i
			}
		}
	}
	for i, t := range tracks {
		id := chosen[i]
		member := members[id]
		if member == nil {
			id, err = createScrapedAlbumTrack(ctx, tx, owner, t.input)
			if err != nil {
				return err
			}
			member = &scrapedAlbumMember{id: id, title: t.input.Title}
		}
		used[id] = true
		if err = updateScrapedAlbumMember(ctx, tx, owner, member, t); err != nil {
			return err
		}
		if len(member.pending) > 0 {
			if err = bindPendingAlbumSources(ctx, tx, album, member, t, changedPending); err != nil {
				return err
			}
		}
		if pos, ok := positions[id]; ok && pos != i {
			continue
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO music_manual_album_tracks(album_id,track_id,position) VALUES(?,?,?)
 ON CONFLICT(album_id,track_id) DO UPDATE SET position=excluded.position`, album, id, i); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO music_album_scraped_track_refs(album_id,slot,track_id,plugin_id,release_id,position,external_id)
 VALUES(?,?,?,?,?,?,?)`, album, t.slot, id, field.PluginID, field.Item.ID, i, t.track.ExternalID); err != nil {
			return err
		}
	}
	for _, t := range tracks {
		if prior := members[t.prior]; prior != nil && prior.disposable && !used[prior.id] {
			if _, err := tx.ExecContext(ctx, "DELETE FROM music_tracks WHERE id=? AND owner=? AND scraper_placeholder=1", prior.id, owner); err != nil {
				return err
			}
			if err := deleteUnusedMusicRecording(ctx, tx, owner, prior.id); err != nil {
				return err
			}
		}
	}
	return nil
}
