// Package analyze runs the full gopher-sage pipeline — fetch (or
// load), parse, detect, report — and renders the result as text or
// JSON. It composes internal/profile (transport) with
// internal/profanalyze (parsing + detectors) and owns the report
// shapes both output encodings share.
package analyze

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"time"

	"github.com/faizanf47/gopher-sage/internal/profanalyze"
	"github.com/faizanf47/gopher-sage/internal/profile"
)

// fetchSlack is added on top of a CPU profile's sample window to
// cover the time the runtime takes to dump the captured samples and
// flush the response after sampling stops.
const fetchSlack = 60 * time.Second

// defaultFetchTimeout bounds profile types that respond promptly
// (heap, allocs, ...) so a hung server cannot stall the run.
const defaultFetchTimeout = 30 * time.Second

// serverDefaultCPUSeconds is the sample window the Go runtime uses
// when the request does not carry an explicit `seconds` parameter.
const serverDefaultCPUSeconds = 30

// Options configures a single Run.
type Options struct {
	// Base URL of the running server's pprof endpoint,
	// e.g. "http://localhost:6060" or
	// "http://localhost:6060/debug/pprof/".
	Server string
	// Profiles to capture and analyze.
	Types []profile.Type
	// CPU sample window. Zero means "use the server
	// default" (30s in the standard runtime handler).
	Seconds int
	// Threshold to decide whether to keep the Report or not.
	MinShare float64
	// TopN, when positive, includes the top-N functions (ranked by
	// cumulative value) alongside each profile's findings.
	TopN int
	// Logger receives progress messages (a CPU capture blocks for
	// the whole sample window, so silence reads as a hang). Nil
	// falls back to slog.Default().
	Logger *slog.Logger
}

func (o Options) validate() error {
	if o.Server == "" {
		return fmt.Errorf("analyze: server URL is required")
	}
	if len(o.Types) == 0 {
		return fmt.Errorf("analyze: at least one profile type is required")
	}
	for _, t := range o.Types {
		switch t {
		case profile.TypeCPU, profile.TypeHeap:
		default:
			return fmt.Errorf(
				"analyze: unsupported profile type %q (the detector set covers %q and %q)",
				t, profile.TypeCPU, profile.TypeHeap,
			)
		}
	}
	if o.Seconds < 0 {
		return fmt.Errorf("analyze: seconds must be non-negative, got %d", o.Seconds)
	}
	if o.MinShare < 0 {
		return fmt.Errorf("analyze: min share must be non-negative, got %g", o.MinShare)
	}
	if o.TopN < 0 {
		return fmt.Errorf("analyze: top N must be non-negative, got %d", o.TopN)
	}
	return nil
}

// Report is the full result of one Run or RunFiles, shaped for
// direct JSON serialisation. Server is set for live captures and
// empty for file analysis (each ProfileReport then carries its
// Source path).
type Report struct {
	Server   string          `json:"server,omitempty"`
	Profiles []ProfileReport `json:"profiles"`
}

// ProfileReport carries the findings for one analyzed profile.
type ProfileReport struct {
	Type profile.Type `json:"type"`
	// Source is the file the profile was loaded from. Empty for
	// live captures (the Report.Server field identifies those).
	Source      string   `json:"source,omitempty"`
	Bytes       int      `json:"bytes"`
	SampleTypes []string `json:"sample_types"`
	// Top is the top-N functions by cumulative value, present only
	// when Options.TopN / FileOptions.TopN is positive.
	Top      *profanalyze.TopReport `json:"top,omitempty"`
	Findings []profanalyze.Finding  `json:"findings"`
}

// Run executes the default set of Detectors and organises the report.
func Run(ctx context.Context, fetcher profile.Fetcher, opts Options) (Report, error) {
	if fetcher == nil {
		return Report{}, fmt.Errorf("analyze: fetcher is required")
	}
	if err := opts.validate(); err != nil {
		return Report{}, err
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}

	rep := Report{Server: opts.Server}
	for _, t := range opts.Types {
		pr, err := analyzeOne(ctx, fetcher, opts, t, logger)
		if err != nil {
			return Report{}, fmt.Errorf("analyze %s profile: %w", t, err)
		}
		rep.Profiles = append(rep.Profiles, pr)
	}
	return rep, nil
}

// analyzeOne fetches and analyzes a single profile type.
func analyzeOne(
	ctx context.Context,
	fetcher profile.Fetcher,
	opts Options,
	t profile.Type,
	logger *slog.Logger,
) (ProfileReport, error) {
	src := profile.Source{BaseURL: opts.Server, Type: t}
	if t == profile.TypeCPU {
		src.Seconds = opts.Seconds
	}
	if err := src.Validate(); err != nil {
		return ProfileReport{}, err
	}

	logArgs := []any{"type", t, "server", opts.Server}
	if t == profile.TypeCPU {
		logArgs = append(logArgs, "seconds", effectiveSeconds(opts.Seconds))
	}
	logger.Info("capturing profile", logArgs...)

	fetchCtx, cancel := context.WithTimeout(ctx, fetchTimeout(src))
	defer cancel()
	raw, err := fetcher.Fetch(fetchCtx, src)
	if err != nil {
		return ProfileReport{}, err
	}

	u, err := src.URL()
	if err != nil {
		return ProfileReport{}, err
	}
	prof, err := profanalyze.ParseBytes(u, raw)
	if err != nil {
		return ProfileReport{}, err
	}

	pr, err := buildProfileReport(prof, len(raw), opts.MinShare, opts.TopN)
	if err != nil {
		return ProfileReport{}, err
	}
	pr.Type = t
	return pr, nil
}

