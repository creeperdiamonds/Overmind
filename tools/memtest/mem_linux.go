package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// processBytes returns the resident memory (VmRSS) of a process and the
// highest it has been since the process started (VmHWM).
func processBytes(pid int) (current, peak uint64, err error) {
	f, err := os.Open(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return 0, 0, err
	}
	defer f.Close()
	var gotRSS, gotHWM bool
	s := bufio.NewScanner(f)
	for s.Scan() {
		line := s.Text()
		if rest, ok := strings.CutPrefix(line, "VmRSS:"); ok {
			var kb uint64
			if _, err := fmt.Sscan(rest, &kb); err != nil {
				return 0, 0, err
			}
			current, gotRSS = kb*1024, true
		} else if rest, ok := strings.CutPrefix(line, "VmHWM:"); ok {
			var kb uint64
			if _, err := fmt.Sscan(rest, &kb); err != nil {
				return 0, 0, err
			}
			peak, gotHWM = kb*1024, true
		}
	}
	if !gotRSS || !gotHWM {
		return 0, 0, fmt.Errorf("VmRSS/VmHWM not found")
	}
	return current, peak, nil
}
