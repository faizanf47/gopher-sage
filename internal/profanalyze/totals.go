package profanalyze

// SampleTotal is the profile-wide sum of one sample column — the
// denominator behind every share-of-profile percentage reported for
// that sample type.
type SampleTotal struct {
	SampleType string `json:"sample_type"`
	Unit       string `json:"unit,omitempty"`
	Total      int64  `json:"total"`
}

// Totals sums every sample column of the profile in a single pass,
// returned in profile column order. It needs no sample-index
// resolution, so it works on any profile kind — CPU, heap,
// goroutine, contention — including ones the detector set does not
// cover.
func Totals(p *Profile) []SampleTotal {
	sums := make([]int64, len(p.Raw.SampleType))
	for _, s := range p.Raw.Sample {
		for i := 0; i < len(s.Value) && i < len(sums); i++ {
			sums[i] += s.Value[i]
		}
	}
	out := make([]SampleTotal, len(sums))
	for i, st := range p.Raw.SampleType {
		out[i] = SampleTotal{SampleType: st.Type, Unit: st.Unit, Total: sums[i]}
	}
	return out
}
