// Command memtest measures a running server's whole-process memory while
// bots walk outward in different directions, and prints one table row.
package main

import (
	"flag"
	"fmt"
	"math"
	"os"
	"sync"
	"time"

	"github.com/creeperdiamonds/overmind/bots/bedrock"
)

func main() {
	pid := flag.Int("pid", 0, "process id of the server to measure")
	bots := flag.Int("bots", 0, "number of Bedrock bots to walk")
	seconds := flag.Int("seconds", 60, "how long to measure")
	addr := flag.String("addr", "127.0.0.1:19132", "Bedrock address of the server")
	flag.Parse()
	if *pid == 0 {
		fmt.Fprintln(os.Stderr, "memtest: -pid is required")
		os.Exit(2)
	}

	// Sampling starts before the first bot dials so the burst of chunks
	// sent at join time is part of the peak. The OS-recorded peak (Windows
	// PeakWorkingSetSize, Linux VmHWM) covers anything between samples.
	var (
		mu         sync.Mutex
		peak, last uint64
		sampleErr  error
	)
	stop := make(chan struct{})
	sampled := make(chan struct{})
	go func() {
		defer close(sampled)
		for {
			cur, osPeak, err := processBytes(*pid)
			mu.Lock()
			if err != nil {
				sampleErr = err
				mu.Unlock()
				return
			}
			last = cur
			peak = max(peak, cur, osPeak)
			mu.Unlock()
			select {
			case <-stop:
				return
			case <-time.After(250 * time.Millisecond):
			}
		}
	}()

	// Bots join one at a time: the RakNet dialer has a data race when
	// several dial at once.
	var joined []*bedrock.Bot
	failed := 0
	for i := 0; i < *bots; i++ {
		b, err := bedrock.Join(*addr, fmt.Sprintf("Bot%d", i))
		if err != nil {
			fmt.Fprintf(os.Stderr, "bot %d: %v\n", i, err)
			failed++
			continue
		}
		b.Walk(2 * math.Pi * float64(i) / float64(*bots))
		joined = append(joined, b)
	}

	// The measured period starts now that every bot has had its turn.
	time.Sleep(time.Duration(*seconds) * time.Second)
	close(stop)
	<-sampled
	for _, b := range joined {
		_ = b.Close()
	}
	if sampleErr != nil {
		fmt.Fprintf(os.Stderr, "memtest: read memory: %v\n", sampleErr)
		os.Exit(1)
	}

	const mb = 1024 * 1024
	fmt.Println("| Players | At end of phase | Peak | Ticks per second |")
	fmt.Println("|---|---|---|---|")
	fmt.Printf("| %d walking, %d s (%d joined) | %d MB | %d MB | not measured |\n",
		*bots, *seconds, len(joined), last/mb, peak/mb)
	if failed > 0 {
		fmt.Fprintf(os.Stderr, "memtest: %d of %d bots failed to join\n", failed, *bots)
		os.Exit(1)
	}
}
