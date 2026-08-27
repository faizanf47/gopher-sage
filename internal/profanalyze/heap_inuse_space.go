package profanalyze

import "fmt"

func init() { MustRegister(heapInuseSpaceDetector{}) }

// heapInuseSpaceDetector reports the functions holding the most
// live heap — candidates for retention review.
type heapInuseSpaceDetector struct{}

func (heapInuseSpaceDetector) Meta() Metadata {
	return Metadata{
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
	}
}

func (d heapInuseSpaceDetector) Detect(ctx DetectCtx) []Finding {
	v := ctx.InuseSpace
	if v.Total == 0 {
		return nil
	}
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
		"hot inuse_space frames",
		fmt.Sprintf("top inuse_space frames account for %.2f%% of live heap.", share),
		"Retention candidates. Verify whether the memory is a deliberate cache or an unintended hold (leak, growing slice/map without eviction).",
		names, share, gradeShare(share), ConfidenceHigh,
	)}
}
