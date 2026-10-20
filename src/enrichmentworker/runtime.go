package enrichmentworker

import (
	"github.com/fnMrRice/MusicFS-server/plugins/decoder/ncm"
	"github.com/fnMrRice/MusicFS-server/src/domain"
)

func (s *Service) Runtime(id string) (Runtime, bool) {
	s.runtimeMu.RLock()
	defer s.runtimeMu.RUnlock()
	r, ok := s.runtimes[id]
	return r, ok
}
func (s *Service) runtimeSnapshot() []Runtime {
	s.runtimeMu.RLock()
	defer s.runtimeMu.RUnlock()
	return append([]Runtime{}, s.ordered...)
}
func (s *Service) UpdateRuntime(r Runtime) error {
	s.runtimeMu.Lock()
	defer s.runtimeMu.Unlock()
	old, ok := s.runtimes[r.ID]
	if !ok || old.Owner != r.Owner {
		return domain.Fail("NOT_FOUND", "Scraper instance not found")
	}
	r.Epoch = r.Gate.Epoch()
	s.runtimes[r.ID] = r
	for i := range s.ordered {
		if s.ordered[i].ID == r.ID {
			s.ordered[i] = r
		}
	}
	return nil
}
func (s *Service) UpdateDecoder(decoder *ncmdecoder.Processor) {
	s.runtimeMu.Lock()
	s.config.NCMDecoder = decoder
	s.runtimeMu.Unlock()
}
func (s *Service) decoder() *ncmdecoder.Processor {
	s.runtimeMu.RLock()
	defer s.runtimeMu.RUnlock()
	return s.config.NCMDecoder
}
