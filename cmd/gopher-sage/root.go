package main

import (
	"encoding/json"
	"io"
	"log/slog"
	"os"

	"github.com/spf13/cobra"

	"github.com/faizanf47/gopher-sage/internal/analyze"
	"github.com/faizanf47/gopher-sage/internal/profanalyze"
	"github.com/faizanf47/gopher-sage/internal/profile"
)

// newRootCmd builds the gopher-sage command tree.
func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "gopher-sage",
		Short: "Deterministic pprof analysis with evidence-backed findings",
		Long: "gopher-sage captures Go pprof profiles — live from a server's\n" +
			"net/http/pprof endpoint or from saved files — runs a fixed set of\n" +
			"pattern detectors, and reports findings whose call sites name the\n" +
			"functions to fix. No LLM, no network beyond the pprof fetch; the\n" +
			"same profile always produces the same report.",
		Version:       versionString(),
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(newAnalyzeCmd(), newDetectorsCmd(), newAgentCmd())
	return root
}

// newAnalyzeCmd is the human-facing analysis entry point, covering
// both live capture (--server) and saved files (--file).
func newAnalyzeCmd() *cobra.Command {
	var (
		server   string
		files    string
		types    string
		seconds  int
		minShare float64
		topN     int
		jsonOut  bool
		verbose  bool
	)
	cmd := &cobra.Command{
		Use:   "analyze",
		Short: "Analyze a live server's profiles or saved profile files",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			logger := newLogger(verbose)

			var rep analyze.Report
			var err error
			if files != "" {
				rep, err = analyze.RunFiles(analyze.FileOptions{
					Paths:    splitList(files),
					MinShare: minShare,
					TopN:     topN,
				})
			} else {
				var profTypes []profile.Type
				profTypes, err = parseTypes(types, []profile.Type{profile.TypeCPU, profile.TypeHeap})
				if err != nil {
					return err
				}
				rep, err = analyze.Run(cmd.Context(), profile.NewHTTPFetcher(), analyze.Options{
					Server:   server,
					Types:    profTypes,
					Seconds:  seconds,
					MinShare: minShare,
					TopN:     topN,
					Logger:   logger,
				})
			}
			if err != nil {
				return err
			}
			return writeReport(cmd.OutOrStdout(), rep, jsonOut)
		},
	}
	cmd.Flags().StringVar(&server, "server", "", "base URL of the target server's pprof endpoint, e.g. http://localhost:6060")
	cmd.Flags().StringVar(&files, "file", "", "comma-separated saved pprof files to analyze instead of a live server (profile kinds are inferred)")
	cmd.Flags().StringVar(&types, "type", "cpu,heap", "comma-separated profile types to capture from --server: cpu, heap")
	cmd.Flags().IntVar(&seconds, "seconds", 30, "CPU sample window in seconds (0 uses the server default)")
	cmd.Flags().Float64Var(&minShare, "min-share", 0, "drop findings below this share-of-profile percent (0 keeps the detector default noise floor)")
	cmd.Flags().IntVar(&topN, "top", 0, "include the top-N functions by cumulative value alongside each profile's findings (0 disables)")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "emit the report as JSON instead of text")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "verbose logging")
	cmd.MarkFlagsOneRequired("server", "file")
	cmd.MarkFlagsMutuallyExclusive("server", "file")
	return cmd
}

// newDetectorsCmd prints the transparency catalog: every registered
// detector's static ID, what it checks, how it works, limitations.
func newDetectorsCmd() *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "detectors",
		Short: "List the registered detectors and their transparency contracts",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if jsonOut {
				return analyze.WriteCatalogJSON(cmd.OutOrStdout(), profanalyze.Catalog())
			}
			return analyze.WriteCatalog(cmd.OutOrStdout(), profanalyze.Catalog())
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "emit the catalog as JSON instead of text")
	return cmd
}

// writeReport renders a report as text or indented JSON.
func writeReport(w io.Writer, rep analyze.Report, jsonOut bool) error {
	if jsonOut {
		return writeJSON(w, rep)
	}
	return analyze.WriteText(w, rep)
}

// writeJSON writes v as indented JSON, the shared shape for every
// --json flag.
func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// newLogger builds the process logger (stderr, so stdout stays clean
// for report output) and installs it as the slog default.
func newLogger(verbose bool) *slog.Logger {
	level := slog.LevelInfo
	if verbose {
		level = slog.LevelDebug
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
	slog.SetDefault(logger)
	return logger
}
