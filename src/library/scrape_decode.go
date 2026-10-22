package library

import (
	"context"

	"github.com/fnMrRice/MusicFS-server/plugins/decoder/ncm"
	"github.com/fnMrRice/MusicFS-server/sdk/audioformat"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/metadata/enrichment"
	"github.com/fnMrRice/MusicFS-server/src/plugins/audiocodecs"
)

// DecodeNCM 保留旧调用入口，实际由统一解码链处理插件识别的封装。
func (h *ScrapeInput) DecodeNCM(ctx context.Context, decoder *ncmdecoder.Processor) error {
	return h.DecodeAudio(ctx, decoder)
}

// DecodeAudio 在刮削前按插件优先级逐层准备原生音频。
// 调用者提供独立后台实例，防止占满播放配额；失败仅移除嵌入音频并保留旁车。
func (h *ScrapeInput) DecodeAudio(ctx context.Context, decoder *ncmdecoder.Processor) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := h.Validate(); err != nil {
		return err
	}
	if !h.Encrypted || h.tried {
		return nil
	}
	h.tried = true
	index := -1
	for i := range h.Inputs {
		if h.Inputs[i].Resource.Kind == "audio" {
			index = i
			break
		}
	}
	if index < 0 {
		return domain.Fail("CONFLICT", "Scraper audio input is missing")
	}
	input := h.Inputs[index]
	decodeCtx, cancel := context.WithCancel(h.ctx)
	stop := context.AfterFunc(ctx, cancel)
	defer stop()
	var decoded *audioformat.Decoded
	var err error
	decoded, err = audiocodecs.OpenNative(decodeCtx, decoder, h.task, h.track.AssetID, input.Reader, input.Resource.SizeBytes)
	if err == nil && decoded != nil {
		h.decoded = decoded
		h.decodeCancel = cancel
		h.Inputs[index].Reader = decoded.Reader
		h.Inputs[index].Resource.SizeBytes = decoded.Size
		// 中间输出保留到整个刮削任务关闭或超时。
		return h.Validate()
	}
	cancel()
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := h.Validate(); err != nil {
		return err
	}
	h.Inputs = append(h.Inputs[:index], h.Inputs[index+1:]...)
	h.Issues = append(h.Issues, enrichment.Issue{ResourceRef: input.Resource.ResourceRef, Code: "UNSUPPORTED"})
	return nil
}
