//go:build windows

package sourceaccess

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

var reopenStableFile = windows.NewLazySystemDLL("kernel32.dll").NewProc("ReOpenFile")

func protectStableFile(source *os.File) (stableProtection, error) {
	// Reopen the authorized file object, denying write and delete sharing. This
	// also fails when an earlier writable mapping survives its original handle.
	handle, _, _ := reopenStableFile.Call(source.Fd(), windows.GENERIC_READ, windows.FILE_SHARE_READ, windows.FILE_FLAG_SEQUENTIAL_SCAN)
	if handle == ^uintptr(0) {
		return stableProtection{}, fmt.Errorf("UNSUPPORTED: source does not permit a protected complete read")
	}
	f := os.NewFile(handle, "protected-source")
	return stableProtection{file: f, check: func() error { return nil }, release: func() error { return nil }}, nil
}
