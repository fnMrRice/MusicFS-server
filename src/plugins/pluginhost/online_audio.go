package pluginhost

import (
	"context"
	"io"
	"sync"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
)

// DescribeAudio 仅查询播放描述，不打开或读取媒体响应。
func (w *OnlineWorker) DescribeAudio(ctx context.Context, id string) (pluginproto.OnlineAudio, error) {
	scope, err := w.newScope(ctx)
	if err != nil {
		return pluginproto.OnlineAudio{}, err
	}
	audio, err := onlineCall[pluginproto.OnlineAudio](ctx, w, "provider.online.audioDescribe", scope, pluginproto.OnlineReference{Context: scope, ExternalID: id})
	if err == nil {
		err = audio.Validate()
	}
	if err == nil && audio.ExternalID != id {
		err = domain.Fail("PROTOCOL_MISMATCH", "Online audio identity mismatch")
	}
	return audio, err
}

// OpenAudio 返回有界顺序通道；签名地址和上游请求头不会跨越插件边界。
func (w *OnlineWorker) OpenAudio(ctx context.Context, id string) (pluginproto.OnlineAudio, io.ReadCloser, error) {
	scope, err := w.newScope(ctx)
	if err != nil {
		return pluginproto.OnlineAudio{}, nil, err
	}
	opened, err := onlineCall[pluginproto.OnlineAudioOpened](ctx, w, "provider.online.audioOpen", scope, pluginproto.OnlineReference{Context: scope, ExternalID: id})
	if err != nil {
		return pluginproto.OnlineAudio{}, nil, err
	}
	reader := &onlineAudioReader{ctx: ctx, worker: w, id: opened.StreamID, size: opened.Audio.SizeBytes}
	if opened.Audio.Validate() != nil || opened.Audio.ExternalID != id || !pluginproto.ValidOnlineAudioID(opened.StreamID) {
		reader.Close()
		return pluginproto.OnlineAudio{}, nil, domain.Fail("PROTOCOL_MISMATCH", "Invalid online audio resource")
	}
	return opened.Audio, reader, nil
}

type onlineAudioReader struct {
	ctx          context.Context
	worker       *OnlineWorker
	id           string
	offset, size int64
	pending      []byte
	eof          bool
	once         sync.Once
}

func (r *onlineAudioReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	if len(r.pending) == 0 && !r.eof {
		scope, err := r.worker.newScope(r.ctx)
		if err != nil {
			return 0, err
		}
		chunk, err := onlineCall[pluginproto.OnlineAudioChunk](r.ctx, r.worker, "provider.online.audioRead", scope, pluginproto.OnlineAudioRead{Context: scope, StreamID: r.id, Offset: r.offset})
		if err != nil {
			return 0, err
		}
		limit := r.size
		if limit <= 0 {
			limit = pluginproto.MaxOnlineAudioBytes
		}
		if chunk.Offset != r.offset || len(chunk.Data) > pluginproto.OnlineAudioChunkBytes || int64(len(chunk.Data)) > limit-r.offset || len(chunk.Data) == 0 && !chunk.EOF || chunk.EOF && r.size > 0 && r.offset+int64(len(chunk.Data)) != r.size {
			return 0, domain.Fail("PROTOCOL_MISMATCH", "Invalid online audio chunk")
		}
		r.pending, r.eof = chunk.Data, chunk.EOF
		r.offset += int64(len(chunk.Data))
	}
	n := copy(p, r.pending)
	r.pending = r.pending[n:]
	if n == 0 && r.eof {
		return 0, io.EOF
	}
	return n, nil
}

func (r *onlineAudioReader) Close() error {
	r.once.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		scope, err := r.worker.newScope(ctx)
		if err == nil {
			_, _ = onlineCall[pluginproto.OnlineAudioChunk](ctx, r.worker, "provider.online.audioClose", scope, pluginproto.OnlineAudioRead{Context: scope, StreamID: r.id})
		}
	})
	return nil
}
