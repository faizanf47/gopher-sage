package analyze

import (
	"fmt"
	"io"
	"strings"
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
				&b, "      detector: %s — %.2f%% of %s\n",
				f.Detector, f.SharePerc, f.SampleType,
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
