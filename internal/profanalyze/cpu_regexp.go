package profanalyze

import "slices"

func init() { MustRegister(newCategoryDetector(cpuRegexpSpec)) }

// cpuRegexpSpec reports when regular-expression work consumes a
// meaningful share of CPU, and specifically calls out per-call
// compilation when Compile/MustCompile frames are present.
var cpuRegexpSpec = categorySpec{
	meta: Metadata{
		Scope:  ScopeCPU,
		Num:    2,
		Name:   "high-regexp-cpu",
		Checks: "Whether regular-expression compilation or matching consumes a meaningful share of CPU.",
		Method: "Attributes each CPU sample once to the regexp category when any " +
			"stack frame is a regexp.* or regexp/syntax.* function, then " +
			"reports the category's share of total CPU. When Compile/MustCompile " +
			"frames are among the matches, the recommendation is upgraded to " +
			"call out per-call compilation. Fires above 3% share; severity is " +
			"medium at 10% and high at 25%.",
		Limitations: "Cannot judge whether the pattern itself is pathological or " +
			"the matching volume is irreducible for the workload; it sees that " +
			"regexp work happens, not why.",
	},
	view: cpuView,
	// "regexp." does not match regexp/syntax.* (no dot after the
	// package segment), so the parse/compile machinery needs its own
	// prefix.
	prefixes:  []string{"regexp.", "regexp/syntax."},
	title:     "regexp dominates CPU samples",
	subject:   "regexp frames",
	object:    "CPU",
	recommend: "Regex on the hot path. Verify Compile/MustCompile is hoisted to package init, and consider a plain string operation if the pattern is simple.",
	// regexp.Compile / regexp.MustCompile inside a hot loop is a
	// classic mistake — call it out specifically when those frames
	// are present.
	upgradeRec: func(matched []string) (string, bool) {
		for _, compileFrame := range []string{"regexp.Compile", "regexp.MustCompile", "regexp.compile"} {
			if slices.Contains(matched, compileFrame) {
				return "regexp.Compile/MustCompile observed on the hot path — almost certainly being compiled per call. Move to a package-level var initialised once.", true
			}
		}
		return "", false
	},
	confidence: ConfidenceHigh,
}
