//go:build windows

package pluginrpc

import (
	"os/exec"
	"syscall"
	"unsafe"
)

func configureProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000}
}

type jobLimits struct {
	UserTime, JobTime    int64
	Flags                uint32
	MinMemory, MaxMemory uintptr
	Processes            uint32
	Affinity             uintptr
	Priority, Scheduling uint32
}

type extendedLimits struct {
	Basic                                                      jobLimits
	IO                                                         [6]uint64
	ProcessMemory, JobMemory, PeakProcessMemory, PeakJobMemory uintptr
}

func containProcess(pid int) (func(), error) {
	kernel := syscall.NewLazyDLL("kernel32.dll")
	job, _, err := kernel.NewProc("CreateJobObjectW").Call(0, 0)
	if job == 0 {
		return nil, err
	}
	closeJob := func() { syscall.CloseHandle(syscall.Handle(job)) }
	limits := extendedLimits{}
	limits.Basic.Flags = 0x2000
	ok, _, err := kernel.NewProc("SetInformationJobObject").Call(job, 9, uintptr(unsafe.Pointer(&limits)), unsafe.Sizeof(limits))
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
	ok, _, err = kernel.NewProc("AssignProcessToJobObject").Call(job, uintptr(process))
	if ok == 0 {
		closeJob()
		return nil, err
	}
	return closeJob, nil
}
