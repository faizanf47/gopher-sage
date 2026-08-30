package analyze

import (
	"fmt"
	"math"
	"sort"

	"github.com/faizanf47/gopher-sage/internal/profanalyze"
	"github.com/faizanf47/gopher-sage/internal/profile"
)

// DiffLabel classifies one finding's movement between two captures.
// The labels are mechanical — directional facts, not verdicts; the
// reader (typically a coding agent following the shipped skill)
// judges what they mean.
type DiffLabel string

const (
	// LabelFixed marks a detector that fired before and is absent after.
	LabelFixed DiffLabel = "fixed"
	// LabelNew marks a detector absent before that fired after.
	LabelNew DiffLabel = "new"
	// LabelImproved marks a fall in BOTH share and absolute value.
	LabelImproved DiffLabel = "improved"
	// LabelWorse marks a rise in BOTH share and absolute value.
	LabelWorse DiffLabel = "worse"
	// LabelUnchanged marks movement below the thresholds, or the two
	// signals disagreeing (share redistribution, load variance).
	LabelUnchanged DiffLabel = "unchanged"
	// LabelInconclusive marks an alloc_* rise. Allocation counters
	// accumulate from process start, so a rise can reflect nothing
	// but longer uptime — never auto-labeled worse.
	LabelInconclusive DiffLabel = "inconclusive"
)

// Movement thresholds: a finding moves only when BOTH the share and
// the absolute value cross them in the same direction. Requiring
// agreement is the robustness mechanism — share redistribution alone
// (fixing hotspot A raises B's share) cannot mark a regression, and
// load scaling alone (value up, share flat) cannot either.
const (
	diffSharePts     = 2.0  // share-of-profile points
	diffValueRelPerc = 10.0 // relative change of the absolute value, percent
)

// cpuWindowSkewPerc is how far two CPU sample windows may differ
// before absolute CPU values stop being comparable. Beyond it the
// diff warns; deliberately, it does not downgrade labels — judgment
// stays with the reader.
const cpuWindowSkewPerc = 10.0

const allocCaveat = "alloc_* accumulates since process start — a rise can reflect " +
	"longer uptime, not a regression; the --seconds guard is CPU-only. " +
	"Confirm with a fixed-work A/B."

// DiffReport is the full result of one Diff, shaped for direct JSON
// serialisation.
type DiffReport struct {
	Before   string        `json:"before"`
	After    string        `json:"after"`
	Profiles []ProfileDiff `json:"profiles"`
	Warnings []string      `json:"warnings,omitempty"`
}

// ProfileDiff compares one profile type across the two captures.
type ProfileDiff struct {
	Type       profile.Type `json:"type"`
	BeforeOnly bool         `json:"before_only,omitempty"`
	AfterOnly  bool         `json:"after_only,omitempty"`
	Totals     []TotalDiff  `json:"totals,omitempty"`
	// Durations carry the profile's recorded window (CPU) or process
	// uptime (heap); DurationMismatch is set for CPU pairs whose
	// windows differ beyond cpuWindowSkewPerc.
	BeforeDurationNanos int64         `json:"before_duration_nanos,omitempty"`
	AfterDurationNanos  int64         `json:"after_duration_nanos,omitempty"`
	DurationMismatch    bool          `json:"duration_mismatch,omitempty"`
	Findings            []FindingDiff `json:"findings"`
	// TopDeltas compares top frames of summary profiles (goroutine,
	// block, mutex) — e.g. main.leakyWorker 96250 → 0.
	TopDeltas []TopDelta `json:"top_deltas,omitempty"`
}

// TotalDiff compares one sample column's profile-wide total.
type TotalDiff struct {
	SampleType string  `json:"sample_type"`
	Unit       string  `json:"unit,omitempty"`
	Before     int64   `json:"before"`
	After      int64   `json:"after"`
	DeltaPerc  float64 `json:"delta_perc"` // 0 when before is 0
}

