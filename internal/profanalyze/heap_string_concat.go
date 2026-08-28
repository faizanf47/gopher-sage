package profanalyze

func init() { MustRegister(newCategoryDetector(heapStringConcatSpec)) }

// heapStringConcatSpec reports when string concatenation and
// string<->[]byte conversion drive allocation.
var heapStringConcatSpec = categorySpec{
	meta: Metadata{
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
	},
	view: allocSpaceView,
	prefixes: []string{
		"runtime.concatstrings",
		"runtime.concatstring2",
		"runtime.concatstring3",
		"runtime.concatstring4",
		"runtime.concatstring5",
		"runtime.slicebytetostring",
		"runtime.stringtoslicebyte",
	},
	title:      "string concat / conversion allocates",
	subject:    "string concat / conversion frames",
	object:     "allocation",
	recommend:  "Replace `a + b + c` with strings.Builder, and reuse []byte buffers across calls when possible.",
	confidence: ConfidenceHigh,
}
