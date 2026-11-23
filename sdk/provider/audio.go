package provider

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"sync"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
)

// AudioClient 通过单次受控响应交付完整音频，不跨响应拼接字节。
type AudioClient interface {
	DescribeAudio(context.Context, string) (pluginproto.OnlineAudio, error)
	OpenAudio(context.Context, string) (pluginproto.OnlineAudio, io.ReadCloser, error)
}

type audioStream struct {
	mu           sync.Mutex
	body         io.ReadCloser
	cancel       context.CancelFunc
	offset, size int64
	expires      time.Time
}

func (w *worker) openAudio(ctx context.Context, client Client, id, quality string) (pluginproto.OnlineAudioOpened, error) {
	var empty pluginproto.OnlineAudioOpened
	provider, ok := client.(AudioClient)
	if !ok {
		return empty, unsupported()
	}
	w.audioMu.Lock()
	defer w.audioMu.Unlock()
	for id, stream := range w.audio {
		if time.Now().After(stream.expires) {
			stream.cancel()
			stream.body.Close()
			delete(w.audio, id)
		}
	}
	if len(w.audio) >= 2 {
		return empty, pluginproto.Failure("RATE_LIMITED", "Online audio stream limit reached")
	}
	// 音频响应生命周期独立于短控制请求，仍由工作进程和播放会话上限约束。
	lifetime, cancel := context.WithTimeout(w.ctx, 35*time.Minute)
	stop := context.AfterFunc(ctx, cancel)
	audio, body, err := provider.OpenAudio(WithPlaybackQuality(lifetime, quality), id)
	stop()
	if err == nil {
		err = audio.Validate()
	}
	if err == nil && (body == nil || audio.ExternalID != id) {
		err = pluginproto.Failure("PROTOCOL_MISMATCH", "Online audio identity mismatch")
	}
	if err != nil {
		cancel()
		if body != nil {
			body.Close()
		}
		return empty, err
	}
	var random [24]byte
	if _, err = rand.Read(random[:]); err != nil {
		cancel()
		body.Close()
		return empty, err
	}
	ref := hex.EncodeToString(random[:])
	w.audio[ref] = &audioStream{body: body, cancel: cancel, size: audio.SizeBytes, expires: time.Now().Add(35 * time.Minute)}
	return pluginproto.OnlineAudioOpened{Audio: audio, StreamID: ref}, nil
}

func (w *worker) readAudio(ctx context.Context, r pluginproto.OnlineAudioRead, closeOnly bool) (pluginproto.OnlineAudioChunk, error) {
	var empty pluginproto.OnlineAudioChunk
	w.audioMu.Lock()
	stream := w.audio[r.StreamID]
	if closeOnly {
		delete(w.audio, r.StreamID)
	}
	w.audioMu.Unlock()
	if stream == nil {
		return empty, pluginproto.Failure("NOT_FOUND", "Online audio stream is unavailable")
	}
	if closeOnly {
		stream.cancel()
		return empty, stream.body.Close()
	}
	stream.mu.Lock()
	defer stream.mu.Unlock()
	if r.Offset != stream.offset || time.Now().After(stream.expires) {
		return empty, pluginproto.Failure("RESOURCE_CHANGED", "Online audio stream cannot resume")
	}
	stop := context.AfterFunc(ctx, func() { stream.cancel(); stream.body.Close() })
	defer stop()
	limit := stream.size
	if limit <= 0 {
		limit = pluginproto.MaxOnlineAudioBytes
	}
	remaining := limit - stream.offset
	data := make([]byte, min(int64(pluginproto.OnlineAudioChunkBytes), remaining+1))
	n, err := stream.body.Read(data)
	if err != nil && err != io.EOF {
		return empty, pluginproto.Failure("TEMPORARY_FAILURE", "Online audio read failed")
	}
	if n == 0 && err == nil {
		return empty, pluginproto.Failure("TEMPORARY_FAILURE", "Online audio made no progress")
	}
	if int64(n) > remaining || (stream.size > 0 && err != nil && int64(n) != remaining) {
		return empty, pluginproto.Failure("RESOURCE_CHANGED", "Online audio length changed")
	}
	chunk := pluginproto.OnlineAudioChunk{Offset: stream.offset, Data: data[:n], EOF: err != nil}
	stream.offset += int64(n)
	if chunk.EOF {
		stream.cancel()
		stream.body.Close()
		w.audioMu.Lock()
		delete(w.audio, r.StreamID)
		w.audioMu.Unlock()
	}
	return chunk, nil
}

func (w *worker) closeAudio() {
	w.audioMu.Lock()
	defer w.audioMu.Unlock()
	for id, stream := range w.audio {
		stream.cancel()
		stream.body.Close()
		delete(w.audio, id)
	}
}
