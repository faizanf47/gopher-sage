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
			"stack frame is a growth function (bytes.makeSlice, " +
			"bytes.(*Buffer).grow, bytes.growSlice, strings.(*Builder).grow, " +
			"strings.(*Builder).WriteString, runtime.growslice), then reports " +
			"the category's share of alloc_space. Fires above 3% share; " +
			"severity is medium at 10% and high at 25%.",
		Limitations: "strings.(*Builder).WriteString matches even when the " +
			"Builder is used correctly, and growth cost may already be " +
			"amortised — a match shows growth happens, not that pre-sizing " +
			"is missing.",
	},
	view: allocSpaceView,
	prefixes: []string{
		"bytes.makeSlice",
		"bytes.(*Buffer).grow",
		"bytes.growSlice",
		"strings.(*Builder).grow",
		"strings.(*Builder).WriteString",
		"runtime.growslice",
	},
	title:      "buffer / slice growth allocates aggressively",
	subject:    "buffer-grow frames",
	object:     "allocation",
	recommend:  "Pre-size buffers (make([]T, 0, n) / Builder.Grow) or reuse via sync.Pool to remove the grow-and-copy. Validate the pool win with -benchmem before recommending.",
	confidence: ConfidenceHigh,
}
