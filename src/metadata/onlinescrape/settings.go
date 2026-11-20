package onlinescrape

import (
	"context"
	"net/http"
	"sort"

	"github.com/fnMrRice/MusicFS-server/sdk/scraper"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/plugins/settings"
)

// NewWithSettings 让查询和媒体读取使用当前用户、当前插件已保存的网络设置。
func NewWithSettings(plugins []scraper.Plugin, repo settings.Repository) (*Service, error) {
	s, err := New(plugins)
	if err == nil {
		s.settings = repo
	}
	return s, err
}

func (s *Service) SettingsProviders() []settings.Provider {
	providers := []settings.Provider{}
	for id, plugin := range s.plugins {
		if configurable, ok := plugin.(scraper.SettingsProvider); ok {
			providers = append(providers, settings.Provider{ID: id, Schema: configurable.SettingsSchema(), Validate: func(values settings.Values) error {
				return configurable.ValidateSettings(map[string]any(values))
			}})
		}
	}
	sort.Slice(providers, func(i, j int) bool { return providers[i].ID < providers[j].ID })
	return providers
}

func (s *Service) currentSettings(ctx context.Context, owner, id string) (settings.Record, error) {
	if _, ok := s.plugins[id].(scraper.SettingsProvider); ok && s.settings != nil {
		return s.settings.GetPluginSettings(ctx, owner, id)
	}
	return settings.Record{}, nil
}

func (s *Service) configured(id string, record settings.Record, fallback *http.Client) (scraper.Plugin, *http.Client, func(), error) {
	plugin := s.plugins[id]
	configurable, ok := plugin.(scraper.SettingsProvider)
	if !ok {
		return plugin, fallback, func() {}, nil
	}
	values := settings.Defaults(configurable.SettingsSchema())
	if record.Values != nil {
		var err error
		values, err = settings.Normalize(configurable.SettingsSchema(), record.Values)
		if err != nil {
			return nil, nil, nil, err
		}
	}
	if err := configurable.ValidateSettings(map[string]any(values)); err != nil {
		return nil, nil, nil, err
	}
	configured, client, err := configurable.WithSettings(map[string]any(values))
	if err != nil {
		return nil, nil, nil, err
	}
	if configured == nil || client == nil {
		return nil, nil, nil, domain.Fail("UNSUPPORTED", "Scraper did not provide a configured client")
	}
	return configured, client, client.CloseIdleConnections, nil
}
