package audioffmpeg

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCleanupCodecOutputsOnlyRemovesOwnRegularFiles(t *testing.T) {
	root := t.TempDir()
	stale := filepath.Join(root, "musicfs-codec-stale.tmp")
	unrelated := filepath.Join(root, "another-task.tmp")
	directory := filepath.Join(root, "musicfs-codec-directory.tmp")
	for _, path := range []string{stale, unrelated} {
		if err := os.WriteFile(path, []byte("test data"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(directory, "musicfs-codec-nested.tmp")
	if err := os.WriteFile(nested, []byte("test data"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := CleanupCodecOutputs(root); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatal("stale codec output survived cleanup", err)
	}
	for _, path := range []string{unrelated, directory, nested} {
		if _, err := os.Stat(path); err != nil {
			t.Fatal("unrelated path changed", path, err)
		}
	}
	if err := CleanupCodecOutputs(""); err == nil {
		t.Fatal("empty work directory accepted")
	}
}

func TestCodecOutputCloseRetriesRemovalWithoutClosingLeaseAgain(t *testing.T) {
	root := t.TempDir()
	file, err := os.CreateTemp(root, "musicfs-codec-*.tmp")
	if err != nil {
		t.Fatal(err)
	}
	released := 0
	// 使用非空目录占据原路径，可靠模拟首次删除失败而不依赖系统权限。
	path := file.Name()
	child := filepath.Join(path, "locked")
	cleanup := codecFileCleanup(file, func() {
		released++
		// 此回调在关闭句柄后执行，也适用于禁止移动打开文件的 Windows。
		if err := os.Rename(path, path+".held"); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(child, []byte("test data"), 0600); err != nil {
			t.Fatal(err)
		}
	})
	if err := cleanup(); err == nil {
		t.Fatal("removal error was suppressed")
	}
	if released != 1 {
		t.Fatal("lease was not released after closing the handle")
	}
	if err := os.Remove(child); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path+".held", path); err != nil {
		t.Fatal(err)
	}
	if err := cleanup(); err != nil {
		t.Fatal("removal could not be retried", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("retried cleanup left the file")
	}
	if released != 1 {
		t.Fatal("lease was released more than once")
	}
}
