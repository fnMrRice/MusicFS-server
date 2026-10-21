package writeback

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

type windowsDisk struct {
	parent      windows.Handle
	path        string
	directories []windows.Handle
}

func openDisk(root Root, target Target) (disk, error) {
	volume := filepath.VolumeName(root.Path)
	if len(volume) != 2 || volume[1] != ':' {
		return nil, unsupported("Writeback requires a local drive")
	}
	drive, _ := windows.UTF16PtrFromString(volume + `\`)
	if windows.GetDriveType(drive) != windows.DRIVE_FIXED {
		return nil, unsupported("Writeback requires a fixed local drive")
	}
	d := &windowsDisk{}
	parent := filepath.Join(root.Path, filepath.FromSlash(path.Dir(target.RelativePath)))
	parts := strings.Split(strings.TrimPrefix(filepath.Clean(parent), volume+`\`), `\`)
	current := volume + `\`
	paths := []string{current}
	for _, component := range parts {
		current = filepath.Join(current, component)
		paths = append(paths, current)
	}
	rootSeen := false
	for _, name := range paths {
		p, err := windows.UTF16PtrFromString(name)
		if err != nil {
			d.Close()
			return nil, invalid("Invalid writeback directory")
		}
		h, err := windows.CreateFile(p, windows.FILE_READ_ATTRIBUTES|windows.SYNCHRONIZE, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
		if err != nil {
			d.Close()
			return nil, unsupported("Writeback directory cannot be protected")
		}
		d.directories = append(d.directories, h)
		var info windows.ByHandleFileInformation
		if err = windows.GetFileInformationByHandle(h, &info); err != nil || info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 || info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY == 0 {
			d.Close()
			return nil, unsupported("Reparse points are not supported for writeback")
		}
		if strings.EqualFold(filepath.Clean(name), filepath.Clean(root.Path)) {
			rootSeen = true
			if identity(info) != root.Identity {
				d.Close()
				return nil, changed()
			}
			var fsname [64]uint16
			if err = windows.GetVolumeInformationByHandle(h, nil, 0, nil, nil, nil, &fsname[0], uint32(len(fsname))); err != nil || windows.UTF16ToString(fsname[:]) != "NTFS" {
				d.Close()
				return nil, unsupported("Writeback is only verified on local NTFS")
			}
		}
		d.parent, d.path = h, name
	}
	if !rootSeen {
		d.Close()
		return nil, changed()
	}
	return d, nil
}

func (d *windowsDisk) Open(name string) (*os.File, error) {
	p, err := windows.UTF16PtrFromString(filepath.Join(d.path, name))
	if err != nil {
		return nil, invalid("Invalid writeback filename")
	}
	h, err := windows.CreateFile(p, windows.GENERIC_READ|windows.GENERIC_WRITE|windows.DELETE|windows.READ_CONTROL, windows.FILE_SHARE_READ, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_OPEN_REPARSE_POINT|windows.FILE_FLAG_SEQUENTIAL_SCAN|windows.FILE_FLAG_WRITE_THROUGH, 0)
	if errors.Is(err, windows.ERROR_FILE_NOT_FOUND) {
		return nil, os.ErrNotExist
	}
	if err != nil {
		return nil, unsupported("Source is read-only, busy, or cannot be protected for writeback")
	}
	f := os.NewFile(uintptr(h), "writeback-protected-file")
	if _, err = d.State(f); err == nil {
		err = noNamedStreams(h)
	}
	if err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}

func (d *windowsDisk) Create(name string, source *os.File) (*os.File, error) {
	p, err := windows.UTF16PtrFromString(filepath.Join(d.path, name))
	if err != nil {
		return nil, invalid("Invalid staging filename")
	}
	h, err := windows.CreateFile(p, windows.GENERIC_READ|windows.GENERIC_WRITE|windows.DELETE|windows.READ_CONTROL|windows.WRITE_DAC, windows.FILE_SHARE_READ, nil, windows.CREATE_NEW, windows.FILE_ATTRIBUTE_NORMAL|windows.FILE_FLAG_WRITE_THROUGH, 0)
	if err != nil {
		return nil, fmt.Errorf("TEMPORARY_FAILURE: cannot create writeback staging file")
	}
	f := os.NewFile(uintptr(h), "writeback-staging-file")
	if err = preserveSecurity(windows.Handle(source.Fd()), h); err != nil {
		f.Close()
		return nil, err
	}
	var info windows.ByHandleFileInformation
	if err = windows.GetFileInformationByHandle(windows.Handle(source.Fd()), &info); err == nil {
		err = windows.SetFileTime(h, &info.CreationTime, nil, nil)
	}
	if err != nil {
		f.Close()
		return nil, unsupported("Source file attributes cannot be preserved")
	}
	return f, nil
}

func (d *windowsDisk) Rename(file *os.File, name string) error {
	if name == "" || strings.ContainsAny(name, `\/:`) {
		return invalid("Invalid writeback rename target")
	}
	u, err := windows.UTF16FromString(name)
	if err != nil {
		return invalid("Invalid writeback rename target")
	}
	type renameInformation struct {
		Flags          uint32
		RootDirectory  windows.Handle
		FileNameLength uint32
		FileName       [1]uint16
	}
	var info renameInformation
	size := int(unsafe.Offsetof(info.FileName)) + (len(u)-1)*2
	buffer := make([]byte, size)
	rename := (*renameInformation)(unsafe.Pointer(&buffer[0]))
	// Flags=0: never replace an existing target, even during recovery. Rename
	// the protected object by handle, not a path that another process can swap.
	rename.RootDirectory = d.parent
	rename.FileNameLength = uint32((len(u) - 1) * 2)
	copy(unsafe.Slice(&rename.FileName[0], len(u)-1), u[:len(u)-1])
	var ioStatus windows.IO_STATUS_BLOCK
	if err = windows.NtSetInformationFile(windows.Handle(file.Fd()), &ioStatus, &buffer[0], uint32(size), windows.FileRenameInformation); err != nil {
		return changed()
	}
	return file.Sync()
}

func (d *windowsDisk) Close() error {
	var err error
	for i := len(d.directories) - 1; i >= 0; i-- {
		err = errors.Join(err, windows.CloseHandle(d.directories[i]))
	}
	d.directories = nil
	return err
}

func (d *windowsDisk) Discard(file *os.File) error {
	// Used only for the newly created, unchanged staging object while its
	// deny-delete handle is still held. It cannot unlink a substituted path.
	remove := byte(1)
	return windows.SetFileInformationByHandle(windows.Handle(file.Fd()), windows.FileDispositionInfo, &remove, 1)
}

func publishJournal(root *os.Root, from, to string, replace bool) error {
	src, err := windows.UTF16PtrFromString(filepath.Join(root.Name(), from))
	if err != nil {
		return err
	}
	dst, err := windows.UTF16PtrFromString(filepath.Join(root.Name(), to))
	if err != nil {
		return err
	}
	flags := uint32(windows.MOVEFILE_WRITE_THROUGH)
	if replace {
		flags |= windows.MOVEFILE_REPLACE_EXISTING
	}
	return windows.MoveFileEx(src, dst, flags)
}
