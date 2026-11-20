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

const albumAutoSettingsID = "musicfs.library.album-auto-scrape"

type AlbumAutoSettings struct {
	Revision         int64    `json:"revision"`
	LocalPluginOrder []string `json:"localPluginOrder"`
}

type AlbumAutoSettingsInput struct {
	ExpectedRevision *int64   `json:"expectedRevision"`
	LocalPluginOrder []string `json:"localPluginOrder"`
}

func decodeAlbumAutoSettings(record settings.Record) (AlbumAutoSettings, error) {
	if record.Values == nil {
		return AlbumAutoSettings{LocalPluginOrder: []string{"musicfs.scraper.musicbrainz", "musicfs.scraper.itunes", "musicfs.scraper.netease"}}, nil
	}
	page := AlbumAutoSettings{Revision: record.Revision}
	raw, err := json.Marshal(record.Values["localPluginOrder"])
	if err != nil || json.Unmarshal(raw, &page.LocalPluginOrder) != nil || len(page.LocalPluginOrder) == 0 {
		return AlbumAutoSettings{}, domain.Fail("TEMPORARY_FAILURE", "Stored album scraper order is invalid")
	}
	return page, nil
}

// AlbumAutoSettings 读取当前用户的本地专辑刮削顺序，首次使用时返回默认顺序。
func (s *Service) AlbumAutoSettings(ctx context.Context, owner string) (AlbumAutoSettings, error) {
	if s.settings == nil {
		return decodeAlbumAutoSettings(settings.Record{})
	}
	record, err := s.settings.GetPluginSettings(ctx, owner, albumAutoSettingsID)
	if err != nil {
		return AlbumAutoSettings{}, err
	}
	return decodeAlbumAutoSettings(record)
}

func (s *Service) SaveAlbumAutoSettings(ctx context.Context, owner string, input AlbumAutoSettingsInput, key string) (AlbumAutoSettings, error) {
	if input.ExpectedRevision == nil || *input.ExpectedRevision < 0 || len(input.LocalPluginOrder) == 0 || len(key) < 8 || !works.ValidID(key) {
		return AlbumAutoSettings{}, domain.Fail("INVALID_ARGUMENT", "A revision, local scraper order and idempotency key are required")
	}
	if s.settings == nil {
		return AlbumAutoSettings{}, domain.Fail("UNSUPPORTED", "Album scraper settings storage is unavailable")
	}
	record, err := s.settings.GetPluginSettings(ctx, owner, albumAutoSettingsID)
	if err != nil {
		return AlbumAutoSettings{}, err
	}
	if record.Values["operationKey"] == key {
		return replayAlbumAutoSettings(record, input)
	}
	previous, err := decodeAlbumAutoSettings(record)
	if err != nil {
		return AlbumAutoSettings{}, err
	}
	for i, id := range input.LocalPluginOrder {
		if slices.Contains(input.LocalPluginOrder[:i], id) {
			return AlbumAutoSettings{}, domain.Fail("INVALID_ARGUMENT", "Repeated album scraper")
		}
		// 允许保留已配置或默认的离线来源，排序时无需先删除它。
		if slices.Contains(previous.LocalPluginOrder, id) {
			continue
		}
		plugin := s.plugins[id]
		if plugin == nil {
			return AlbumAutoSettings{}, domain.Fail("INVALID_ARGUMENT", "Unknown album scraper")
		}
		d := plugin.Descriptor()
		if !d.Available || d.RequiresExternalID || !slices.Contains(d.Fields, "albumDetails") || !slices.Contains(d.Fields, "albumTracks") {
			return AlbumAutoSettings{}, domain.Fail("INVALID_ARGUMENT", "Scraper must support album name search and complete tracks")
		}
	}
	record, err = s.settings.PutPluginSettings(ctx, owner, albumAutoSettingsID, *input.ExpectedRevision, settings.Values{
		"localPluginOrder": input.LocalPluginOrder, "operationKey": key,
	})
	if err != nil {
		// 两次相同请求并发时，修订检查的落后方可以重放已经保存的结果。
		var failure *domain.Error
		if errors.As(err, &failure) && failure.Code == "CONFLICT" {
			latest, readErr := s.settings.GetPluginSettings(ctx, owner, albumAutoSettingsID)
			if readErr != nil {
				return AlbumAutoSettings{}, readErr
			}
			if latest.Values["operationKey"] == key {
				return replayAlbumAutoSettings(latest, input)
			}
		}
		return AlbumAutoSettings{}, err
	}
	return AlbumAutoSettings{Revision: record.Revision, LocalPluginOrder: input.LocalPluginOrder}, nil
}

func replayAlbumAutoSettings(record settings.Record, input AlbumAutoSettingsInput) (AlbumAutoSettings, error) {
	previous, err := decodeAlbumAutoSettings(record)
	if err != nil {
		return AlbumAutoSettings{}, err
	}
	if previous.Revision == *input.ExpectedRevision+1 && slices.Equal(input.LocalPluginOrder, previous.LocalPluginOrder) {
		return previous, nil
	}
	return AlbumAutoSettings{}, domain.Fail("CONFLICT", "Album scraper settings key was already used")
}
