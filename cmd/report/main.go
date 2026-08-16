// Command report reads the recorder's JSONL output and prints the numbers
// that decide whether this solver is worth taking further.
//
// The metric that matters is coverage: the share of auctions for which the
// engine produced any solution at all. Everything else is diagnosis.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type auctionRecord struct {
	AuctionID string `json:"auctionId"`
	ElapsedMs int64  `json:"elapsedMs"`
	Stats     struct {
		Orders               int            `json:"orders"`
		PoolsUsable          int            `json:"poolsUsable"`
		PoolsSkipped         map[string]int `json:"poolsSkipped"`
		CoWMatches           int            `json:"cowMatches"`
		BaselineRoutes       int            `json:"baselineRoutes"`
		DroppedNoRoute       int            `json:"droppedNoRoute"`
		DroppedLimit         int            `json:"droppedLimitPrice"`
		DroppedNotProfitable int            `json:"droppedNotProfitable"`
		Solutions            int            `json:"solutions"`
	} `json:"stats"`
}

type notifyRecord struct {
	Notify struct {
		AuctionID string `json:"auctionId"`
		Kind      string `json:"kind"`
	} `json:"notify"`
}

func main() {
	dir := flag.String("dir", "./data", "directory containing the recorder output")
	flag.Parse()

	var recs []auctionRecord
	if err := eachLine(*dir, "auctions-*.jsonl", func(b []byte) {
		var r auctionRecord
		if json.Unmarshal(b, &r) == nil {
			recs = append(recs, r)
		}
	}); err != nil {
		fmt.Fprintln(os.Stderr, "read auctions:", err)
		os.Exit(1)
	}

	notes := map[string]int{}
	_ = eachLine(*dir, "notifications-*.jsonl", func(b []byte) {
		var r notifyRecord
		if json.Unmarshal(b, &r) == nil && r.Notify.Kind != "" {
			notes[r.Notify.Kind]++
		}
	})

	if len(recs) == 0 {
		fmt.Println("No auctions recorded yet in", *dir)
		return
	}

	var (
		withSolution, totalOrders, totalSolutions int
		cows, routes                              int
		noRoute, limit, unprofitable              int
		latencies                                 []int64
		skipped                                   = map[string]int{}
	)
	for _, r := range recs {
		if r.Stats.Solutions > 0 {
			withSolution++
		}
		totalOrders += r.Stats.Orders
		totalSolutions += r.Stats.Solutions
		cows += r.Stats.CoWMatches
		routes += r.Stats.BaselineRoutes
		noRoute += r.Stats.DroppedNoRoute
		limit += r.Stats.DroppedLimit
		unprofitable += r.Stats.DroppedNotProfitable
		latencies = append(latencies, r.ElapsedMs)
		for k, v := range r.Stats.PoolsSkipped {
			skipped[k] += v
		}
	}
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })

	n := len(recs)
	fmt.Printf("Auctions seen        %d\n", n)
	fmt.Printf("Coverage             %d (%.1f%%) produced at least one solution\n",
		withSolution, pct(withSolution, n))
	fmt.Printf("Solve latency        p50 %dms  p95 %dms  max %dms\n",
		latencies[len(latencies)/2], latencies[len(latencies)*95/100], latencies[len(latencies)-1])
	fmt.Println()

	fmt.Printf("Orders seen          %d\n", totalOrders)
	fmt.Printf("Solutions proposed   %d  (CoW matches %d, routed %d)\n", totalSolutions, cows, routes)
	fmt.Println()

	fmt.Println("Why orders were dropped")
	dropped := noRoute + limit + unprofitable
	fmt.Printf("  no route found     %d (%.1f%%)\n", noRoute, pct(noRoute, dropped))
	fmt.Printf("  limit price unmet  %d (%.1f%%)\n", limit, pct(limit, dropped))
	fmt.Printf("  gas exceeds edge   %d (%.1f%%)\n", unprofitable, pct(unprofitable, dropped))
	fmt.Println()

	if len(skipped) > 0 {
		fmt.Println("Liquidity kinds not yet supported (build these next, most common first)")
		type kv struct {
			k string
			v int
		}
		var list []kv
		for k, v := range skipped {
			list = append(list, kv{k, v})
		}
		sort.Slice(list, func(i, j int) bool { return list[i].v > list[j].v })
		for _, e := range list {
			fmt.Printf("  %-22s %d pools\n", e.k, e.v)
		}
		fmt.Println()
	}

	if len(notes) > 0 {
		fmt.Println("Driver feedback on submitted solutions")
		var kinds []string
		for k := range notes {
			kinds = append(kinds, k)
		}
		sort.Slice(kinds, func(i, j int) bool { return notes[kinds[i]] > notes[kinds[j]] })
		total := 0
		for _, v := range notes {
			total += v
		}
		for _, k := range kinds {
			fmt.Printf("  %-22s %d (%.1f%%)\n", k, notes[k], pct(notes[k], total))
		}
		fmt.Println()
	}

	fmt.Println(strings.Repeat("-", 60))
	fmt.Println("Coverage is the gate. If it stays low, the routing model is the")
	fmt.Println("problem. If coverage is high but the driver reports failures, the")
	fmt.Println("settlement encoding is. Neither is fixed by going live sooner.")
}

func pct(a, b int) float64 {
	if b == 0 {
		return 0
	}
	return float64(a) * 100 / float64(b)
}

func eachLine(dir, pattern string, fn func([]byte)) error {
	paths, err := filepath.Glob(filepath.Join(dir, pattern))
	if err != nil {
		return err
	}
	for _, p := range paths {
		f, err := os.Open(p)
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 1<<20), 256<<20)
		for sc.Scan() {
			b := sc.Bytes()
			if len(b) > 0 {
				fn(b)
			}
		}
		f.Close()
	}
	return nil
}
