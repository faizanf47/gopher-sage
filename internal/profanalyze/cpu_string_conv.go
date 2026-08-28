package profanalyze

func init() { MustRegister(newCategoryDetector(cpuStringConvSpec)) }

// cpuStringConvSpec reports when string<->[]byte conversion and
// string concatenation consume a meaningful share of CPU.
var cpuStringConvSpec = categorySpec{
	meta: Metadata{
		Scope:  ScopeCPU,
		Num:    7,
		Name:   "expensive-string-conversion",
		Checks: "Whether string<->[]byte conversions and string concatenation consume a meaningful share of CPU.",
		Method: "Attributes each CPU sample once to the category when any stack " +
			"frame is one of the runtime's conversion/concatenation helpers " +
			"(slicebytetostring, stringtoslicebyte, slicerunetostring, " +
			"stringtoslicerune, concatstring*, concatbyte*), then reports " +
			"the category's share of total CPU. The non-allocating " +
			"slicebytetostringtmp variant is excluded so free conversions do " +
			"not inflate the category. Fires above 3% share; severity is " +
			"medium at 10% and high at 25%.",
		Limitations: "Call-site attribution names the nearest non-stdlib caller, " +
			"which may be a third-party library rather than first-party code. " +
			"Confidence is medium.",
	},
	view: cpuView,
	// "runtime.concatstring" covers concatstrings and the arity
	// variants concatstring2..5; "runtime.concatbyte" covers the
	// byte-slice concatenation helpers (Go >= 1.21).
	prefixes: []string{
		"runtime.slicebytetostring",
		"runtime.stringtoslicebyte",
		"runtime.slicerunetostring",
		"runtime.stringtoslicerune",
		"runtime.concatstring",
		"runtime.concatbyte",
	},
	// slicebytetostringtmp is the compiler's non-allocating variant —
	// matching it would flag conversions that cost nothing.
	exclude:    []string{"runtime.slicebytetostringtmp"},
	title:      "string / []byte conversion on hot path",
	subject:    "string<->[]byte conversion frames",
	object:     "CPU",
	recommend:  "Look for []byte(s) / string(b) inside loops or per-request paths. Replace string concatenation with strings.Builder, or reuse a []byte buffer across calls.",
	confidence: ConfidenceMedium,
}
