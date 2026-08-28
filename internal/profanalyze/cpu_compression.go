package profanalyze

func init() { MustRegister(newCategoryDetector(cpuCompressionSpec)) }

// cpuCompressionSpec reports when compress/* work consumes a
// meaningful share of CPU.
var cpuCompressionSpec = categorySpec{
	meta: Metadata{
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
	},
	view: cpuView,
	prefixes: []string{
		"compress/gzip.",
		"compress/flate.",
		"compress/zlib.",
		"compress/lzw.",
		"compress/bzip2.",
	},
	title:      "compression dominates CPU samples",
	subject:    "compress/* frames",
	object:     "CPU",
	recommend:  "Lower the compression level, cache compressed output, or skip compression for small/already-compressed payloads.",
	confidence: ConfidenceHigh,
}
