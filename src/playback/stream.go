package playback

import (
	"context"
	"io"
	"os"
	"sync"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/auth"
	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/plugins/audioffmpeg"
)

type Stream struct {
	Description Description
	File        *os.File
	ETag        string
	Size        int64
	Context     context.Context
	Cancel      context.CancelFunc
	release     func()
	once        sync.Once
	processor   *audioffmpeg.Processor
	gain        float64
	process     func(io.Writer) error
}

func (s *Stream) Close() { s.once.Do(func() { s.Cancel(); s.File.Close(); s.release() }) }
func (s *Stream) Process(output io.Writer) error {
	if s.process != nil {
		return s.process(output)
	}
	return s.processDirect(output)
}
func (s *Stream) processDirect(output io.Writer) error {
	return s.processor.Process(s.Context, audioffmpeg.Plan{TaskID: s.Description.AttemptID, AssetID: s.Description.AssetID, Profile: "mp3-128", GainDB: s.gain, PositionMs: s.Description.PositionOriginMs}, s.File, s.Size, output)
}
func (s *Service) OpenStream(ctx context.Context, who auth.Session, id, attempt string, head bool) (*Stream, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, err := s.find(who, id)
	if err != nil {
		return nil, err
	}
	if a.description.AttemptID != attempt {
		return nil, domain.Fail("CONFLICT", "Playback attempt is no longer current")
	}
	if a.active >= 4 || (a.description.DeliveryMode == "processed" && !head && a.consumers[attempt] > 0) {
		return nil, domain.Fail("RATE_LIMITED", "Too many stream consumers")
	}
	f, err := os.Open(a.path)
	if err != nil {
		return nil, domain.Fail("SESSION_EXPIRED", "Playback representation is unavailable")
	}
	streamCtx, cancel := context.WithCancel(a.attemptCtx)
	unlink := context.AfterFunc(ctx, cancel)
	closeFile := context.AfterFunc(streamCtx, func() { f.Close() })
	a.active++
	path := a.path
	s.readers[path]++
	if !head {
		if a.consumers == nil {
			a.consumers = map[string]int{}
		}
		a.consumers[attempt]++
	}
	a.lastAccess = time.Now()
	stream := &Stream{Description: a.description, File: f, Size: a.size, ETag: a.etag, Context: streamCtx, processor: a.processor, gain: a.gain}
	if s.cache != nil && a.description.DeliveryMode == "processed" {
		scope := s.cacheScope(a)
		stream.process = func(output io.Writer) error { return s.processCached(stream, scope, output) }
	}
	stream.Cancel = func() { unlink(); closeFile(); cancel() }
	stream.release = func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		a.active--
		s.readers[path]--
		if s.readers[path] == 0 {
			delete(s.readers, path)
		}
		if !head {
			a.consumers[attempt]--
			if a.consumers[attempt] == 0 {
				delete(a.consumers, attempt)
			}
		}
		a.lastAccess = time.Now()
		s.collect()
	}
	return stream, nil
}
