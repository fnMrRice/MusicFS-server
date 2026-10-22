package playback

import (
	"context"
	"errors"
	"io"

	audioffmpeg "github.com/fnMrRice/MusicFS-server/plugins/decoder/ffmpeg"
	"github.com/fnMrRice/MusicFS-server/sdk/audioformat"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/plugins/audiocodecs"
	"github.com/fnMrRice/MusicFS-server/src/plugins/codecsettings"
	"github.com/fnMrRice/MusicFS-server/src/plugins/sourceaccess"
)

// prepareInput 保留源资产身份，使用插件解码链生成独立播放表示。
func (p *preparedInput) prepareInput(ctx context.Context, task string, input *sourceaccess.Handle, track *library.Track) (io.Reader, int64, bool, func(), error) {
	noop := func() {}
	if err := ctx.Err(); err != nil {
		return nil, 0, false, noop, err
	}
	match, matched, err := audiocodecs.Detect(input, track.Size)
	if err != nil {
		return nil, 0, false, noop, err
	}
	p.decodeOutput, err = codecsettings.DecodeOutput(ctx, p.service.repo, track.Owner)
	if err != nil {
		return nil, 0, false, noop, err
	}
	wrapped := matched && match.RequiresDecode
	if wrapped {
		ctx, err = p.decoderLease(ctx)
		if err != nil {
			return nil, 0, false, noop, err
		}
	}
	limit, release := track.Size, noop
	if p.decodeOutput != "native" {
		ctx, err = p.processingLease(ctx)
		if err != nil {
			return nil, 0, false, noop, err
		}
		// 解码临时文件与播放副本同时计费，最多占用一半总配额以留出复制空间。
		limit = min(audioffmpeg.DefaultDecodeOutputBytes, p.service.budget.Limit()/2)
		if p.service.cache != nil {
			p.service.cache.MakeRoom(ctx, limit)
		}
		if limit < 1 || !p.service.budget.TryAcquire(limit) {
			return nil, 0, false, noop, domain.Fail("RATE_LIMITED", "Decoder output exceeds the cache budget")
		}
		release = func() { p.service.budget.Release(limit) }
	}
	decoded, err := audiocodecs.Decode(ctx, p.processor, p.decoder, audioformat.Request{
		TaskID: task, AssetID: track.AssetID, OutputFormat: p.decodeOutput, MaxOutputBytes: limit,
	}, audioformat.Input{Reader: input, Size: track.Size})
	if err != nil {
		if !errors.Is(err, audioformat.ErrCleanup) {
			release()
		} else {
			logPlaybackFailure(ctx, "Decoder output cleanup failed; cache reservation retained", err, "sessionId", task)
		}
		if canceled := ctx.Err(); canceled != nil {
			return nil, 0, false, noop, canceled
		}
		code := pluginproto.ErrorCode(err)
		if code != "RATE_LIMITED" && code != "ACCESS_DENIED" && code != "PLUGIN_DISABLED" {
			code = "UNSUPPORTED"
		}
		return nil, 0, false, noop, domain.Fail(code, "Audio source could not be decoded")
	}
	if wrapped || p.decodeOutput != "native" || decoded.Format == "flac" {
		format := decoded.Format
		track.Container = &format
		track.Codec = &format
	}
	track.MIME = decoded.MIME
	return io.NewSectionReader(decoded.Reader, 0, decoded.Size), decoded.Size, wrapped, func() {
		if err := decoded.Close(); err != nil {
			if err = decoded.Close(); err != nil {
				logPlaybackFailure(ctx, "Decoder output cleanup failed; cache reservation retained", err, "sessionId", task)
				return
			}
		}
		release()
	}, nil
}
