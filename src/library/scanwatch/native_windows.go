//go:build windows

package scanwatch

import (
	"context"
	"errors"
	"os"
	"runtime"
	"unsafe"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"golang.org/x/sys/windows"
)

const nativeChanges = windows.FILE_NOTIFY_CHANGE_FILE_NAME | windows.FILE_NOTIFY_CHANGE_DIR_NAME |
	windows.FILE_NOTIFY_CHANGE_SIZE | windows.FILE_NOTIFY_CHANGE_LAST_WRITE | windows.FILE_NOTIFY_CHANGE_ATTRIBUTES

type directoryWatch struct {
	handle     windows.Handle
	overlapped windows.Overlapped
	buffer     [2048]uint32 // DWORD aligned, 8 KiB per directory.
	pin        runtime.Pinner
	pending    bool
}

func startNative(ctx context.Context, dirs []*os.File, hint, failed func()) (func(), error) {
	if err := validateNative(ctx, dirs, hint, failed); err != nil {
		return nil, err
	}
	port, err := windows.CreateIoCompletionPort(windows.InvalidHandle, 0, 0, 1)
	if err != nil {
		return nil, domain.Fail("UNSUPPORTED", "Native directory notification is unavailable")
	}
	watches := make([]*directoryWatch, 0, len(dirs))
	cleanup := func() {
		// Cancel every operation first, then wait before releasing any buffer.
		pending := 0
		for _, w := range watches {
			if w.pending {
				pending++
				_ = windows.CancelIoEx(w.handle, &w.overlapped)
			}
		}
		// With IOCP, completion status is delivered by removing its packet.
		// Waiting on the file handle instead can leave cancelled I/O unobserved.
		for pending > 0 {
			var bytes uint32
			var key uintptr
			var operation *windows.Overlapped
			_ = windows.GetQueuedCompletionStatus(port, &bytes, &key, &operation, windows.INFINITE)
			for _, w := range watches {
				if w.pending && operation == &w.overlapped {
					w.pending = false
					pending--
					break
				}
			}
		}
		for _, w := range watches {
			_ = windows.CloseHandle(w.handle)
			w.pin.Unpin()
		}
		_ = windows.CloseHandle(port)
	}
	for _, dir := range dirs {
		if err = ctx.Err(); err != nil {
			cleanup()
			return nil, err
		}
		handle, reopenErr := reopenNotificationDirectory(dir)
		if reopenErr != nil {
			cleanup()
			return nil, domain.Fail("UNSUPPORTED", "Watch directory cannot be reopened for notification")
		}
		w := &directoryWatch{handle: handle}
		w.pin.Pin(&w.overlapped)
		w.pin.Pin(&w.buffer[0])
		watches = append(watches, w)
		if _, err = windows.CreateIoCompletionPort(w.handle, port, 0, 1); err == nil {
			err = w.arm()
		}
		if err != nil {
			cleanup()
			return nil, domain.Fail("UNSUPPORTED", "Watch directory notification registration failed")
		}
	}
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer cancel()
		defer cleanup()
		runWindowsNotifications(ctx, port, watches, hint, failed)
	}()
	return func() { cancel(); <-done }, nil
}

func reopenNotificationDirectory(dir *os.File) (windows.Handle, error) {
	// Empty relative name opens the anchored directory itself, even after rename.
	// No synchronous option is set: the owned handle supports overlapped I/O.
	name, _ := windows.NewNTUnicodeString("")
	attributes := windows.OBJECT_ATTRIBUTES{RootDirectory: windows.Handle(dir.Fd()), ObjectName: name}
	attributes.Length = uint32(unsafe.Sizeof(attributes))
	var handle windows.Handle
	err := windows.NtCreateFile(&handle, windows.FILE_LIST_DIRECTORY|windows.SYNCHRONIZE,
		&attributes, &windows.IO_STATUS_BLOCK{}, nil, 0,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		windows.FILE_OPEN, windows.FILE_DIRECTORY_FILE, 0, 0)
	runtime.KeepAlive(dir)
	return handle, err
}

func (w *directoryWatch) arm() error {
	w.overlapped = windows.Overlapped{}
	err := windows.ReadDirectoryChanges(w.handle, (*byte)(unsafe.Pointer(&w.buffer[0])),
		uint32(len(w.buffer)*4), false, nativeChanges, nil, &w.overlapped, 0)
	if err == nil || errors.Is(err, windows.ERROR_IO_PENDING) {
		w.pending = true
		return nil
	}
	return err
}

func runWindowsNotifications(ctx context.Context, port windows.Handle, watches []*directoryWatch, hint, failed func()) {
	byOperation := make(map[*windows.Overlapped]*directoryWatch, len(watches))
	for _, w := range watches {
		byOperation[&w.overlapped] = w
	}
	for ctx.Err() == nil {
		var bytes uint32
		var key uintptr
		var operation *windows.Overlapped
		err := windows.GetQueuedCompletionStatus(port, &bytes, &key, &operation, 250)
		if operation == nil && errors.Is(err, windows.WAIT_TIMEOUT) {
			continue
		}
		w, ok := byOperation[operation]
		if ok {
			w.pending = false
		}
		if ctx.Err() != nil {
			return
		}
		if !ok || (err != nil && !errors.Is(err, windows.ERROR_NOTIFY_ENUM_DIR)) {
			failed()
			return
		}
		// Both a zero-byte completion and ENUM_DIR mean that events were lost.
		// Every completion therefore asks the host to reconcile, without names.
		hint()
		if err = w.arm(); err != nil {
			failed()
			return
		}
	}
}
