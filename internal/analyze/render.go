package analyze

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/faizanf47/gopher-sage/internal/profanalyze"
	"github.com/faizanf47/gopher-sage/internal/profile"
)

// WriteText renders the report as human-readable text. The layout is
// deterministic: profiles appear in capture order and findings in
// the order Run produced (severity, then share).
func WriteText(w io.Writer, rep Report) error {
	var b strings.Builder
	if rep.Server != "" {
		fmt.Fprintf(&b, "gopher-sage report — %s\n", rep.Server)
	} else {
		b.WriteString("gopher-sage report — saved profiles\n")
	}

	for _, pr := range rep.Profiles {
		label := string(pr.Type)
		if pr.Source != "" {
			label = fmt.Sprintf("%s [%s]", pr.Type, pr.Source)
		}
		fmt.Fprintf(
			&b, "\n%s profile (%d bytes; sample types: %s)\n",
			label, pr.Bytes, strings.Join(pr.SampleTypes, ", "),
		)
		writeTotalsLine(&b, pr)
		writeTopTable(&b, pr.Top)
		if pr.Summary {
			b.WriteString("  no detectors cover this profile type; totals and top frames shown\n")
			continue
		}
		if len(pr.Findings) == 0 {
			b.WriteString("  no findings above the share threshold\n")
			continue
		}
		for i, f := range pr.Findings {
			fmt.Fprintf(
				&b, "\n  [%d] (%s severity, %s confidence) %s\n",
				i+1, f.Severity, f.Confidence, f.Title,
			)
			fmt.Fprintf(
				&b, "      detector: %s [%s] — %.2f%% of %s\n",
				f.Detector, f.ID, f.SharePerc, f.SampleType,
			)
			fmt.Fprintf(&b, "      evidence: %s\n", f.Evidence)
			if len(f.Functions) > 0 {
				fmt.Fprintf(
					&b, "      functions: %s\n",
					strings.Join(f.Functions, ", "),
				)
			}
			if len(f.CallSites) > 0 {
				sites := make([]string, 0, len(f.CallSites))
				for _, cs := range f.CallSites {
					sites = append(sites, fmt.Sprintf("%s (%.2f%%)", cs.Function, cs.SharePerc))
				}
				fmt.Fprintf(&b, "      call sites: %s\n", strings.Join(sites, ", "))
			}
			if f.Recommendation != "" {
				fmt.Fprintf(&b, "      suggestion: %s\n", f.Recommendation)
			}
		}
	}

	_, err := io.WriteString(w, b.String())
	return err
}

// writeTotalsLine renders the profile-wide sample-column totals —
// the denominators behind every finding's share. The capture window
// is appended for CPU profiles only: heap profiles record process
// uptime in the same field, which is not a window.
func writeTotalsLine(b *strings.Builder, pr ProfileReport) {
	if len(pr.Totals) == 0 {
		return
	}
	parts := make([]string, 0, len(pr.Totals))
	for _, t := range pr.Totals {
		parts = append(parts, fmt.Sprintf("%s %s", t.SampleType, profanalyze.HumanizeValue(t.Total, t.Unit)))
	}
	fmt.Fprintf(b, "  totals: %s", strings.Join(parts, ", "))
	if pr.Type == profile.TypeCPU && pr.DurationNanos > 0 {
		fmt.Fprintf(b, " (window %s)", profanalyze.HumanizeValue(pr.DurationNanos, "nanoseconds"))
	}
	b.WriteString("\n")
}

// writeTopTable renders the optional top-N function table under a
// profile header. A nil report (TopN was zero) writes nothing.
func writeTopTable(b *strings.Builder, top *profanalyze.TopReport) {
	if top == nil || len(top.Entries) == 0 {
		return
	}
	fmt.Fprintf(
		b, "\n  top %d functions (%s, by cum)\n",
		len(top.Entries), top.SampleType,
	)
	fmt.Fprintf(b, "    %7s %7s  %s\n", "flat%", "cum%", "function")
	for _, e := range top.Entries {
		fmt.Fprintf(
			b, "    %6.2f%% %6.2f%%  %s\n",
			e.FlatPerc, e.CumPerc, e.Function,
		)
	}
}

// WriteCatalog renders the detector catalog as human-readable text:
// every registered detector's static ID, what it checks, how it
// works, and its limitations.
func WriteCatalog(w io.Writer, cat []profanalyze.Metadata) error {
	var b strings.Builder
	fmt.Fprintf(&b, "registered detectors (%d)\n", len(cat))
	for _, m := range cat {
		fmt.Fprintf(&b, "\n%s  %s (%s)\n", m.ID(), m.Name, m.Scope)
		fmt.Fprintf(&b, "  checks:      %s\n", m.Checks)
		fmt.Fprintf(&b, "  method:      %s\n", m.Method)
		fmt.Fprintf(&b, "  limitations: %s\n", m.Limitations)
	}
	_, err := io.WriteString(w, b.String())
	return err
}

// WriteCatalogJSON renders the detector catalog as JSON, with each
// entry carrying its static ID alongside the published metadata.
func WriteCatalogJSON(w io.Writer, cat []profanalyze.Metadata) error {
	type entry struct {
		ID string `json:"id"`
		profanalyze.Metadata
	}
	entries := make([]entry, 0, len(cat))
	for _, m := range cat {
		entries = append(entries, entry{ID: m.ID(), Metadata: m})
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(entries)
}
