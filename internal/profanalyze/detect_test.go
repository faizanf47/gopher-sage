package profanalyze

import (
	"fmt"
	"strings"
	"testing"

	pp "github.com/google/pprof/profile"
)

// heavyJSONCPUProfile builds a CPU profile where ~70% of samples
// run through encoding/json.Marshal — enough to fire the
// high-json-cpu detector at high severity.
func heavyJSONCPUProfile() *pp.Profile {
	handler := &pp.Function{ID: 1, Name: "main.handler", Filename: "main.go"}
	jsonM := &pp.Function{ID: 2, Name: "encoding/json.Marshal", Filename: "encoding/json/encode.go"}
	other := &pp.Function{ID: 3, Name: "main.other", Filename: "main.go"}

	locHandler := &pp.Location{ID: 1, Line: []pp.Line{{Function: handler, Line: 1}}}
	locJSON := &pp.Location{ID: 2, Line: []pp.Line{{Function: jsonM, Line: 100}}}
	locOther := &pp.Location{ID: 3, Line: []pp.Line{{Function: other, Line: 20}}}

	return &pp.Profile{
		SampleType: []*pp.ValueType{{Type: "cpu", Unit: "nanoseconds"}},
		PeriodType: &pp.ValueType{Type: "cpu", Unit: "nanoseconds"},
		Period:     10_000_000,
		Function:   []*pp.Function{handler, jsonM, other},
		Location:   []*pp.Location{locHandler, locJSON, locOther},
		Sample: []*pp.Sample{
			{Location: []*pp.Location{locJSON, locHandler}, Value: []int64{700}},
			{Location: []*pp.Location{locOther, locHandler}, Value: []int64{300}},
		},
	}
}

// heapProfileWithBufferGrowth builds a heap profile where
// bytes.makeSlice + runtime.growslice drive most of alloc_space —
// the signature the buffer-growth detector recognises.
func heapProfileWithBufferGrowth() *pp.Profile {
	handler := &pp.Function{ID: 1, Name: "main.handler", Filename: "main.go"}
	grow := &pp.Function{ID: 2, Name: "runtime.growslice", Filename: "runtime/slice.go"}
	bytesMake := &pp.Function{ID: 3, Name: "bytes.makeSlice", Filename: "bytes/buffer.go"}

	locHandler := &pp.Location{ID: 1, Line: []pp.Line{{Function: handler, Line: 1}}}
	locGrow := &pp.Location{ID: 2, Line: []pp.Line{{Function: grow, Line: 10}}}
	locBytes := &pp.Location{ID: 3, Line: []pp.Line{{Function: bytesMake, Line: 10}}}

	return &pp.Profile{
		SampleType: []*pp.ValueType{
			{Type: "alloc_objects", Unit: "count"},
			{Type: "alloc_space", Unit: "bytes"},
			{Type: "inuse_objects", Unit: "count"},
			{Type: "inuse_space", Unit: "bytes"},
		},
		PeriodType:        &pp.ValueType{Type: "space", Unit: "bytes"},
		Period:            524288,
		DefaultSampleType: "inuse_space",
		Function:          []*pp.Function{handler, grow, bytesMake},
		Location:          []*pp.Location{locHandler, locGrow, locBytes},
		Sample: []*pp.Sample{
			// 80% of alloc_space goes through growslice via handler.
			{Location: []*pp.Location{locGrow, locHandler}, Value: []int64{10, 800, 0, 0}},
			// 20% goes through bytes.makeSlice, also via handler.
			{Location: []*pp.Location{locBytes, locHandler}, Value: []int64{5, 200, 0, 0}},
		},
	}
}

// heapProfileWithRetention builds a heap profile where
// 'main.cache' shows up much heavier in inuse_space than in
// alloc_space — the retention-hotspot detector's signature.
func heapProfileWithRetention() *pp.Profile {
	cache := &pp.Function{ID: 1, Name: "main.cache", Filename: "main.go"}
	churn := &pp.Function{ID: 2, Name: "main.churn", Filename: "main.go"}

	locCache := &pp.Location{ID: 1, Line: []pp.Line{{Function: cache, Line: 1}}}
	locChurn := &pp.Location{ID: 2, Line: []pp.Line{{Function: churn, Line: 1}}}

	return &pp.Profile{
		SampleType: []*pp.ValueType{
			{Type: "alloc_objects", Unit: "count"},
			{Type: "alloc_space", Unit: "bytes"},
			{Type: "inuse_objects", Unit: "count"},
			{Type: "inuse_space", Unit: "bytes"},
		},
		PeriodType:        &pp.ValueType{Type: "space", Unit: "bytes"},
		Period:            524288,
		DefaultSampleType: "inuse_space",
		Function:          []*pp.Function{cache, churn},
		Location:          []*pp.Location{locCache, locChurn},
		Sample: []*pp.Sample{
			// main.cache: small alloc, large retained.
			{Location: []*pp.Location{locCache}, Value: []int64{1, 100, 1, 900}},
			// main.churn: large alloc, small retained.
			{Location: []*pp.Location{locChurn}, Value: []int64{10, 900, 1, 100}},
		},
	}
}

