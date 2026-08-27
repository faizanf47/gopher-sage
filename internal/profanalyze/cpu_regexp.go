package profanalyze

import "fmt"

func init() { MustRegister(cpuRegexpDetector{}) }

// cpuRegexpDetector reports when regular-expression work consumes a
// meaningful share of CPU, and specifically calls out per-call
// compilation when Compile/MustCompile frames are present.
type cpuRegexpDetector struct{}

func (cpuRegexpDetector) Meta() Metadata {
	return Metadata{
		Scope:  ScopeCPU,
		Num:    2,
		Name:   "high-regexp-cpu",
		Checks: "Whether regular-expression compilation or matching consumes a meaningful share of CPU.",
		Method: "Attributes each CPU sample once to the regexp category when any " +
			"stack frame is a regexp.* function, then reports the category's " +
			"share of total CPU. When Compile/MustCompile frames are among the " +
			"matches, the recommendation is upgraded to call out per-call " +
			"compilation. Fires above 3% share; severity is medium at 10% and " +
			"high at 25%.",
		Limitations: "Cannot judge whether the pattern itself is pathological or " +
			"the matching volume is irreducible for the workload; it sees that " +
			"regexp work happens, not why.",
	}
}

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
		d.Meta(), v,
		"regexp dominates CPU samples",
		fmt.Sprintf("regexp frames account for %.2f%% of CPU.", share),
		rec,
		names, share, gradeShare(share), ConfidenceHigh,
	)}
}
