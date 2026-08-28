package profanalyze

func init() { MustRegister(newTopFlatDetector(heapAllocSpaceSpec)) }

// heapAllocSpaceSpec reports the functions driving cumulative
// allocation volume — churn hotspots that pressure the GC. The top-3
// frames are pulled individually so the finding carries concrete
// attribution.
var heapAllocSpaceSpec = topFlatSpec{
	meta: Metadata{
		Scope:  ScopeHeap,
		Num:    1,
		Name:   "high-alloc-space",
		Checks: "Which functions drive cumulative allocation volume (alloc_space) — churn hotspots.",
		Method: "Ranks functions by their flat alloc_space value and reports the " +
			"top 3 whose individual share exceeds 3% of total allocation. Flat " +
			"attribution lands on the allocating function itself, so the named " +
			"frames are usually workspace code.",
		Limitations: "This is a ranking, not an anomaly signal: every process " +
			"has top allocators, so a finding here is orientation rather than " +
			"proof of a problem. alloc_space also accumulates since process " +
			"start, so long-running processes weight history over the present.",
	},
	view:       allocSpaceView,
	n:          3,
	title:      "hot alloc_space frames",
	subject:    "top alloc_space frames",
	object:     "total allocation",
	recommend:  "Churn hotspots — high garbage rate pressures the GC even when live memory looks fine. Reduce per-call allocation: pre-size buffers, reuse instances, or pool via sync.Pool (validate with -benchmem).",
	confidence: ConfidenceHigh,
}
