package profanalyze

import "fmt"

func init() { MustRegister(heapAllocSpaceDetector{}) }

// heapAllocSpaceDetector reports the functions driving cumulative
// allocation volume — churn hotspots that pressure the GC.
type heapAllocSpaceDetector struct{}

func (heapAllocSpaceDetector) Meta() Metadata {
	return Metadata{
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
	}
}

func (d heapAllocSpaceDetector) Detect(ctx DetectCtx) []Finding {
	v := ctx.AllocSpace
	if v.Total == 0 {
		return nil
	}
	// Pick the top alloc_space frames whose share is meaningful. We
	// pull the top-3 individually so the finding carries concrete
	// attribution.
	top := topFlatFrames(v, 3, shareThreshold)
	if len(top) == 0 {
		return nil
	}
	var names []string
	var share float64
	for _, t := range top {
		names = append(names, t.name)
		share += percentOf(t.flat, v.Total)
	}
	return []Finding{makeFinding(
		d.Meta(), v,
		"hot alloc_space frames",
		fmt.Sprintf("top alloc_space frames account for %.2f%% of total allocation.", share),
		"Churn hotspots — high garbage rate pressures the GC even when live memory looks fine. Reduce per-call allocation: pre-size buffers, reuse instances, or pool via sync.Pool (validate with -benchmem).",
		names, share, gradeShare(share), ConfidenceHigh,
	)}
}
