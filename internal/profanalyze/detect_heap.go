package profanalyze

import "fmt"

// Heap detectors look at the four sample types a heap profile
// carries — inuse_space, inuse_objects, alloc_space, alloc_objects.
// Each detector picks the view that is meaningful for the pattern it
// recognises. Where a detector compares retention against churn it
// uses both inuse_space and alloc_space.

type heapAllocSpaceDetector struct{}

func (heapAllocSpaceDetector) Name() string { return "high-alloc-space" }
func (heapAllocSpaceDetector) Scope() Scope { return ScopeHeap }
func (d heapAllocSpaceDetector) Detect(ctx DetectCtx) []Finding {
	v := ctx.AllocSpace
	if v.Total == 0 {
		return nil
	}
	// Pick the top alloc_space frames whose cumulative share is
	// meaningful. We pull the top-3 individually so the finding
	// carries concrete attribution.
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
		d.Name(), ScopeHeap, v,
		"hot alloc_space frames",
		fmt.Sprintf("top alloc_space frames account for %.2f%% of total allocation.", share),
		"Churn hotspots — high garbage rate pressures the GC even when live memory looks fine. Reduce per-call allocation: pre-size buffers, reuse instances, or pool via sync.Pool (validate with -benchmem).",
		names, share, gradeShare(share), ConfidenceHigh,
	)}
}

type heapInuseSpaceDetector struct{}

func (heapInuseSpaceDetector) Name() string { return "high-inuse-space" }
func (heapInuseSpaceDetector) Scope() Scope { return ScopeHeap }
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
		d.Name(), ScopeHeap, v,
		"hot inuse_space frames",
		fmt.Sprintf("top inuse_space frames account for %.2f%% of live heap.", share),
		"Retention candidates. Verify whether the memory is a deliberate cache or an unintended hold (leak, growing slice/map without eviction).",
		names, share, gradeShare(share), ConfidenceHigh,
	)}
}

type heapBufferGrowthDetector struct{}

func (heapBufferGrowthDetector) Name() string { return "buffer-growth-pressure" }
func (heapBufferGrowthDetector) Scope() Scope { return ScopeHeap }
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
		d.Name(), ScopeHeap, v,
		"buffer / slice growth allocates aggressively",
		fmt.Sprintf("buffer-grow frames account for %.2f%% of allocation.", share),
		"Pre-size buffers (make([]T, 0, n) / Builder.Grow) or reuse via sync.Pool to remove the grow-and-copy. Validate the pool win with -benchmem before recommending.",
		names, share, gradeShare(share), ConfidenceHigh,
	)}
}

type heapStringConcatDetector struct{}

func (heapStringConcatDetector) Name() string { return "string-concat-allocation" }
func (heapStringConcatDetector) Scope() Scope { return ScopeHeap }
func (d heapStringConcatDetector) Detect(ctx DetectCtx) []Finding {
	v := ctx.AllocSpace
	if v.Total == 0 {
		return nil
	}
	names, total := matchBySample(v,
		"runtime.concatstrings",
		"runtime.concatstring2",
		"runtime.concatstring3",
		"runtime.concatstring4",
		"runtime.concatstring5",
		"runtime.slicebytetostring",
		"runtime.stringtoslicebyte",
	)
	share := percentOf(total, v.Total)
	if share < shareThreshold {
		return nil
	}
	return []Finding{makeFinding(
		d.Name(), ScopeHeap, v,
		"string concat / conversion allocates",
		fmt.Sprintf("string concat / conversion frames account for %.2f%% of allocation.", share),
		"Replace `a + b + c` with strings.Builder, and reuse []byte buffers across calls when possible.",
		names, share, gradeShare(share), ConfidenceHigh,
	)}
}

type heapJSONAllocDetector struct{}

