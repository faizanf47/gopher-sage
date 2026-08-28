package profanalyze

import (
	"fmt"
	"math"
	"slices"
	"sort"
	"strings"
)

// Severity grades how impactful a finding is.
type Severity string

// Severity levels, from a minor observation to a dominant cost.
const (
	SeverityLow    Severity = "low"
	SeverityMedium Severity = "medium"
	SeverityHigh   Severity = "high"
)

// Confidence grades how much of the observed pattern the profile
// alone supports. A CPU profile, for instance, cannot prove lock
// contention — the matching detector reports findings with
// ConfidenceLow so the reader treats it as a lead, not a verdict.
type Confidence string

// Confidence levels, from a lead worth checking to a near-certain
// diagnosis.
const (
	ConfidenceLow    Confidence = "low"
	ConfidenceMedium Confidence = "medium"
	ConfidenceHigh   Confidence = "high"
)

// Scope tags a detector by the kind of profile it is meaningful on.
// Run() filters detectors by scope so the heap detectors never run
// against a CPU profile and vice versa.
type Scope string

// The profile kinds the detector set covers.
const (
	ScopeCPU  Scope = "cpu"
	ScopeHeap Scope = "heap"
)

// Finding is one deterministic observation about the profile.
// Evidence and Recommendation are kept separate so the reported
// fact can be cited verbatim while the suggestion is understood as
// a starting point, not a verdict.
type Finding struct {
	// ID is the static identifier of the detector that produced the
	// finding (e.g. "CPU-001"). Stable across runs and releases, so
	// reports and tooling can reference it.
	ID       string `json:"id"`
	Detector string `json:"detector"`
	Scope    Scope  `json:"scope"`
	Title    string `json:"title"`
	// Evidence is the deterministic fact the detector observed —
	// share-of-profile, sample type, the kind of frames matched.
	// Safe to quote in a recommendation's Rationale.
	Evidence string `json:"evidence"`
	// Recommendation is the canonical remediation pattern for this
	// finding (e.g. "hoist regexp.Compile to package init"). It is
	// a starting point to adapt to the specific function the
	// evidence points at, not a finished verdict.
	Recommendation string  `json:"recommendation,omitempty"`
	SampleType     string  `json:"sample_type,omitempty"`
	SharePerc      float64 `json:"share_perc"`
	// MatchedValue is the absolute sample value SharePerc was
	// computed from, in Unit (e.g. the matched bytes of alloc_space).
	MatchedValue int64 `json:"matched_value,omitempty"`
	// Unit is the sample type's unit: "bytes", "nanoseconds", "count".
	Unit       string     `json:"unit,omitempty"`
	Severity   Severity   `json:"severity"`
	Confidence Confidence `json:"confidence"`
	// Functions lists the symbols (workspace or stdlib/runtime)
	// that triggered the detector, ranked by their contribution.
	// Capped at a small number to keep the JSON manageable.
	Functions []string `json:"functions,omitempty"`
}

// Detector is the contract the deterministic analysis layer is
// built on. Each detector is a self-contained rule: it inspects
// the supplied DetectCtx and returns zero or more findings.
//
// Detectors live one per file and self-register into the default
// Registry from an init() (see registry.go). Meta() publishes the
// detector's static ID components and its transparency contract —
// what it checks, how it decides, and its limitations; the registry
// refuses detectors that leave any of it blank.
//
// The CPU detectors inspect ctx.CPU; the heap detectors inspect the
// heap views (inuse_space / alloc_space / ...) they need. Run()
// populates only the views the profile actually carries — a heap
// detector run against a CPU profile is skipped silently rather
// than emitting noise.
type Detector interface {
	Meta() Metadata
	Detect(ctx DetectCtx) []Finding
}

// DetectCtx is the bundle of precomputed views passed to a
// Detector. Views are populated lazily by Run(): a CPU profile
// produces only the CPU view, a heap profile produces all four
// heap views. A detector that asks for a view its scope did not
// populate gets a zero-valued View it can safely range over.
type DetectCtx struct {
	CPU          View
	InuseSpace   View
	InuseObjects View
	AllocSpace   View
	AllocObjects View
}

