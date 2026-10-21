package playback

import (
	"context"
	"io"
	"log/slog"
	"strings"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/playback/processedcache"
)

type cacheScope struct {
	auth     processedcache.Authorization
	identity processedcache.Identity
	track    library.Track
}

// cacheScope is captured under the playback mutex; no subsequent attempt can
// mutate the immutable processing identity held by an active stream.
func (s *Service) cacheScope(a *session) cacheScope {
	tool := a.processor.ProcessingIdentity()
	return cacheScope{
		auth:  processedcache.Authorization{Owner: a.user, AuthID: a.owner, SessionID: a.description.SessionID, AttemptID: a.description.AttemptID},
		track: a.track,
		identity: processedcache.Identity{Owner: a.user, ProviderID: a.track.ProviderID, SourceTrackID: a.track.ID, AssetID: a.track.AssetID, Version: a.track.Version,
			InputSHA256: strings.Trim(a.etag, "\""), WorkerSHA256: tool.WorkerSHA256, FFmpegSHA256: tool.FFmpegSHA256, PluginVersion: tool.PluginVersion,
			PipelineRevision: tool.PipelineRevision, Profile: a.description.OutputProfile.ID, GainDB: a.gain, PositionMs: a.description.PositionOriginMs},
	}
}

func (s *Service) processCached(stream *Stream, scope cacheScope, output io.Writer) error {
	if _, err := scope.identity.Key(); err != nil {
		return stream.processDirect(output)
	}
	if lease := s.cache.Acquire(scope.identity); lease != nil {
		slog.DebugContext(stream.Context, "Playback processed cache hit", "sessionId", stream.Description.SessionID)
		defer lease.Close()
		if err := s.checkCacheSource(stream.Context, scope); err != nil {
			return err
		}
		s.mu.Lock()
		err := s.authorizeCacheLocked(stream.Context, scope, lease.Record, false)
		if err == nil {
			s.attachCachePinLocked(scope, lease.Pin())
		}
		s.mu.Unlock()
		if err != nil {
			return err
		}
		_, err = io.Copy(output, preparationReader{ctx: stream.Context, input: lease.File})
		return err
	}
	writer := s.cache.Begin(scope.identity)
	if writer == nil {
		slog.DebugContext(stream.Context, "Playback processed cache unavailable", "sessionId", stream.Description.SessionID)
		return stream.processDirect(output)
	}
	slog.DebugContext(stream.Context, "Playback processed cache fill started", "sessionId", stream.Description.SessionID)
	defer writer.Abort()
	// Deliver first. Cache exhaustion or failure never adds a failure to a
	// deliverable stream, and a blocked consumer still provides backpressure.
	if err := stream.processDirect(io.MultiWriter(output, writer)); err != nil {
		return err
	}
	if stream.Context.Err() != nil || s.checkCacheSource(stream.Context, scope) != nil {
		return nil
	}
	pin, err := writer.Complete(stream.Context, func(ctx context.Context, record processedcache.Record) error {
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.authorizeCacheLocked(ctx, scope, record, true)
	})
	if err == nil {
		s.mu.Lock()
		s.attachCachePinLocked(scope, pin)
		s.mu.Unlock()
		slog.DebugContext(stream.Context, "Playback processed cache published", "sessionId", stream.Description.SessionID)
	} else {
		logPlaybackFailure(stream.Context, "Playback processed cache publication failed", err, "sessionId", stream.Description.SessionID)
	}
	return nil
}

// A completed cache never substitutes for a fresh provider grant. The source
// lease already held by the session prevents managed writeback during playback.
func (s *Service) checkCacheSource(ctx context.Context, scope cacheScope) error {
	track, err := s.library.Track(ctx, scope.auth.Owner, scope.track.ID)
	if err != nil {
		return err
	}
	if target(track) != target(scope.track) || track.ProviderID != scope.track.ProviderID {
		return domain.Fail("RESOURCE_CHANGED", "Processing source changed")
	}
	task := domain.NewID()
	defer s.library.Revoke(track.ProviderID, task)
	handle, err := s.library.Open(ctx, track, task)
	if err != nil {
		return err
	}
	defer handle.Close()
	if err = handle.Validate(); err != nil {
		return domain.Fail("RESOURCE_CHANGED", "Processing source changed")
	}
	return ctx.Err()
}

// The cache manager never holds its lock across this callback. The final DB
// transaction checks current auth, source asset and playback attempt again.
func (s *Service) authorizeCacheLocked(ctx context.Context, scope cacheScope, r processedcache.Record, publish bool) error {
	a := s.sessions[scope.auth.SessionID]
	if s.ctx.Err() != nil || ctx.Err() != nil || a == nil || a.owner != scope.auth.AuthID || a.description.AttemptID != scope.auth.AttemptID || s.cacheScope(a).identity != scope.identity {
		return domain.Fail("CONFLICT", "Processing cache attempt changed")
	}
	repo := s.repo.(processedcache.Repository)
	if publish {
		return repo.PublishProcessedCache(ctx, r, scope.auth)
	}
	return repo.CheckProcessedCache(ctx, r, scope.auth)
}

func (s *Service) attachCachePinLocked(scope cacheScope, pin *processedcache.Pin) {
	a := s.sessions[scope.auth.SessionID]
	if a == nil || a.owner != scope.auth.AuthID || a.description.AttemptID != scope.auth.AttemptID {
		pin.Close()
		return
	}
	a.cachePin.Close()
	a.cachePin = pin
}
