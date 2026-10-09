//go:build windows

package audioffmpeg

import (
	"fmt"
	"os/exec"
	"syscall"
	"unsafe"
)

var kernel32 = syscall.NewLazyDLL("kernel32.dll")

func configureProcess(cmd *exec.Cmd) {}

type basicLimits struct {
	perProcessUserTime, perJobUserTime   int64
	flags                                uint32
	minimumWorkingSet, maximumWorkingSet uintptr
	activeProcessLimit                   uint32
	affinity                             uintptr
	priorityClass, schedulingClass       uint32
}
type ioCounters struct{ readOps, writeOps, otherOps, readBytes, writeBytes, otherBytes uint64 }
type extendedLimits struct {
	basic                                                      basicLimits
	io                                                         ioCounters
	processMemory, jobMemory, peakProcessMemory, peakJobMemory uintptr
}

func containProcess(pid int) (func(), error) {
	job, _, err := kernel32.NewProc("CreateJobObjectW").Call(0, 0)
	if job == 0 {
		return nil, err
	}
	closeJob := func() { syscall.CloseHandle(syscall.Handle(job)) }
	limits := extendedLimits{}
	limits.basic.flags = 0x2000
	ok, _, err := kernel32.NewProc("SetInformationJobObject").Call(job, 9, uintptr(unsafe.Pointer(&limits)), unsafe.Sizeof(limits))
	if ok == 0 {
		closeJob()
		return nil, err
	}
	process, err := syscall.OpenProcess(0x0100|0x0001, false, uint32(pid))
	if err != nil {
		closeJob()
		return nil, err
	}
	defer syscall.CloseHandle(process)
	ok, _, err = kernel32.NewProc("AssignProcessToJobObject").Call(job, uintptr(process))
	if ok == 0 {
		closeJob()
		return nil, fmt.Errorf("assign child process: %w", err)
	}
	return closeJob, nil
}
