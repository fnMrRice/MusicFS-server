//go:build windows

package resources_test

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

type processSample struct {
	PID            uint32    `json:"pid"`
	ParentPID      uint32    `json:"parentPid"`
	CreatedAt      time.Time `json:"createdAt"`
	Name           string    `json:"name"`
	WorkingSet     uint64    `json:"workingSetBytes"`
	PrivateBytes   uint64    `json:"privateBytes"`
	Handles        uint32    `json:"handles"`
	Threads        uint32    `json:"threads"`
	TCPConnections int       `json:"tcpConnections"`
	CPUSeconds     float64   `json:"cpuSeconds"`
}
type sample struct {
	Elapsed    float64         `json:"elapsedSeconds"`
	Phase      string          `json:"phase"`
	Processes  []processSample `json:"processes"`
	CacheBytes int64           `json:"cacheBytes"`
	MediaBytes int64           `json:"mediaBytes"`
}
type monitor struct {
	mu      sync.Mutex
	phase   string
	samples []sample
	errors  []string
	stop    chan struct{}
	done    chan struct{}
}

func newMonitor(f fixture) *monitor {
	m := &monitor{phase: "baseline", stop: make(chan struct{}), done: make(chan struct{})}
	go func() {
		defer close(m.done)
		start := time.Now()
		ticker := time.NewTicker(200 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-m.stop:
				return
			case <-ticker.C:
			}
			processes, err := processTree(uint32(f.pid))
			cache, e1 := directoryBytes(f.cache)
			media, e2 := directoryBytes(f.media)
			m.mu.Lock()
			for _, e := range []error{err, e1, e2} {
				if e != nil {
					m.errors = append(m.errors, e.Error())
				}
			}
			m.samples = append(m.samples, sample{Elapsed: time.Since(start).Seconds(), Phase: m.phase, Processes: processes, CacheBytes: cache, MediaBytes: media})
			m.mu.Unlock()
		}
	}()
	return m
}
func (m *monitor) setPhase(phase string) { m.mu.Lock(); defer m.mu.Unlock(); m.phase = phase }
func (m *monitor) finish()               { close(m.stop); <-m.done }

func directoryBytes(root string) (int64, error) {
	var size int64
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			if isGone(err) {
				return nil
			}
			return err
		}
		if entry.Type().IsRegular() {
			info, e := entry.Info()
			if e != nil {
				if isGone(e) {
					return nil
				}
				return e
			}
			size += info.Size()
		}
		return nil
	})
	return size, err
}

func processTree(pid uint32) ([]processSample, error) {
	connections, err := tcpConnections()
	if err != nil {
		return nil, err
	}
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, err
	}
	defer windows.CloseHandle(snapshot)
	entry := windows.ProcessEntry32{Size: uint32(unsafe.Sizeof(windows.ProcessEntry32{}))}
	entries := map[uint32]windows.ProcessEntry32{}
	for err = windows.Process32First(snapshot, &entry); err == nil; err = windows.Process32Next(snapshot, &entry) {
		entries[entry.ProcessID] = entry
	}
	if err != windows.ERROR_NO_MORE_FILES {
		return nil, err
	}
	included := map[uint32]bool{pid: true}
	for changed := true; changed; {
		changed = false
		for id, e := range entries {
			if !included[id] && included[e.ParentProcessID] {
				included[id] = true
				changed = true
			}
		}
	}
	measured := map[uint32]processSample{}
	for id := range included {
		p, e := measureProcess(id)
		if e != nil {
			// A subprocess may exit between enumeration and opening its handle.
			if id != pid {
				continue
			}
			return nil, e
		}
		entry := entries[id]
		p.Name = windows.UTF16ToString(entry.ExeFile[:])
		p.ParentPID = entry.ParentProcessID
		p.Threads = entry.Threads
		p.TCPConnections = connections[id]
		measured[id] = p
	}
	return verifiedTree(pid, measured), nil
}

