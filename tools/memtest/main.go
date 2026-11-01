// Command memtest measures a running server's whole-process memory while
// bots walk outward in different directions, and prints one table row.
package main

import (
	"flag"
	"fmt"
	"math"
	"os"
	"time"

	"github.com/overmind/overmind/bots/bedrock"
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

	// Bots join one at a time: the RakNet dialer has a data race when
	// several dial at once.
	var joined []*bedrock.Bot
	for i := 0; i < *bots; i++ {
		b, err := bedrock.Join(*addr, fmt.Sprintf("Bot%d", i))
		if err != nil {
			fmt.Fprintf(os.Stderr, "bot %d: %v\n", i, err)
			continue
		}
		b.Walk(2 * math.Pi * float64(i) / float64(*bots))
		joined = append(joined, b)
	}

	var peak, last uint64
	end := time.Now().Add(time.Duration(*seconds) * time.Second)
	for time.Now().Before(end) {
		b, err := processBytes(*pid)
		if err != nil {
			fmt.Fprintf(os.Stderr, "memtest: read memory: %v\n", err)
			os.Exit(1)
		}
		last = b
		peak = max(peak, b)
		time.Sleep(250 * time.Millisecond)
	}
	for _, b := range joined {
		_ = b.Close()
	}

	const mb = 1024 * 1024
	fmt.Println("| Players | At end of phase | Peak | Ticks per second |")
	fmt.Println("|---|---|---|---|")
	fmt.Printf("| %d walking, %d s (%d joined) | %d MB | %d MB | not measured |\n",
		*bots, *seconds, len(joined), last/mb, peak/mb)
}
