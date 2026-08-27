// Command gopher-sage captures Go pprof profiles — live from a
// server's net/http/pprof endpoint or from saved files — and reports
// deterministic, evidence-backed performance findings.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"runtime/debug"
	"strings"
	"syscall"

	"github.com/faizanf47/gopher-sage/internal/analyze"
	"github.com/faizanf47/gopher-sage/internal/profanalyze"
	"github.com/faizanf47/gopher-sage/internal/profile"
)

func main() {
	if err := run(); err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintf(os.Stderr, "gopher-sage: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		server        = flag.String("server", "", "base URL of the target server's pprof endpoint, e.g. http://localhost:6060")
		files         = flag.String("file", "", "comma-separated saved pprof files to analyze instead of a live server (profile kinds are inferred)")
		types         = flag.String("type", "cpu,heap", "comma-separated profile types to capture from -server: cpu, heap")
		seconds       = flag.Int("seconds", 30, "CPU sample window in seconds (0 uses the server default)")
		minShare      = flag.Float64("min-share", 0, "drop findings below this share-of-profile percent (0 keeps the detector default noise floor)")
		topN          = flag.Int("top", 0, "include the top-N functions by cumulative value alongside each profile's findings (0 disables)")
		jsonOut       = flag.Bool("json", false, "emit the report as JSON instead of text")
		listDetectors = flag.Bool("detectors", false, "list the registered detectors (ID, what each checks, how it works, limitations) and exit")
		showVersion   = flag.Bool("version", false, "print the version and exit")
		verbose       = flag.Bool("v", false, "verbose logging")
	)
	flag.Parse()

	logLevel := slog.LevelInfo
	if *verbose {
		logLevel = slog.LevelDebug
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: logLevel}))
	slog.SetDefault(logger)

	if *showVersion {
		fmt.Println("gopher-sage", versionString())
		return nil
	}
	if *listDetectors {
		if *jsonOut {
			return analyze.WriteCatalogJSON(os.Stdout, profanalyze.Catalog())
		}
		return analyze.WriteCatalog(os.Stdout, profanalyze.Catalog())
	}

	rep, err := buildReport(*server, *files, *types, *seconds, *minShare, *topN, logger)
	if err != nil {
		return err
	}

	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(rep)
	}
	return analyze.WriteText(os.Stdout, rep)
}

// buildReport dispatches between the live-capture and saved-file
// analysis paths based on which of -server / -file was supplied.
func buildReport(
	server, files, types string,
	seconds int,
	minShare float64,
	topN int,
	logger *slog.Logger,
) (analyze.Report, error) {
	switch {
	case server != "" && files != "":
		return analyze.Report{}, errors.New("-server and -file are mutually exclusive")
	case server == "" && files == "":
		flag.Usage()
		return analyze.Report{}, errors.New("one of -server or -file is required")
	}

	if files != "" {
		return analyze.RunFiles(analyze.FileOptions{
			Paths:    splitList(files),
			MinShare: minShare,
			TopN:     topN,
		})
	}

	profTypes, err := parseTypes(types)
	if err != nil {
		return analyze.Report{}, err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	return analyze.Run(ctx, profile.NewHTTPFetcher(), analyze.Options{
		Server:   server,
		Types:    profTypes,
		Seconds:  seconds,
		MinShare: minShare,
		TopN:     topN,
		Logger:   logger,
	})
}

// versionString reports the module version plus, when the binary was
// built from a VCS checkout, the (possibly dirty) revision.
func versionString() string {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	version := bi.Main.Version
	var revision, dirty string
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			revision = s.Value
		case "vcs.modified":
			if s.Value == "true" {
				dirty = " (dirty)"
			}
		}
	}
	if len(revision) > 12 {
		revision = revision[:12]
	}
	if revision != "" {
		return fmt.Sprintf("%s %s%s", version, revision, dirty)
	}
	return version
}

// splitList splits a comma-separated flag value into its non-empty,
// trimmed elements.
func splitList(s string) []string {
	var out []string
	for part := range strings.SplitSeq(s, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

// parseTypes turns the -type flag value into the profile types to
// capture, preserving the order the user asked for.
func parseTypes(s string) ([]profile.Type, error) {
	var out []profile.Type
	for _, part := range splitList(s) {
		t := profile.Type(strings.ToLower(part))
		switch t {
		case profile.TypeCPU, profile.TypeHeap:
			out = append(out, t)
		default:
			return nil, fmt.Errorf("unsupported profile type %q (supported: cpu, heap)", part)
		}
	}
	if len(out) == 0 {
		return nil, errors.New("-type must name at least one of: cpu, heap")
	}
	return out, nil
}
