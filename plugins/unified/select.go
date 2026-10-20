// Package unified assembles official plugins into one selectable executable.
package unified

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite"
	"github.com/fnMrRice/MusicFS-server/plugins/decoder/ffmpeg"
	"github.com/fnMrRice/MusicFS-server/plugins/decoder/ncm"
	"github.com/fnMrRice/MusicFS-server/plugins/scraper/local"
	"github.com/fnMrRice/MusicFS-server/plugins/source/localfiles"
	onlineplugins "github.com/fnMrRice/MusicFS-server/plugins/source/online"
	"github.com/fnMrRice/MusicFS-server/src/storage/rpcbackend"
)

// Worker runs a single role. Its streams belong only to that role's protocol.
type Worker func(context.Context, io.ReadCloser, io.WriteCloser) error

// Select fixes the role before opening stdin/stdout. RPC requests cannot change
// the selected role, platform, persistence backend or DSP implementation.
func Select(args []string) (Worker, error) {
	var role string
	switch {
	case len(args) == 2 && args[0] == "--plugin":
		role = args[1]
	case len(args) == 1 && strings.HasPrefix(args[0], "--plugin="):
		role = strings.TrimPrefix(args[0], "--plugin=")
	default:
		return nil, fmt.Errorf("Exactly one --plugin role argument is required")
	}
	switch role {
	case "local":
		return localfiles.Run, nil
	case "audio-ffmpeg":
		return audioffmpeg.RunWorker, nil
	case "ncm":
		return ncmdecoder.RunWorker, nil
	case "scraper-local":
		return scraperlocal.RunWorker, nil
	case "netease", "qqmusic", "applemusic":
		return func(ctx context.Context, input io.ReadCloser, output io.WriteCloser) error {
			return onlineplugins.Run(ctx, role, input, output)
		}, nil
	case "storage-sqlite":
		return func(ctx context.Context, input io.ReadCloser, output io.WriteCloser) error {
			return rpcbackend.Serve(ctx, input, output, sqlite.Driver())
		}, nil
	case "dsp-gain":
		return runGain, nil
	default:
		return nil, fmt.Errorf("Unsupported plugin role")
	}
}
