//go:build windows

package sourceaccess

import (
	"fmt"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

func fileChangeVersion(f *os.File) (string, error) {
	var info struct {
		CreationTime, LastAccessTime, LastWriteTime, ChangeTime int64
		Attributes                                              uint32
		Padding                                                 uint32
	}
	if err := windows.GetFileInformationByHandleEx(windows.Handle(f.Fd()), windows.FileBasicInfo, (*byte)(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		return "", err
	}
	return fmt.Sprint(info.ChangeTime), nil
}
