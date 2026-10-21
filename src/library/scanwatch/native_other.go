//go:build !windows && !linux

package scanwatch

import (
	"context"
	"os"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
)

func startNative(context.Context, []*os.File, func(), func()) (func(), error) {
	return nil, domain.Fail("UNSUPPORTED", "Native directory notification is unavailable on this platform")
}