func findingsByName(in []Finding) map[string]Finding {
	out := make(map[string]Finding, len(in))
	for _, f := range in {
		out[f.Detector] = f
	}
	return out
}

func TestRun_highJSONCPU_fires(t *testing.T) {
	t.Parallel()
	prof := &Profile{Raw: heavyJSONCPUProfile()}
	findings, err := Run(prof, DefaultDetectors())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	by := findingsByName(findings)
	got, ok := by["high-json-cpu"]
	if !ok {
		t.Fatalf("expected high-json-cpu finding, got: %+v", by)
	}
	if got.Scope != ScopeCPU {
		t.Errorf("scope = %q, want %q", got.Scope, ScopeCPU)
	}
	if got.SampleType != string(SampleCPU) {
		t.Errorf("SampleType = %q, want cpu", got.SampleType)
	}
	if got.SharePerc < 60 {
		t.Errorf("share = %.2f, want >= 60", got.SharePerc)
	}
	if got.Severity != SeverityHigh {
		t.Errorf("severity = %q, want high", got.Severity)
	}
	if !contains(got.Functions, "encoding/json.Marshal") {
		t.Errorf("functions missing json.Marshal: %v", got.Functions)
	}
	if got.MatchedValue != 700 || got.Unit != "nanoseconds" {
		t.Errorf("matched value = %d %q, want 700 nanoseconds", got.MatchedValue, got.Unit)
	}
	if !strings.Contains(got.Evidence, "700ns of 1.0µs") {
		t.Errorf("evidence missing humanized values: %q", got.Evidence)
	}
}

func TestRun_heapDetectorsSkippedOnCPUProfile(t *testing.T) {
	t.Parallel()
	prof := &Profile{Raw: heavyJSONCPUProfile()}
	findings, err := Run(prof, DefaultDetectors())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, f := range findings {
		if f.Scope == ScopeHeap {
			t.Errorf("heap detector fired on CPU profile: %+v", f)
		}
	}
}

func TestRun_bufferGrowth_fires(t *testing.T) {
	t.Parallel()
	prof := &Profile{Raw: heapProfileWithBufferGrowth()}
	findings, err := Run(prof, DefaultDetectors())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	by := findingsByName(findings)
	got, ok := by["buffer-growth-pressure"]
	if !ok {
		t.Fatalf("expected buffer-growth-pressure finding, got: %+v", by)
	}
	if got.Scope != ScopeHeap {
		t.Errorf("scope = %q, want %q", got.Scope, ScopeHeap)
	}
	if got.SampleType != string(SampleAllocSpace) {
		t.Errorf("SampleType = %q, want alloc_space", got.SampleType)
	}
	if got.SharePerc < 90 {
		t.Errorf("share = %.2f, want >= 90", got.SharePerc)
	}
	if got.MatchedValue != 1000 || got.Unit != "bytes" {
		t.Errorf("matched value = %d %q, want 1000 bytes", got.MatchedValue, got.Unit)
	}
}

func TestRun_cpuDetectorsSkippedOnHeapProfile(t *testing.T) {
	t.Parallel()
	prof := &Profile{Raw: heapProfileWithBufferGrowth()}
	findings, err := Run(prof, DefaultDetectors())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, f := range findings {
		if f.Scope == ScopeCPU {
			t.Errorf("CPU detector fired on heap profile: %+v", f)
		}
	}
}

func TestRun_retentionHotspot_fires(t *testing.T) {
	t.Parallel()
	prof := &Profile{Raw: heapProfileWithRetention()}
	findings, err := Run(prof, DefaultDetectors())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	by := findingsByName(findings)
	got, ok := by["possible-retention-hotspot"]
	if !ok {
		t.Fatalf("expected possible-retention-hotspot finding, got: %+v", by)
	}
	if !contains(got.Functions, "main.cache") {
		t.Errorf("functions missing main.cache: %v", got.Functions)
	}
	// main.churn allocates much more than it retains — must NOT be
	// reported as a retention hotspot.
	if contains(got.Functions, "main.churn") {
		t.Errorf("main.churn should not appear as retention hotspot: %v", got.Functions)
	}
}

