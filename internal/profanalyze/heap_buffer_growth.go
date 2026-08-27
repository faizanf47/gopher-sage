package profanalyze

import "fmt"

func init() { MustRegister(heapBufferGrowthDetector{}) }

// heapBufferGrowthDetector reports when grow-and-copy of slices and
// byte/string builders drives allocation.
type heapBufferGrowthDetector struct{}

func (heapBufferGrowthDetector) Meta() Metadata {
	return Metadata{
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
	}
}

func (d heapBufferGrowthDetector) Detect(ctx DetectCtx) []Finding {
	v := ctx.AllocSpace
	if v.Total == 0 {
		return nil
	}
	names, total := matchBySample(v,
		"bytes.makeSlice",
		"bytes.(*Buffer).grow",
		"bytes.growSlice",
		"strings.(*Builder).grow",
		"strings.(*Builder).WriteString",
		"runtime.growslice",
	)
	share := percentOf(total, v.Total)
	if share < shareThreshold {
		return nil
	}
	return []Finding{makeFinding(
		d.Meta(), v,
		"buffer / slice growth allocates aggressively",
		fmt.Sprintf("buffer-grow frames account for %.2f%% of allocation.", share),
		"Pre-size buffers (make([]T, 0, n) / Builder.Grow) or reuse via sync.Pool to remove the grow-and-copy. Validate the pool win with -benchmem before recommending.",
		names, share, gradeShare(share), ConfidenceHigh,
	)}
}