// FileOptions configures a single RunFiles over saved profiles.
type FileOptions struct {
	// Paths of the pprof files to analyze, in report order.
	Paths []string
	// MinShare drops findings below this share-of-profile percent.
	MinShare float64
	// TopN, when positive, includes the top-N functions (ranked by
	// cumulative value) alongside each profile's findings.
	TopN int
}

func (o FileOptions) validate() error {
	if len(o.Paths) == 0 {
		return fmt.Errorf("analyze: at least one profile file is required")
	}
	if o.MinShare < 0 {
		return fmt.Errorf("analyze: min share must be non-negative, got %g", o.MinShare)
	}
	if o.TopN < 0 {
		return fmt.Errorf("analyze: top N must be non-negative, got %d", o.TopN)
	}
	return nil
}

// RunFiles analyzes saved pprof files from disk — the offline
// counterpart to Run for profiles captured earlier (production
// grabs, CI artifacts). The profile kind is inferred from the sample
// types each file carries, so no -type equivalent is needed.
func RunFiles(opts FileOptions) (Report, error) {
	if err := opts.validate(); err != nil {
		return Report{}, err
	}
	var rep Report
	for _, path := range opts.Paths {
		pr, err := analyzeFile(path, opts)
		if err != nil {
			return Report{}, fmt.Errorf("analyze %s: %w", path, err)
		}
		rep.Profiles = append(rep.Profiles, pr)
	}
	return rep, nil
}

// analyzeFile loads and analyzes a single saved profile.
func analyzeFile(path string, opts FileOptions) (ProfileReport, error) {
	prof, err := profanalyze.Load(path)
	if err != nil {
		return ProfileReport{}, err
	}
	t, err := profileKind(prof)
	if err != nil {
		return ProfileReport{}, err
	}
	fi, err := os.Stat(path)
	if err != nil {
		return ProfileReport{}, fmt.Errorf("stat profile: %w", err)
	}

	pr, err := buildProfileReport(prof, int(fi.Size()), opts.MinShare, opts.TopN)
	if err != nil {
		return ProfileReport{}, err
	}
	pr.Type = t
	pr.Source = path
	return pr, nil
}

// profileKind classifies a parsed profile by the sample types it
// carries so file analysis does not need the user to declare the
// kind up front.
func profileKind(prof *profanalyze.Profile) (profile.Type, error) {
	switch {
	case prof.HasCPUSamples():
		return profile.TypeCPU, nil
	case prof.HasHeapSamples():
		return profile.TypeHeap, nil
	}
	return "", fmt.Errorf(
		"profile carries no CPU or heap sample types (available: %v); the detector set covers %q and %q",
		prof.AvailableSampleTypes(), profile.TypeCPU, profile.TypeHeap,
	)
}

// buildProfileReport runs the detector set (and, when requested, the
// Top report) over an already-parsed profile. Shared by the live and
// file paths; the caller fills in Type and Source.
func buildProfileReport(prof *profanalyze.Profile, nbytes int, minShare float64, topN int) (ProfileReport, error) {
	findings, err := profanalyze.Run(prof, profanalyze.DefaultDetectors())
	if err != nil {
		return ProfileReport{}, err
	}
	findings = filterByShare(findings, minShare)
	sortFindings(findings)
	if findings == nil {
		// Keep JSON output as [] rather than null when nothing fired.
		findings = []profanalyze.Finding{}
	}

	pr := ProfileReport{
		Bytes:       nbytes,
		SampleTypes: prof.AvailableSampleTypes(),
		Findings:    findings,
	}
	if topN > 0 {
		top, err := profanalyze.Top(prof, profanalyze.TopOptions{Limit: topN})
		if err != nil {
			return ProfileReport{}, fmt.Errorf("top report: %w", err)
		}
		pr.Top = &top
	}
	return pr, nil
}

func fetchTimeout(src profile.Source) time.Duration {
	if src.Type != profile.TypeCPU {
		return defaultFetchTimeout
	}
	return time.Duration(effectiveSeconds(src.Seconds))*time.Second + fetchSlack
}

// effectiveSeconds resolves the "zero means server default" CPU
// sample window so deadlines and log lines reflect the real wait.
func effectiveSeconds(seconds int) int {
	if seconds <= 0 {
		return serverDefaultCPUSeconds
	}
	return seconds
}

// filterByShare drops findings below the min-share cut-off.
func filterByShare(in []profanalyze.Finding, minShare float64) []profanalyze.Finding {
	if minShare <= 0 {
		return in
	}
	out := in[:0]
	for _, f := range in {
		if f.SharePerc >= minShare {
			out = append(out, f)
		}
	}
	return out
}

// sortFindings orders findings by severity (high first), then share
// of profile (largest first), then detector name so equal findings
// have a stable order across runs.
func sortFindings(in []profanalyze.Finding) {
	sort.SliceStable(in, func(i, j int) bool {
		if a, b := severityRank(in[i].Severity), severityRank(in[j].Severity); a != b {
			return a > b
		}
		if in[i].SharePerc != in[j].SharePerc {
			return in[i].SharePerc > in[j].SharePerc
		}
		return in[i].Detector < in[j].Detector
	})
}

func severityRank(s profanalyze.Severity) int {
	switch s {
	case profanalyze.SeverityHigh:
		return 3
	case profanalyze.SeverityMedium:
		return 2
	case profanalyze.SeverityLow:
		return 1
	}
	return 0
}
