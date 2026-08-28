package profanalyze

func init() { MustRegister(newCategoryDetector(heapMapGrowthSpec)) }

// heapMapGrowthSpec reports when map allocation and growth drive
// allocation volume.
var heapMapGrowthSpec = categorySpec{
	meta: Metadata{
		Scope:  ScopeHeap,
		Num:    6,
		Name:   "map-growth-pressure",
		Checks: "Whether map allocation and growth drive allocation.",
		Method: "Attributes each allocation sample once to the category when any " +
			"stack frame is a runtime map function (makemap, makemap_small, " +
			"mapassign*, hashGrow, growWork), then reports the category's " +
			"share of alloc_space. Fires above 3% share; severity is medium at " +
			"10% and high at 25%. Confidence is medium.",
		Limitations: "The runtime's map internals change across Go versions " +
			"(e.g. the Swiss-table rewrite), so the frame list can silently go " +
			"stale and understate map pressure on newer toolchains.",
	},
	view: allocSpaceView,
	prefixes: []string{
		"runtime.makemap",
		"runtime.makemap_small",
		"runtime.mapassign",
		"runtime.mapassign_fast",
		"runtime.mapassign_faststr",
		"runtime.hashGrow",
		"runtime.growWork",
	},
	title:      "map allocation / growth on the hot path",
	subject:    "map allocation/growth frames",
	object:     "allocation",
	recommend:  "Reuse maps across calls, pre-size with make(map[T]U, n), or swap to a slice when keys are dense / small in number.",
	confidence: ConfidenceMedium,
}
