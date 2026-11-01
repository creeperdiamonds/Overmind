package main

import (
	"log/slog"
	"net/http"
	_ "net/http/pprof" // registers /debug/pprof on the default mux
	"os"
)

// servePprof exposes Go profiling on the address in OVERMIND_PPROF
// (for example 127.0.0.1:6060). Off when the variable is empty.
func servePprof() {
	addr := os.Getenv("OVERMIND_PPROF")
	if addr == "" {
		return
	}
	go func() { slog.Error("pprof stopped", "err", http.ListenAndServe(addr, nil)) }()
}
