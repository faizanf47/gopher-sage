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
			"(concatstring*, concatbyte*, slicebytetostring, " +
			"stringtoslicebyte), then reports the category's share of " +
			"alloc_space. Fires above 3% share; severity is medium at 10% and " +
			"high at 25%.",
		Limitations: "Native Go heap profiles strip leading runtime frames, so " +
			"this detector fires only on profiles that retain them (foreign " +
			"writers, hand-built profiles); on native profiles concat " +
			"allocation lands flat on the calling function and surfaces via " +
			"HEAP-001 instead. Conversions forced by third-party APIs look " +
			"identical to avoidable ones.",
	},
	view: allocSpaceView,
	prefixes: []string{
		"runtime.concatstring",
		"runtime.concatbyte",
		"runtime.slicebytetostring",
		"runtime.stringtoslicebyte",
	},
	exclude:    []string{"runtime.slicebytetostringtmp"},
	title:      "string concat / conversion allocates",
	subject:    "string concat / conversion frames",
	object:     "allocation",
	recommend:  "Replace `a + b + c` with strings.Builder, and reuse []byte buffers across calls when possible.",
	confidence: ConfidenceHigh,
}
