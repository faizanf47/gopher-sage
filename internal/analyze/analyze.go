package analyze

import (
	"context"
	"fmt"
	"log/slog"
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
	return nil
}

// Report is the full result of one Run, shaped for direct JSON
// serialisation.
type Report struct {
	Server   string          `json:"server"`
	Profiles []ProfileReport `json:"profiles"`
}

// ProfileReport carries the findings for one captured profile.
type ProfileReport struct {
	Type        profile.Type          `json:"type"`
	Bytes       int                   `json:"bytes"`
	SampleTypes []string              `json:"sample_types"`
	Findings    []profanalyze.Finding `json:"findings"`
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

	findings, err := profanalyze.Run(prof, profanalyze.DefaultDetectors())
	if err != nil {
		return ProfileReport{}, err
	}
	findings = filterByShare(findings, opts.MinShare)
	sortFindings(findings)
	if findings == nil {
		// Keep JSON output as [] rather than null when nothing fired.
		findings = []profanalyze.Finding{}
	}

	return ProfileReport{
		Type:        t,
		Bytes:       len(raw),
		SampleTypes: prof.AvailableSampleTypes(),
		Findings:    findings,
	}, nil
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
