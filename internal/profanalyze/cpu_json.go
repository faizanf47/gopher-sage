package profanalyze

import "fmt"

func init() { MustRegister(cpuJSONDetector{}) }

// cpuJSONDetector reports when encoding/json marshalling or
// unmarshalling consumes a meaningful share of CPU.
type cpuJSONDetector struct{}

func (cpuJSONDetector) Meta() Metadata {
	return Metadata{
		Scope:  ScopeCPU,
		Num:    1,
		Name:   "high-json-cpu",
		Checks: "Whether encoding/json marshalling/unmarshalling consumes a meaningful share of CPU.",
		Method: "Attributes each CPU sample once to the JSON category when any " +
			"stack frame is an encoding/json function, then reports the " +
			"category's share of total CPU. Fires above 3% share; severity is " +
			"medium at 10% and high at 25%.",
		Limitations: "Cannot say which workspace call site drives the cost, and a " +
			"high share may be legitimate for a service whose job is JSON " +
			"transformation.",
	}
}

func (d cpuJSONDetector) Detect(ctx DetectCtx) []Finding {
	v := ctx.CPU
	names, total := matchBySample(v,
		"encoding/json.",
	)
	share := percentOf(total, v.Total)
	if share < shareThreshold {
		return nil
	}
	return []Finding{makeFinding(
		d.Meta(), v,
		"encoding/json dominates CPU samples",
		fmt.Sprintf("encoding/json frames account for %.2f%% of CPU.", share),
		"Likely repeated marshal/unmarshal on the hot path. Cache encoded output, switch to easyjson/codegen, or use json.RawMessage for pass-through fields.",
		names, share, gradeShare(share), ConfidenceHigh,
	)}
}
