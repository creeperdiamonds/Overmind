package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// processBytes returns the resident memory (VmRSS) of a process.
func processBytes(pid int) (uint64, error) {
	f, err := os.Open(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return 0, err
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	for s.Scan() {
		if rest, ok := strings.CutPrefix(s.Text(), "VmRSS:"); ok {
			var kb uint64
			if _, err := fmt.Sscan(rest, &kb); err != nil {
				return 0, err
			}
			return kb * 1024, nil
		}
	}
	return 0, fmt.Errorf("VmRSS not found")
}