// FindingDiff compares one detector's finding across the captures,
// joined by its stable ID.
type FindingDiff struct {
	ID             string    `json:"id"`
	Detector       string    `json:"detector"`
	Title          string    `json:"title"`
	SampleType     string    `json:"sample_type,omitempty"`
	Unit           string    `json:"unit,omitempty"`
	Label          DiffLabel `json:"label"`
	Note           string    `json:"note,omitempty"`
	BeforeShare    float64   `json:"before_share_perc"`
	AfterShare     float64   `json:"after_share_perc"`
	ShareDeltaPts  float64   `json:"share_delta_pts"`
	BeforeValue    int64     `json:"before_value"`
	AfterValue     int64     `json:"after_value"`
	ValueDeltaPerc float64   `json:"value_delta_perc"`
}

// TopDelta compares one function's flat value between two summary
// profiles; a side where the function is absent reads 0.
type TopDelta struct {
	Function string `json:"function"`
	Before   int64  `json:"before"`
	After    int64  `json:"after"`
}

// Diff mechanically compares two Reports produced over a before and
// an after capture. It joins profiles by type and findings by their
// stable detector IDs, attaches directional labels and caveats, and
// renders no verdict — reading the result is the caller's job.
func Diff(before, after Report, beforeLabel, afterLabel string) (DiffReport, error) {
	d := DiffReport{Before: beforeLabel, After: afterLabel}

	beforeByType, beforeOrder, err := profilesByType(before, "before")
	if err != nil {
		return DiffReport{}, err
	}
	afterByType, afterOrder, err := profilesByType(after, "after")
	if err != nil {
		return DiffReport{}, err
	}

	for _, t := range beforeOrder {
		bp := beforeByType[t]
		ap, ok := afterByType[t]
		if !ok {
			d.Profiles = append(d.Profiles, ProfileDiff{
				Type: t, BeforeOnly: true, BeforeDurationNanos: bp.DurationNanos,
				Findings: []FindingDiff{},
			})
			d.Warnings = append(d.Warnings, fmt.Sprintf("%s profile present only in before; nothing to compare it against", t))
			continue
		}
		pd, warns := diffProfilePair(bp, ap)
		d.Profiles = append(d.Profiles, pd)
		d.Warnings = append(d.Warnings, warns...)
	}
	for _, t := range afterOrder {
		if _, ok := beforeByType[t]; ok {
			continue
		}
		ap := afterByType[t]
		d.Profiles = append(d.Profiles, ProfileDiff{
			Type: t, AfterOnly: true, AfterDurationNanos: ap.DurationNanos,
			Findings: []FindingDiff{},
		})
		d.Warnings = append(d.Warnings, fmt.Sprintf("%s profile present only in after; nothing to compare it against", t))
	}
	return d, nil
}

// profilesByType indexes a report's profiles, rejecting duplicates —
// a side with two cpu profiles has no well-defined comparison.
func profilesByType(rep Report, side string) (map[profile.Type]ProfileReport, []profile.Type, error) {
	byType := make(map[profile.Type]ProfileReport, len(rep.Profiles))
	order := make([]profile.Type, 0, len(rep.Profiles))
	for _, pr := range rep.Profiles {
		if _, dup := byType[pr.Type]; dup {
			return nil, nil, fmt.Errorf(
				"diff: %s side has more than one %s profile; pass explicit files instead of a directory", side, pr.Type,
			)
		}
		byType[pr.Type] = pr
		order = append(order, pr.Type)
	}
	return byType, order, nil
}

