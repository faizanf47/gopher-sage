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
			"stack frame is a runtime map function (makemap*, mapassign*, the " +
			"Swiss-table internals under internal/runtime/maps, and the " +
			"pre-1.24 hashGrow/growWork), then reports the category's share " +
			"of alloc_space. Fires above 3% share; severity is medium at 10% " +
			"and high at 25%. Confidence is medium.",
		Limitations: "Native Go heap profiles strip leading runtime frames, so " +
			"this detector fires only on profiles that retain them (foreign " +
			"writers, hand-built profiles); on native profiles map allocation " +
			"lands flat on the calling function and surfaces via HEAP-001 " +
			"instead.",
	},
	view: allocSpaceView,
	// "runtime.makemap" covers makemap_small/makemap64 and
	// "runtime.mapassign" covers every mapassign_fast* variant.
	// internal/runtime/maps.* is the Go >= 1.24 Swiss-table
	// machinery; hashGrow/growWork exist only in profiles from
	// pre-Swiss-table runtimes.
	prefixes: []string{
		"runtime.makemap",
		"runtime.mapassign",
		"internal/runtime/maps.",
		"runtime.hashGrow",
		"runtime.growWork",
	},
	title:      "map allocation / growth on the hot path",
	subject:    "map allocation/growth frames",
	object:     "allocation",
	recommend:  "Reuse maps across calls, pre-size with make(map[T]U, n), or swap to a slice when keys are dense / small in number.",
	confidence: ConfidenceMedium,
}
