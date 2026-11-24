package playback

import (
	"bytes"
	"context"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/core/auth"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/playback/onlinecache"
)

type networkFormat struct {
	ready    chan struct{}
	started  atomic.Bool
	once     sync.Once
	channels int
	version  onlinecache.Version
}

// 文件探测成功后使用该版本的完整实测结果，未知字段留空。
func measuredOnlineProfile(profile OutputProfile, v onlinecache.Version) OutputProfile {
	if !v.FormatMeasured {
		return profile
	}
	profile.MIME, profile.Codec, profile.Container = v.MIME, &v.Codec, &v.Container
	profile.BitrateBps = 0
	if v.BitrateBps != nil {
		profile.BitrateBps = int(*v.BitrateBps)
	}
	profile.SampleRate, profile.Channels = v.SampleRateHz, v.Channels
	profile.BitDepth = v.BitDepth
	return profile
}

func (f *networkFormat) finish(channels int) {
	f.once.Do(func() {
		f.channels = channels
		close(f.ready)
	})
}

func (f *networkFormat) available(profile OutputProfile) OutputProfile {
	if f != nil {
		select {
		case <-f.ready:
			profile = measuredOnlineProfile(profile, f.version)
			if f.channels > 0 {
				profile.Channels = f.channels
			}
		default:
		}
	}
	return profile
}

// 只在播放真正打开正文后探测。已下载文件与增长缓存都通过同一份表示读取。
func (s *Service) networkReader(ctx context.Context, a *session) (io.ReadCloser, error) {
	body, err := s.openNetworkReader(ctx, a)
	f := a.network.format
	if err != nil || f == nil {
		return body, err
	}
	// 授权响应在打开正文时才给出版本的情况，直接采用实际打开的完整文件。
	if h, ok := body.(interface {
		DownloadVersion() (onlinecache.Version, bool)
	}); ok {
		if version, found := h.DownloadVersion(); found {
			if f.started.CompareAndSwap(false, true) {
				f.version = version
				f.finish(version.Channels)
			}
			if version.MIME != a.network.profile.MIME && !supports(a.capabilities, version.MIME) {
				body.Close()
				return nil, domain.Fail("UNSUPPORTED", "Client does not support the downloaded audio format")
			}
			return body, nil
		}
	}
	if !a.network.profilePending || !f.started.CompareAndSwap(false, true) {
		return body, nil
	}
	if channels := a.network.profile.Channels; channels > 0 {
		f.finish(channels)
		return body, nil
	}
	codec := a.network.audio.Codec
	probe := func(ctx context.Context, input io.ReaderAt, size int64) int {
		if channels := onlineHeaderChannels(input, codec, size); channels != 0 {
			return channels
		}
		processor, _ := s.audioSnapshot()
		metadata, err := processor.ProbeOutputMetadata(ctx, io.NewSectionReader(input, 0, size), size)
		if err != nil || metadata.Channels < 1 || metadata.Channels > 32 {
			return 0
		}
		return metadata.Channels
	}
	if ra, ok := body.(onlinecache.SeekableAudio); ok && a.network.ranges != nil {
		size := a.network.ranges.size
		s.startNetworkProbe(ctx, f, func(run context.Context) int {
			return probe(run, formatReaderAt{run, ra, size}, size)
		})
		return body, nil
	}
	// 没有可独立寻址的文件时只旁听本次顺序读取的有界前缀，不预读或改变返回的字节。
	attempt := a.attemptCtx
	return &prefixObserver{ReadCloser: body, codec: codec, finish: f.finish, inspect: func(prefix []byte) {
		s.startNetworkProbe(attempt, f, func(run context.Context) int {
			return probe(run, bytes.NewReader(prefix), int64(len(prefix)))
		})
	}}, nil
}

func (s *Service) startNetworkProbe(ctx context.Context, f *networkFormat, inspect func(context.Context) int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ctx.Err() != nil {
		f.finish(0)
		return
	}
	s.profileProbes.Go(func() {
		run, cancel := context.WithTimeout(ctx, 4*time.Second)
		defer cancel()
		f.finish(inspect(run))
	})
}

type formatReaderAt struct {
	ctx  context.Context
	body onlinecache.SeekableAudio
	size int64
}

func (r formatReaderAt) ReadAt(p []byte, offset int64) (int, error) {
	total := 0
	for total < len(p) {
		if err := r.ctx.Err(); err != nil {
			return total, err
		}
		n, err := r.body.ReadAvailableAt(r.ctx, p[total:], offset+int64(total), r.size)
		total += n
		if err != nil {
			return total, err
		}
		if n == 0 {
			return total, io.ErrNoProgress
		}
	}
	return total, nil
}

type prefixObserver struct {
	io.ReadCloser
	codec   string
	data    []byte
	finish  func(int)
	inspect func([]byte)
}

func (r *prefixObserver) Read(p []byte) (int, error) {
	const limit = 128 << 10
	n, err := r.ReadCloser.Read(p)
	if r.inspect != nil {
		r.data = append(r.data, p[:min(n, limit-len(r.data))]...)
		if channels := onlineHeaderChannels(bytes.NewReader(r.data), r.codec, int64(len(r.data))); channels != 0 {
			r.finish(channels)
			r.data, r.inspect = nil, nil
			return n, err
		}
		if len(r.data) == limit || err != nil {
			r.inspect(r.data)
			r.data, r.inspect = nil, nil
		}
	}
	return n, err
}

// AttemptOutputProfile 只读取当前尝试的格式。短暂等待已启动的本地探测，不触发音频读取。
func (s *Service) AttemptOutputProfile(ctx context.Context, who auth.Session, id, attempt string) (OutputProfile, error) {
	current := func() (OutputProfile, *networkFormat, error) {
		s.mu.Lock()
		defer s.mu.Unlock()
		a, err := s.find(who, id)
		if err != nil {
			return OutputProfile{}, nil, err
		}
		if a.description.AttemptID != attempt {
			return OutputProfile{}, nil, domain.Fail("CONFLICT", "Playback attempt is no longer current")
		}
		return a.description.OutputProfile, a.network.format, nil
	}
	profile, f, err := current()
	if err != nil || f == nil || !f.started.Load() {
		return profile, err
	}
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return OutputProfile{}, ctx.Err()
	case <-f.ready:
	case <-timer.C:
	}
	profile, f, err = current()
	return f.available(profile), err
}
