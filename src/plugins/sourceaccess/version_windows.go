//go:build windows

package sourceaccess

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
)

// ObservedVersion 只按完整内容识别普通文件。目录由身份和最后写入时间识别，
// 包内文件的独立版本另行计入目录树快照。
func ObservedVersion(f *os.File, info os.FileInfo, identity string) (string, error) {
	digest := sha256.New()
	if info.IsDir() {
		fmt.Fprintf(digest, "%s:%d", identity, info.ModTime().UnixNano())
	} else {
		size := info.Size()
		n, err := io.Copy(digest, io.NewSectionReader(f, 0, size))
		if err != nil {
			return "", err
		}
		if n != size {
			return "", io.ErrUnexpectedEOF
		}
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}
