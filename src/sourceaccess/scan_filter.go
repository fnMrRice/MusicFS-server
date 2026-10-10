package sourceaccess

import (
	"errors"
	"io/fs"
	"os"
	"path"

	"github.com/fnMrRice/MusicFS-server/src/pluginproto"
)

func excludedEntry(root *os.Root, scope pluginproto.ScanScope, relative string, info fs.FileInfo) (bool, error) {
	if scope.SkipHidden && relative != "." && (pluginproto.HasHiddenPathSegment(relative) || hiddenAttribute(info)) {
		return true, nil
	}
	if !scope.RespectNoMedia || !info.IsDir() {
		return false, nil
	}
	// Lstat tests marker existence without opening it or following a symlink.
	_, err := root.Lstat(path.Join(relative, ".nomedia"))
	if err == nil {
		return true, nil
	}
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	return false, err
}
