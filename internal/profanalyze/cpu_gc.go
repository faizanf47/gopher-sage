package profanalyze

import "fmt"

func init() { MustRegister(cpuGCDetector{}) }

// cpuGCDetector reports when garbage-collection and allocation
// machinery consume a meaningful share of CPU.
type cpuGCDetector struct{}

func (cpuGCDetector) Meta() Metadata {
	return Metadata{
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
	}
}

func (d cpuGCDetector) Detect(ctx DetectCtx) []Finding {
	v := ctx.CPU
	// runtime.gcBgMarkWorker is the dedicated background mark
	// worker; runtime.scanobject / runtime.greyobject / mark*
	// frames are the scanner; runtime.mallocgc is allocation cost
	// (often the actual driver of GC pressure).
	names, total := matchBySample(v,
		"runtime.gcBgMarkWorker",
		"runtime.gcDrain",
		"runtime.scanobject",
		"runtime.greyobject",
		"runtime.markroot",
		"runtime.gcAssist",
		"runtime.gcMarkDone",
		"runtime.mallocgc",
		"runtime.sweepone",
	)
	share := percentOf(total, v.Total)
	if share < shareThreshold {
		return nil
	}
	return []Finding{makeFinding(
		d.Meta(), v,
		"GC / allocator dominates CPU samples",
		fmt.Sprintf("runtime GC / allocator frames account for %.2f%% of CPU.", share),
		"Workload is allocation-heavy. Cross-reference with a heap profile (alloc_space) to locate the workspace allocation sites and reduce per-call allocations.",
		names, share, gradeShare(share), ConfidenceMedium,
	)}
}
