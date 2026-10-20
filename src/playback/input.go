package playback

import (
	"context"
	"io"
	"path/filepath"
	"strings"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/sourceaccess"
)

// prepareInput keeps the source asset identity while selecting a separate output representation.
func (p *preparedInput) prepareInput(ctx context.Context, task string, input *sourceaccess.Handle, track *library.Track) (io.Reader, int64, bool, func(), error) {
	noop := func() {}
	if err := ctx.Err(); err != nil {
		return nil, 0, false, noop, err
	}
	var header [8]byte
	n, _ := input.ReadAt(header[:], 0)
	encrypted := n == len(header) && string(header[:]) == "CTENFDAM"
	if !encrypted && track.MIME != "audio/x-ncm" && !strings.EqualFold(filepath.Ext(track.Path), ".ncm") {
		return input, track.Size, false, noop, nil
	}
	if p.decoder == nil {
		return nil, 0, false, noop, domain.Fail("UNSUPPORTED", "NCM decryption plugin is unavailable")
	}
	ctx, err := p.decoderLease(ctx)
	if err != nil {
		return nil, 0, false, noop, err
	}
	decoded, err := p.decoder.Open(ctx, task, track.AssetID, input, track.Size)
	if err != nil {
		if canceled := ctx.Err(); canceled != nil {
			return nil, 0, false, noop, canceled
		}
		code := pluginproto.ErrorCode(err)
		if code != "RATE_LIMITED" && code != "ACCESS_DENIED" && code != "PLUGIN_DISABLED" {
			code = "UNSUPPORTED"
		}
		return nil, 0, false, noop, domain.Fail(code, "NCM source could not be decrypted")
	}
	format := decoded.Metadata().Format
	if decoded.Size() <= 0 || decoded.Size() > track.Size || (format != "mp3" && format != "flac") {
		decoded.Close()
		return nil, 0, false, noop, domain.Fail("UNSUPPORTED", "NCM plugin returned an invalid representation")
	}
	track.Container = &format
	track.Codec = &format
	track.MIME = "audio/mpeg"
	if format == "flac" {
		track.MIME = "audio/flac"
	}
	return io.NewSectionReader(decoded, 0, decoded.Size()), decoded.Size(), true, func() { decoded.Close() }, nil
}
