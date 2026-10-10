package bootstrap

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/identification"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/plugins/audioffmpeg"
	"github.com/fnMrRice/MusicFS-server/src/plugins/ncmdecoder"
	"github.com/fnMrRice/MusicFS-server/src/sourceaccess"
)

func metadataProbe(ffprobe string, decoder *ncmdecoder.Processor) library.Probe {
	if ffprobe == "" && decoder == nil {
		return nil
	}
	return func(ctx context.Context, input io.ReadSeeker, size int64) (library.Observation, error) {
		var result library.Observation
		reader, ok := input.(io.ReaderAt)
		if !ok {
			return result, fmt.Errorf("UNSUPPORTED: independent input readers required")
		}
		var header [8]byte
		n, _ := reader.ReadAt(header[:], 0)
		encrypted := n == len(header) && string(header[:]) == "CTENFDAM"
		var metadata ncmdecoder.Metadata
		if encrypted {
			if decoder == nil {
				return result, fmt.Errorf("UNSUPPORTED: NCM plugin unavailable")
			}
			asset := domain.NewID()
			if source, ok := input.(*sourceaccess.Handle); ok {
				asset = source.Entry.ObservedVersion
			}
			decoded, err := decoder.Open(ctx, domain.NewID(), asset, reader, size)
			if err != nil {
				return result, err
			}
			defer decoded.Close()
			metadata = decoded.Metadata()
			input = io.NewSectionReader(decoded, 0, decoded.Size())
			size = decoded.Size()
		}
		if ffprobe != "" {
			m, err := audioffmpeg.ProbeMetadata(ctx, ffprobe, input, size)
			if err != nil {
				return result, err
			}
			result.Container, result.Title, result.Artists = m.Container, m.Title, m.Artists
			result.Codec, result.DurationMs, result.BitrateBps = m.Codec, m.DurationMs, m.BitrateBps
			result.MIME = m.MIME
			if m.BitrateBps != nil {
				now := time.Now().UTC()
				configRevision := "first-audio-stream-v1"
				if encrypted {
					configRevision = "ncm-payload-first-audio-stream-v1"
				}
				result.Evidence = []identification.Evidence{{Spec: identification.Spec{Kind: "bitrate", Origin: m.BitrateOrigin, Algorithm: "ffprobe", AlgorithmVersion: identification.AlgorithmVersionID(m.ProbeVersion), ConfigRevision: configRevision, Range: identification.Range{Unit: "milliseconds", End: m.DurationMs}}, Result: identification.Result{Status: "reported", BitrateBps: m.BitrateBps, BitrateMode: "unknown", ComputedAt: &now}}}
			}
		} else if !encrypted {
			return result, fmt.Errorf("UNSUPPORTED: metadata probe unavailable")
		}
		if encrypted {
			container := "ncm"
			result.Container = &container
			result.MIME = "audio/x-ncm"
			if result.Codec == nil {
				format := metadata.Format
				result.Codec = &format
			}
			if result.Title == nil && metadata.Title != "" {
				title := metadata.Title
				result.Title = &title
			}
			if len(result.Artists) == 0 {
				result.Artists = metadata.Artists
			}
			// Duration and bitrate must come from the real payload probe, not the envelope claims.
		}
		return result, nil
	}
}
