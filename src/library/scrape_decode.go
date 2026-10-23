package library

import (
	"context"
	"slices"

	"github.com/fnMrRice/MusicFS-server/sdk/audioformat"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/metadata/enrichment"
	"github.com/fnMrRice/MusicFS-server/src/plugins/audiocodecs"
	"github.com/fnMrRice/MusicFS-server/src/plugins/audioruntime"
)

// DecodeAudio 在刮削前按插件优先级逐层准备原生音频。
// 调用者提供独立后台实例，防止占满播放配额；失败仅移除嵌入音频并保留旁车。
func (h *ScrapeInput) DecodeAudio(ctx context.Context, decoder audioruntime.Decoder) error {
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
		if err := h.addContainerArtwork(decoded.Attachments); err != nil {
			return err
		}
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

// addContainerArtwork 只授权本次请求的附件，并沿用原始资产的稳定保护。
// 超出资源预算的图片不阻断音频、歌词或其他封面候选。
func (h *ScrapeInput) addContainerArtwork(attachments []audioformat.Attachment) error {
	if !slices.Contains(h.fields, "artwork") {
		return nil
	}
	var resourceBytes int64
	for _, input := range h.Inputs {
		if input.Resource.Kind != "audio" {
			resourceBytes += input.Resource.SizeBytes
		}
	}
	for _, attachment := range attachments {
		if attachment.Kind != "artwork" {
			continue
		}
		ref, err := newScrapeRef()
		if err != nil {
			return err
		}
		if len(h.Inputs) >= enrichment.MaxResources || attachment.Size > (32<<20)-resourceBytes {
			h.Issues = append(h.Issues, enrichment.Issue{ResourceRef: ref, Code: "UNSUPPORTED"})
			continue
		}
		resourceBytes += attachment.Size
		h.Inputs = append(h.Inputs, enrichment.Input{
			Resource: enrichment.Resource{ResourceRef: ref, Version: h.track.Version, Kind: "artwork", SizeBytes: attachment.Size, Match: "containerEmbedded"},
			Reader:   attachment.Reader, Validate: h.Validate,
		})
	}
	return nil
}
