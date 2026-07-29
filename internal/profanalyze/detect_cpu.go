package profanalyze

import "fmt"

// CPU detectors attribute samples to a category per-sample: a
// sample counts toward (say) the JSON share when any frame on its
// stack matches the category's prefixes, and counts exactly once no
// matter how many matching frames the stack carries. This answers
// the flame-graph question — "how much of the profile passes
// through this category" — and keeps shares true percentages even
// when stdlib frames nest (regexp.MustCompile → regexp.Compile →
// regexp.compile).

type cpuJSONDetector struct{}

func (cpuJSONDetector) Name() string { return "high-json-cpu" }
func (cpuJSONDetector) Scope() Scope { return ScopeCPU }
func (d cpuJSONDetector) Detect(ctx DetectCtx) []Finding {
	v := ctx.CPU
	names, total := matchBySample(v,
		"encoding/json.",
		"encoding/json.(*Encoder)",
		"encoding/json.(*Decoder)",
	)
	share := percentOf(total, v.Total)
	if share < shareThreshold {
		return nil
	}
	return []Finding{makeFinding(
		d.Name(), ScopeCPU, v,
		"encoding/json dominates CPU samples",
		fmt.Sprintf("encoding/json frames account for %.2f%% of CPU.", share),
		"Likely repeated marshal/unmarshal on the hot path. Cache encoded output, switch to easyjson/codegen, or use json.RawMessage for pass-through fields.",
		names, share, gradeShare(share), ConfidenceHigh,
	)}
}

type cpuRegexpDetector struct{}

func (cpuRegexpDetector) Name() string { return "high-regexp-cpu" }
func (cpuRegexpDetector) Scope() Scope { return ScopeCPU }
func (d cpuRegexpDetector) Detect(ctx DetectCtx) []Finding {
	v := ctx.CPU
	names, total := matchBySample(v, "regexp.")
	share := percentOf(total, v.Total)
	if share < shareThreshold {
		return nil
	}
	// regexp.Compile / regexp.MustCompile inside a hot loop is a
	// classic mistake — call it out specifically when those frames
	// are present.
	rec := "Regex on the hot path. Verify Compile/MustCompile is hoisted to package init, and consider a plain string operation if the pattern is simple."
	for _, n := range names {
		if n == "regexp.Compile" || n == "regexp.MustCompile" || n == "regexp.compile" {
			rec = "regexp.Compile/MustCompile observed on the hot path — almost certainly being compiled per call. Move to a package-level var initialised once."
			break
		}
	}
	return []Finding{makeFinding(
		d.Name(), ScopeCPU, v,
		"regexp dominates CPU samples",
		fmt.Sprintf("regexp frames account for %.2f%% of CPU.", share),
		rec,
		names, share, gradeShare(share), ConfidenceHigh,
	)}
}

type cpuCompressionDetector struct{}

func (cpuCompressionDetector) Name() string { return "high-compression-cpu" }
func (cpuCompressionDetector) Scope() Scope { return ScopeCPU }
func (d cpuCompressionDetector) Detect(ctx DetectCtx) []Finding {
	v := ctx.CPU
	names, total := matchBySample(v,
		"compress/gzip.",
		"compress/flate.",
		"compress/zlib.",
		"compress/lzw.",
		"compress/bzip2.",
	)
	share := percentOf(total, v.Total)
	if share < shareThreshold {
		return nil
	}
	return []Finding{makeFinding(
		d.Name(), ScopeCPU, v,
		"compression dominates CPU samples",
		fmt.Sprintf("compress/* frames account for %.2f%% of CPU.", share),
		"Lower the compression level, cache compressed output, or skip compression for small/already-compressed payloads.",
		names, share, gradeShare(share), ConfidenceHigh,
	)}
}

type cpuReflectionDetector struct{}

func (cpuReflectionDetector) Name() string { return "high-reflection-cpu" }
func (cpuReflectionDetector) Scope() Scope { return ScopeCPU }
func (d cpuReflectionDetector) Detect(ctx DetectCtx) []Finding {
	v := ctx.CPU
	names, total := matchBySample(v, "reflect.")
	share := percentOf(total, v.Total)
	if share < shareThreshold {
		return nil
	}
	return []Finding{makeFinding(
		d.Name(), ScopeCPU, v,
		"reflection dominates CPU samples",
		fmt.Sprintf("reflect.* frames account for %.2f%% of CPU.", share),
		"Reflection on the hot path — replace with a type switch, generics, or codegen to remove the overhead.",
		names, share, gradeShare(share), ConfidenceHigh,
	)}
}

