package library

import (
	"context"

	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/enrichment"
	"github.com/fnMrRice/MusicFS-server/src/plugins/ncmdecoder"
)

// DecodeNCM prepares the inputs before Host.Open. The caller supplies a separate
// bounded background decoder; sharing a foreground decoder can exhaust playback.
// Missing or unsupported decryption removes only the embedded input, retains
// sidecars, and adds an issue. Cancellation and changed source versions are fatal.
func (h *ScrapeInput) DecodeNCM(ctx context.Context, decoder *ncmdecoder.Processor) error {
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
	var decoded *ncmdecoder.Decoded
	var err error
	if decoder != nil {
		decoded, err = decoder.Open(decodeCtx, h.task, h.track.AssetID, input.Reader, input.Resource.SizeBytes)
	}
	if err == nil && decoded != nil {
		h.decoded = decoded
		h.decodeCancel = cancel
		h.Inputs[index].Reader = decoded
		h.Inputs[index].Resource.SizeBytes = decoded.Size()
		// The decoded worker owns its context until Close or the bundle deadline.
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
