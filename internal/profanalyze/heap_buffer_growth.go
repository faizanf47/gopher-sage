package profanalyze

func init() { MustRegister(newCategoryDetector(heapBufferGrowthSpec)) }

// heapBufferGrowthSpec reports when grow-and-copy of slices and
// byte/string builders drives allocation.
var heapBufferGrowthSpec = categorySpec{
	meta: Metadata{
		Scope:  ScopeHeap,
		Num:    3,
		Name:   "buffer-growth-pressure",
		Checks: "Whether grow-and-copy of slices, bytes.Buffer, and strings.Builder drives allocation.",
		Method: "Attributes each allocation sample once to the category when any " +
			"stack frame is a growth function (bytes.(*Buffer).grow, " +
			"bytes.growSlice, bytes.makeSlice on older profiles, " +
			"strings.(*Builder).grow, runtime.growslice), then reports " +
			"the category's share of alloc_space. Fires above 3% share; " +
			"severity is medium at 10% and high at 25%.",
		Limitations: "Growth cost may already be amortised — a match shows " +
			"growth happens, not that pre-sizing is missing. Go's heap " +
			"profiler strips leading runtime frames, so plain slice append " +
			"growth (runtime.growslice) is attributed to the calling function " +
			"and caught by HEAP-001, not here; only bytes.Buffer / " +
			"strings.Builder growth is visible to this detector on native " +
			"profiles.",
	},
	view: allocSpaceView,
	// bytes.makeSlice predates bytes.growSlice; kept for profiles
	// saved from older Go runtimes. strings.(*Builder).WriteString is
	// deliberately absent: it matches correct Builder use too.
	prefixes: []string{
		"bytes.makeSlice",
		"bytes.(*Buffer).grow",
		"bytes.growSlice",
		"strings.(*Builder).grow",
		"runtime.growslice",
	},
	title:      "buffer / slice growth allocates aggressively",
	subject:    "buffer-grow frames",
	object:     "allocation",
	recommend:  "Pre-size buffers (make([]T, 0, n) / Builder.Grow) or reuse via sync.Pool to remove the grow-and-copy. Validate the pool win with -benchmem before recommending.",
	confidence: ConfidenceHigh,
}
