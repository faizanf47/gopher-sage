package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/spf13/cobra"

	"github.com/faizanf47/gopher-sage/internal/analyze"
	"github.com/faizanf47/gopher-sage/internal/profile"
)

// agentWorkflow is the optimize loop, spelled out in the `agent`
// command's help so a coding agent without the shipped skill can
// still discover the intended usage from `gopher-sage agent --help`.
const agentWorkflow = `Subcommands for coding agents: deterministic, non-interactive, no ANSI.

The optimize loop:

  1. capture profiles from the running service under representative load
       gopher-sage agent capture --server http://localhost:6060 -o .gopher-sage/before
  2. report bottlenecks; the "call sites:" lines name YOUR functions to edit
       gopher-sage agent report --dir .gopher-sage/before
  3. apply each finding's suggestion to its call sites, rebuild, RESTART the
     service (heap alloc counters are cumulative per process), same load again
  4. re-capture and compare the two reports yourself
       gopher-sage agent capture --server http://localhost:6060 -o .gopher-sage/after
       gopher-sage agent report --dir .gopher-sage/after --json

Comparison notes: findings join across runs by their stable detector ID.
Shares are relative — fixing the top hotspot raises every other share, so
judge regressions by share AND absolute matched_value together, under
comparable load. Run "gopher-sage detectors" for the catalog behind the IDs.`

// newAgentCmd groups the agent-facing subcommands.
func newAgentCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agent",
		Short: "Subcommands for coding agents (capture, report)",
		Long:  agentWorkflow,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(newAgentCaptureCmd(), newAgentReportCmd(), newAgentDiffCmd())
	return cmd
}

// diffTopN guarantees summary profiles carry a top table for the
// diff's frame deltas; cpu/heap findings ignore it.
const diffTopN = 10

// newAgentDiffCmd mechanically compares two capture directories —
// the terminal step of every optimize loop. It labels movements and
// warns about confounders; it renders no verdict and gates nothing.
func newAgentDiffCmd() *cobra.Command {
	var (
		beforeDir string
		afterDir  string
		jsonOut   bool
		verbose   bool
	)
	cmd := &cobra.Command{
		Use:   "diff",
		Short: "Compare two capture directories: per-finding deltas by detector ID",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			logger := newLogger(verbose)
			run := func(dir string) (analyze.Report, error) {
				paths, err := resolveProfileArgs(dir, "")
				if err != nil {
					return analyze.Report{}, err
				}
				return analyze.RunFiles(analyze.FileOptions{
					Paths:   paths,
					TopN:    diffTopN,
					Lenient: true,
					Logger:  logger,
				})
			}
			before, err := run(beforeDir)
			if err != nil {
				return fmt.Errorf("before side: %w", err)
			}
			after, err := run(afterDir)
			if err != nil {
				return fmt.Errorf("after side: %w", err)
			}
			d, err := analyze.Diff(before, after, beforeDir, afterDir)
			if err != nil {
				return err
			}
			if jsonOut {
				return writeJSON(cmd.OutOrStdout(), d)
			}
			return analyze.WriteDiffText(cmd.OutOrStdout(), d)
		},
	}
	cmd.Flags().StringVar(&beforeDir, "before", "", "directory of the baseline capture (required)")
	cmd.Flags().StringVar(&afterDir, "after", "", "directory of the capture taken after the code change (required)")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "emit the diff as JSON instead of text")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "verbose logging")
	_ = cmd.MarkFlagRequired("before")
	_ = cmd.MarkFlagRequired("after")
	return cmd
}

