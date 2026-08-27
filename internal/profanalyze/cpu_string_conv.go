package profanalyze

import "fmt"

func init() { MustRegister(cpuStringConvDetector{}) }

// cpuStringConvDetector reports when string<->[]byte conversion and
// string concatenation consume a meaningful share of CPU.
type cpuStringConvDetector struct{}

func (cpuStringConvDetector) Meta() Metadata {
	return Metadata{
		Scope:  ScopeCPU,
		Num:    7,
		Name:   "expensive-string-conversion",
		Checks: "Whether string<->[]byte conversions and string concatenation consume a meaningful share of CPU.",
		Method: "Attributes each CPU sample once to the category when any stack " +
			"frame is one of the runtime's conversion/concatenation helpers " +
			"(slicebytetostring, stringtoslicebyte, slicerunetostring, " +
			"stringtoslicerune, concatstrings, concatstring2..5), then reports " +
			"the category's share of total CPU. Fires above 3% share; severity " +
			"is medium at 10% and high at 25%.",
		Limitations: "Cannot identify the converting call site, and conversions " +
			"inside third-party libraries look identical to workspace code. " +
			"Confidence is medium.",
	}
}

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
		d.Meta(), v,
		"string / []byte conversion on hot path",
		fmt.Sprintf("string<->[]byte conversion frames account for %.2f%% of CPU.", share),
		"Look for []byte(s) / string(b) inside loops or per-request paths. Replace string concatenation with strings.Builder, or reuse a []byte buffer across calls.",
		names, share, gradeShare(share), ConfidenceMedium,
	)}
}
