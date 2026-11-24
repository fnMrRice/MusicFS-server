package bootstrap

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/audioformat"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/metadata/identification"
	"github.com/fnMrRice/MusicFS-server/src/plugins/audiocodecs"
	"github.com/fnMrRice/MusicFS-server/src/plugins/audioruntime"
	"github.com/fnMrRice/MusicFS-server/src/plugins/sourceaccess"
)

func metadataProbe(probe audioruntime.Probe, decoder audioruntime.Decoder) library.Probe {
	if probe == nil && decoder == nil {
		return nil
	}
	return func(ctx context.Context, input io.ReadSeeker, size int64) (library.Observation, error) {
		var result library.Observation
		reader, ok := input.(io.ReaderAt)
		if !ok {
			return result, fmt.Errorf("UNSUPPORTED: independent input readers required")
		}
		match, matched, err := audiocodecs.Detect(reader, size)
		if err != nil {
			return result, err
		}
		wrapped := matched && match.RequiresDecode
		var metadata audioformat.Metadata
		var decodedFormat string
		if wrapped {
			asset := domain.NewID()
			if source, ok := input.(*sourceaccess.Handle); ok {
				asset = source.Entry.ObservedVersion
			}
			decoded, err := audiocodecs.OpenNative(ctx, decoder, domain.NewID(), asset, reader, size)
			if err != nil {
				return result, err
			}
			defer decoded.Close()
			metadata, decodedFormat = decoded.Metadata, decoded.Format
			input = io.NewSectionReader(decoded.Reader, 0, decoded.Size)
			size = decoded.Size
		}
		if probe != nil {
			m, err := probe(ctx, input, size)
			if err != nil {
				return result, err
			}
			result.Container, result.Title, result.Artists = m.Container, m.Title, m.Artists
			m.AddNetEaseID(metadata.NetEaseID)
			result.NetEaseID, result.NetEaseIDConflict = m.NetEaseID, m.NetEaseIDConflict
			result.Album, result.Genre, result.Year, result.SampleRateHz = m.Album, m.Genre, m.Year, m.SampleRateHz
			result.Codec, result.DurationMs, result.BitrateBps = m.Codec, m.DurationMs, m.BitrateBps
			result.MIME = m.MIME
			if m.BitrateBps != nil {
				now := time.Now().UTC()
				configRevision := "first-audio-stream-v1"
				if wrapped {
					configRevision = "decoded-payload-first-audio-stream-v1"
				}
				result.Evidence = []identification.Evidence{{Spec: identification.Spec{Kind: "bitrate", Origin: m.BitrateOrigin, Algorithm: m.ProbeAlgorithm, AlgorithmVersion: identification.AlgorithmVersionID(m.ProbeVersion), ConfigRevision: configRevision, Range: identification.Range{Unit: "milliseconds", End: m.DurationMs}}, Result: identification.Result{Status: "reported", BitrateBps: m.BitrateBps, BitrateMode: "unknown", ComputedAt: &now}}}
			}
		} else if !wrapped {
			return result, fmt.Errorf("UNSUPPORTED: metadata probe unavailable")
		} else {
			result.NetEaseID = metadata.NetEaseID
		}
		if wrapped {
			container := match.Format
			result.Container = &container
			result.MIME = match.MIME
			if result.Codec == nil {
				result.Codec = &decodedFormat
			}
			if result.Title == nil && metadata.Title != "" {
				title := metadata.Title
				result.Title = &title
			}
			if len(result.Artists) == 0 {
				result.Artists = metadata.Artists
			}
			if result.Album == nil && metadata.Album != "" {
				album := metadata.Album
				result.Album = &album
			}
			// 时长和码率必须来自实际音频探测，不能信任外层封装的自报值。
		}
		return result, nil
	}
}
