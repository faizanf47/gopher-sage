package profanalyze

// The tests in this file are the guard against Go runtime symbol
// drift: instead of hand-building profiles, they run real workloads,
// capture a profile from the live runtime, and assert the detectors
// still recognise what they claim to. If a Go release renames
// bytes.growSlice, or stops stripping leading runtime frames from
// heap samples (hideRuntime in runtime/pprof/protomem.go), these
// fail and force the frame lists to be revisited.
//
// Assertions are presence-only — fires / does not fire / contains a
// name — never exact shares, so allocation noise from the rest of
// the test binary cannot flake them.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"runtime"
	"runtime/pprof"
	"slices"
	"strings"
	"testing"
	"time"
)

// Sinks keep workload results reachable so the compiler cannot
// optimise the allocations away.
var (
	rtSinkBytes  []byte
	rtSinkString string
	rtSinkMap    map[int][128]byte
	rtSinkInt    int
)

// rtWorkloadBufferGrowth churns ~64 MiB through an un-presized
// bytes.Buffer so growth lands in bytes.growSlice / (*Buffer).grow.
func rtWorkloadBufferGrowth() {
	var buf bytes.Buffer
	chunk := make([]byte, 1024)
	for range 32 * 1024 {
		buf.Write(chunk)
	}
	rtSinkBytes = buf.Bytes()
}

// rtWorkloadJSON churns >100 MiB through encoding/json. The payload
// is sized so the marshalled output dominates the pooled encoder
// internals and the category clears the 3% share floor even next to
// the map/concat workloads.
func rtWorkloadJSON() {
	type payload struct {
		Name   string
		Values []int
		Nested map[string]string
	}
	p := payload{
		Name:   "runtime-truth",
		Values: make([]int, 256),
		Nested: map[string]string{"a": "b", "c": "d"},
	}
	for i := range p.Values {
		p.Values[i] = 1234567 + i
	}
	for range 64 * 1024 {
		raw, err := json.Marshal(p)
		if err != nil {
			panic(err)
		}
		rtSinkInt += len(raw)
	}
}

// rtWorkloadMapFill fills a large map without pre-sizing; on native
// profiles the bucket allocations land flat on this function because
// the profiler strips the runtime map frames.
func rtWorkloadMapFill() {
	m := make(map[int][128]byte)
	for i := range 1 << 20 {
		m[i] = [128]byte{byte(i)}
	}
	rtSinkMap = m
}

// rtWorkloadConcat concatenates with += in a loop (~290 MiB churn);
// on native profiles the concat allocations land flat here because
// runtime.concatstrings is stripped.
func rtWorkloadConcat() {
	chunk := strings.Repeat("x", 1024)
	s := ""
	for range 768 {
		s += chunk
	}
	rtSinkString = s
}

func TestHeapRuntimeTruth(t *testing.T) {
	rtWorkloadBufferGrowth()
	rtWorkloadJSON()
	rtWorkloadMapFill()
	rtWorkloadConcat()

	// The heap profile publishes at GC boundaries and may lag by up
	// to two cycles; force it current before capturing.
	runtime.GC()
	runtime.GC()
	var buf bytes.Buffer
	if err := pprof.Lookup("allocs").WriteTo(&buf, 0); err != nil {
		t.Fatalf("write allocs profile: %v", err)
	}

	prof, err := ParseBytes("runtime-truth", buf.Bytes())
	if err != nil {
		t.Fatalf("ParseBytes: %v", err)
	}
	findings, err := Run(prof, DefaultDetectors())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	by := findingsByName(findings)

	// HEAP-003: bytes.Buffer growth frames survive runtime-frame
	// stripping (they are stdlib, not runtime). If a Go release
	// renames them, this is the failure that says so.
	growth, ok := by["buffer-growth-pressure"]
	if !ok {
		t.Fatalf("buffer-growth-pressure did not fire on a live allocs profile; findings: %v", detectorNames(findings))
	}
	if !containsAny(growth.Functions, "bytes.growSlice", "bytes.(*Buffer).grow") {
		t.Errorf("buffer-growth functions missing bytes grow frames: %v", growth.Functions)
	}
	if !callSitesContain(growth.CallSites, "rtWorkloadBufferGrowth") {
		t.Errorf("buffer-growth call sites missing rtWorkloadBufferGrowth: %+v", growth.CallSites)
	}

	// HEAP-005: encoding/json frames are plain stdlib and always
	// visible.
	jsonAlloc, ok := by["json-allocation-pressure"]
	if !ok {
		t.Fatalf("json-allocation-pressure did not fire on a live allocs profile; findings: %v", detectorNames(findings))
	}
	if !callSitesContain(jsonAlloc.CallSites, "rtWorkloadJSON") {
		t.Errorf("json-allocation call sites missing rtWorkloadJSON: %+v", jsonAlloc.CallSites)
	}

	// Inverted canary: the heap profiler strips leading runtime.*
	// frames (hideRuntime, runtime/pprof/protomem.go), so the
	// all-runtime categories must NOT fire on a native profile even
	// though the workloads hammered maps and string concat. If a Go
	// release stops stripping, these detectors come alive and this
	// assertion forces a deliberate revisit.
	for _, name := range []string{"string-concat-allocation", "map-growth-pressure"} {
		if f, fired := by[name]; fired {
			t.Errorf("%s fired on a native allocs profile — has the runtime stopped stripping leading runtime frames? %+v", name, f)
		}
	}

	// The map and concat churn is not lost: stripping attributes it
	// flat to the workload functions, where HEAP-001 reports it.
	alloc, ok := by["high-alloc-space"]
	if !ok {
		t.Fatalf("high-alloc-space did not fire on a live allocs profile; findings: %v", detectorNames(findings))
	}
	for _, workload := range []string{"rtWorkloadMapFill", "rtWorkloadConcat"} {
		if !containsSubstring(alloc.Functions, workload) {
			t.Errorf("high-alloc-space functions missing %s: %v", workload, alloc.Functions)
		}
	}
}

