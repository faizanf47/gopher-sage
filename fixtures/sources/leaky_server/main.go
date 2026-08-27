// leaky_server is a deliberately terrible Go program. It exists
// solely as a gopher-sage demo fixture — every function contains at
// least one performance anti-pattern for the detectors to find.
//
// Anti-patterns included:
//   - goroutine leak (workers block forever on a channel nobody closes)
//   - unbounded cache growth (memory leak)
//   - string concatenation in a hot loop (excessive allocation)
//   - global mutex held across expensive work on the hot path
//   - regexp compilation inside a loop
//   - fmt.Sprintf in a tight loop instead of strconv
//   - channel send without select/timeout (blocking)
//   - unnecessary reflect usage
//   - slice append without pre-allocation
//
// Do NOT fix this file. If a linter or reviewer flags it, that means
// it is working.
package main

import (
	"fmt"
	"math/rand"
	"net/http"
	_ "net/http/pprof"
	"reflect"
	"regexp"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Memory leak: unbounded global cache that is never evicted, guarded
// by a single global mutex that is also held across expensive work.
// ---------------------------------------------------------------------------

var (
	cacheMu sync.Mutex
	cache   = make(map[string][]byte)
)

// cacheSet stores the payload AND recomputes the total cached bytes
// while holding the global lock — an O(len(cache)) scan under a
// mutex on the hot path, so contention grows as the cache leaks.
func cacheSet(key string, val []byte) (entries, totalBytes int) {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	cache[key] = val
	for _, v := range cache {
		totalBytes += len(v)
	}
	return len(cache), totalBytes
}

// ---------------------------------------------------------------------------
// Goroutine leak: spawns workers that never exit because nothing
// ever closes or sends on their channel.
// ---------------------------------------------------------------------------

func leakyWorker() {
	ch := make(chan struct{})
	// BUG: nothing ever closes or sends on ch, so this goroutine
	// blocks here forever.
	<-ch
}

func spawnLeakyWorkers(n int) {
	for i := 0; i < n; i++ {
		go leakyWorker()
	}
}

// ---------------------------------------------------------------------------
// CPU hot path: compiles a regexp on every loop iteration, builds the
// result via string concatenation (O(n²) allocations), and uses
// fmt.Sprintf for int→string instead of strconv.
// ---------------------------------------------------------------------------

func processItems(items []string) string {
	result := ""
	for i, item := range items {
		// BAD: compiled once per iteration instead of once per process.
		sanitizer := regexp.MustCompile(`[^a-zA-Z0-9]`)
		cleaned := sanitizer.ReplaceAllString(item, "")
		// BAD: += reallocates the whole string every iteration, and
		// Sprintf is a slow way to format an int.
		result += fmt.Sprintf("%d:%s,", i, cleaned)
	}
	return result
}

// ---------------------------------------------------------------------------
// Reflect abuse: uses reflect to sum an int slice instead of a type
// switch or generics.
// ---------------------------------------------------------------------------

func reflectSum(data interface{}) int64 {
	v := reflect.ValueOf(data)
	var total int64
	for i := 0; i < v.Len(); i++ {
		total += v.Index(i).Int()
	}
	return total
}

// ---------------------------------------------------------------------------
// Allocation-heavy: builds a large slice one append at a time with no
// capacity hint.
// ---------------------------------------------------------------------------

func buildPayload(n int) []byte {
	// BAD: no pre-allocation, so append regrows the slice repeatedly.
	var buf []byte
	for i := 0; i < n; i++ {
		buf = append(buf, byte(rand.Intn(256)))
	}
	return buf
}

// ---------------------------------------------------------------------------
// Blocking channel sender: sends without select/default, so it blocks
// when the buffer is full. The handler fires it in a goroutine, so
// blocked sends pile up as extra live goroutines under load.
// ---------------------------------------------------------------------------

var metricsCh = make(chan int, 10)

func emitMetric(val int) {
	// BAD: blocks when the buffer is full; no select with default.
	metricsCh <- val
}

func metricsDrain() {
	for range metricsCh {
		// BAD: drains far slower than the handler emits.
		time.Sleep(50 * time.Millisecond)
	}
}

// ---------------------------------------------------------------------------
// Handler: every request leaks goroutines, writes to the unbounded
// cache under the global lock, runs the CPU-heavy processItems, and
// does a pointless reflect sum.
// ---------------------------------------------------------------------------

func handleWork(w http.ResponseWriter, r *http.Request) {
	// Leak goroutines on every request.
	spawnLeakyWorkers(2)
	go emitMetric(rand.Intn(1000))

	// Build a large payload and cache it under a unique key, holding
	// the global mutex across an O(cache) scan.
	payload := buildPayload(64 * 1024) // 64 KiB
	key := fmt.Sprintf("req-%d", time.Now().UnixNano())
	entries, cachedBytes := cacheSet(key, payload)

	// CPU-bound string processing.
	items := make([]string, 50)
	for i := range items {
		items[i] = fmt.Sprintf("item-%d-value-%d", i, rand.Intn(10000))
	}
	_ = processItems(items)

	// Pointless reflect sum.
	nums := make([]int64, 200)
	for i := range nums {
		nums[i] = int64(rand.Intn(1000))
	}
	_ = reflectSum(nums)

	fmt.Fprintf(w, "ok (cache entries: %d, cached bytes: %d)\n", entries, cachedBytes)
}

func main() {
	go metricsDrain()

	http.HandleFunc("/work", handleWork)

	fmt.Println("leaky_server listening on :6060")
	fmt.Println("  pprof at http://localhost:6060/debug/pprof/")
	if err := http.ListenAndServe(":6060", nil); err != nil {
		fmt.Printf("server error: %v\n", err)
	}
}
