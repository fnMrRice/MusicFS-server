package config

import "errors"

func (c Config) validateMetadata() error {
	p := c.Metadata.Automatic
	if _, err := p.Normalize(); err != nil {
		return err
	}
	scrapers := map[string]bool{}
	active := 0
	needed := p.Enabled
	for _, plugin := range c.Plugins {
		for _, instance := range plugin.Instances {
			enabled := plugin.CanStart(instance)
			if plugin.PluginID == "musicfs.scraper.local" {
				scrapers[instance.InstanceID] = enabled
				if enabled {
					active++
				}
			}
			if plugin.PluginID == "musicfs.localfiles" && enabled {
				defaults, err := EffectiveImportDefaults(plugin.PluginID, instance)
				if err != nil {
					return err
				}
				needed = needed || defaults.ImportLyrics
			}
		}
	}
	for _, id := range p.InstancePriority {
		_, exists := scrapers[id]
		if !exists {
			return errors.New("metadata priority must reference a configured local scraper instance")
		}
	}
	if needed {
		if len(scrapers) == 0 {
			return errors.New("automatic metadata or lyrics import requires a configured local scraper")
		}
		if len(p.InstancePriority) == 0 && active > 4 {
			return errors.New("automatic metadata requires at most four priority instances")
		}
	}
	return nil
}
