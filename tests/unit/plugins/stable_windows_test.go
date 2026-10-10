package plugins_test

import (
	"context"
	"os"
	"testing"

	"golang.org/x/sys/windows"
)

func TestWindowsStableDeniesWritersAndRename(t *testing.T) {
	_, h, path := stableSource(t, []byte("original"))
	s, err := h.Stable(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if f, err := os.OpenFile(path, os.O_WRONLY, 0); err == nil {
		f.Close()
		t.Fatal("protected source accepted a writer")
	}
	if err := os.Rename(path, path+".moved"); err == nil {
		t.Fatal("protected source was renamed")
	}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestWindowsStableRejectsExistingWritableMapping(t *testing.T) {
	_, h, path := stableSource(t, []byte("original"))
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	if s, err := h.Stable(context.Background()); err == nil {
		s.Close()
		t.Fatal("existing writer accepted")
	}
	mapping, err := windows.CreateFileMapping(windows.Handle(f.Fd()), nil, windows.PAGE_READWRITE, 0, 0, nil)
	if err != nil {
		f.Close()
		t.Fatal(err)
	}
	defer windows.CloseHandle(mapping)
	view, err := windows.MapViewOfFile(mapping, windows.FILE_MAP_WRITE, 0, 0, 0)
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	defer windows.UnmapViewOfFile(view)
	if s, err := h.Stable(context.Background()); err == nil {
		s.Close()
		t.Fatal("writable mapping accepted after original handle closed")
	}
}
