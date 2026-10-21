package writeback

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"os"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

func identity(info windows.ByHandleFileInformation) string {
	return fmt.Sprintf("%08x:%08x%08x", info.VolumeSerialNumber, info.FileIndexHigh, info.FileIndexLow)
}

func (d *windowsDisk) State(file *os.File) (FileState, error) {
	var result FileState
	h := windows.Handle(file.Fd())
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(h, &info); err != nil {
		return result, changed()
	}
	// Other storage features (ADS, EFS, sparse/compressed/cloud/reparse, custom
	// special attributes) are deliberately unsupported rather than discarded.
	allowed := uint32(windows.FILE_ATTRIBUTE_NORMAL | windows.FILE_ATTRIBUTE_ARCHIVE)
	if info.NumberOfLinks != 1 || info.FileAttributes & ^allowed != 0 {
		return result, unsupported("Writeback requires a regular, single-link, writable NTFS file")
	}
	var basic struct {
		CreationTime, LastAccessTime, LastWriteTime, ChangeTime int64
		Attributes, Padding                                     uint32
	}
	if err := windows.GetFileInformationByHandleEx(h, windows.FileBasicInfo, (*byte)(unsafe.Pointer(&basic)), uint32(unsafe.Sizeof(basic))); err != nil {
		return result, changed()
	}
	stat, err := file.Stat()
	if err != nil || !stat.Mode().IsRegular() {
		return result, changed()
	}
	result.Identity, result.Size = identity(info), stat.Size()
	version := fmt.Sprintf("%s:%d:%d:%d", result.Identity, result.Size, stat.ModTime().UnixNano(), basic.ChangeTime)
	result.Version = fmt.Sprintf("%x", sha256.Sum256([]byte(version)))
	return result, nil
}

func noNamedStreams(handle windows.Handle) error {
	var eaSize uint32
	var status windows.IO_STATUS_BLOCK
	if err := windows.NtQueryInformationFile(handle, &status, (*byte)(unsafe.Pointer(&eaSize)), uint32(unsafe.Sizeof(eaSize)), windows.FileEaInformation); err != nil || eaSize != 0 {
		return unsupported("Extended source attributes are not supported")
	}
	// FILE_STREAM_INFO describes the unnamed data stream too. A bounded buffer
	// failure or any additional stream rejects editing rather than losing data.
	// FILE_STREAM_INFO includes LARGE_INTEGER fields and the native query
	// requires an eight-byte-aligned buffer. A byte slice may be stack-allocated
	// without that alignment, producing ERROR_NOACCESS in some call chains.
	aligned := make([]uint64, 65536/8)
	buffer := unsafe.Slice((*byte)(unsafe.Pointer(&aligned[0])), 65536)
	if err := windows.GetFileInformationByHandleEx(handle, windows.FileStreamInfo, &buffer[0], uint32(len(buffer))); err != nil {
		return unsupported("Cannot verify source data streams")
	}
	next, length := binary.LittleEndian.Uint32(buffer), binary.LittleEndian.Uint32(buffer[4:])
	if next != 0 || length != 14 {
		return unsupported("Named source data streams are not supported")
	}
	name := unsafe.Slice((*uint16)(unsafe.Pointer(&buffer[24])), int(length/2))
	if windows.UTF16ToString(name) != "::$DATA" {
		return unsupported("Named source data streams are not supported")
	}
	return nil
}

func preserveSecurity(source, target windows.Handle) error {
	flags := windows.SECURITY_INFORMATION(windows.OWNER_SECURITY_INFORMATION | windows.GROUP_SECURITY_INFORMATION | windows.DACL_SECURITY_INFORMATION)
	sd, err := windows.GetSecurityInfo(source, windows.SE_FILE_OBJECT, flags)
	if err != nil {
		return unsupported("Source permissions cannot be read")
	}
	dest, err := windows.GetSecurityInfo(target, windows.SE_FILE_OBJECT, flags)
	if err != nil {
		return unsupported("Staging permissions cannot be read")
	}
	owner, _, err := sd.Owner()
	if err != nil {
		return unsupported("Source owner cannot be read")
	}
	newOwner, _, err := dest.Owner()
	if err != nil || !windows.EqualSid(owner, newOwner) {
		return unsupported("Source ownership cannot be preserved")
	}
	group, _, err := sd.Group()
	if err != nil {
		return unsupported("Source group cannot be read")
	}
	newGroup, _, err := dest.Group()
	if err != nil || !windows.EqualSid(group, newGroup) {
		return unsupported("Source group cannot be preserved")
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return unsupported("Source permissions cannot be read")
	}
	control, _, err := sd.Control()
	if err != nil {
		return unsupported("Source permission inheritance cannot be read")
	}
	securityFlags := windows.SECURITY_INFORMATION(windows.DACL_SECURITY_INFORMATION | windows.UNPROTECTED_DACL_SECURITY_INFORMATION)
	if control&windows.SE_DACL_PROTECTED != 0 {
		securityFlags = windows.DACL_SECURITY_INFORMATION | windows.PROTECTED_DACL_SECURITY_INFORMATION
	}
	err = windows.SetSecurityInfo(target, windows.SE_FILE_OBJECT, securityFlags, nil, nil, dacl, nil)
	runtime.KeepAlive(sd)
	if err != nil {
		return unsupported("Source permissions cannot be preserved")
	}
	return nil
}