// View is the precomputed data a Detector inspects. Building this
// once and sharing it across detectors keeps Run() linear in the
// number of detectors instead of quadratic in samples × detectors.
type View struct {
	// SampleType is the resolved sample type ("cpu", "samples",
	// "inuse_space", ...). Detectors use it to decide whether they
	// apply (a CPU detector against a heap view would only emit
	// noise).
	SampleType string
	Unit       string
	// Total is the sum of the chosen sample value across the
	// profile. SharePerc fields are computed against it.
	Total int64
	// FlatByFn / CumByFn map function name to the aggregated
	// flat / cumulative value for the resolved sample type.
	FlatByFn map[string]int64
	CumByFn  map[string]int64
	// Samples carries each sample's value and deduped, leaf-first
	// stack. Category detectors walk it (via matchBySample) so a
	// sample counts toward a category exactly once, no matter how
	// many of its frames match.
	Samples []SampleStack
}

// SampleStack is one sample's value and deduped call stack
// (leaf-first) for the view's resolved sample type.
type SampleStack struct {
	Value  int64
	Frames []string
}

// BuildView turns a parsed profile into the per-function aggregates
// and per-sample stacks the detectors consume. The View shares no
// mutable state with the parsed profile after construction, so
// detectors are free to be called concurrently against the same
// View.
func BuildView(p *Profile, idx SampleIndex) (View, error) {
	i, resolved, unit, err := p.ResolveSampleIndex(idx)
	if err != nil {
		return View{}, err
	}
	byFn, total, stacks := aggregateSamples(p.Raw, i, true)
	v := View{
		SampleType: string(resolved),
		Unit:       unit,
		Total:      total,
		FlatByFn:   make(map[string]int64, len(byFn)),
		CumByFn:    make(map[string]int64, len(byFn)),
		Samples:    stacks,
	}
	for name, a := range byFn {
		v.FlatByFn[name] = a.flat
		v.CumByFn[name] = a.cum
	}
	return v, nil
}

// Run walks the supplied detectors against p. It builds at most
// one CPU view and the four heap views the profile carries, then
// hands every detector the same DetectCtx. Detectors whose scope
// does not match a sample type available on the profile are
// silently skipped — for example, heap detectors are not run on a
// CPU profile, which carries no inuse/alloc columns.
//
// Findings are returned in detector-order so output is stable
// across runs against the same profile.
func Run(p *Profile, detectors []Detector) ([]Finding, error) {
	if p == nil || p.Raw == nil {
		return nil, fmt.Errorf("profanalyze: nil profile")
	}

	ctx, haveCPU, haveHeap, err := buildContext(p)
	if err != nil {
		return nil, err
	}

	var findings []Finding
	for _, d := range detectors {
		meta := d.Meta()
		switch meta.Scope {
		case ScopeCPU:
			if !haveCPU {
				continue
			}
		case ScopeHeap:
			if !haveHeap {
				continue
			}
		default:
			return nil, fmt.Errorf("profanalyze: detector %q has unknown scope %q", meta.Name, meta.Scope)
		}
		findings = append(findings, d.Detect(ctx)...)
	}
	return findings, nil
}

// buildContext populates DetectCtx with the views the profile
// actually carries. Heap profiles carry four sample types; CPU
// profiles carry one. Missing views stay zero-valued so detectors
// can range over them safely.
func buildContext(p *Profile) (ctx DetectCtx, haveCPU, haveHeap bool, err error) {
	if p.HasCPUSamples() {
		v, err := BuildView(p, SampleCPU)
		if err != nil {
			return DetectCtx{}, false, false, err
		}
		ctx.CPU = v
		haveCPU = true
	}
	if p.HasHeapSamples() {
		for _, pair := range []struct {
			idx  SampleIndex
			into *View
		}{
			{SampleInuseSpace, &ctx.InuseSpace},
			{SampleInuseObjects, &ctx.InuseObjects},
			{SampleAllocSpace, &ctx.AllocSpace},
			{SampleAllocObjects, &ctx.AllocObjects},
		} {
			if i := indexOfSampleType(p.Raw.SampleType, string(pair.idx)); i < 0 {
				continue
			}
			v, err := BuildView(p, pair.idx)
			if err != nil {
				return DetectCtx{}, false, false, err
			}
			*pair.into = v
		}
		haveHeap = true
	}
	return ctx, haveCPU, haveHeap, nil
}

// hasSampleType reports whether the profile carries any of the
// supplied sample-type names. It backs the exported HasCPUSamples /
// HasHeapSamples helpers the detector machinery and callers use to
// classify a profile.
func hasSampleType(p *Profile, names ...string) bool {
	for _, st := range p.Raw.SampleType {
		if slices.Contains(names, st.Type) {
			return true
		}
	}
	return false
}

