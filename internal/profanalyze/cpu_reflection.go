package profanalyze

func init() { MustRegister(newCategoryDetector(cpuReflectionSpec)) }

// cpuReflectionSpec reports when reflect.* work consumes a
// meaningful share of CPU.
var cpuReflectionSpec = categorySpec{
	meta: Metadata{
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
	},
	view:       cpuView,
	prefixes:   []string{"reflect."},
	title:      "reflection dominates CPU samples",
	subject:    "reflect.* frames",
	object:     "CPU",
	recommend:  "Reflection on the hot path — replace with a type switch, generics, or codegen to remove the overhead.",
	confidence: ConfidenceHigh,
}
