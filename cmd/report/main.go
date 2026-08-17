// Command report reads recorder JSONL output and prints the numbers that decide
// whether this solver is ready for a longer shadow trial.
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
		Orders                  int            `json:"orders"`
		PoolsUsable             int            `json:"poolsUsable"`
		PoolsSkipped            map[string]int `json:"poolsSkipped"`
		CoWMatches              int            `json:"cowMatches"`
		BaselineRoutes          int            `json:"baselineRoutes"`
		DroppedUnsupportedOrder int            `json:"droppedUnsupportedOrder"`
		DroppedNoRoute          int            `json:"droppedNoRoute"`
		DroppedLimit            int            `json:"droppedLimitPrice"`
		DroppedNotProfitable    int            `json:"droppedNotProfitable"`
		CandidateSolutions      int            `json:"candidateSolutions"`
		Solutions               int            `json:"solutions"`
	} `json:"stats"`
}

type notifyRecord struct {
	Notify struct {
		Kind string `json:"kind"`
	} `json:"notify"`
}

func main() {
	dir := flag.String("dir", "./data", "directory containing recorder output")
	flag.Parse()

	var records []auctionRecord
	if err := eachLine(*dir, "auctions-*.jsonl", func(path string, line int, data []byte) error {
		var record auctionRecord
		if err := json.Unmarshal(data, &record); err != nil {
			return fmt.Errorf("%s:%d: decode auction record: %w", path, line, err)
		}
		records = append(records, record)
		return nil
	}); err != nil {
		fmt.Fprintln(os.Stderr, "read auctions:", err)
		os.Exit(1)
	}

	notifications := map[string]int{}
	if err := eachLine(*dir, "notifications-*.jsonl", func(path string, line int, data []byte) error {
		var record notifyRecord
		if err := json.Unmarshal(data, &record); err != nil {
			return fmt.Errorf("%s:%d: decode notification record: %w", path, line, err)
		}
		if record.Notify.Kind != "" {
			notifications[record.Notify.Kind]++
		}
		return nil
	}); err != nil {
		fmt.Fprintln(os.Stderr, "read notifications:", err)
		os.Exit(1)
	}

	if len(records) == 0 {
		fmt.Println("No auctions recorded yet in", *dir)
		return
	}

	var (
		withSolution, totalOrders, totalCandidates, totalSolutions int
		cows, routes                                               int
		unsupported, noRoute, limit, unprofitable                  int
		latencies                                                  []int64
		skipped                                                    = map[string]int{}
	)
	for _, record := range records {
		if record.Stats.Solutions > 0 {
			withSolution++
		}
		totalOrders += record.Stats.Orders
		totalCandidates += record.Stats.CandidateSolutions
		totalSolutions += record.Stats.Solutions
		cows += record.Stats.CoWMatches
		routes += record.Stats.BaselineRoutes
		unsupported += record.Stats.DroppedUnsupportedOrder
		noRoute += record.Stats.DroppedNoRoute
		limit += record.Stats.DroppedLimit
		unprofitable += record.Stats.DroppedNotProfitable
		latencies = append(latencies, record.ElapsedMs)
		for kind, count := range record.Stats.PoolsSkipped {
			skipped[kind] += count
		}
	}
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })

	n := len(records)
	fmt.Printf("Auctions seen        %d\n", n)
	fmt.Printf("Coverage             %d (%.1f%%) produced at least one solution\n", withSolution, pct(withSolution, n))
	fmt.Printf("Solve latency        p50 %dms  p95 %dms  max %dms\n",
		percentile(latencies, 50), percentile(latencies, 95), latencies[len(latencies)-1])
	fmt.Println()

	fmt.Printf("Eligible orders      %d\n", totalOrders)
	fmt.Printf("Candidates accepted  %d  (CoW matches %d, routed %d)\n", totalCandidates, cows, routes)
	fmt.Printf("Solutions returned   %d\n", totalSolutions)
	fmt.Println()

	fmt.Println("Why orders were dropped")
	dropped := unsupported + noRoute + limit + unprofitable
	fmt.Printf("  unsupported shape  %d (%.1f%%)\n", unsupported, pct(unsupported, dropped))
	fmt.Printf("  no route found     %d (%.1f%%)\n", noRoute, pct(noRoute, dropped))
	fmt.Printf("  limit price unmet  %d (%.1f%%)\n", limit, pct(limit, dropped))
	fmt.Printf("  gas exceeds edge   %d (%.1f%%)\n", unprofitable, pct(unprofitable, dropped))
	fmt.Println()

	if len(skipped) > 0 {
		fmt.Println("Liquidity skipped or malformed (most common first)")
		type entry struct {
			kind  string
			count int
		}
		list := make([]entry, 0, len(skipped))
		for kind, count := range skipped {
			list = append(list, entry{kind: kind, count: count})
		}
		sort.Slice(list, func(i, j int) bool {
			if list[i].count != list[j].count {
				return list[i].count > list[j].count
			}
			return list[i].kind < list[j].kind
		})
		for _, item := range list {
			fmt.Printf("  %-22s %d\n", item.kind, item.count)
		}
		fmt.Println()
	}

	if len(notifications) > 0 {
		fmt.Println("Driver feedback on submitted solutions")
		kinds := make([]string, 0, len(notifications))
		total := 0
		for kind, count := range notifications {
			kinds = append(kinds, kind)
			total += count
		}
		sort.Slice(kinds, func(i, j int) bool {
			if notifications[kinds[i]] != notifications[kinds[j]] {
				return notifications[kinds[i]] > notifications[kinds[j]]
			}
			return kinds[i] < kinds[j]
		})
		for _, kind := range kinds {
			fmt.Printf("  %-22s %d (%.1f%%)\n", kind, notifications[kind], pct(notifications[kind], total))
		}
		fmt.Println()
	}

	fmt.Println(strings.Repeat("-", 64))
	fmt.Println("Coverage is the first gate. Driver success feedback is the second.")
	fmt.Println("A winner-beating rate requires competition objective evidence that")
	fmt.Println("the current driver notification contract does not guarantee.")
}

func pct(a, b int) float64 {
	if b == 0 {
		return 0
	}
	return float64(a) * 100 / float64(b)
}

// percentile uses the nearest-rank definition on an ascending slice.
func percentile(sorted []int64, percent int) int64 {
	if len(sorted) == 0 {
		return 0
	}
	if percent <= 0 {
		return sorted[0]
	}
	if percent >= 100 {
		return sorted[len(sorted)-1]
	}
	rank := (percent*len(sorted) + 99) / 100
	return sorted[rank-1]
}

type lineFn func(path string, line int, data []byte) error

func eachLine(dir, pattern string, fn lineFn) error {
	paths, err := filepath.Glob(filepath.Join(dir, pattern))
	if err != nil {
		return err
	}
	sort.Strings(paths)
	for _, path := range paths {
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		scanner := bufio.NewScanner(file)
		scanner.Buffer(make([]byte, 1<<20), 256<<20)
		line := 0
		for scanner.Scan() {
			line++
			data := scanner.Bytes()
			if len(data) == 0 {
				continue
			}
			if err := fn(path, line, data); err != nil {
				_ = file.Close()
				return err
			}
		}
		scanErr := scanner.Err()
		closeErr := file.Close()
		if scanErr != nil {
			return fmt.Errorf("scan %s: %w", path, scanErr)
		}
		if closeErr != nil {
			return fmt.Errorf("close %s: %w", path, closeErr)
		}
	}
	return nil
}
