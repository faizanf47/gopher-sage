package profanalyze

func init() { MustRegister(newCategoryDetector(heapJSONAllocSpec)) }

// heapJSONAllocSpec reports when encoding/json drives allocation
// volume.
var heapJSONAllocSpec = categorySpec{
	meta: Metadata{
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
	},
	view:       allocSpaceView,
	prefixes:   []string{"encoding/json."},
	title:      "encoding/json drives allocation",
	subject:    "encoding/json frames",
	object:     "allocation",
	recommend:  "Reflection-driven encoding/decoding allocates heavily. Switch to easyjson/codegen, use json.RawMessage for pass-through fields, or cache encoded output where the payload repeats.",
	confidence: ConfidenceHigh,
}
