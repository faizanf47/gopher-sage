package profanalyze

import "fmt"

func init() { MustRegister(cpuReflectionDetector{}) }

// cpuReflectionDetector reports when reflect.* work consumes a
// meaningful share of CPU.
type cpuReflectionDetector struct{}

func (cpuReflectionDetector) Meta() Metadata {
	return Metadata{
		Scope:  ScopeCPU,
		Num:    4,
		Name:   "high-reflection-cpu",
		Checks: "Whether reflection (reflect.*) consumes a meaningful share of CPU.",
		Method: "Attributes each CPU sample once to the reflection category when " +
			"any stack frame is a reflect.* function, then reports the " +
			"category's share of total CPU. Fires above 3% share; severity is " +
			"medium at 10% and high at 25%.",
		Limitations: "encoding/json and similar libraries drive reflection " +
			"internally, so this finding often overlaps high-json-cpu (CPU-001) " +
			"rather than indicating a separate problem.",
	}
}

func (d cpuReflectionDetector) Detect(ctx DetectCtx) []Finding {
	v := ctx.CPU
	names, total := matchBySample(v, "reflect.")
	share := percentOf(total, v.Total)
	if share < shareThreshold {
		return nil
	}
	return []Finding{makeFinding(
		d.Meta(), v,
		"reflection dominates CPU samples",
		fmt.Sprintf("reflect.* frames account for %.2f%% of CPU.", share),
		"Reflection on the hot path — replace with a type switch, generics, or codegen to remove the overhead.",
		names, share, gradeShare(share), ConfidenceHigh,
	)}
}
