package writeback

import "golang.org/x/sys/windows"

func inspectCapability(root Root) error {
	fs, err := openDisk(root, Target{RelativePath: "capability-check.flac"})
	if err != nil {
		return err
	}
	defer fs.Close()
	path, err := windows.UTF16PtrFromString(root.Path)
	if err != nil {
		return invalid("Invalid writeback root")
	}
	// FILE_ADD_FILE shares FILE_WRITE_DATA's bit. Renaming is authorized by
	// DELETE on the protected file handles, checked by Open/Create. Requiring
	// directory FILE_DELETE_CHILD would wrongly reject ordinary Modify ACLs.
	h, err := windows.CreateFile(path, windows.FILE_WRITE_DATA|windows.FILE_READ_ATTRIBUTES, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return unsupported("Writeback root does not permit creating and replacing files")
	}
	defer windows.CloseHandle(h)
	var flags uint32
	if err = windows.GetVolumeInformationByHandle(h, nil, 0, nil, nil, &flags, nil, 0); err != nil || flags&windows.FILE_READ_ONLY_VOLUME != 0 {
		return unsupported("Writeback volume is read-only or unavailable")
	}
	return nil
}