type cpuGCDetector struct{}

func (cpuGCDetector) Name() string { return "high-gc-cpu" }
func (cpuGCDetector) Scope() Scope { return ScopeCPU }
func (d cpuGCDetector) Detect(ctx DetectCtx) []Finding {
	v := ctx.CPU
	// runtime.gcBgMarkWorker is the dedicated background mark
	// worker; runtime.scanobject / runtime.greyobject / mark*
	// frames are the scanner; runtime.mallocgc is allocation cost
	// (often the actual driver of GC pressure).
	names, total := matchBySample(v,
		"runtime.gcBgMarkWorker",
		"runtime.gcDrain",
		"runtime.scanobject",
		"runtime.greyobject",
		"runtime.markroot",
		"runtime.gcAssist",
		"runtime.gcMarkDone",
		"runtime.mallocgc",
		"runtime.sweepone",
	)
	share := percentOf(total, v.Total)
	if share < shareThreshold {
		return nil
	}
	return []Finding{makeFinding(
		d.Name(), ScopeCPU, v,
		"GC / allocator dominates CPU samples",
		fmt.Sprintf("runtime GC / allocator frames account for %.2f%% of CPU.", share),
		"Workload is allocation-heavy. Cross-reference with a heap profile (alloc_space) to locate the workspace allocation sites and reduce per-call allocations.",
		names, share, gradeShare(share), ConfidenceMedium,
	)}
}

type cpuLockContentionDetector struct{}

func (cpuLockContentionDetector) Name() string { return "high-lock-contention-signals" }
func (cpuLockContentionDetector) Scope() Scope { return ScopeCPU }
func (d cpuLockContentionDetector) Detect(ctx DetectCtx) []Finding {
	v := ctx.CPU
	// A CPU profile alone CANNOT prove lock contention — this
	// detector reports a signal, not a verdict. Confidence is held
	// at low so the finding reads as a lead for follow-up rather
	// than a claim.
	names, total := matchBySample(v,
		"sync.(*Mutex).Lock",
		"sync.(*RWMutex).Lock",
		"sync.(*RWMutex).RLock",
		"runtime.semacquire",
		"runtime.lock",
		"runtime.chansend",
		"runtime.chanrecv",
		"sync.runtime_SemacquireMutex",
	)
	share := percentOf(total, v.Total)
	if share < shareThreshold {
		return nil
	}
	return []Finding{makeFinding(
		d.Name(), ScopeCPU, v,
		"possible lock / channel contention signal",
		fmt.Sprintf("sync/runtime lock + channel frames account for %.2f%% of CPU.", share),
		"A CPU profile cannot prove contention by itself. Collect a mutex or block profile to confirm before recommending any change; treat this finding as a hypothesis only.",
		names, share, gradeShare(share), ConfidenceLow,
	)}
}

type cpuStringConvDetector struct{}

func (cpuStringConvDetector) Name() string { return "expensive-string-conversion" }
func (cpuStringConvDetector) Scope() Scope { return ScopeCPU }
func (d cpuStringConvDetector) Detect(ctx DetectCtx) []Finding {
	v := ctx.CPU
	names, total := matchBySample(v,
		"runtime.slicebytetostring",
		"runtime.stringtoslicebyte",
		"runtime.slicerunetostring",
		"runtime.stringtoslicerune",
		"runtime.concatstrings",
		"runtime.concatstring2",
		"runtime.concatstring3",
		"runtime.concatstring4",
		"runtime.concatstring5",
	)
	share := percentOf(total, v.Total)
	if share < shareThreshold {
		return nil
	}
	return []Finding{makeFinding(
		d.Name(), ScopeCPU, v,
		"string / []byte conversion on hot path",
		fmt.Sprintf("string<->[]byte conversion frames account for %.2f%% of CPU.", share),
		"Look for []byte(s) / string(b) inside loops or per-request paths. Replace string concatenation with strings.Builder, or reuse a []byte buffer across calls.",
		names, share, gradeShare(share), ConfidenceMedium,
	)}
}
