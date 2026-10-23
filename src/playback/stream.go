package playback

import (
	"context"
	"io"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/fnMrRice/MusicFS-server/plugins/decoder/ffmpeg"
	"github.com/fnMrRice/MusicFS-server/src/core/auth"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
)

type Stream struct {
	Description  Description
	File         *os.File
	ETag         string
	Size         int64
	Context      context.Context
	Cancel       context.CancelFunc
	release      func()
	once         sync.Once
	processor    *audioffmpeg.Processor
	gain         float64
	decodeOutput string
	process      func(io.Writer) error
	Online       bool
	openOnline   func() (io.ReadCloser, error)
}

func (s *Stream) Close() {
	s.once.Do(func() {
		s.Cancel()
		if s.File != nil {
			s.File.Close()
		}
		s.release()
	})
}
func (s *Stream) Process(output io.Writer) (err error) {
	started := time.Now()
	slog.DebugContext(s.Context, "Playback processing started", "sessionId", s.Description.SessionID)
	defer func() {
		if err != nil {
			logPlaybackFailure(s.Context, "Playback processing failed", err, "sessionId", s.Description.SessionID, "durationMs", time.Since(started).Milliseconds())
		} else {
			slog.DebugContext(s.Context, "Playback processing completed", "sessionId", s.Description.SessionID, "durationMs", time.Since(started).Milliseconds())
		}
	}()
	if s.process != nil {
		return s.process(output)
	}
	return s.processDirect(output)
}
func (s *Stream) processDirect(output io.Writer) error {
	return s.processor.Process(s.Context, audioffmpeg.Plan{TaskID: s.Description.AttemptID, AssetID: s.Description.AssetID, Profile: s.Description.OutputProfile.ID, GainDB: s.gain, PositionMs: s.Description.PositionOriginMs, DecodeOutput: decodePlanOutput(s.decodeOutput)}, s.File, s.Size, output)
}
func (s *Service) OpenStream(ctx context.Context, who auth.Session, id, attempt string, head bool) (result *Stream, resultErr error) {
	consumers := 0
	defer func() {
		if result != nil && slog.Default().Enabled(ctx, slog.Level(-8)) {
			slog.Log(ctx, slog.Level(-8), "Playback stream opened", "sessionId", id, "head", head, "consumers", consumers)
		}
	}()
	s.mu.Lock()
	defer s.mu.Unlock()
	a, err := s.find(who, id)
	if err != nil {
		return nil, err
	}
	if a.description.AttemptID != attempt {
		return nil, domain.Fail("CONFLICT", "Playback attempt is no longer current")
	}
	if a.online {
		return s.onlineStream(ctx, a, id, attempt, head)
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
	consumers = a.active
	stream := &Stream{Description: a.description, File: f, Size: a.size, ETag: a.etag, Context: streamCtx, processor: a.processor, gain: a.gain}
	stream.decodeOutput = a.decodeOutput
	if s.cache != nil && a.description.DeliveryMode == "processed" {
		scope := s.cacheScope(a)
		stream.process = func(output io.Writer) error { return s.processCached(stream, scope, output) }
	}
	stream.Cancel = func() { unlink(); closeFile(); cancel() }
	stream.release = func() {
		s.mu.Lock()
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
		s.collect()
		consumers := a.active
		s.mu.Unlock()
		if slog.Default().Enabled(context.Background(), slog.Level(-8)) {
			slog.Log(context.Background(), slog.Level(-8), "Playback stream closed", "sessionId", id, "consumers", consumers)
		}
	}
	return stream, nil
}
