package profanalyze

func init() { MustRegister(newTopFlatDetector(heapInuseSpaceSpec)) }

// heapInuseSpaceSpec reports the functions holding the most live
// heap — candidates for retention review.
var heapInuseSpaceSpec = topFlatSpec{
	meta: Metadata{
		Scope:  ScopeHeap,
		Num:    2,
		Name:   "high-inuse-space",
		Checks: "Which functions hold the most live heap (inuse_space) — retention candidates.",
		Method: "Ranks functions by their flat inuse_space value and reports the " +
			"top 3 whose individual share exceeds 3% of live heap. Flat " +
			"attribution lands on the allocating function itself, so the named " +
			"frames are usually workspace code.",
		Limitations: "Nearly every healthy process concentrates live heap in a " +
			"few sites, so a high share alone does not indicate a leak — it " +
			"says where the memory lives, not whether it should.",
	},
	view:       inuseSpaceView,
	n:          3,
	title:      "hot inuse_space frames",
	subject:    "top inuse_space frames",
	object:     "live heap",
	recommend:  "Retention candidates. Verify whether the memory is a deliberate cache or an unintended hold (leak, growing slice/map without eviction).",
	confidence: ConfidenceHigh,
}
