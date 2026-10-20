package fingerprintworker

import (
	"context"
	"github.com/fnMrRice/MusicFS-server/plugins/decoder/ncm"
	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/identification"
	"github.com/fnMrRice/MusicFS-server/src/mediastore"
	"github.com/fnMrRice/MusicFS-server/src/plugincontrol"
	"time"
)

// NewDormant preserves durable job reads when the configured audio dependency
// is disabled at startup. Only a later validated runtime can admit new work.
func NewDormant(repo identification.FingerprintRepository, source identification.FingerprintSource, media *mediastore.Service, c Config) (*Service, error) {
	if c.Timeout == 0 {
		c.Timeout = 30 * time.Second
	}
	if repo == nil || source == nil || media == nil || c.MaxConcurrent < 1 || c.MaxConcurrent > 2 || c.Timeout < time.Second || c.Timeout > 2*time.Minute {
		return nil, domain.Fail("INVALID_ARGUMENT", "Invalid fingerprint dependencies")
	}
	return &Service{repo: repo, source: source, media: media, config: c, running: map[string]context.CancelFunc{}}, nil
}
func (s *Service) UpdateRuntime(extractor Extractor, decoder *ncmdecoder.Processor, gate *plugincontrol.Gate) {
	s.runtimeMu.Lock()
	s.extractor = extractor
	s.config.NCMDecoder = decoder
	s.gate = gate
	s.epoch = gate.Epoch()
	s.runtimeMu.Unlock()
}
func (s *Service) runtime() (Extractor, *ncmdecoder.Processor, *plugincontrol.Gate, uint64) {
	s.runtimeMu.RLock()
	defer s.runtimeMu.RUnlock()
	return s.extractor, s.config.NCMDecoder, s.gate, s.epoch
}
