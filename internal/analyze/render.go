package analyze

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/faizanf47/gopher-sage/internal/profanalyze"
)

// WriteText renders the report as human-readable text. The layout is
// deterministic: profiles appear in capture order and findings in
// the order Run produced (severity, then share).
func WriteText(w io.Writer, rep Report) error {
	var b strings.Builder
	fmt.Fprintf(&b, "gopher-sage report — %s\n", rep.Server)

	for _, pr := range rep.Profiles {
		fmt.Fprintf(
			&b, "\n%s profile (%d bytes; sample types: %s)\n",
			pr.Type, pr.Bytes, strings.Join(pr.SampleTypes, ", "),
		)
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
			if f.Recommendation != "" {
				fmt.Fprintf(&b, "      suggestion: %s\n", f.Recommendation)
			}
		}
	}

	_, err := io.WriteString(w, b.String())
	return err
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
