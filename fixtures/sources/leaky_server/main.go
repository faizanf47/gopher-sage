// leaky_server is a deliberately terrible Go program.
// It exists solely as an Astora test fixture — every function
// contains at least one performance anti-pattern.
//
// Anti-patterns included:
//   - goroutine leak (no context cancellation)
//   - unbounded cache growth (memory leak)
//   - string concatenation in a hot loop (excessive allocation)
//   - global mutex protecting a hot path
//   - regexp compilation inside a loop
//   - fmt.Sprintf in a tight loop instead of strconv
//   - channel send without select/timeout (blocking)
//   - unnecessary reflect usage
//   - slice append without pre-allocation
package main

import (
	"fmt"
	"math/rand"
	"net/http"
	_ "net/http/pprof"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Memory leak: unbounded global cache that is never evicted.
// ---------------------------------------------------------------------------

// itemSanitizer is compiled once at program start and reused on every call to
// processItems, avoiding 50 regexp compilations per request.
var itemSanitizer = regexp.MustCompile(`[^a-zA-Z0-9]`)

var (
	cacheMu sync.Mutex
	cache   = make(map[string][]byte)
)

func cacheSet(key string, val []byte) {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	cache[key] = val
}

func cacheGet(key string) ([]byte, bool) {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	v, ok := cache[key]
	return v, ok
}

// ---------------------------------------------------------------------------
// Goroutine leak: spawns workers that never exit because there is no
// cancellation signal or done channel.
// ---------------------------------------------------------------------------

func leakyWorker(id int) {
	ch := make(chan struct{})
	// BUG: nothing ever closes ch, so this goroutine lives forever.
	close(ch)
	_ = id
}

func spawnLeakyWorkers(n int) {
	for i := 0; i < n; i++ {
		go leakyWorker(i)
	}
}

// ---------------------------------------------------------------------------
// CPU hot path: builds strings via concatenation (O(n²) allocations),
// compiles a regexp on every call, and uses fmt.Sprintf for int→string.
// ---------------------------------------------------------------------------

func processItems(items []string) string {
	var sb strings.Builder
	sb.Grow(len(items) * 16) // pre-allocate a reasonable estimate
	for i, item := range items {
		cleaned := itemSanitizer.ReplaceAllString(item, "")
		sb.WriteString(strconv.Itoa(i))
		sb.WriteByte(':')
		sb.WriteString(cleaned)
		sb.WriteByte(',')
	}
	return sb.String()
}

// ---------------------------------------------------------------------------
// Reflect abuse: uses reflect to sum an int slice instead of a type switch
// or generics.
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
// capacity hint, then copies it into the cache.
// ---------------------------------------------------------------------------

func buildPayload(n int) []byte {
	// BAD: no pre-allocation
	var buf []byte
	for i := 0; i < n; i++ {
		buf = append(buf, byte(rand.Intn(256)))
	}
	return buf
}

// ---------------------------------------------------------------------------
// Handler: every request leaks a goroutine, writes to the unbounded cache,
// runs the CPU-heavy processItems, and does a reflect sum.
// ---------------------------------------------------------------------------

func handleWork(w http.ResponseWriter, r *http.Request) {
	// Leak a goroutine on every request.
	spawnLeakyWorkers(2)

	// Build a large payload and cache it with a unique key.
	payload := buildPayload(64 * 1024) // 64 KiB
	key := fmt.Sprintf("req-%d", time.Now().UnixNano())
	cacheSet(key, payload)

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

	fmt.Fprintf(w, "ok (cache size: %d)\n", len(cache))
}

// ---------------------------------------------------------------------------
// Blocking channel sender: sends without select, so it blocks when the
// buffer is full.
// ---------------------------------------------------------------------------

var metricsCh = make(chan int, 10)

func emitMetric(val int) {
	// BAD: blocks when buffer full; no select with default.
	metricsCh <- val
}

func metricsDrain() {
	for range metricsCh {
		// discard
	}
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
