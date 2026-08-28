package profanalyze

func init() { MustRegister(newCategoryDetector(cpuJSONSpec)) }

// cpuJSONSpec reports when encoding/json marshalling or
// unmarshalling consumes a meaningful share of CPU.
var cpuJSONSpec = categorySpec{
	meta: Metadata{
		Scope:  ScopeCPU,
		Num:    1,
		Name:   "high-json-cpu",
		Checks: "Whether encoding/json marshalling/unmarshalling consumes a meaningful share of CPU.",
		Method: "Attributes each CPU sample once to the JSON category when any " +
			"stack frame is an encoding/json function, then reports the " +
			"category's share of total CPU. Fires above 3% share; severity is " +
			"medium at 10% and high at 25%.",
		Limitations: "Call-site attribution names the nearest non-stdlib caller, " +
			"which may be a third-party library rather than first-party code, " +
			"and a high share may be legitimate for a service whose job is JSON " +
			"transformation.",
	},
	view:       cpuView,
	prefixes:   []string{"encoding/json."},
	title:      "encoding/json dominates CPU samples",
	subject:    "encoding/json frames",
	object:     "CPU",
	recommend:  "Likely repeated marshal/unmarshal on the hot path. Cache encoded output, switch to easyjson/codegen, or use json.RawMessage for pass-through fields.",
	confidence: ConfidenceHigh,
}
