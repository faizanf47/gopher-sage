package profanalyze

import "fmt"

func init() { MustRegister(cpuCompressionDetector{}) }

// cpuCompressionDetector reports when compress/* work consumes a
// meaningful share of CPU.
type cpuCompressionDetector struct{}

func (cpuCompressionDetector) Meta() Metadata {
	return Metadata{
		Scope:  ScopeCPU,
		Num:    3,
		Name:   "high-compression-cpu",
		Checks: "Whether compression (gzip, flate, zlib, lzw, bzip2) consumes a meaningful share of CPU.",
		Method: "Attributes each CPU sample once to the compression category when " +
			"any stack frame belongs to a compress/* package, then reports the " +
			"category's share of total CPU. Fires above 3% share; severity is " +
			"medium at 10% and high at 25%.",
		Limitations: "A high compression share is expected — not a defect — for " +
			"services that compress payloads by design; the detector cannot " +
			"judge intent, only cost.",
	}
}

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
		d.Meta(), v,
		"compression dominates CPU samples",
		fmt.Sprintf("compress/* frames account for %.2f%% of CPU.", share),
		"Lower the compression level, cache compressed output, or skip compression for small/already-compressed payloads.",
		names, share, gradeShare(share), ConfidenceHigh,
	)}
}
