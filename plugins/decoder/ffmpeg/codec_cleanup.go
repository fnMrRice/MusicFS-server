package audioffmpeg

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// CleanupCodecOutputs 仅在持有数据目录独占锁且尚无任务运行时调用。
// 不递归、不跟随链接，只清理工作目录内本插件前缀的普通临时文件。
func CleanupCodecOutputs(workDir string) error {
	if workDir == "" {
		return fmt.Errorf("INVALID_ARGUMENT: codec work directory is required")
	}
	root, err := filepath.Abs(workDir)
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	var failures error
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, "musicfs-codec-") || !strings.HasSuffix(name, ".tmp") || entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			failures = errors.Join(failures, err)
			continue
		}
		if !info.Mode().IsRegular() {
			continue
		}
		path := filepath.Join(root, name)
		if filepath.Dir(path) != root {
			return fmt.Errorf("ACCESS_DENIED: codec output path escaped work directory")
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			failures = errors.Join(failures, err)
		}
	}
	return failures
}
