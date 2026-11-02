package main

import (
	"fmt"
	"syscall"
	"unsafe"
)

var getProcessMemoryInfo = syscall.NewLazyDLL("psapi.dll").NewProc("GetProcessMemoryInfo")

// processMemoryCounters mirrors PROCESS_MEMORY_COUNTERS.
type processMemoryCounters struct {
	cb                         uint32
	PageFaultCount             uint32
	PeakWorkingSetSize         uintptr
	WorkingSetSize             uintptr
	QuotaPeakPagedPoolUsage    uintptr
	QuotaPagedPoolUsage        uintptr
	QuotaPeakNonPagedPoolUsage uintptr
	QuotaNonPagedPoolUsage     uintptr
	PagefileUsage              uintptr
	PeakPagefileUsage          uintptr
}

// processBytes returns the working set (memory held in RAM) of a process and
// the highest working set Windows has recorded for it since it started.
func processBytes(pid int) (current, peak uint64, err error) {
	h, err := syscall.OpenProcess(0x0400|0x0010, false, uint32(pid)) // query + read
	if err != nil {
		return 0, 0, err
	}
	defer syscall.CloseHandle(h)
	var c processMemoryCounters
	c.cb = uint32(unsafe.Sizeof(c))
	r, _, e := getProcessMemoryInfo.Call(uintptr(h), uintptr(unsafe.Pointer(&c)), uintptr(c.cb))
	if r == 0 {
		return 0, 0, fmt.Errorf("GetProcessMemoryInfo: %w", e)
	}
	return uint64(c.WorkingSetSize), uint64(c.PeakWorkingSetSize), nil
}
