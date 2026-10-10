package scanwatch

import (
	"context"
	"os"

	"github.com/fnMrRice/MusicFS-server/src/domain"
)

// StartNative watches only the supplied directory objects, never their trees.
// Callbacks must be nonblocking. The caller retains ownership of dirs and may
// close them after this returns. The returned function cancels and joins all I/O.
func StartNative(ctx context.Context, dirs []*os.File, hint, failed func()) (func(), error) {
	return startNative(ctx, dirs, hint, failed)
}

func validateNative(ctx context.Context, dirs []*os.File, hint, failed func()) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(dirs) == 0 || len(dirs) > MaxDirectories || hint == nil || failed == nil {
		return domain.Fail("INVALID_ARGUMENT", "Invalid native watch configuration")
	}
	for _, dir := range dirs {
		if dir == nil {
			return domain.Fail("INVALID_ARGUMENT", "Invalid watch directory handle")
		}
		info, err := dir.Stat()
		if err != nil || !info.IsDir() {
			return domain.Fail("SOURCE_OFFLINE", "Watch directory handle is unavailable")
		}
	}
	return nil
}