// newAgentCaptureCmd fetches profiles and persists them for later
// report runs — the capture half of the before/after loop.
func newAgentCaptureCmd() *cobra.Command {
	var (
		server  string
		outDir  string
		types   string
		seconds int
		jsonOut bool
		verbose bool
	)
	cmd := &cobra.Command{
		Use:   "capture",
		Short: "Capture profiles from a running server into a directory",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			logger := newLogger(verbose)
			profTypes, err := parseTypes(types, profile.AllTypes())
			if err != nil {
				return err
			}
			files, err := analyze.Capture(cmd.Context(), profile.NewHTTPFetcher(), analyze.CaptureOptions{
				Server:  server,
				Types:   profTypes,
				Seconds: seconds,
				OutDir:  outDir,
				Logger:  logger,
			})
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			if jsonOut {
				return writeJSON(out, struct {
					Server string                 `json:"server"`
					Files  []analyze.CapturedFile `json:"files"`
				}{Server: server, Files: files})
			}
			for _, f := range files {
				if _, err := fmt.Fprintf(out, "wrote %s\n", f.Path); err != nil {
					return err
				}
			}
			_, err = fmt.Fprintf(out, "next: gopher-sage agent report --dir %s\n", outDir)
			return err
		},
	}
	cmd.Flags().StringVar(&server, "server", "", "base URL of the target server's pprof endpoint (required), e.g. http://localhost:6060")
	cmd.Flags().StringVarP(&outDir, "out", "o", "", "directory for the captured <type>.pb.gz files (required), e.g. .gopher-sage/before")
	cmd.Flags().StringVar(&types, "types", "cpu,heap,goroutine",
		"comma-separated profile types to capture: cpu, heap, allocs, goroutine, block, mutex, threadcreate "+
			"(block and mutex are empty unless the target sets runtime.SetBlockProfileRate / SetMutexProfileFraction)")
	cmd.Flags().IntVar(&seconds, "seconds", 30, "CPU sample window in seconds (0 uses the server default)")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "emit the written files as JSON instead of text")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "verbose logging")
	_ = cmd.MarkFlagRequired("server")
	_ = cmd.MarkFlagRequired("out")
	return cmd
}

// newAgentReportCmd analyzes previously captured profiles — a thin,
// namespaced wrapper over the same pipeline `analyze --file` uses.
func newAgentReportCmd() *cobra.Command {
	var (
		dir      string
		files    string
		minShare float64
		topN     int
		jsonOut  bool
		verbose  bool
	)
	cmd := &cobra.Command{
		Use:   "report",
		Short: "Report bottlenecks in captured profiles, with call sites to edit",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			logger := newLogger(verbose)
			paths, err := resolveProfileArgs(dir, files)
			if err != nil {
				return err
			}
			rep, err := analyze.RunFiles(analyze.FileOptions{
				Paths:    paths,
				MinShare: minShare,
				TopN:     topN,
				Lenient:  true,
				Logger:   logger,
			})
			if err != nil {
				return err
			}
			return writeReport(cmd.OutOrStdout(), rep, jsonOut)
		},
	}
	cmd.Flags().StringVar(&dir, "dir", "", "directory of captured .pb.gz profiles (as written by agent capture)")
	cmd.Flags().StringVar(&files, "file", "", "comma-separated pprof files to analyze")
	cmd.Flags().Float64Var(&minShare, "min-share", 0, "drop findings below this share-of-profile percent")
	cmd.Flags().IntVar(&topN, "top", 0, "include the top-N functions by cumulative value (0 disables)")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "emit the report as JSON instead of text")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "verbose logging")
	cmd.MarkFlagsOneRequired("dir", "file")
	cmd.MarkFlagsMutuallyExclusive("dir", "file")
	return cmd
}

// resolveProfileArgs expands the --dir/--file pair into profile
// paths: a directory contributes its *.pb.gz entries in lexical
// order; a file list is split on commas.
func resolveProfileArgs(dir, files string) ([]string, error) {
	if files != "" {
		return splitList(files), nil
	}
	matches, err := filepath.Glob(filepath.Join(dir, "*.pb.gz"))
	if err != nil {
		return nil, fmt.Errorf("list %s: %w", dir, err)
	}
	if len(matches) == 0 {
		if _, statErr := os.Stat(dir); statErr != nil {
			return nil, fmt.Errorf("read profile directory: %w", statErr)
		}
		return nil, fmt.Errorf("no .pb.gz profiles in %s (capture some first: gopher-sage agent capture --server URL -o %s)", dir, dir)
	}
	sort.Strings(matches)
	return matches, nil
}
