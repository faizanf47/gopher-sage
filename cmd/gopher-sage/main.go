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
		server        = flag.String("server", "", "base URL of the target server's pprof endpoint, e.g. http://localhost:6060 (required)")
		types         = flag.String("type", "cpu,heap", "comma-separated profile types to analyze: cpu, heap")
		seconds       = flag.Int("seconds", 30, "CPU sample window in seconds (0 uses the server default)")
		minShare      = flag.Float64("min-share", 0, "drop findings below this share-of-profile percent (0 keeps the detector default noise floor)")
		jsonOut       = flag.Bool("json", false, "emit the report as JSON instead of text")
		listDetectors = flag.Bool("detectors", false, "list the registered detectors (ID, what each checks, how it works, limitations) and exit")
		verbose       = flag.Bool("v", false, "verbose logging")
	)
	flag.Parse()

	logLevel := slog.LevelInfo
	if *verbose {
		logLevel = slog.LevelDebug
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: logLevel}))
	slog.SetDefault(logger)

	if *listDetectors {
		if *jsonOut {
			return analyze.WriteCatalogJSON(os.Stdout, profanalyze.Catalog())
		}
		return analyze.WriteCatalog(os.Stdout, profanalyze.Catalog())
	}

	if *server == "" {
		flag.Usage()
		return errors.New("-server is required")
	}
	profTypes, err := parseTypes(*types)
	if err != nil {
		return err
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	rep, err := analyze.Run(ctx, profile.NewHTTPFetcher(), analyze.Options{
		Server:   *server,
		Types:    profTypes,
		Seconds:  *seconds,
		MinShare: *minShare,
		Logger:   logger,
	})
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

// parseTypes turns the -type flag value into the profile types to
// capture, preserving the order the user asked for.
func parseTypes(s string) ([]profile.Type, error) {
	var out []profile.Type
	for part := range strings.SplitSeq(s, ",") {
		part = strings.ToLower(strings.TrimSpace(part))
		if part == "" {
			continue
		}
		t := profile.Type(part)
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
