package fingerprintworker

import (
	"context"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/metadata/identification"
	"github.com/fnMrRice/MusicFS-server/src/plugins/audioruntime"
	"github.com/fnMrRice/MusicFS-server/src/plugins/plugincontrol"
	"github.com/fnMrRice/MusicFS-server/src/storage/mediastore"
)

// NewDormant 在音频依赖或分析工具不可用时仍保留持久任务查询；
// 只有后续验证通过的运行实例才能接收新任务。
func NewDormant(repo identification.FingerprintRepository, source identification.FingerprintSource, media *mediastore.Service, c Config) (*Service, error) {
	if c.Timeout == 0 {
		c.Timeout = 30 * time.Second
	}
	if repo == nil || source == nil || media == nil || c.MaxConcurrent < 1 || c.MaxConcurrent > 2 || c.Timeout < time.Second || c.Timeout > 2*time.Minute {
		return nil, domain.Fail("INVALID_ARGUMENT", "Invalid fingerprint dependencies")
	}
	return &Service{repo: repo, source: source, media: media, config: c, running: map[string]context.CancelFunc{}}, nil
}
func (s *Service) UpdateRuntime(extractor Extractor, decoder audioruntime.Decoder, gate *plugincontrol.Gate) {
	s.runtimeMu.Lock()
	s.extractor = extractor
	s.config.Decoder = decoder
	s.gate = gate
	s.epoch = gate.Epoch()
	s.runtimeMu.Unlock()
}
func (s *Service) runtime() (Extractor, audioruntime.Decoder, *plugincontrol.Gate, uint64) {
	s.runtimeMu.RLock()
	defer s.runtimeMu.RUnlock()
	return s.extractor, s.config.Decoder, s.gate, s.epoch
}