// Windows retains numeric parent PIDs after parent exit. An old orphan whose
// parent PID was later reused is not a child of that new process incarnation.
func verifiedTree(root uint32, measured map[uint32]processSample) []processSample {
	accepted := map[uint32]bool{root: true}
	for changed := true; changed; {
		changed = false
		for id, child := range measured {
			parent, exists := measured[child.ParentPID]
			if !accepted[id] && exists && accepted[child.ParentPID] && !child.CreatedAt.Before(parent.CreatedAt) {
				accepted[id] = true
				changed = true
			}
		}
	}
	result := []processSample{}
	for id := range accepted {
		if p, ok := measured[id]; ok {
			result = append(result, p)
		}
	}
	return result
}

var tcpTable = windows.NewLazySystemDLL("iphlpapi.dll").NewProc("GetExtendedTcpTable")

// Fixture listeners use IPv4. TIME_WAIT rows without an owning PID are not
// attributed to the server; unrelated applications never enter this count.
func tcpConnections() (map[uint32]int, error) {
	var size uint32
	code, _, _ := tcpTable.Call(0, uintptr(unsafe.Pointer(&size)), 0, windows.AF_INET, 5, 0)
	if code != uintptr(windows.ERROR_INSUFFICIENT_BUFFER) {
		return nil, fmt.Errorf("TCP table sizing: %d", code)
	}
	for attempt := 0; attempt < 3; attempt++ {
		buffer := make([]byte, size)
		code, _, _ = tcpTable.Call(uintptr(unsafe.Pointer(&buffer[0])), uintptr(unsafe.Pointer(&size)), 0, windows.AF_INET, 5, 0)
		if code == uintptr(windows.ERROR_INSUFFICIENT_BUFFER) {
			continue
		}
		if code != 0 {
			return nil, fmt.Errorf("TCP table query: %d", code)
		}
		rows := *(*uint32)(unsafe.Pointer(&buffer[0]))
		if uint64(rows)*24+4 > uint64(len(buffer)) {
			return nil, fmt.Errorf("invalid TCP table length")
		}
		result := map[uint32]int{}
		for i := uint32(0); i < rows; i++ {
			pid := *(*uint32)(unsafe.Pointer(&buffer[4+i*24+20]))
			result[pid]++
		}
		return result, nil
	}
	return nil, fmt.Errorf("TCP table changed too frequently")
}

var memoryInfo = windows.NewLazySystemDLL("psapi.dll").NewProc("GetProcessMemoryInfo")
var handleCount = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetProcessHandleCount")

type memoryCounters struct {
	Size, PageFaults                                             uint32
	PeakWorkingSet, WorkingSet                                   uintptr
	QuotaPeakPaged, QuotaPaged, QuotaPeakNonPaged, QuotaNonPaged uintptr
	Pagefile, PeakPagefile, PrivateUsage                         uintptr
}

func measureProcess(pid uint32) (processSample, error) {
	p := processSample{PID: pid}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_INFORMATION|windows.PROCESS_VM_READ, false, pid)
	if err != nil {
		return p, err
	}
	defer windows.CloseHandle(h)
	memory := memoryCounters{Size: uint32(unsafe.Sizeof(memoryCounters{}))}
	if ok, _, e := memoryInfo.Call(uintptr(h), uintptr(unsafe.Pointer(&memory)), uintptr(memory.Size)); ok == 0 {
		return p, fmt.Errorf("memory query: %w", e)
	}
	if ok, _, e := handleCount.Call(uintptr(h), uintptr(unsafe.Pointer(&p.Handles))); ok == 0 {
		return p, fmt.Errorf("handle query: %w", e)
	}
	var created, exited, kernel, user windows.Filetime
	if err = windows.GetProcessTimes(h, &created, &exited, &kernel, &user); err != nil {
		return p, err
	}
	p.CreatedAt = time.Unix(0, created.Nanoseconds()).UTC()
	p.WorkingSet, p.PrivateBytes = uint64(memory.WorkingSet), uint64(memory.PrivateUsage)
	ticks := uint64(kernel.HighDateTime)<<32 | uint64(kernel.LowDateTime)
	ticks += uint64(user.HighDateTime)<<32 | uint64(user.LowDateTime)
	p.CPUSeconds = float64(ticks) / 1e7
	return p, nil
}
