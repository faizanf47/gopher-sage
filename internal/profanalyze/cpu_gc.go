package profanalyze

func init() { MustRegister(newCategoryDetector(cpuGCSpec)) }

// cpuGCSpec reports when garbage-collection and allocation
// machinery consume a meaningful share of CPU.
//
// runtime.gcBgMarkWorker is the dedicated background mark worker;
// runtime.scanobject / runtime.greyobject / mark* frames are the
// scanner; runtime.mallocgc is allocation cost (often the actual
// driver of GC pressure).
var cpuGCSpec = categorySpec{
	meta: Metadata{
		Scope:  ScopeCPU,
		Num:    5,
		Name:   "high-gc-cpu",
		Checks: "Whether garbage collection and allocation machinery consume a meaningful share of CPU.",
		Method: "Attributes each CPU sample once to the GC category when any " +
			"stack frame is one of the runtime's mark/sweep/alloc functions " +
			"(gcBgMarkWorker, gcDrain, scanobject, greyobject, markroot, " +
			"gcAssist, gcMarkDone, mallocgc, sweepone), then reports the " +
			"category's share of total CPU. Fires above 3% share; severity is " +
			"medium at 10% and high at 25%.",
		Limitations: "The frame list does not cover the scavenger " +
			"(runtime.madvise under bgscavenge) or memory-clearing paths, so " +
			"allocation pressure can be understated. Confidence is medium: a " +
			"heap profile (alloc_space) is needed to locate the allocation " +
			"sites responsible.",
	},
	view: cpuView,
	prefixes: []string{
		"runtime.gcBgMarkWorker",
		"runtime.gcDrain",
		"runtime.scanobject",
		"runtime.greyobject",
		"runtime.markroot",
		"runtime.gcAssist",
		"runtime.gcMarkDone",
		"runtime.mallocgc",
		"runtime.sweepone",
	},
	title:      "GC / allocator dominates CPU samples",
	subject:    "runtime GC / allocator frames",
	object:     "CPU",
	recommend:  "Workload is allocation-heavy. Cross-reference with a heap profile (alloc_space) to locate the workspace allocation sites and reduce per-call allocations.",
	confidence: ConfidenceMedium,
}
