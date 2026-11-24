package onlinescrape

import (
	"context"
	"encoding/json"
	"errors"
	"slices"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library/works"
	"github.com/fnMrRice/MusicFS-server/src/plugins/settings"
)

const lyricsAutoSettingsID = "musicfs.library.lyrics-auto-scrape"
const LocalLyricsPluginID = "musicfs.scraper.local"

type LyricsAutoSettings struct {
	Revision    int64    `json:"revision"`
	PluginOrder []string `json:"pluginOrder"`
}

type LyricsAutoSettingsInput struct {
	ExpectedRevision *int64   `json:"expectedRevision"`
	PluginOrder      []string `json:"pluginOrder"`
}

func decodeLyricsOrder(record settings.Record) (LyricsAutoSettings, error) {
	page := LyricsAutoSettings{Revision: record.Revision}
	if record.Values == nil {
		page.PluginOrder = []string{LocalLyricsPluginID, "musicfs.scraper.netease", "musicfs.scraper.qqmusic"}
		return page, nil
	}
	raw, err := json.Marshal(record.Values["pluginOrder"])
	if err != nil || json.Unmarshal(raw, &page.PluginOrder) != nil || page.PluginOrder == nil {
		return LyricsAutoSettings{}, domain.Fail("TEMPORARY_FAILURE", "Stored lyrics scraper order is invalid")
	}
	return page, nil
}

func (s *Service) LyricsAutoSettings(ctx context.Context, owner string) (LyricsAutoSettings, error) {
	if s.settings == nil {
		return decodeLyricsOrder(settings.Record{})
	}
	record, err := s.settings.GetPluginSettings(ctx, owner, lyricsAutoSettingsID)
	if err != nil {
		return LyricsAutoSettings{}, err
	}
	return decodeLyricsOrder(record)
}

func (s *Service) SaveLyricsAutoSettings(ctx context.Context, owner string, input LyricsAutoSettingsInput, key string) (LyricsAutoSettings, error) {
	if input.ExpectedRevision == nil || *input.ExpectedRevision < 0 || input.PluginOrder == nil || len(input.PluginOrder) > 32 || len(key) < 8 || !works.ValidID(key) {
		return LyricsAutoSettings{}, domain.Fail("INVALID_ARGUMENT", "A revision, lyrics scraper order and idempotency key are required")
	}
	if s.settings == nil {
		return LyricsAutoSettings{}, domain.Fail("UNSUPPORTED", "Lyrics scraper settings storage is unavailable")
	}
	record, err := s.settings.GetPluginSettings(ctx, owner, lyricsAutoSettingsID)
	if err != nil {
		return LyricsAutoSettings{}, err
	}
	replay := func(record settings.Record) (LyricsAutoSettings, error) {
		saved, err := decodeLyricsOrder(record)
		if err == nil && (saved.Revision != *input.ExpectedRevision+1 || !slices.Equal(saved.PluginOrder, input.PluginOrder)) {
			err = domain.Fail("CONFLICT", "Lyrics scraper settings key was already used")
		}
		return saved, err
	}
	if record.Values["operationKey"] == key {
		return replay(record)
	}
	for i, id := range input.PluginOrder {
		if slices.Contains(input.PluginOrder[:i], id) {
			return LyricsAutoSettings{}, domain.Fail("INVALID_ARGUMENT", "Repeated lyrics scraper")
		}
		if id == LocalLyricsPluginID || id == "musicfs.scraper.netease" || id == "musicfs.scraper.qqmusic" {
			continue
		}
		plugin := s.plugins[id]
		if plugin == nil || !slices.Contains(plugin.Descriptor().Fields, "lyrics") || plugin.Descriptor().RequiresExternalID {
			return LyricsAutoSettings{}, domain.Fail("INVALID_ARGUMENT", "Scraper must support lyrics search by track name")
		}
	}
	record, err = s.settings.PutPluginSettings(ctx, owner, lyricsAutoSettingsID, *input.ExpectedRevision,
		settings.Values{"pluginOrder": input.PluginOrder, "operationKey": key})
	if err != nil {
		var conflict *domain.Error
		if errors.As(err, &conflict) && conflict.Code == "CONFLICT" {
			latest, readErr := s.settings.GetPluginSettings(ctx, owner, lyricsAutoSettingsID)
			if readErr != nil {
				return LyricsAutoSettings{}, readErr
			}
			if latest.Values["operationKey"] == key {
				return replay(latest)
			}
		}
		return LyricsAutoSettings{}, err
	}
	return decodeLyricsOrder(record)
}
