// pccp-bench runs the F3 latency/streaming benchmark (harness plan F)
// and prints the comparison table destined for the arXiv paper.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"time"

	"github.com/patrickrho-patty/pccp/internal/bench"
)

func main() {
	turns := flag.Int("turns", 20, "measured turns per arm")
	tokens := flag.Int("tokens", 64, "canned tokens per turn")
	delay := flag.Float64("itl-ms", 2.0, "canned inter-token delay (ms)")
	flag.Parse()

	sched := bench.Schedule{
		Tokens:          *tokens,
		TokenText:       "tok ",
		InterTokenDelay: time.Duration(*delay * float64(time.Millisecond)),
		FirstTokenDelay: 5 * time.Millisecond,
	}

	fmt.Printf("pccp-bench: turns=%d tokens/turn=%d itl=%v\n\n", *turns, *tokens, sched.InterTokenDelay)
	results, err := bench.Run(context.Background(), *turns, sched)
	if err != nil {
		log.Fatalf("bench: %v", err)
	}

	fmt.Printf("%-38s %10s %10s %10s %10s %10s %10s %8s\n",
		"Arm", "TTFT ms", "ITL p50", "ITL p95", "Total ms", "Cold ms", "Warm ms", "B/turn")
	for _, r := range results {
		fmt.Printf("%-38s %10.2f %10.2f %10.2f %10.2f %10.2f %10.2f %8d\n",
			r.Arm, r.TTFTms, r.ITLp50ms, r.ITLp95ms, r.TotalMs, r.ColdStartMs, r.WarmTurnMs, r.BytesPerTurn)
	}
	fmt.Println("\nTopology: macOS localhost; TLS 1.3 (DARI), HTTP/1.1 (SSE), RFC6455 (WS).")
	fmt.Println("Security: DARI arm carries mutual auth, lease/epoch governance, DLP, receipts; SSE/WS arms carry none.")
}