func (heapJSONAllocDetector) Name() string { return "json-allocation-pressure" }
func (heapJSONAllocDetector) Scope() Scope { return ScopeHeap }
func (d heapJSONAllocDetector) Detect(ctx DetectCtx) []Finding {
	v := ctx.AllocSpace
	if v.Total == 0 {
		return nil
	}
	names, total := matchBySample(v,
		"encoding/json.",
		"encoding/json.(*Encoder)",
		"encoding/json.(*Decoder)",
	)
	share := percentOf(total, v.Total)
	if share < shareThreshold {
		return nil
	}
	return []Finding{makeFinding(
		d.Name(), ScopeHeap, v,
		"encoding/json drives allocation",
		fmt.Sprintf("encoding/json accounts for %.2f%% of allocation.", share),
		"Reflection-driven encoding/decoding allocates heavily. Switch to easyjson/codegen, use json.RawMessage for pass-through fields, or cache encoded output where the payload repeats.",
		names, share, gradeShare(share), ConfidenceHigh,
	)}
}

type heapMapGrowthDetector struct{}

func (heapMapGrowthDetector) Name() string { return "map-growth-pressure" }
func (heapMapGrowthDetector) Scope() Scope { return ScopeHeap }
func (d heapMapGrowthDetector) Detect(ctx DetectCtx) []Finding {
	v := ctx.AllocSpace
	if v.Total == 0 {
		return nil
	}
	names, total := matchBySample(v,
		"runtime.makemap",
		"runtime.makemap_small",
		"runtime.mapassign",
		"runtime.mapassign_fast",
		"runtime.mapassign_faststr",
		"runtime.hashGrow",
		"runtime.growWork",
	)
	share := percentOf(total, v.Total)
	if share < shareThreshold {
		return nil
	}
	return []Finding{makeFinding(
		d.Name(), ScopeHeap, v,
		"map allocation / growth on the hot path",
		fmt.Sprintf("map allocation/growth frames account for %.2f%% of allocation.", share),
		"Reuse maps across calls, pre-size with make(map[T]U, n), or swap to a slice when keys are dense / small in number.",
		names, share, gradeShare(share), ConfidenceMedium,
	)}
}

type heapRetentionHotspotDetector struct{}

func (heapRetentionHotspotDetector) Name() string { return "possible-retention-hotspot" }
func (heapRetentionHotspotDetector) Scope() Scope { return ScopeHeap }
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
	// Order by retention share so the most-suspect candidates lead.
	for i := 0; i < len(cands); i++ {
		for j := i + 1; j < len(cands); j++ {
			if cands[j].inuseShare > cands[i].inuseShare {
				cands[i], cands[j] = cands[j], cands[i]
			}
		}
	}
	var names []string
	var share float64
	for _, c := range cands {
		names = append(names, c.name)
		share += c.inuseShare
	}
	return []Finding{makeFinding(
		d.Name(), ScopeHeap, inuse,
		"long-lived retention candidate(s)",
		fmt.Sprintf("frames retain materially more memory than they churn (inuse:alloc ratio >= %.0fx); %.2f%% of live heap sits here.", minRatio, share),
		"Check whether the retention is a deliberate cache (and if so, whether eviction is bounded) or an unintended hold from a long-lived reference / unbounded buffer.",
		names, share, gradeShare(share), ConfidenceMedium,
	)}
}

// topFlatFrames returns the top-n frames by flat value, dropping
// any whose flat share is below shareThreshold. Used by the
// generic high-alloc / high-inuse detectors to attribute the
// hotspot to a small, named set of functions rather than a blob
// share.
func topFlatFrames(v View, n int, minShare float64) []struct {
	name string
	flat int64
} {
	type pair struct {
		name string
		flat int64
	}
	pairs := make([]pair, 0, len(v.FlatByFn))
	for name, flat := range v.FlatByFn {
		if percentOf(flat, v.Total) < minShare {
			continue
		}
		pairs = append(pairs, pair{name, flat})
	}
	// Selection sort top-n; n is tiny (<=5) so this stays cheap and
	// avoids importing sort just for this.
	for i := 0; i < n && i < len(pairs); i++ {
		max := i
		for j := i + 1; j < len(pairs); j++ {
			if pairs[j].flat > pairs[max].flat {
				max = j
			}
		}
		pairs[i], pairs[max] = pairs[max], pairs[i]
	}
	if n > len(pairs) {
		n = len(pairs)
	}
	out := make([]struct {
		name string
		flat int64
	}, n)
	for i := 0; i < n; i++ {
		out[i] = struct {
			name string
			flat int64
		}{pairs[i].name, pairs[i].flat}
	}
	return out
}
