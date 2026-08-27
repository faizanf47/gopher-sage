package profanalyze

import "fmt"

func init() { MustRegister(heapJSONAllocDetector{}) }

// heapJSONAllocDetector reports when encoding/json drives
// allocation volume.
type heapJSONAllocDetector struct{}

func (heapJSONAllocDetector) Meta() Metadata {
	return Metadata{
		Scope:  ScopeHeap,
		Num:    5,
		Name:   "json-allocation-pressure",
		Checks: "Whether encoding/json marshalling/unmarshalling drives allocation.",
		Method: "Attributes each allocation sample once to the JSON category " +
			"when any stack frame is an encoding/json function, then reports " +
			"the category's share of alloc_space. Fires above 3% share; " +
			"severity is medium at 10% and high at 25%.",
		Limitations: "Overlaps high-json-cpu (CPU-001) on JSON-heavy workloads — " +
			"the two findings describe one cause from two profiles, not two " +
			"problems. Cannot say which workspace call site drives the volume.",
	}
}

func (d heapJSONAllocDetector) Detect(ctx DetectCtx) []Finding {
	v := ctx.AllocSpace
	if v.Total == 0 {
		return nil
	}
	names, total := matchBySample(v,
		"encoding/json.",
	)
	share := percentOf(total, v.Total)
	if share < shareThreshold {
		return nil
	}
	return []Finding{makeFinding(
		d.Meta(), v,
		"encoding/json drives allocation",
		fmt.Sprintf("encoding/json accounts for %.2f%% of allocation.", share),
		"Reflection-driven encoding/decoding allocates heavily. Switch to easyjson/codegen, use json.RawMessage for pass-through fields, or cache encoded output where the payload repeats.",
		names, share, gradeShare(share), ConfidenceHigh,
	)}
}
