package enrichmentworker

import (
	"slices"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/plugins/audioruntime"
)

func (s *Service) Runtime(id string) (Runtime, bool) {
	s.runtimeMu.RLock()
	defer s.runtimeMu.RUnlock()
	r, ok := s.runtimes[id]
	r.Descriptor.Fields = slices.Clone(r.Descriptor.Fields)
	return r, ok
}
func (s *Service) runtimeSnapshot() []Runtime {
	s.runtimeMu.RLock()
	defer s.runtimeMu.RUnlock()
	result := append([]Runtime{}, s.ordered...)
	for i := range result {
		result[i].Descriptor.Fields = slices.Clone(result[i].Descriptor.Fields)
	}
	return result
}
func (s *Service) UpdateRuntime(r Runtime) error {
	s.runtimeMu.Lock()
	defer s.runtimeMu.Unlock()
	old, ok := s.runtimes[r.ID]
	if !ok || old.Owner != r.Owner {
		return domain.Fail("NOT_FOUND", "Scraper instance not found")
	}
	if r.Host != nil {
		r.Descriptor = r.Host.Descriptor()
	} else if r.Descriptor.PluginID == "" {
		r.Descriptor = old.Descriptor
	}
	if err := r.Descriptor.Validate(); err != nil {
		return err
	}
	if old.Descriptor.PluginID != "" && old.Descriptor.PluginID != r.Descriptor.PluginID {
		return domain.Fail("CONFLICT", "Scraper plugin identity changed")
	}
	r.Descriptor.Fields = slices.Clone(r.Descriptor.Fields)
	r.Epoch = r.Gate.Epoch()
	s.runtimes[r.ID] = r
	for i := range s.ordered {
		if s.ordered[i].ID == r.ID {
			s.ordered[i] = r
		}
	}
	return nil
}
func (s *Service) UpdateDecoder(decoder audioruntime.Decoder) {
	s.runtimeMu.Lock()
	s.config.Decoder = decoder
	s.runtimeMu.Unlock()
}
func (s *Service) decoder() audioruntime.Decoder {
	s.runtimeMu.RLock()
	defer s.runtimeMu.RUnlock()
	return s.config.Decoder
}
