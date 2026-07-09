package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/zxq97/rental-agent/internal/replay"
)

func main() {
	path := flag.String("store", "replay.jsonl", "replay JSONL store path")
	traceID := flag.String("trace_id", "", "trace id")
	mode := flag.String("mode", "dry", "dry only")
	flag.Parse()
	if *traceID == "" {
		fmt.Fprintln(os.Stderr, "trace_id is required")
		os.Exit(2)
	}
	if *mode != "dry" {
		fmt.Fprintln(os.Stderr, "only dry mode is implemented locally")
		os.Exit(2)
	}
	store := replay.NewFileStore(*path)
	snaps, err := store.FindByTraceID(context.Background(), *traceID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read replay store: %v\n", err)
		os.Exit(1)
	}
	if len(snaps) == 0 {
		fmt.Println("no snapshots")
		return
	}
	for i, snap := range snaps {
		fmt.Printf("# snapshot %d stage=%s model=%s\n", i+1, snap.Stage, snap.Model)
		fmt.Print(replay.DryReport(snap, snap))
	}
}