func TestRun_noiseFloor_dropsBelowThreshold(t *testing.T) {
	t.Parallel()

	// A CPU profile where encoding/json takes only ~1% of samples
	// must NOT fire the json detector (below shareThreshold).
	jsonM := &pp.Function{ID: 1, Name: "encoding/json.Marshal", Filename: "encoding/json/encode.go"}
	other := &pp.Function{ID: 2, Name: "main.other", Filename: "main.go"}
	locJSON := &pp.Location{ID: 1, Line: []pp.Line{{Function: jsonM, Line: 1}}}
	locOther := &pp.Location{ID: 2, Line: []pp.Line{{Function: other, Line: 1}}}

	prof := &Profile{Raw: &pp.Profile{
		SampleType: []*pp.ValueType{{Type: "cpu", Unit: "nanoseconds"}},
		PeriodType: &pp.ValueType{Type: "cpu", Unit: "nanoseconds"},
		Period:     10_000_000,
		Function:   []*pp.Function{jsonM, other},
		Location:   []*pp.Location{locJSON, locOther},
		Sample: []*pp.Sample{
			{Location: []*pp.Location{locJSON}, Value: []int64{1}},
			{Location: []*pp.Location{locOther}, Value: []int64{999}},
		},
	}}

	findings, err := Run(prof, DefaultDetectors())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, f := range findings {
		if f.Detector == "high-json-cpu" {
			t.Errorf("high-json-cpu fired at low share: %+v", f)
		}
	}
}

func TestRun_nilProfile(t *testing.T) {
	t.Parallel()
	if _, err := Run(nil, DefaultDetectors()); err == nil {
		t.Fatal("expected error for nil profile")
	}
	if _, err := Run(&Profile{}, DefaultDetectors()); err == nil {
		t.Fatal("expected error for empty profile")
	}
}

func contains(haystack []string, needle string) bool {
	for _, h := range haystack {
		if strings.Contains(h, needle) {
			return true
		}
	}
	return false
}

// nestedJSONCPUProfile builds a CPU profile where one sample passes
// through TWO encoding/json frames (Encoder.Encode → Marshal).
// Per-sample attribution must count that sample once: the true JSON
// share is 60%, not the 120% that summing per-function cum values
// would report.
func nestedJSONCPUProfile() *pp.Profile {
	handler := &pp.Function{ID: 1, Name: "main.handler", Filename: "main.go"}
	enc := &pp.Function{ID: 2, Name: "encoding/json.(*Encoder).Encode", Filename: "encoding/json/stream.go"}
	marshal := &pp.Function{ID: 3, Name: "encoding/json.Marshal", Filename: "encoding/json/encode.go"}
	other := &pp.Function{ID: 4, Name: "main.other", Filename: "main.go"}

	locHandler := &pp.Location{ID: 1, Line: []pp.Line{{Function: handler, Line: 1}}}
	locEnc := &pp.Location{ID: 2, Line: []pp.Line{{Function: enc, Line: 10}}}
	locMarshal := &pp.Location{ID: 3, Line: []pp.Line{{Function: marshal, Line: 100}}}
	locOther := &pp.Location{ID: 4, Line: []pp.Line{{Function: other, Line: 20}}}

	return &pp.Profile{
		SampleType: []*pp.ValueType{{Type: "cpu", Unit: "nanoseconds"}},
		PeriodType: &pp.ValueType{Type: "cpu", Unit: "nanoseconds"},
		Period:     10_000_000,
		Function:   []*pp.Function{handler, enc, marshal, other},
		Location:   []*pp.Location{locHandler, locEnc, locMarshal, locOther},
		Sample: []*pp.Sample{
			{Location: []*pp.Location{locMarshal, locEnc, locHandler}, Value: []int64{600}},
			{Location: []*pp.Location{locOther, locHandler}, Value: []int64{400}},
		},
	}
}

func TestRun_nestedCategoryFramesCountOnce(t *testing.T) {
	t.Parallel()
	findings, err := Run(&Profile{Raw: nestedJSONCPUProfile()}, DefaultDetectors())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	got, ok := findingsByName(findings)["high-json-cpu"]
	if !ok {
		t.Fatalf("expected high-json-cpu finding, got: %+v", findings)
	}
	if got.SharePerc != 60 {
		t.Errorf("share = %.2f, want exactly 60 (one count per sample)", got.SharePerc)
	}
	if got.Severity != SeverityHigh {
		t.Errorf("severity = %q, want high", got.Severity)
	}
	for _, fn := range []string{"encoding/json.Marshal", "encoding/json.(*Encoder).Encode"} {
		if !contains(got.Functions, fn) {
			t.Errorf("functions missing %s: %v", fn, got.Functions)
		}
	}
}