// rtCPUWorkloadRegexp compiles its pattern on every call — the
// per-call-compilation mistake CPU-002 exists to catch.
func rtCPUWorkloadRegexp() {
	re := regexp.MustCompile(`[a-z]+[0-9]{2,}[a-z]+`)
	rtSinkInt += len(re.FindAllString("abc123def456ghi789jkl", -1))
}

func rtCPUWorkloadJSON() {
	type payload struct {
		Name   string
		Values []int
	}
	p := payload{Name: "cpu-truth", Values: make([]int, 64)}
	raw, err := json.Marshal(p)
	if err != nil {
		panic(err)
	}
	rtSinkInt += len(raw)
}

// TestCPURuntimeTruth is the CPU-side staleness canary. It is
// deliberately lighter than the heap test: in-test CPU profiling
// needs real wall time and is scheduler-dependent, so it asserts
// only that the detectors fire and attribute to the workloads, never
// shares or severities. Skipped under -short.
func TestCPURuntimeTruth(t *testing.T) {
	if testing.Short() {
		t.Skip("CPU profiling needs wall time; skipped under -short")
	}

	var buf bytes.Buffer
	if err := pprof.StartCPUProfile(&buf); err != nil {
		t.Fatalf("start CPU profile: %v", err)
	}
	deadline := time.Now().Add(600 * time.Millisecond)
	for time.Now().Before(deadline) {
		rtCPUWorkloadRegexp()
		rtCPUWorkloadJSON()
	}
	pprof.StopCPUProfile()

	prof, err := ParseBytes("cpu-truth", buf.Bytes())
	if err != nil {
		t.Fatalf("ParseBytes: %v", err)
	}
	findings, err := Run(prof, DefaultDetectors())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	by := findingsByName(findings)

	// Per-call regexp compilation is expensive enough to clear the 3%
	// floor under any instrumentation, so it is the hard assertion.
	regexpFinding, ok := by["high-regexp-cpu"]
	if !ok {
		t.Fatalf("high-regexp-cpu did not fire on a live CPU profile; findings: %v", detectorNames(findings))
	}
	if !callSitesContain(regexpFinding.CallSites, "rtCPUWorkloadRegexp") {
		t.Errorf("regexp call sites missing rtCPUWorkloadRegexp: %+v", regexpFinding.CallSites)
	}

	// The JSON share can dip below the noise floor when sampling is
	// skewed (e.g. under -race, whose instrumentation steals samples
	// from fine-grained reflection work), so only its attribution is
	// asserted — when it fires, it must name the workload.
	if jsonFinding, fired := by["high-json-cpu"]; fired {
		if !callSitesContain(jsonFinding.CallSites, "rtCPUWorkloadJSON") {
			t.Errorf("json call sites missing rtCPUWorkloadJSON: %+v", jsonFinding.CallSites)
		}
	}
}

func detectorNames(findings []Finding) []string {
	names := make([]string, 0, len(findings))
	for _, f := range findings {
		names = append(names, fmt.Sprintf("%s(%.1f%%)", f.Detector, f.SharePerc))
	}
	return names
}

func containsAny(haystack []string, wanted ...string) bool {
	for _, w := range wanted {
		if slices.Contains(haystack, w) {
			return true
		}
	}
	return false
}

func containsSubstring(haystack []string, sub string) bool {
	for _, h := range haystack {
		if strings.Contains(h, sub) {
			return true
		}
	}
	return false
}

func callSitesContain(sites []CallSite, sub string) bool {
	for _, cs := range sites {
		if strings.Contains(cs.Function, sub) {
			return true
		}
	}
	return false
}