// matchByPrefix returns the function names from byFn whose names
// match any of the supplied prefixes, ranked by their aggregated
// value (largest first). It deliberately does NOT return a value
// total: summing per-function values across a category counts one
// sample several times when nested frames all match — use
// matchBySample for shares.
func matchByPrefix(byFn map[string]int64, prefixes ...string) []string {
	type hit struct {
		name string
		val  int64
	}
	hits := make([]hit, 0, 8)
	for name, val := range byFn {
		for _, pre := range prefixes {
			if strings.HasPrefix(name, pre) {
				hits = append(hits, hit{name, val})
				break
			}
		}
	}
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].val != hits[j].val {
			return hits[i].val > hits[j].val
		}
		return hits[i].name < hits[j].name
	})
	matched := make([]string, 0, len(hits))
	for _, h := range hits {
		matched = append(matched, h.name)
	}
	return matched
}

// categoryMatch is the result of attributing a view's samples to a
// frame category.
type categoryMatch struct {
	// names are the matched frames, ranked by cumulative value.
	names []string
	// value is the total sample value attributed ONCE per sample
	// whose stack reaches any matching frame.
	value int64
}

// matchCategory answers the flame-graph question — "how much of the
// profile passes through this category" — for the frames matching
// any of the supplied prefixes (minus exact names in exclude). Each
// sample counts once no matter how many of its frames match, so the
// category value cannot exceed the view total even when a stack
// carries several matching frames (e.g. regexp.MustCompile →
// regexp.Compile → regexp.compile).
func matchCategory(v View, prefixes, exclude []string) categoryMatch {
	matched := matchByPrefix(v.CumByFn, prefixes...)
	if len(exclude) > 0 {
		matched = slices.DeleteFunc(matched, func(name string) bool {
			return slices.Contains(exclude, name)
		})
	}
	if len(matched) == 0 {
		return categoryMatch{}
	}
	inCategory := make(map[string]struct{}, len(matched))
	for _, name := range matched {
		inCategory[name] = struct{}{}
	}
	var m categoryMatch
	m.names = matched
	for _, s := range v.Samples {
		for _, frame := range s.Frames {
			if _, ok := inCategory[frame]; ok {
				m.value += s.Value
				break
			}
		}
	}
	return m
}

// capNames trims a function list to at most n entries so detector
// output stays compact even when many frames hit a category.
func capNames(in []string, n int) []string {
	if len(in) <= n {
		return in
	}
	return in[:n]
}

// gradeShare picks a severity based on the share-of-profile a
// category accounts for. The thresholds are conservative so a
// finding never claims "high" on a noisy 1% blip.
func gradeShare(perc float64) Severity {
	switch {
	case perc >= 25:
		return SeverityHigh
	case perc >= 10:
		return SeverityMedium
	default:
		return SeverityLow
	}
}

// shareThreshold is the minimum share-of-profile a category must
// reach before a detector emits a finding. Below this the signal
// is too noisy to be actionable.
const shareThreshold = 3.0

func makeFinding(meta Metadata, view View, title, evidence, recommendation string, names []string, matched int64, share float64, severity Severity, confidence Confidence) Finding {
	return Finding{
		ID:             meta.ID(),
		Detector:       meta.Name,
		Scope:          meta.Scope,
		Title:          title,
		Evidence:       evidence,
		Recommendation: recommendation,
		SampleType:     view.SampleType,
		SharePerc:      roundPerc(share),
		MatchedValue:   matched,
		Unit:           view.Unit,
		Severity:       severity,
		Confidence:     confidence,
		Functions:      capNames(names, 5),
	}
}

// topFlatFrames returns the top-n frames by flat value (ties broken
// by name so equal frames order deterministically), dropping any
// whose flat share is below minShare. Used by the generic high-alloc
// / high-inuse detectors to attribute a hotspot to a small, named
// set of functions rather than a blob share.
func topFlatFrames(v View, n int, minShare float64) []struct {
	name string
	flat int64
} {
	pairs := make([]struct {
		name string
		flat int64
	}, 0, len(v.FlatByFn))
	for name, flat := range v.FlatByFn {
		if percentOf(flat, v.Total) < minShare {
			continue
		}
		pairs = append(pairs, struct {
			name string
			flat int64
		}{name, flat})
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].flat != pairs[j].flat {
			return pairs[i].flat > pairs[j].flat
		}
		return pairs[i].name < pairs[j].name
	})
	if n < len(pairs) {
		pairs = pairs[:n]
	}
	return pairs
}

func roundPerc(p float64) float64 {
	// Two decimal places of precision in the JSON output is plenty
	// for human review and keeps test expectations stable.
	return math.Round(p*100) / 100
}
