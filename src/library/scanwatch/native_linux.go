//go:build linux

package scanwatch

import (
	"context"
	"encoding/binary"
	"errors"
	"os"
	"runtime"
	"strconv"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"golang.org/x/sys/unix"
)

func startNative(ctx context.Context, dirs []*os.File, hint, failed func()) (func(), error) {
	if err := validateNative(ctx, dirs, hint, failed); err != nil {
		return nil, err
	}
	fd, err := unix.InotifyInit1(unix.IN_NONBLOCK | unix.IN_CLOEXEC)
	if err != nil {
		return nil, domain.Fail("UNSUPPORTED", "Native directory notification is unavailable")
	}
	mask := uint32(unix.IN_ONLYDIR | unix.IN_CREATE | unix.IN_DELETE | unix.IN_MOVED_FROM |
		unix.IN_MOVED_TO | unix.IN_MODIFY | unix.IN_ATTRIB | unix.IN_CLOSE_WRITE | unix.IN_DELETE_SELF | unix.IN_MOVE_SELF)
	for _, dir := range dirs {
		if err = ctx.Err(); err == nil {
			// The proc descriptor binds the open object; no user path is reopened.
			_, err = unix.InotifyAddWatch(fd, "/proc/self/fd/"+strconv.FormatUint(uint64(dir.Fd()), 10), mask)
			runtime.KeepAlive(dir)
		}
		if err != nil {
			_ = unix.Close(fd)
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, domain.Fail("UNSUPPORTED", "Watch directory notification registration failed")
		}
	}
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer cancel()
		defer unix.Close(fd)
		runLinuxNotifications(ctx, fd, hint, failed)
	}()
	return func() { cancel(); <-done }, nil
}

func runLinuxNotifications(ctx context.Context, fd int, hint, failed func()) {
	buffer := make([]byte, 32*1024)
	poll := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
	for ctx.Err() == nil {
		n, err := unix.Poll(poll, 250)
		if ctx.Err() != nil {
			return
		}
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil || poll[0].Revents&(unix.POLLERR|unix.POLLHUP|unix.POLLNVAL) != 0 {
			failed()
			return
		}
		if n == 0 {
			continue
		}
		n, err = unix.Read(fd, buffer)
		if errors.Is(err, unix.EAGAIN) || errors.Is(err, unix.EINTR) {
			continue
		}
		if ctx.Err() != nil {
			return
		}
		if err != nil || n == 0 || !validNotifications(buffer[:n]) {
			failed()
			return
		}
		// A batch (including overflow) is a single hint. Names are never decoded.
		hint()
	}
}

func validNotifications(data []byte) bool {
	for len(data) > 0 {
		if len(data) < unix.SizeofInotifyEvent {
			return false
		}
		mask := binary.NativeEndian.Uint32(data[4:8])
		length := uint64(binary.NativeEndian.Uint32(data[12:16])) + unix.SizeofInotifyEvent
		if length > uint64(len(data)) || mask&(unix.IN_DELETE_SELF|unix.IN_MOVE_SELF|unix.IN_UNMOUNT|unix.IN_IGNORED) != 0 {
			return false
		}
		data = data[int(length):]
	}
	return true
}
