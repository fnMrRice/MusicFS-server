//go:build linux

package sourceaccess

import (
	"fmt"
	"os"
	"os/signal"

	"golang.org/x/sys/unix"
)

func protectStableFile(source *os.File) (stableProtection, error) {
	// A new open description avoids sharing a lease/offset with an ordinary
	// playback reader. /proc/self/fd reopens the already authorized inode.
	f, err := os.Open(fmt.Sprintf("/proc/self/fd/%d", source.Fd()))
	if err != nil {
		return stableProtection{}, fmt.Errorf("UNSUPPORTED: stable source handle is unavailable")
	}
	notice := make(chan os.Signal, 1)
	signal.Notify(notice, unix.SIGIO)
	if _, err := unix.FcntlInt(f.Fd(), unix.F_SETLEASE, unix.F_RDLCK); err != nil {
		signal.Stop(notice)
		f.Close()
		return stableProtection{}, fmt.Errorf("UNSUPPORTED: source does not permit a stable read lease")
	}
	return stableProtection{
		file: f, notice: notice,
		check: func() error {
			lease, err := unix.FcntlInt(f.Fd(), unix.F_GETLEASE, 0)
			if err != nil || lease != unix.F_RDLCK {
				return fmt.Errorf("RESOURCE_CHANGED: source read lease was broken")
			}
			return nil
		},
		release: func() error {
			_, err := unix.FcntlInt(f.Fd(), unix.F_SETLEASE, unix.F_UNLCK)
			signal.Stop(notice)
			return err
		},
	}, nil
}