// heapProfileWithGrowChain builds a heap profile where a single
// allocation reaches THREE buffer-grow frames stacked on one
// another ((*Buffer).grow → growSlice → runtime.growslice). The
// buffer-growth share must be the sample's true 80%, not 240%.
func heapProfileWithGrowChain() *pp.Profile {
	handler := &pp.Function{ID: 1, Name: "main.handler", Filename: "main.go"}
	bufGrow := &pp.Function{ID: 2, Name: "bytes.(*Buffer).grow", Filename: "bytes/buffer.go"}
	growSlice := &pp.Function{ID: 3, Name: "bytes.growSlice", Filename: "bytes/buffer.go"}
	rtGrow := &pp.Function{ID: 4, Name: "runtime.growslice", Filename: "runtime/slice.go"}
	other := &pp.Function{ID: 5, Name: "main.other", Filename: "main.go"}

	locHandler := &pp.Location{ID: 1, Line: []pp.Line{{Function: handler, Line: 1}}}
	locBufGrow := &pp.Location{ID: 2, Line: []pp.Line{{Function: bufGrow, Line: 10}}}
	locGrowSlice := &pp.Location{ID: 3, Line: []pp.Line{{Function: growSlice, Line: 20}}}
	locRtGrow := &pp.Location{ID: 4, Line: []pp.Line{{Function: rtGrow, Line: 30}}}
	locOther := &pp.Location{ID: 5, Line: []pp.Line{{Function: other, Line: 1}}}

	return &pp.Profile{
		SampleType: []*pp.ValueType{
			{Type: "alloc_objects", Unit: "count"},
			{Type: "alloc_space", Unit: "bytes"},
			{Type: "inuse_objects", Unit: "count"},
			{Type: "inuse_space", Unit: "bytes"},
		},
		PeriodType:        &pp.ValueType{Type: "space", Unit: "bytes"},
		Period:            524288,
		DefaultSampleType: "inuse_space",
		Function:          []*pp.Function{handler, bufGrow, growSlice, rtGrow, other},
		Location:          []*pp.Location{locHandler, locBufGrow, locGrowSlice, locRtGrow, locOther},
		Sample: []*pp.Sample{
			{Location: []*pp.Location{locRtGrow, locGrowSlice, locBufGrow, locHandler}, Value: []int64{10, 800, 0, 0}},
			{Location: []*pp.Location{locOther, locHandler}, Value: []int64{5, 200, 0, 0}},
		},
	}
}

func TestRun_growChainCountsOnce(t *testing.T) {
	t.Parallel()
	findings, err := Run(&Profile{Raw: heapProfileWithGrowChain()}, DefaultDetectors())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	got, ok := findingsByName(findings)["buffer-growth-pressure"]
	if !ok {
		t.Fatalf("expected buffer-growth-pressure finding, got: %+v", findings)
	}
	if got.SharePerc != 80 {
		t.Errorf("share = %.2f, want exactly 80 (one count per sample)", got.SharePerc)
	}
}

func TestRun_categoryBeyondTop20Detected(t *testing.T) {
	t.Parallel()

	// 24 filler functions, each hotter than the JSON frame, push
	// encoding/json.Marshal past rank 20. Views must carry every
	// function — a category above the noise floor has to be
	// detected even when its frames sit outside a top-20
	// leaderboard.
	p := &pp.Profile{
		SampleType: []*pp.ValueType{{Type: "cpu", Unit: "nanoseconds"}},
		PeriodType: &pp.ValueType{Type: "cpu", Unit: "nanoseconds"},
		Period:     10_000_000,
	}
	for i := range 24 {
		fn := &pp.Function{ID: uint64(i + 1), Name: fmt.Sprintf("main.filler%02d", i), Filename: "main.go"}
		loc := &pp.Location{ID: uint64(i + 1), Line: []pp.Line{{Function: fn, Line: 1}}}
		p.Function = append(p.Function, fn)
		p.Location = append(p.Location, loc)
		p.Sample = append(p.Sample, &pp.Sample{Location: []*pp.Location{loc}, Value: []int64{41}})
	}
	handler := &pp.Function{ID: 100, Name: "main.handler", Filename: "main.go"}
	marshal := &pp.Function{ID: 101, Name: "encoding/json.Marshal", Filename: "encoding/json/encode.go"}
	locHandler := &pp.Location{ID: 100, Line: []pp.Line{{Function: handler, Line: 1}}}
	locMarshal := &pp.Location{ID: 101, Line: []pp.Line{{Function: marshal, Line: 5}}}
	p.Function = append(p.Function, handler, marshal)
	p.Location = append(p.Location, locHandler, locMarshal)
	// 40 of 1024 total → 3.91%, above the 3% noise floor.
	p.Sample = append(p.Sample, &pp.Sample{Location: []*pp.Location{locMarshal, locHandler}, Value: []int64{40}})

	findings, err := Run(&Profile{Raw: p}, DefaultDetectors())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	got, ok := findingsByName(findings)["high-json-cpu"]
	if !ok {
		t.Fatalf("expected high-json-cpu finding for category outside top-20, got: %+v", findings)
	}
	if got.SharePerc != 3.91 {
		t.Errorf("share = %.2f, want 3.91", got.SharePerc)
	}
}
