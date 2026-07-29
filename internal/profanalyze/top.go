package profanalyze

import (
	"sort"

	pp "github.com/google/pprof/profile"
)

// TopEntry is one row of a structured "top" report, equivalent to a
// single line of `go tool pprof -top` output but emitted as plain
// data so callers do not have to parse text.
type TopEntry struct {
	// Function is the resolved function name. For frames the
	// profile does not have a symbol for (HasFunctions=false) this
	// falls back to an "(unknown)" placeholder so an entry is never
	// silently empty.
	Function string `json:"function"`
	// File is the source file the function lives in, when the
	// profile carries that information. Empty otherwise.
	File string `json:"file,omitempty"`
	// Flat is the value attributed to the function body itself,
	// excluding callees, expressed in the resolved sample type's
	// raw unit.
	Flat int64 `json:"flat"`
	// Cum is the value for the function PLUS everything it called.
	Cum int64 `json:"cum"`
	// FlatPerc is Flat as a share of the profile total for the
	// resolved sample type, in the range [0, 100].
	FlatPerc float64 `json:"flat_perc"`
	// CumPerc is Cum as a share of the profile total for the
	// resolved sample type, in the range [0, 100].
	CumPerc float64 `json:"cum_perc"`
}

// TopReport is the full structured output of a Top call.
type TopReport struct {
	// SampleType is the resolved sample-type name (e.g. "cpu",
	// "inuse_space"). When the caller asked for "cpu" but the
	// profile carries "samples", this is the value that was
	// actually used.
	SampleType string `json:"sample_type"`
	// Unit is the sample-type's unit (e.g. "nanoseconds", "bytes",
	// "count").
	Unit string `json:"unit,omitempty"`
	// Total is the sum of Flat values across all functions for the
	// chosen sample type. Equal to the sum of Sample.Value[idx].
	Total int64 `json:"total"`
	// Entries is the per-function summary, ordered as the caller
	// asked (flat or cum desc).
	Entries []TopEntry `json:"entries"`
}

// SortBy controls which column TopReport.Entries is ranked by.
type SortBy string

const (
	SortByFlat SortBy = "flat"
	SortByCum  SortBy = "cum"
)

// TopOptions configures a Top call. Zero values produce a sensible
// CPU-or-heap default: top 20 functions ranked by cum.
type TopOptions struct {
	SampleIndex SampleIndex
	SortBy      SortBy
	Limit       int
}

// Top computes a structured Top report from p. It mirrors what
// `go tool pprof -top` produces but emits typed data rather than
// formatted text.
//
// The accounting is the canonical pprof "flat / cum per function":
//
//   - a frame's flat counts only when the function is the LEAF of a
//     sample (the deepest entry on the call stack);
//   - a frame's cum counts whenever the function appears anywhere on
//     the call stack — once per sample, even if recursion or inlining
//     stacked it multiple times, matching `go tool pprof` semantics.
//
// Percentages are computed against the profile total for the
// resolved sample type, not against the rendered top-N.
func Top(p *Profile, opts TopOptions) (TopReport, error) {
	idx, resolved, unit, err := p.ResolveSampleIndex(opts.SampleIndex)
	if err != nil {
		return TopReport{}, err
	}

	byFn, total, _ := aggregateSamples(p.Raw, idx, false)

	entries := make([]TopEntry, 0, len(byFn))
	for name, a := range byFn {
		entries = append(entries, TopEntry{
			Function: name,
			File:     a.file,
			Flat:     a.flat,
			Cum:      a.cum,
			FlatPerc: percentOf(a.flat, total),
			CumPerc:  percentOf(a.cum, total),
		})
	}

	sortBy := opts.SortBy
	if sortBy == "" {
		sortBy = SortByCum
	}
	sort.SliceStable(entries, func(i, j int) bool {
		if sortBy == SortByFlat {
			if entries[i].Flat != entries[j].Flat {
				return entries[i].Flat > entries[j].Flat
			}
			return entries[i].Cum > entries[j].Cum
		}
		if entries[i].Cum != entries[j].Cum {
			return entries[i].Cum > entries[j].Cum
		}
		return entries[i].Flat > entries[j].Flat
	})

	limit := opts.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit < len(entries) {
		entries = entries[:limit]
	}

	return TopReport{
		SampleType: string(resolved),
		Unit:       unit,
		Total:      total,
		Entries:    entries,
	}, nil
}

// fnAgg is the per-function flat/cum accounting for one resolved
// sample column.
type fnAgg struct {
	flat int64
	cum  int64
	file string
}

// aggregateSamples walks the profile once for the resolved sample
// column and produces the complete per-function flat/cum aggregates
// plus the profile total. When collectStacks is set it also records
// each sample's value and deduped, leaf-first stack so callers can
// attribute a sample to a category exactly once (see matchBySample).
//
// The accounting is the canonical pprof "flat / cum per function":
// flat counts only the leaf frame of a sample; cum counts every
// function on the stack once per sample, even when recursion or
// inlining stacked it multiple times.
func aggregateSamples(p *pp.Profile, idx int, collectStacks bool) (byFn map[string]*fnAgg, total int64, stacks []SampleStack) {
	byFn = make(map[string]*fnAgg)
	for _, s := range p.Sample {
		if idx >= len(s.Value) {
			continue
		}
		v := s.Value[idx]
		if v == 0 {
			continue
		}
		total += v

		// Iterate locations in pprof order: index 0 is the leaf
		// (deepest call), index len-1 is the root. Each location
		// can carry multiple Lines for inlined frames, with the
		// innermost inline at index 0.
		var frames []string
		seen := make(map[string]struct{})
		isLeaf := true
		for _, loc := range s.Location {
			for _, ln := range loc.Line {
				name, file := frameIdentity(ln)
				if _, dup := seen[name]; !dup {
					seen[name] = struct{}{}
					entry := byFn[name]
					if entry == nil {
						entry = &fnAgg{file: file}
						byFn[name] = entry
					}
					entry.cum += v
					if entry.file == "" {
						entry.file = file
					}
					if collectStacks {
						frames = append(frames, name)
					}
				}
				if isLeaf {
					entry := byFn[name]
					entry.flat += v
					isLeaf = false
				}
			}
		}
		if collectStacks {
			stacks = append(stacks, SampleStack{Value: v, Frames: frames})
		}
	}
	return byFn, total, stacks
}

func frameIdentity(ln pp.Line) (name, file string) {
	if ln.Function == nil {
		return "(unknown)", ""
	}
	name = ln.Function.Name
	if name == "" {
		name = ln.Function.SystemName
	}
	if name == "" {
		name = "(unknown)"
	}
	return name, ln.Function.Filename
}

func percentOf(v, total int64) float64 {
	if total == 0 {
		return 0
	}
	return 100.0 * float64(v) / float64(total)
}
