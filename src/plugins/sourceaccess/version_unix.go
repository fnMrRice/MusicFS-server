//go:build darwin || linux

package sourceaccess

import (
	"crypto/sha256"
	"fmt"
	"os"
)

// ObservedVersion 保留原有的文件和目录版本编码，包含 inode 状态变更时间。
func ObservedVersion(f *os.File, info os.FileInfo, identity string) (string, error) {
	change, err := fileChangeVersion(f)
	if err != nil {
		return "", err
	}
	metadata := identity
	if !info.IsDir() {
		metadata += fmt.Sprintf(":%d", info.Size())
	}
	fields := fmt.Sprintf("%s:%d:%s", metadata, info.ModTime().UnixNano(), change)
	return fmt.Sprintf("%x", sha256.Sum256([]byte(fields))), nil
}