// diffProfilePair compares one profile type present on both sides.
func diffProfilePair(bp, ap ProfileReport) (ProfileDiff, []string) {
	pd := ProfileDiff{
		Type:                bp.Type,
		Totals:              diffTotals(bp.Totals, ap.Totals),
		BeforeDurationNanos: bp.DurationNanos,
		AfterDurationNanos:  ap.DurationNanos,
		Findings:            []FindingDiff{},
	}
	var warns []string

	if bp.Type == profile.TypeCPU {
		switch {
		case bp.DurationNanos > 0 && ap.DurationNanos > 0:
			if skew := relSkewPerc(bp.DurationNanos, ap.DurationNanos); skew > cpuWindowSkewPerc {
				pd.DurationMismatch = true
				warns = append(warns, fmt.Sprintf(
					"cpu sample windows differ (%s vs %s): absolute CPU values are not comparable",
					profanalyze.HumanizeValue(bp.DurationNanos, "nanoseconds"),
					profanalyze.HumanizeValue(ap.DurationNanos, "nanoseconds"),
				))
			}
		case bp.DurationNanos > 0 || ap.DurationNanos > 0:
			warns = append(warns, "cpu profile records no sample window on one side; absolute comparability unknown")
		}
	}

	// uptimeNote enriches alloc caveats: Go heap profiles record
	// process uptime in DurationNanos, which is exactly the
	// confounder behind cumulative alloc_* movements.
	var uptimeNote string
	if bp.Type == profile.TypeHeap && bp.DurationNanos > 0 && ap.DurationNanos > 0 {
		uptimeNote = fmt.Sprintf(
			" Process uptime at capture: ~%s → ~%s.",
			profanalyze.HumanizeValue(bp.DurationNanos, "nanoseconds"),
			profanalyze.HumanizeValue(ap.DurationNanos, "nanoseconds"),
		)
	}

	afterByID := make(map[string]profanalyze.Finding, len(ap.Findings))
	for _, f := range ap.Findings {
		if _, dup := afterByID[f.ID]; dup {
			warns = append(warns, fmt.Sprintf("duplicate finding %s in after %s profile; keeping the first", f.ID, ap.Type))
			continue
		}
		afterByID[f.ID] = f
	}
	seen := make(map[string]bool, len(bp.Findings))
	for i := range bp.Findings {
		b := bp.Findings[i]
		if seen[b.ID] {
			warns = append(warns, fmt.Sprintf("duplicate finding %s in before %s profile; keeping the first", b.ID, bp.Type))
			continue
		}
		seen[b.ID] = true
		if a, ok := afterByID[b.ID]; ok {
			pd.Findings = append(pd.Findings, diffFindingPair(b, a, uptimeNote))
			continue
		}
		fd := baseFindingDiff(b)
		fd.Label = LabelFixed
		fd.BeforeShare = b.SharePerc
		fd.BeforeValue = b.MatchedValue
		fd.ShareDeltaPts = -b.SharePerc
		pd.Findings = append(pd.Findings, fd)
	}
	afterSeen := make(map[string]bool, len(ap.Findings))
	for _, a := range ap.Findings {
		if afterSeen[a.ID] {
			continue // duplicate; warned when the index was built
		}
		afterSeen[a.ID] = true
		if seen[a.ID] {
			continue // already paired with a before finding
		}
		fd := baseFindingDiff(a)
		fd.Label = LabelNew
		fd.AfterShare = a.SharePerc
		fd.AfterValue = a.MatchedValue
		fd.ShareDeltaPts = a.SharePerc
		if isAllocSampleType(a.SampleType) {
			fd.Note = allocCaveat + uptimeNote
		}
		pd.Findings = append(pd.Findings, fd)
	}

	if bp.Summary && ap.Summary {
		pd.TopDeltas = diffTopEntries(bp.Top, ap.Top)
	}
	return pd, warns
}

// diffFindingPair labels a finding present on both sides.
func diffFindingPair(b, a profanalyze.Finding, uptimeNote string) FindingDiff {
	fd := baseFindingDiff(a)
	fd.BeforeShare = b.SharePerc
	fd.AfterShare = a.SharePerc
	fd.ShareDeltaPts = roundHundredth(a.SharePerc - b.SharePerc)
	fd.BeforeValue = b.MatchedValue
	fd.AfterValue = a.MatchedValue

	valueRel, valueKnown := relDeltaPerc(b.MatchedValue, a.MatchedValue)
	fd.ValueDeltaPerc = roundHundredth(valueRel)

	unitsMatch := b.Unit == a.Unit && b.MatchedValue > 0
	switch {
	case unitsMatch && valueKnown:
		switch {
		case fd.ShareDeltaPts <= -diffSharePts && valueRel <= -diffValueRelPerc:
			fd.Label = LabelImproved
		case fd.ShareDeltaPts >= diffSharePts && valueRel >= diffValueRelPerc:
			fd.Label = LabelWorse
		default:
			fd.Label = LabelUnchanged
		}
	default:
		// Defensive share-only fallback: zero/unknown absolute value
		// or mismatched units leave only the share to judge by.
		switch {
		case fd.ShareDeltaPts <= -diffSharePts:
			fd.Label = LabelImproved
		case fd.ShareDeltaPts >= diffSharePts:
			fd.Label = LabelWorse
		default:
			fd.Label = LabelUnchanged
		}
	}

	if fd.Label == LabelWorse && isAllocSampleType(a.SampleType) {
		fd.Label = LabelInconclusive
		fd.Note = allocCaveat + uptimeNote
	}
	return fd
}

