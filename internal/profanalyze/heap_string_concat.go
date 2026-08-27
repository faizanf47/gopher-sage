package profanalyze

import "fmt"

func init() { MustRegister(heapStringConcatDetector{}) }

// heapStringConcatDetector reports when string concatenation and
// string<->[]byte conversion drive allocation.
type heapStringConcatDetector struct{}

func (heapStringConcatDetector) Meta() Metadata {
	return Metadata{
		Scope:  ScopeHeap,
		Num:    4,
		Name:   "string-concat-allocation",
		Checks: "Whether string concatenation and string<->[]byte conversion drive allocation.",
		Method: "Attributes each allocation sample once to the category when any " +
			"stack frame is a runtime concatenation/conversion helper " +
			"(concatstrings, concatstring2..5, slicebytetostring, " +
			"stringtoslicebyte), then reports the category's share of " +
			"alloc_space. Fires above 3% share; severity is medium at 10% and " +
			"high at 25%.",
		Limitations: "Cannot identify the concatenating call site; conversions " +
			"forced by third-party APIs look identical to avoidable ones in " +
			"workspace code.",
	}
}

func (d heapStringConcatDetector) Detect(ctx DetectCtx) []Finding {
	v := ctx.AllocSpace
	if v.Total == 0 {
		return nil
	}
	names, total := matchBySample(v,
		"runtime.concatstrings",
		"runtime.concatstring2",
		"runtime.concatstring3",
		"runtime.concatstring4",
		"runtime.concatstring5",
		"runtime.slicebytetostring",
		"runtime.stringtoslicebyte",
	)
	share := percentOf(total, v.Total)
	if share < shareThreshold {
		return nil
	}
	return []Finding{makeFinding(
		d.Meta(), v,
		"string concat / conversion allocates",
		fmt.Sprintf("string concat / conversion frames account for %.2f%% of allocation.", share),
		"Replace `a + b + c` with strings.Builder, and reuse []byte buffers across calls when possible.",
		names, share, gradeShare(share), ConfidenceHigh,
	)}
}
