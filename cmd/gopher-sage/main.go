package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"runtime/debug"
	"strings"
	"syscall"

	"github.com/faizanf47/gopher-sage/internal/profile"
)

func main() {
	os.Exit(run())
}

// run executes the command tree under a signal-aware context and
// returns the process exit code, so main's os.Exit cannot skip the
// deferred signal cleanup.
func run() int {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := newRootCmd().ExecuteContext(ctx); err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintf(os.Stderr, "gopher-sage: %v\n", err)
		return 1
	}
	return 0
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

// parseTypes turns a profile-types flag value into the types to
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
		return nil, errors.New("profile types must name at least one of: cpu, heap")
	}
	return out, nil
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
