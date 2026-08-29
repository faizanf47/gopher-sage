package analyze

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/faizanf47/gopher-sage/internal/profanalyze"
	"github.com/faizanf47/gopher-sage/internal/profile"
)

// CaptureOptions configures a single Capture: fetch profiles from a
// live server and persist the raw bytes so they can be analyzed and
// compared later (before/after an optimization).
type CaptureOptions struct {
	// Server is the base URL of the target's pprof endpoint.
	Server string
	// Types to capture, in output order.
	Types []profile.Type
	// Seconds is the CPU sample window. Zero means "use the server
	// default" (30s in the standard runtime handler).
	Seconds int
	// OutDir receives one <type>.pb.gz file per captured profile.
	// It is created if missing; existing files are overwritten.
	OutDir string
	// Logger receives progress messages. Nil falls back to
	// slog.Default().
	Logger *slog.Logger
}

func (o CaptureOptions) validate() error {
	if o.Server == "" {
		return fmt.Errorf("capture: server URL is required")
	}
	if len(o.Types) == 0 {
		return fmt.Errorf("capture: at least one profile type is required")
	}
	for _, t := range o.Types {
		switch t {
		case profile.TypeCPU, profile.TypeHeap:
		default:
			return fmt.Errorf(
				"capture: unsupported profile type %q (the detector set covers %q and %q)",
				t, profile.TypeCPU, profile.TypeHeap,
			)
		}
	}
	if o.Seconds < 0 {
		return fmt.Errorf("capture: seconds must be non-negative, got %d", o.Seconds)
	}
	if o.OutDir == "" {
		return fmt.Errorf("capture: output directory is required")
	}
	return nil
}

// CapturedFile records one profile Capture wrote.
type CapturedFile struct {
	Type  profile.Type `json:"type"`
	Path  string       `json:"path"`
	Bytes int          `json:"bytes"`
}

// Capture fetches each requested profile from the server and writes
// it to OutDir/<type>.pb.gz, returning the files in Types order.
// Every response is parse-validated before it is written, so a
// misconfigured endpoint (an HTML 200, say) fails loudly instead of
// leaving a poisoned file for a later report run. Files left behind
// by a previous capture with a wider type set are not removed.
func Capture(ctx context.Context, fetcher profile.Fetcher, opts CaptureOptions) ([]CapturedFile, error) {
	if fetcher == nil {
		return nil, fmt.Errorf("capture: fetcher is required")
	}
	if err := opts.validate(); err != nil {
		return nil, err
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	if err := os.MkdirAll(opts.OutDir, 0o755); err != nil {
		return nil, fmt.Errorf("capture: create output directory: %w", err)
	}

	files := make([]CapturedFile, 0, len(opts.Types))
	for _, t := range opts.Types {
		f, err := captureOne(ctx, fetcher, opts, t, logger)
		if err != nil {
			return nil, fmt.Errorf("capture %s profile: %w", t, err)
		}
		files = append(files, f)
	}
	return files, nil
}

// captureOne fetches, validates, and persists a single profile type.
func captureOne(
	ctx context.Context,
	fetcher profile.Fetcher,
	opts CaptureOptions,
	t profile.Type,
	logger *slog.Logger,
) (CapturedFile, error) {
	src := profile.Source{BaseURL: opts.Server, Type: t}
	if t == profile.TypeCPU {
		src.Seconds = opts.Seconds
	}
	if err := src.Validate(); err != nil {
		return CapturedFile{}, err
	}

	logArgs := []any{"type", t, "server", opts.Server, "out", opts.OutDir}
	if t == profile.TypeCPU {
		logArgs = append(logArgs, "seconds", effectiveSeconds(opts.Seconds))
	}
	logger.Info("capturing profile", logArgs...)

	fetchCtx, cancel := context.WithTimeout(ctx, fetchTimeout(src))
	defer cancel()
	raw, err := fetcher.Fetch(fetchCtx, src)
	if err != nil {
		return CapturedFile{}, err
	}

	u, err := src.URL()
	if err != nil {
		return CapturedFile{}, err
	}
	if _, err := profanalyze.ParseBytes(u, raw); err != nil {
		return CapturedFile{}, fmt.Errorf(
			"response is not a pprof profile (is net/http/pprof mounted at %s?): %w", u, err,
		)
	}

	path := filepath.Join(opts.OutDir, string(t)+".pb.gz")
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		return CapturedFile{}, fmt.Errorf("write profile: %w", err)
	}
	return CapturedFile{Type: t, Path: path, Bytes: len(raw)}, nil
}
