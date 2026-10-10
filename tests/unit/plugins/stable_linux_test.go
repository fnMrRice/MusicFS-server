package plugins_test

import (
	"context"
	"os"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestLinuxStableLeaseBreakReleasesWriterAndRejectsResult(t *testing.T) {
	_, h, path := stableSource(t, []byte("original"))
	s, err := h.Stable(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	finished := make(chan error, 1)
	go func() {
		f, err := os.OpenFile(path, os.O_WRONLY, 0)
		if err == nil {
			err = f.Close()
		}
		finished <- err
	}()
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("writer waited for the kernel lease-break timeout")
	}
	if err := s.Publish(func() error { t.Fatal("broken lease published"); return nil }); err == nil {
		t.Fatal("broken lease remained valid")
	}
}

func TestLinuxStableRejectsWritableMappingAndChangedPath(t *testing.T) {
	_, h, path := stableSource(t, []byte("original"))
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	data, err := unix.Mmap(int(f.Fd()), 0, 8, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	if s, err := h.Stable(context.Background()); err == nil {
		s.Close()
		unix.Munmap(data)
		t.Fatal("existing writable mapping accepted")
	}
	unix.Munmap(data)
	s, err := h.Stable(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := os.Rename(path, path+".moved"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("replaced"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.Validate(); err == nil {
		t.Fatal("replaced source path remained valid")
	}
}

func TestLinuxPublicationRetainsProtection(t *testing.T) {
	_, h, path := stableSource(t, []byte("original"))
	s, err := h.Stable(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	finished := make(chan error, 1)
	err = s.Publish(func() error {
		go func() {
			f, err := os.OpenFile(path, os.O_WRONLY, 0)
			if err == nil {
				err = f.Close()
			}
			finished <- err
		}()
		select {
		case <-finished:
			t.Fatal("writer acquired source during publication")
		case <-time.After(50 * time.Millisecond):
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("writer not released after publication")
	}
}
