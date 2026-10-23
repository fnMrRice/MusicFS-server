package ncmdecoder

import (
	"context"
	"fmt"

	"github.com/fnMrRice/MusicFS-server/sdk/audioformat"
)

// AudioDecoder 解开 NCM 封装，输出的普通音频交回注册表继续选择解码器。
func (p *Processor) AudioDecoder() audioformat.Decoder {
	return audioformat.Decoder{Detector: FormatDetector(), Decode: func(ctx context.Context, request audioformat.Request, input audioformat.Input) (audioformat.Output, error) {
		if p == nil {
			return audioformat.Output{}, fmt.Errorf("UNSUPPORTED: NCM decoder is unavailable")
		}
		decoded, err := p.Open(ctx, request.TaskID, request.AssetID, input.Reader, input.Size)
		if err != nil {
			return audioformat.Output{}, err
		}
		metadata := decoded.Metadata()
		var attachments []audioformat.Attachment
		if artwork, size := decoded.Artwork(); artwork != nil {
			attachments = append(attachments, audioformat.Attachment{Kind: "artwork", Reader: artwork, Size: size})
		}
		return audioformat.Output{
			Input: audioformat.Input{Reader: decoded, Size: decoded.Size(), Format: metadata.Format, MIME: metadata.MIME, Metadata: audioformat.Metadata{
				Title: metadata.Title, Album: metadata.Album, Artists: metadata.Artists, DurationMs: metadata.DurationMs, BitrateBps: metadata.BitrateBps,
			}},
			NeedsDecoding: true, Close: decoded.Close, Attachments: attachments,
		}, nil
	}}
}
