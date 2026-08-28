package profanalyze

import "fmt"

// categorySpec declares a share-of-profile category detector: the
// view it reads, the frame prefixes that define the category, and
// the text it reports. The shared engine (catDetector) owns
// matching, thresholding, severity grading, and evidence formatting,
// so a new category detector is a declaration, not an
// implementation.
type categorySpec struct {
	meta Metadata
	// view selects the profile view the category is measured on.
	view func(DetectCtx) View
	// prefixes are the frame-name prefixes that define the category.
	prefixes []string
	// exclude lists exact frame names ignored even when a prefix
	// matches (e.g. a non-allocating runtime variant that would
	// inflate the category).
	exclude []string
	title   string
	// subject and object compose the evidence sentence:
	// "<subject> account for N% of <object>".
	subject string
	object  string
	// recommend is the canonical remediation for the category.
	recommend string
	// upgradeRec, when set, may replace the recommendation based on
	// the matched frames (e.g. per-call regexp compilation observed).
	upgradeRec func(matched []string) (string, bool)
	confidence Confidence
}

// newCategoryDetector wraps a spec in the shared category engine.
func newCategoryDetector(spec categorySpec) Detector { return catDetector{spec} }

type catDetector struct{ spec categorySpec }

// categoryAttributionNote documents the engine-owned call-site
// attribution in every category detector's published Method, so the
// catalog can never drift from what the engine actually does.
const categoryAttributionNote = "Each matched sample is additionally " +
	"attributed to its nearest non-stdlib caller above the deepest " +
	"matched frame; the top call sites are reported with their share " +
	"of the profile."

func (d catDetector) Meta() Metadata {
	m := d.spec.meta
	m.Method += " " + categoryAttributionNote
	return m
}

func (d catDetector) Detect(ctx DetectCtx) []Finding {
	s := d.spec
	v := s.view(ctx)
	m := matchCategory(v, s.prefixes, s.exclude)
	share := percentOf(m.value, v.Total)
	if share < shareThreshold {
		return nil
	}
	rec := s.recommend
	if s.upgradeRec != nil {
		if upgraded, ok := s.upgradeRec(m.names); ok {
			rec = upgraded
		}
	}
	evidence := fmt.Sprintf(
		"%s account for %.2f%% of %s (%s of %s).",
		s.subject, share, s.object,
		humanizeValue(m.value, v.Unit), humanizeValue(v.Total, v.Unit),
	)
	f := makeFinding(
		d.Meta(), v, s.title, evidence, rec,
		m.names, m.value, share, gradeShare(share), s.confidence,
	)
	f.CallSites = m.callSites
	return []Finding{f}
}

// topFlatSpec declares a ranking detector that reports the top-n
// functions by flat value on a view — orientation findings like
// "these functions hold the most live heap".
type topFlatSpec struct {
	meta       Metadata
	view       func(DetectCtx) View
	n          int
	title      string
	subject    string
	object     string
	recommend  string
	confidence Confidence
}

// newTopFlatDetector wraps a spec in the shared top-flat engine.
func newTopFlatDetector(spec topFlatSpec) Detector { return topFlatDetector{spec} }

type topFlatDetector struct{ spec topFlatSpec }

func (d topFlatDetector) Meta() Metadata { return d.spec.meta }

func (d topFlatDetector) Detect(ctx DetectCtx) []Finding {
	s := d.spec
	v := s.view(ctx)
	top := topFlatFrames(v, s.n, shareThreshold)
	if len(top) == 0 {
		return nil
	}
	var names []string
	var matched int64
	var share float64
	for _, t := range top {
		names = append(names, t.name)
		matched += t.flat
		share += percentOf(t.flat, v.Total)
	}
	evidence := fmt.Sprintf(
		"%s account for %.2f%% of %s (%s of %s).",
		s.subject, share, s.object,
		humanizeValue(matched, v.Unit), humanizeValue(v.Total, v.Unit),
	)
	return []Finding{makeFinding(
		s.meta, v, s.title, evidence, s.recommend,
		names, matched, share, gradeShare(share), s.confidence,
	)}
}

// View selectors shared by the detector specs.
func cpuView(c DetectCtx) View        { return c.CPU }
func allocSpaceView(c DetectCtx) View { return c.AllocSpace }
func inuseSpaceView(c DetectCtx) View { return c.InuseSpace }
