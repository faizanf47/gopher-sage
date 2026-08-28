package profanalyze

import (
	"fmt"
	"sort"
)

func init() { MustRegister(heapRetentionHotspotDetector{}) }

// heapRetentionHotspotDetector compares live heap against
// allocation churn to flag memory that stays alive instead of
// cycling through the GC.
type heapRetentionHotspotDetector struct{}

func (heapRetentionHotspotDetector) Meta() Metadata {
	return Metadata{
		Scope:  ScopeHeap,
		Num:    7,
		Name:   "possible-retention-hotspot",
		Checks: "Functions whose inuse_space share is materially higher than their alloc_space share — memory that stays alive instead of cycling.",
		Method: "Compares each function's share of live heap (inuse_space) " +
			"against its share of cumulative allocation (alloc_space). Flags " +
			"functions holding at least 5% of live heap whose inuse share is " +
			"at least 2x their alloc share, ranked by retention share. " +
			"Confidence is medium.",
		Limitations: "A site that continuously allocates AND retains — the most " +
			"common leak shape — has an inuse:alloc ratio near 1 and is " +
			"missed, while deliberate startup caches match the pattern and " +
			"can be false positives. Treat as a review queue, not a leak " +
			"verdict.",
	}
}

func (d heapRetentionHotspotDetector) Detect(ctx DetectCtx) []Finding {
	inuse := ctx.InuseSpace
	alloc := ctx.AllocSpace
	if inuse.Total == 0 || alloc.Total == 0 {
		return nil
	}

	// A retention hotspot is a function whose inuse_space share is
	// materially higher than its alloc_space share — bytes it
	// allocates STAY alive instead of cycling through the GC.
	const (
		minInuseShare = 5.0
		minRatio      = 2.0
	)

	type cand struct {
		name       string
		inuseShare float64
		ratio      float64
	}
	var cands []cand
	for name, inuseVal := range inuse.FlatByFn {
		inuseShare := percentOf(inuseVal, inuse.Total)
		if inuseShare < minInuseShare {
			continue
		}
		allocVal := alloc.FlatByFn[name]
		allocShare := percentOf(allocVal, alloc.Total)
		if allocShare == 0 {
			// Nothing to compare against; skip to avoid divide-by-zero.
			continue
		}
		ratio := inuseShare / allocShare
		if ratio < minRatio {
			continue
		}
		cands = append(cands, cand{name: name, inuseShare: inuseShare, ratio: ratio})
	}
	if len(cands) == 0 {
		return nil
	}
	// Order by retention share so the most-suspect candidates lead;
	// ties break by name for deterministic output.
	sort.Slice(cands, func(i, j int) bool {
		if cands[i].inuseShare != cands[j].inuseShare {
			return cands[i].inuseShare > cands[j].inuseShare
		}
		return cands[i].name < cands[j].name
	})
	var names []string
	var matched int64
	var share float64
	for _, c := range cands {
		names = append(names, c.name)
		matched += inuse.FlatByFn[c.name]
		share += c.inuseShare
	}
	return []Finding{makeFinding(
		d.Meta(), inuse,
		"long-lived retention candidate(s)",
		fmt.Sprintf(
			"frames retain materially more memory than they churn (inuse:alloc ratio >= %.0fx); %.2f%% of live heap (%s of %s) sits here.",
			minRatio, share, humanizeValue(matched, inuse.Unit), humanizeValue(inuse.Total, inuse.Unit),
		),
		"Check whether the retention is a deliberate cache (and if so, whether eviction is bounded) or an unintended hold from a long-lived reference / unbounded buffer.",
		names, matched, share, gradeShare(share), ConfidenceMedium,
	)}
}
