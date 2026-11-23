package playback

import (
	"context"
	"io"
	"sync/atomic"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/sdk/provider"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library"
)

// prepareOnline 只验证权限和播放能力；建立会话不触发上游音频请求。
func (s *Service) prepareOnline(ctx context.Context, owner string, t library.Track, r CreateRequest) (*preparedInput, error) {
	input, err := s.networkInput(ctx, t)
	if err != nil {
		return nil, err
	}
	ctx = provider.WithPlaybackQuality(ctx, input.preferences.Quality)
	audio, err := s.library.DescribeNetworkAudio(ctx, t)
	if err != nil {
		return nil, err
	}
	if audio.Validate() != nil || audio.ExternalID != t.ExternalID {
		return nil, domain.Fail("PROTOCOL_MISMATCH", "Invalid network audio description")
	}
	if !supports(r.Capabilities, audio.MIME) {
		return nil, domain.Fail("UNSUPPORTED", "Client does not support the network audio format")
	}
	t.MIME, t.Codec, t.Container = audio.MIME, &audio.Codec, &audio.Container
	processor, _ := s.audioSnapshot()
	processor, err = s.processorForUser(ctx, owner, processor, r.Preferences.DSPExecution == "client")
	if err != nil {
		return nil, err
	}
	if r.Preferences.ForceProcessing || r.Preferences.DSPExecution != "client" && (s.config.RequireDSP || r.Preferences.GainDB != 0 || processor.HasDSP() || processor.RequiresChannelConversion()) {
		return nil, domain.Fail("UNSUPPORTED", "Server audio processing is not available for online streams")
	}
	lease, err := s.library.HoldReadLifetime(ctx, s.ctx, owner, t.ProviderID)
	if err != nil {
		return nil, err
	}
	input.audio = audio
	return &preparedInput{service: s, track: t, lease: lease, online: true, network: input, dspExecution: r.Preferences.DSPExecution}, nil
}

func (p *preparedInput) describeOnline(id string, expiry time.Time, transport string) Description {
	return Description{SessionID: id, SourceTrackID: p.track.ID, AssetID: p.track.AssetID, DeliveryMode: "proxy", OutputProfile: OutputProfile{MIME: p.track.MIME, Codec: p.track.Codec, Container: p.track.Container, Transport: transport}, ExpiresAt: expiry, SeekMode: "none", DSPExecution: p.dspExecution, EffectiveProcessing: []Processing{}, ResumeDecision: "continue"}
}

// onlineStream 在已有会话鉴权和实例租约之下按需打开一个顺序响应。
// 同一 attempt 不可再次打开，避免没有版本证据时把不同请求当成同一表示。
func (s *Service) onlineStream(ctx context.Context, a *session, id, attempt string, head bool) (*Stream, error) {
	if a.active >= 4 {
		return nil, domain.Fail("RATE_LIMITED", "Too many stream consumers")
	}
	streamCtx, cancel := context.WithCancel(a.attemptCtx)
	unlink := context.AfterFunc(ctx, cancel)
	a.active++
	stream := &Stream{Description: a.description, Context: streamCtx, Online: true, Cancel: func() { unlink(); cancel() }}
	stream.release = func() { s.mu.Lock(); a.active--; s.mu.Unlock() }
	stream.openOnline = func() (io.ReadCloser, error) {
		s.mu.Lock()
		if a.onlineOpened || a.description.AttemptID != attempt || a.ctx.Err() != nil {
			s.mu.Unlock()
			return nil, domain.Fail("RESTART_REQUIRED", "Online playback must restart with a new session")
		}
		a.onlineOpened = true
		s.mu.Unlock()
		body, err := s.networkReader(streamCtx, a)
		if err != nil {
			a.cancel()
			return nil, err
		}
		return &boundedOnlineReader{ReadCloser: body, remaining: pluginproto.MaxOnlineAudioBytes, cancel: a.cancel}, nil
	}
	return stream, nil
}

func (s *Stream) OpenOnline() (io.ReadCloser, error) {
	if !s.Online || s.openOnline == nil {
		return nil, domain.Fail("UNSUPPORTED", "Online stream is unavailable")
	}
	return s.openOnline()
}

type boundedOnlineReader struct {
	io.ReadCloser
	remaining int64
	cancel    context.CancelFunc
	complete  atomic.Bool
}

func (r *boundedOnlineReader) Read(p []byte) (int, error) {
	if r.remaining <= 0 {
		var extra [1]byte
		n, err := r.ReadCloser.Read(extra[:])
		if n == 0 && err == io.EOF {
			r.complete.Store(true)
			return 0, io.EOF
		}
		r.cancel()
		return 0, domain.Fail("RATE_LIMITED", "Online audio exceeded its byte budget")
	}
	if int64(len(p)) > r.remaining {
		p = p[:r.remaining]
	}
	n, err := r.ReadCloser.Read(p)
	r.remaining -= int64(n)
	if err != nil && err != io.EOF {
		r.cancel()
	}
	if err == io.EOF {
		r.complete.Store(true)
	}
	return n, err
}

func (r *boundedOnlineReader) Close() error {
	if !r.complete.Load() {
		r.cancel()
	}
	return r.ReadCloser.Close()
}