func baseFindingDiff(f profanalyze.Finding) FindingDiff {
	return FindingDiff{
		ID:         f.ID,
		Detector:   f.Detector,
		Title:      f.Title,
		SampleType: f.SampleType,
		Unit:       f.Unit,
	}
}

func isAllocSampleType(st string) bool {
	return st == string(profanalyze.SampleAllocSpace) || st == string(profanalyze.SampleAllocObjects)
}

// diffTotals joins two totals lists by sample type, before's column
// order first, after-only columns appended.
func diffTotals(before, after []profanalyze.SampleTotal) []TotalDiff {
	afterByType := make(map[string]profanalyze.SampleTotal, len(after))
	for _, t := range after {
		afterByType[t.SampleType] = t
	}
	out := make([]TotalDiff, 0, len(before))
	seen := make(map[string]bool, len(before))
	for _, b := range before {
		seen[b.SampleType] = true
		td := TotalDiff{SampleType: b.SampleType, Unit: b.Unit, Before: b.Total}
		if a, ok := afterByType[b.SampleType]; ok {
			td.After = a.Total
			if rel, known := relDeltaPerc(b.Total, a.Total); known {
				td.DeltaPerc = roundHundredth(rel)
			}
		}
		out = append(out, td)
	}
	for _, a := range after {
		if !seen[a.SampleType] {
			out = append(out, TotalDiff{SampleType: a.SampleType, Unit: a.Unit, After: a.Total})
		}
	}
	return out
}

// diffTopEntries joins two top-frame tables by function on their
// flat values, keeping the five largest movements by either side.
func diffTopEntries(before, after *profanalyze.TopReport) []TopDelta {
	if before == nil || after == nil {
		return nil
	}
	byFn := make(map[string]*TopDelta)
	for _, e := range before.Entries {
		byFn[e.Function] = &TopDelta{Function: e.Function, Before: e.Flat}
	}
	for _, e := range after.Entries {
		if d, ok := byFn[e.Function]; ok {
			d.After = e.Flat
			continue
		}
		byFn[e.Function] = &TopDelta{Function: e.Function, After: e.Flat}
	}
	out := make([]TopDelta, 0, len(byFn))
	for _, d := range byFn {
		out = append(out, *d)
	}
	sort.Slice(out, func(i, j int) bool {
		if mi, mj := max(out[i].Before, out[i].After), max(out[j].Before, out[j].After); mi != mj {
			return mi > mj
		}
		return out[i].Function < out[j].Function
	})
	const maxTopDeltas = 5
	if len(out) > maxTopDeltas {
		out = out[:maxTopDeltas]
	}
	return out
}

// relDeltaPerc is the relative change from before to after in
// percent; known is false when before is zero (undefined ratio).
func relDeltaPerc(before, after int64) (perc float64, known bool) {
	if before == 0 {
		return 0, false
	}
	return 100 * float64(after-before) / float64(before), true
}

// relSkewPerc measures how far apart two positive values are,
// relative to the larger one.
func relSkewPerc(a, b int64) float64 {
	diff := a - b
	if diff < 0 {
		diff = -diff
	}
	return 100 * float64(diff) / float64(max(a, b))
}

// roundHundredth keeps deltas at the same two-decimal precision the
// findings themselves use.
func roundHundredth(v float64) float64 {
	return math.Round(v*100) / 100
}
